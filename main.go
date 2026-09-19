// asgoto: a small tree-style switcher across repos, worktrees and panes.
//
// It talks to herdr through its CLI (workspace list / pane list to read,
// workspace focus to act, plus pane.focus over the socket API). Designed to run inside a herdr pane
// (type = "pane" keybind), full screen, single shot: open, pick, exit.
//
// The hierarchy (repos -> worktrees/panes) and the filter-that-keeps-ancestors
// are custom (no off-the-shelf tree component fits). The generic parts lean on
// the official bubbles: textinput (search box), viewport (scroll), key + help.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---- herdr CLI JSON shapes ----

type worktree struct {
	RepoKey      string `json:"repo_key"`
	RepoName     string `json:"repo_name"`
	RepoRoot     string `json:"repo_root"`
	CheckoutPath string `json:"checkout_path"`
	IsLinked     bool   `json:"is_linked_worktree"`
}

type wsInfo struct {
	ID       string    `json:"workspace_id"`
	Label    string    `json:"label"`
	Number   int       `json:"number"`
	Focused  bool      `json:"focused"` // the workspace asgoto was opened from
	Worktree *worktree `json:"worktree"`
}

type paneInfo struct {
	ID          string `json:"pane_id"`
	WsID        string `json:"workspace_id"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
	Cwd         string `json:"cwd"`
}

type wsResp struct {
	Result struct {
		Workspaces []wsInfo `json:"workspaces"`
	} `json:"result"`
}

type paneResp struct {
	Result struct {
		Panes []paneInfo `json:"panes"`
	} `json:"result"`
}

// agentResp is `agent list`, read only for state_change_seq: `pane list` does
// not carry it, and the priority order needs it as the recency tiebreaker.
type agentResp struct {
	Result struct {
		Agents []struct {
			PaneID string `json:"pane_id"`
			Seq    uint64 `json:"state_change_seq"`
		} `json:"agents"`
	} `json:"result"`
}

// ---- tree ----

type node struct {
	kind     string // "repo" | "worktree" | "pane"
	label    string // own display text (matched against the query); repo name, worktree branch (folder when unknown), pane leaf
	branch   string // git branch of the checkout ("" when detached/unknown); shown dim on repo rows, extra search text
	folder   string // worktree only: checkout folder name (herdr's workspace label); shown dim when it no longer matches the branch, extra search text
	checkout string // repo/worktree: path of the checkout, for the async ahead/behind lookup ("" when unknown)
	ahead    int    // commits ahead of the upstream (filled async by deltaMsg)
	behind   int    // commits behind the upstream (filled async by deltaMsg)
	staged   int    // staged files in the checkout (filled async by deltaMsg)
	unstaged int    // tracked files modified but not staged (filled async by deltaMsg)
	untrack  int    // untracked files (filled async by deltaMsg)
	deltaW   int    // width of the ahead/behind column shared by every row (0 = no row has a delta)
	stagedW  int    // width of the "+n" column shared by every row (0 = no row has staged files)
	unstagW  int    // width of the "!n" column shared by every row (0 = no row has unstaged files)
	untrkW   int    // width of the "?n" column shared by every row (0 = no row has untracked files)
	ticket   string // normalized Jira key ("FED-2030") from branch/label, or the PR title as fallback
	ghSlug   string // "owner/repo" of the GitHub origin; "" when non-GitHub/unknown
	pr       *prRef // PR for this branch, annotated from cache/gh; nil until known or when none
	tktW     int    // ticket column width within the sibling group (0 = no prefix)
	prW      int    // "#123" column width within the sibling group (0 = no PR column)
	wsID     string // workspace to focus (repo/worktree, and a pane's workspace)
	paneID   string // pane to focus
	status   string // aggregated agent status (see statusDot); drives the gutter dot
	seq      uint64 // herdr's state_change_seq of the agent behind status (aggregated like it); priority-order tiebreaker
	ord      int    // position among its siblings as built (see buildTree); the default order, restored when priority sort is off
	hasAgent bool   // pane hosts a herdr-recognized agent (its process is implied by the leaf/dot)
	proc     string // pane only: foreground command running in it; such panes are always listed, labelled by the command
	pids     []int  // pane only: pids of the foreground process group, for the ports lookup
	ports    []int  // pane only: TCP ports its process tree listens on; right-aligned
	expanded bool
	children []*node
}

func herdrBin() string {
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	return "herdr"
}

// ---- persisted UI state ----

type persisted struct {
	ShowPanes    bool `json:"show_panes"`
	PrioritySort bool `json:"priority_sort"`
}

func stateFile() string {
	// Running as a herdr plugin: herdr creates and injects a per-plugin state
	// dir; runtime state must live there, not in the plugin checkout.
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return filepath.Join(dir, "state.json")
	}
	// Standalone fallback (fixed-path install at ~/.config/herdr/asgoto-tui).
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if h, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(h, ".config")
		}
	}
	return filepath.Join(base, "herdr", "asgoto-tui", "state.json")
}

func loadState() persisted {
	var s persisted
	if data, err := os.ReadFile(stateFile()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveStateCmd(s persisted) tea.Cmd {
	return func() tea.Msg {
		if data, err := json.Marshal(s); err == nil {
			path := stateFile()
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.WriteFile(path, data, 0o644)
		}
		return nil
	}
}

// ---- GitHub PR info (via gh, async) ----

// prRef is the PR shown next to a branch. Ticket is extracted from the PR
// title and used only when the branch/label carry no ticket themselves.
type prRef struct {
	Number int    `json:"number"`
	State  string `json:"state"` // "open" | "draft" | "merged" | "closed"
	Ticket string `json:"ticket,omitempty"`
}

// ghPR mirrors one item of `gh pr list --json number,headRefName,state,isDraft,title`.
type ghPR struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	State       string `json:"state"` // OPEN | MERGED | CLOSED
	IsDraft     bool   `json:"isDraft"`
	Title       string `json:"title"`
}

func (p ghPR) ref() prRef {
	state := strings.ToLower(p.State)
	if p.IsDraft && p.State == "OPEN" {
		state = "draft"
	}
	return prRef{Number: p.Number, State: state, Ticket: ticketFrom(p.Title)}
}

type repoPRs struct {
	FetchedAt time.Time        `json:"fetched_at"`
	Branches  map[string]prRef `json:"branches"`
}

// prCache is the stale-while-revalidate disk cache of branch -> PR per repo
// and of the git hints (ahead/behind, staged/unstaged/untracked) per checkout path: cached
// entries render immediately on startup while gh / git refresh them in the
// background, so the info is visible even in this tool's open-pick-exit
// lifetime, and the hint columns are sized from the first paint instead of
// shifting when the refresh lands.
type prCache struct {
	Repos map[string]repoPRs  `json:"repos"`
	Hints map[string]gitDelta `json:"hints,omitempty"`
}

// prCacheFresh is how recent a repo's cached PRs must be to skip the
// background refresh. It only debounces rapid reopen cycles; older entries
// are still shown immediately while they revalidate.
const prCacheFresh = 60 * time.Second

func prCacheFile() string {
	return filepath.Join(filepath.Dir(stateFile()), "prcache.json")
}

func loadPRCache() prCache {
	var c prCache
	if data, err := os.ReadFile(prCacheFile()); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	if c.Repos == nil {
		c.Repos = map[string]repoPRs{}
	}
	if c.Hints == nil {
		c.Hints = map[string]gitDelta{}
	}
	return c
}

// savePRCacheCmd writes the cache to disk off the update loop. It is
// serialized here, synchronously, so the async write never reads the maps
// while a later message mutates them.
func savePRCacheCmd(c prCache) tea.Cmd {
	data, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		path := prCacheFile()
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, data, 0o644)
		return nil
	}
}

// repoPRsMsg delivers one repo's branch -> PR mapping fetched from gh. err
// leaves nodes and cache untouched (missing gh, network down, non-GitHub);
// degradation is silent by design.
type repoPRsMsg struct {
	slug     string
	byBranch map[string]prRef
	err      bool
}

// fetchRepoPRsCmd resolves the PR of each local branch with one
// `gh pr list --head <branch>` call per branch, run in parallel. A repo-wide
// listing would miss older PRs in busy shared repos, where the newest N PRs
// are mostly other people's. prev (the previously cached mapping) fills in
// branches whose lookup failed transiently, so an error never wipes a valid
// cached PR; only when every branch fails is the whole message marked err.
func fetchRepoPRsCmd(slug string, branches []string, prev map[string]prRef) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		type result struct {
			pr  *prRef
			err bool
		}
		results := make([]result, len(branches))
		var wg sync.WaitGroup
		for i, branch := range branches {
			wg.Add(1)
			go func(i int, branch string) {
				defer wg.Done()
				out, err := exec.CommandContext(ctx, "gh", "pr", "list", "--repo", slug,
					"--head", branch, "--state", "all",
					"--json", "number,headRefName,state,isDraft,title",
					"--limit", "5").Output()
				if err != nil {
					results[i] = result{err: true}
					return
				}
				var prs []ghPR
				if json.Unmarshal(out, &prs) != nil {
					results[i] = result{err: true}
					return
				}
				if len(prs) > 0 {
					p := prs[0].ref() // newest first
					results[i] = result{pr: &p}
				}
			}(i, branch)
		}
		wg.Wait()

		byBranch := map[string]prRef{}
		errs := 0
		for i, r := range results {
			switch {
			case r.err:
				errs++
				if p, ok := prev[branches[i]]; ok {
					byBranch[branches[i]] = p
				}
			case r.pr != nil:
				byBranch[branches[i]] = *r.pr
			}
		}
		if errs == len(branches) {
			return repoPRsMsg{slug: slug, err: true}
		}
		return repoPRsMsg{slug: slug, byBranch: byBranch}
	}
}

// annotatePRs applies a branch -> PR mapping to one repo's nodes, clearing
// entries whose branch no longer has a PR and falling back to the PR-title
// ticket when branch/label yielded none.
func annotatePRs(nodes []*node, byBranch map[string]prRef) {
	for _, n := range nodes {
		p, ok := byBranch[n.branch]
		if !ok {
			n.pr = nil
			continue
		}
		pp := p
		n.pr = &pp
		if n.ticket == "" && p.Ticket != "" {
			n.ticket = p.Ticket
		}
	}
}

// ---- foreground processes (herdr pane process-info) ----

type procInfoResp struct {
	Result struct {
		ProcessInfo struct {
			ShellPID  int `json:"shell_pid"`
			GroupID   int `json:"foreground_process_group_id"`
			Processes []struct {
				PID   int      `json:"pid"`
				Name  string   `json:"name"`
				Argv0 string   `json:"argv0"`
				Argv  []string `json:"argv"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	} `json:"result"`
}

// procLabel is the short command a pane is running in the foreground, or ""
// when the shell sits at its prompt (the group leader is the shell itself).
// The leader is the process whose pid equals the foreground process group id;
// its children (MCP servers, caffeinate, ...) are noise for a one-line label.
func procLabel(r procInfoResp) string {
	pi := r.Result.ProcessInfo
	if pi.GroupID == 0 || pi.GroupID == pi.ShellPID {
		return ""
	}
	for _, p := range pi.Processes {
		if p.PID != pi.GroupID {
			continue
		}
		if len(p.Argv) > 0 {
			return shortCmd(p.Argv)
		}
		// herdr cannot always read a process's argv; its argv0 is then the
		// most descriptive text it has ("npm exec foo@latest"), name is just
		// the executable ("node").
		if p.Argv0 != "" {
			return p.Argv0
		}
		return p.Name
	}
	return ""
}

// interpreters whose first non-flag argument is the script that matters
// ("node /path/pnpm dev" -> "pnpm dev").
var interpreters = map[string]bool{
	"node": true, "python": true, "python3": true, "ruby": true, "perl": true,
	"sh": true, "bash": true, "zsh": true, "bun": true, "deno": true,
}

// shortCmd compresses an argv into a readable label: paths are reduced to
// their basename and a leading interpreter is dropped when it runs a script.
func shortCmd(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, a := range argv {
		if strings.Contains(a, "/") && !strings.HasPrefix(a, "-") {
			a = filepath.Base(a)
		}
		parts = append(parts, a)
	}
	if len(parts) > 1 && interpreters[parts[0]] && !strings.HasPrefix(parts[1], "-") {
		parts = parts[1:]
	}
	return strings.Join(parts, " ")
}

// procPIDs lists every pid in the pane's foreground process group (leader and
// children); listeners are looked up among these and their descendants.
func procPIDs(r procInfoResp) []int {
	pi := r.Result.ProcessInfo
	if pi.GroupID == 0 || pi.GroupID == pi.ShellPID {
		return nil
	}
	pids := make([]int, 0, len(pi.Processes))
	for _, p := range pi.Processes {
		pids = append(pids, p.PID)
	}
	return pids
}

// listeningPorts maps pid -> TCP ports in LISTEN state, from one
// `lsof -nP -iTCP -sTCP:LISTEN -Fpn` call (no root needed for own processes).
func listeningPorts(ctx context.Context) map[int][]int {
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpn").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return parseLsof(string(out))
}

// processParents maps pid -> ppid for every process, from one `ps -axo pid,ppid`.
// Dev servers often detach into their own process group (`pnpm nx dev` ->
// next-server), so the pane's foreground group alone would miss the listener;
// walking descendants finds it.
func processParents(ctx context.Context) map[int]int {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return parsePS(string(out))
}

func parsePS(out string) map[int]int {
	parents := map[int]int{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			parents[pid] = ppid
		}
	}
	return parents
}

// descendants returns roots plus every process below them, per parents.
func descendants(roots []int, parents map[int]int) []int {
	children := map[int][]int{}
	for pid, ppid := range parents {
		children[ppid] = append(children[ppid], pid)
	}
	seen := map[int]bool{}
	var out []int
	var walk func(pid int)
	walk = func(pid int) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		out = append(out, pid)
		for _, c := range children[pid] {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return out
}

// parseLsof reads lsof -F output: "p<pid>" starts a process, "n<addr>:<port>"
// lists one socket. Ports are deduped per pid (IPv4 + IPv6 listeners).
func parseLsof(out string) map[int][]int {
	byPID := map[int][]int{}
	pid := 0
	seen := map[int]map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			i := strings.LastIndex(line, ":")
			if i < 0 || pid == 0 {
				continue
			}
			port, err := strconv.Atoi(line[i+1:])
			if err != nil {
				continue
			}
			if seen[pid] == nil {
				seen[pid] = map[int]bool{}
			}
			if !seen[pid][port] {
				seen[pid][port] = true
				byPID[pid] = append(byPID[pid], port)
			}
		}
	}
	return byPID
}

// procEntry is what one pane is running: the foreground command and the pids
// of its process group (ports are resolved later against these).
type procEntry struct {
	label string
	pids  []int
}

// fetchProcInfos queries `pane process-info` for every pane in parallel. It
// runs before the first paint (a few ms over the herdr socket) so process
// rows are in place from the start instead of pushing the list around when
// they arrive. A missing command (older herdr) or a timeout degrades to no
// process rows at all.
func fetchProcInfos(paneIDs []string) map[string]procEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	infos := make([]procInfoResp, len(paneIDs))
	var wg sync.WaitGroup
	for i, id := range paneIDs {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			out, err := exec.CommandContext(ctx, herdrBin(), "pane", "process-info", "--pane", id).Output()
			if err != nil {
				return
			}
			json.Unmarshal(out, &infos[i])
		}(i, id)
	}
	wg.Wait()
	byPane := make(map[string]procEntry, len(paneIDs))
	for i, id := range paneIDs {
		e := procEntry{label: procLabel(infos[i])}
		if e.label != "" {
			e.pids = procPIDs(infos[i])
		}
		byPane[id] = e
	}
	return byPane
}

// annotateProcs turns each pane running a foreground command into a process
// row: its label becomes the command and it stays listed even while plain
// panes are hidden. Agent panes are skipped: the agent is already conveyed by
// the leaf text and the status dot.
func annotateProcs(roots []*node, byPane map[string]procEntry) {
	var walk func(n *node)
	walk = func(n *node) {
		if n.kind == "pane" {
			n.proc, n.pids, n.ports = "", nil, nil
			if e := byPane[n.paneID]; !n.hasAgent && e.label != "" {
				n.proc, n.pids = e.label, e.pids
				n.label = e.label
			}
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
}

type portsMsg struct {
	byPane map[string][]int // pane id -> sorted listening ports
}

// fetchPortsCmd resolves listening ports for the process rows: one lsof
// (~80ms, the expensive part, hence async after the first paint) and one ps
// for the process tree, matched against each row's pids and their
// descendants. Ports only fill the right column, so their late arrival never
// shifts rows.
func fetchPortsCmd(procNodes []*node) tea.Cmd {
	pids := fetchPortsCmdPids(procNodes)
	return func() tea.Msg {
		return portsMsg{byPane: fetchPorts(pids)}
	}
}

func fetchPorts(pidsByPane map[string][]int) map[string][]int {
	if len(pidsByPane) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var ports map[int][]int
	var parents map[int]int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ports = listeningPorts(ctx)
	}()
	go func() {
		defer wg.Done()
		parents = processParents(ctx)
	}()
	wg.Wait()
	byPane := make(map[string][]int, len(pidsByPane))
	for id, pids := range pidsByPane {
		var out []int
		for _, pid := range descendants(pids, parents) {
			out = append(out, ports[pid]...)
		}
		sort.Ints(out)
		byPane[id] = out
	}
	return byPane
}

// fetchPortsCmdPids is the pane -> pids map fetchPorts takes, for the
// synchronous -dump path.
func fetchPortsCmdPids(procNodes []*node) map[string][]int {
	pids := make(map[string][]int, len(procNodes))
	for _, n := range procNodes {
		pids[n.paneID] = n.pids
	}
	return pids
}

// annotatePorts stamps listening ports on process rows.
func annotatePorts(procNodes []*node, byPane map[string][]int) {
	for _, n := range procNodes {
		n.ports = byPane[n.paneID]
	}
}

// procRows lists the pane nodes that became process rows.
func procRows(nodes []*node) []*node {
	var out []*node
	for _, n := range nodes {
		if n.kind == "pane" && n.proc != "" {
			out = append(out, n)
		}
	}
	return out
}

// portsText renders a node's listening ports as ":4200 :4201" ("" when none).
func portsText(n *node) string {
	parts := make([]string, 0, len(n.ports))
	for _, p := range n.ports {
		parts = append(parts, ":"+strconv.Itoa(p))
	}
	return strings.Join(parts, " ")
}

// ---- ahead/behind vs upstream ----

// gitDelta is one checkout's commit delta against its upstream plus its
// working-tree counters (staged, unstaged, untracked), the same numbers the
// shell prompt and the Claude Code statusline show as "+n !n ?n".
type gitDelta struct {
	Ahead     int `json:"ahead"`
	Behind    int `json:"behind"`
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
}

// deltaMsg delivers the ahead/behind + working-tree state of every repo/worktree
// checkout, keyed by checkout path (the cache key too). Checkouts where git
// failed are absent, leaving whatever was cached.
type deltaMsg struct {
	byCheckout map[string]gitDelta
}

// deltaNodes lists the repo/worktree rows whose checkout is known.
func deltaNodes(nodes []*node) []*node {
	var out []*node
	for _, n := range nodes {
		if n.kind != "pane" && n.checkout != "" {
			out = append(out, n)
		}
	}
	return out
}

// fetchDeltasCmd resolves the ahead/behind and working-tree hints of each
// checkout after the TUI is on screen. They need one `git status` per
// checkout (not derivable from the filesystem without walking the index),
// run in parallel, so it is async like the ports: the hints only fill the
// right column and never shift rows.
func fetchDeltasCmd(nodes []*node) tea.Cmd {
	return func() tea.Msg {
		return deltaMsg{byCheckout: fetchDeltas(nodes)}
	}
}

func fetchDeltas(nodes []*node) map[string]gitDelta {
	if len(nodes) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		checkout string
		d        gitDelta
		ok       bool
	}
	results := make([]result, len(nodes))
	// git status refreshes the index with a thread per core, so 20 checkouts
	// at once would thrash; a few at a time keeps the burst short (a large
	// repo costs ~80ms wall / ~1s CPU) without serializing everything.
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func(i int, n *node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var d gitDelta
			ok := false
			// One `git status -b` feeds everything, exactly like the shell
			// prompt: the header carries ahead/behind vs the upstream (absent
			// when there is no upstream / detached HEAD, which just means "no
			// delta") and the entry lines are counted into staged/unstaged/
			// untracked.
			if out, err := exec.CommandContext(ctx, "git", "-C", n.checkout,
				"status", "--porcelain=v1", "-b", "--untracked-files=normal").Output(); err == nil {
				d, ok = parseStatus(string(out))
			}
			results[i] = result{checkout: n.checkout, d: d, ok: ok}
		}(i, n)
	}
	wg.Wait()
	byCheckout := map[string]gitDelta{}
	for _, r := range results {
		if r.ok {
			byCheckout[r.checkout] = r.d
		}
	}
	return byCheckout
}

// parseStatus reads `git status --porcelain=v1 -b` output: the "## " header
// line for the ahead/behind counts ("## branch...upstream [ahead 1, behind
// 2]") and one entry line per changed file, counted into staged (index
// column set), unstaged (worktree column set) and untracked ("??").
func parseStatus(out string) (gitDelta, bool) {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "## ") {
		return gitDelta{}, false
	}
	var d gitDelta
	header := lines[0]
	if i := strings.LastIndex(header, " ["); i >= 0 && strings.HasSuffix(header, "]") {
		for _, part := range strings.Split(header[i+2:len(header)-1], ", ") {
			if v, found := strings.CutPrefix(part, "ahead "); found {
				d.Ahead, _ = strconv.Atoi(v)
			} else if v, found := strings.CutPrefix(part, "behind "); found {
				d.Behind, _ = strconv.Atoi(v)
			}
		}
	}
	for _, line := range lines[1:] {
		if len(line) < 2 {
			continue
		}
		if line[0] == '?' {
			d.Untracked++
			continue
		}
		if line[0] != ' ' {
			d.Staged++
		}
		if line[1] != ' ' {
			d.Unstaged++
		}
	}
	return d, true
}

// annotateDeltas stamps the ahead/behind and working-tree counts on
// repo/worktree rows, from the cache at startup and from git when it lands.
func annotateDeltas(nodes []*node, byCheckout map[string]gitDelta) {
	for _, n := range nodes {
		if d, ok := byCheckout[n.checkout]; ok {
			n.ahead, n.behind = d.Ahead, d.Behind
			n.staged, n.unstaged, n.untrack = d.Staged, d.Unstaged, d.Untracked
		}
	}
}

// layoutHints sizes the hint columns right of the names: the ahead/behind
// column plus one column per working-tree counter (staged, unstaged,
// untracked), each as wide as the widest value of any repo/worktree row
// and absent (width 0) when no row has it, so the symbols align vertically
// and rows with a shorter/absent value pad the slot. All widths are
// stamped on pane rows too, whose blank slots keep ports ending on the
// column the names end on. Together they make the names right-align on one
// column across the list.
func layoutHints(nodes []*node) {
	var dw, sw, uw, tw int
	for _, n := range nodes {
		if n.kind != "pane" {
			if w := lipgloss.Width(deltaText(n)); w > dw {
				dw = w
			}
			if w := lipgloss.Width(countText('+', n.staged)); w > sw {
				sw = w
			}
			if w := lipgloss.Width(countText('!', n.unstaged)); w > uw {
				uw = w
			}
			if w := lipgloss.Width(countText('?', n.untrack)); w > tw {
				tw = w
			}
		}
	}
	for _, n := range nodes {
		n.deltaW, n.stagedW, n.unstagW, n.untrkW = dw, sw, uw, tw
	}
}

// deltaText is the ahead/behind hint, in the same shape as the shell prompt:
// "↑n" commits to push, "↓n" commits to pull, "" when in sync or unknown.
func deltaText(n *node) string {
	var b strings.Builder
	if n.ahead > 0 {
		fmt.Fprintf(&b, "↑%d", n.ahead)
	}
	if n.behind > 0 {
		fmt.Fprintf(&b, "↓%d", n.behind)
	}
	return b.String()
}

// countText is one working-tree counter as plain text, in the same shape
// as the shell prompt: "+n" staged, "!n" unstaged, "?n" untracked, ""
// when zero.
func countText(sym rune, v int) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%c%d", sym, v)
}

// countsText joins a node's non-zero counters ("+n !n ?n"), for -dump and
// the search corpus-free places that want them unpadded.
func countsText(n *node) string {
	var parts []string
	for _, c := range []string{
		countText('+', n.staged),
		countText('!', n.unstaged),
		countText('?', n.untrack),
	} {
		if c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, " ")
}

func loadJSON(args []string, out any) error {
	data, err := exec.Command(herdrBin(), args...).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func homeRel(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

// resolveGitDir returns the git dir of the checkout at path, handling both
// main checkouts (.git dir) and linked worktrees (.git file with a "gitdir:"
// pointer). Returns "" for non-repos. Reads the filesystem directly (no
// subprocess; a `git` call per workspace would slow startup).
func resolveGitDir(path string) string {
	if path == "" {
		return ""
	}
	gitdir := filepath.Join(path, ".git")
	if fi, err := os.Stat(gitdir); err != nil {
		return ""
	} else if !fi.IsDir() {
		data, err := os.ReadFile(gitdir)
		if err != nil {
			return ""
		}
		target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
		if target == "" {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(path, target)
		}
		gitdir = target
	}
	return gitdir
}

// gitTopLevel walks up from path to the nearest directory holding a .git
// entry (dir or linked-worktree file). Returns "" when none is found. Used
// for workspaces herdr reports no worktree metadata for, whose checkout is
// only known through a pane's cwd.
func gitTopLevel(path string) string {
	for p := path; p != "" && p != "/"; p = filepath.Dir(p) {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return p
		}
	}
	return ""
}

// gitBranch resolves the branch checked out at path by reading .git/HEAD
// directly. Returns "" for detached HEAD or non-repos.
func gitBranch(path string) string {
	gitdir := resolveGitDir(path)
	if gitdir == "" {
		return ""
	}
	head, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(head))
	if b, ok := strings.CutPrefix(ref, "ref: refs/heads/"); ok {
		return b
	}
	return "" // detached HEAD
}

// worktreeCreatedAt returns when the checkout at path was created: the
// directory's birth time where the platform reports one (birthTime, see
// birth_darwin.go and birth_other.go), its modification time elsewhere. Zero
// time when the path can't be stat'ed, which sorts those entries first.
func worktreeCreatedAt(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	if t, ok := birthTime(fi); ok {
		return t
	}
	return fi.ModTime()
}

// ticketRe matches a Jira-style ticket key: a project key of 2+ letters, a
// dash and digits (FED-2030, plat-1193). Letters-only before the dash keeps
// slugs like "e2e" or "v1-2" from matching.
var ticketRe = regexp.MustCompile(`(?i)\b([a-z][a-z]+-[0-9]+)\b`)

// ticketFrom extracts a normalized (uppercase) ticket key from the first
// source that contains one. Returns "" when none matches.
func ticketFrom(sources ...string) string {
	for _, s := range sources {
		if m := ticketRe.FindString(s); m != "" {
			return strings.ToUpper(m)
		}
	}
	return ""
}

// githubSlug resolves "owner/repo" from the origin remote of the repo at
// repoRoot, reading the git config file directly (startup-safe, no
// subprocess). Returns "" when the origin is missing or not on github.com.
func githubSlug(repoRoot string) string {
	gitdir := resolveGitDir(repoRoot)
	if gitdir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(gitdir, "config"))
	if err != nil {
		// A linked-worktree gitdir keeps config in the common dir.
		common, cerr := os.ReadFile(filepath.Join(gitdir, "commondir"))
		if cerr != nil {
			return ""
		}
		target := strings.TrimSpace(string(common))
		if !filepath.IsAbs(target) {
			target = filepath.Join(gitdir, target)
		}
		if data, err = os.ReadFile(filepath.Join(target, "config")); err != nil {
			return ""
		}
	}
	return githubSlugFromURL(originURL(string(data)))
}

// originURL scans git config content for the url of [remote "origin"].
func originURL(config string) string {
	inOrigin := false
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "url"); ok {
			rest = strings.TrimSpace(rest)
			if v, ok := strings.CutPrefix(rest, "="); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// githubSlugFromURL extracts "owner/repo" from the ssh/https GitHub remote
// URL forms. Non-GitHub hosts return "".
func githubSlugFromURL(url string) string {
	var rest string
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		rest = strings.TrimPrefix(url, "git@github.com:")
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		rest = strings.TrimPrefix(url, "ssh://git@github.com/")
	case strings.HasPrefix(url, "https://github.com/"):
		rest = strings.TrimPrefix(url, "https://github.com/")
	default:
		return ""
	}
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
	if strings.Count(rest, "/") != 1 {
		return ""
	}
	return rest
}

func paneLeaf(p paneInfo) string {
	name := p.Agent
	status := ""
	if name != "" {
		status = "  [" + p.AgentStatus + "]"
	} else {
		name = "shell"
	}
	return name + status + "  " + filepath.Base(homeRel(p.Cwd))
}

// paneStatus is the agent status used for the gutter dot. Shell panes (no agent)
// carry no status so they don't pull a workspace's aggregate toward a stale dot.
func paneStatus(p paneInfo) string {
	if p.Agent == "" {
		return ""
	}
	return p.AgentStatus
}

// statusRank orders agent statuses when aggregating a workspace's panes,
// mirroring herdr's workspace_attention_priority: blocked beats done, done beats
// working, working beats idle, idle beats none/unknown.
func statusRank(s string) int {
	switch s {
	case "blocked":
		return 4
	case "done":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	default: // "unknown", "" (shell / no agent)
		return 0
	}
}

// aggregateStatus sets n.status to the highest-priority status among n and its
// descendants, so a repo/worktree reflects its panes' state even when panes are
// hidden. n.seq follows the winning status (the most recent change among the
// panes holding it). Returns the resolved status for the recursion.
func aggregateStatus(n *node) string {
	best, seq := n.status, n.seq
	for _, c := range n.children {
		s := aggregateStatus(c)
		switch {
		case statusRank(s) > statusRank(best):
			best, seq = s, c.seq
		case statusRank(s) == statusRank(best) && c.seq > seq:
			seq = c.seq
		}
	}
	n.status, n.seq = best, seq
	return best
}

// sortTree orders every sibling group in place. Off, it restores the built
// order (node.ord). On, it is herdr's Agents panel "priority" sort
// (ui.agent_panel_sort) applied per level: highest statusRank first, then the
// most recent state change, then the built order. A repo's own panes stay
// above its worktrees either way, as the repo row is the main checkout.
func sortTree(nodes []*node, byPriority bool) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if (a.kind == "pane") != (b.kind == "pane") {
			return a.kind == "pane"
		}
		if byPriority {
			if ra, rb := statusRank(a.status), statusRank(b.status); ra != rb {
				return ra > rb
			}
			if a.seq != b.seq {
				return a.seq > b.seq
			}
		}
		return a.ord < b.ord
	})
	for _, n := range nodes {
		sortTree(n.children, byPriority)
	}
}

// stampOrder records each node's built position among its siblings, the
// order sortTree falls back to.
func stampOrder(nodes []*node) {
	for i, n := range nodes {
		n.ord = i
		stampOrder(n.children)
	}
}

func buildTree(wss []wsInfo, panes []paneInfo, seqs map[string]uint64) []*node {
	byWs := map[string][]paneInfo{}
	for _, p := range panes {
		byWs[p.WsID] = append(byWs[p.WsID], p)
	}

	paneNodes := func(wsID string) []*node {
		var out []*node
		for _, p := range byWs[wsID] {
			leaf := paneLeaf(p)
			var seq uint64
			if p.Agent != "" { // like paneStatus: shells never weigh on the order
				seq = seqs[p.ID]
			}
			out = append(out, &node{
				seq:  seq,
				kind: "pane", label: leaf,
				wsID: wsID, paneID: p.ID, status: paneStatus(p), hasAgent: p.Agent != "",
				expanded: true,
			})
		}
		return out
	}

	// Group workspaces into repos.
	type group struct {
		key  string
		wss  []wsInfo
		minN int
	}
	order := []string{}
	groups := map[string]*group{}
	for _, ws := range wss {
		key := ws.ID
		if ws.Worktree != nil {
			switch {
			case ws.Worktree.RepoKey != "":
				key = ws.Worktree.RepoKey
			case ws.Worktree.CheckoutPath != "":
				key = ws.Worktree.CheckoutPath
			}
		} else if ps := byWs[ws.ID]; len(ps) > 0 {
			key = ps[0].Cwd
		}
		g := groups[key]
		if g == nil {
			g = &group{key: key, minN: 1 << 30}
			groups[key] = g
			order = append(order, key)
		}
		g.wss = append(g.wss, ws)
		if ws.Number < g.minN {
			g.minN = ws.Number
		}
	}
	glist := make([]*group, 0, len(order))
	for _, k := range order {
		glist = append(glist, groups[k])
	}
	sort.SliceStable(glist, func(i, j int) bool { return glist[i].minN < glist[j].minN })

	slugCache := map[string]string{}
	slugFor := func(root string) string {
		if s, ok := slugCache[root]; ok {
			return s
		}
		s := githubSlug(root)
		slugCache[root] = s
		return s
	}

	repoName := func(ws wsInfo) string {
		if ws.Worktree != nil && ws.Worktree.RepoName != "" {
			return ws.Worktree.RepoName
		}
		return ws.Label
	}
	isMain := func(ws wsInfo) bool {
		return ws.Worktree == nil || !ws.Worktree.IsLinked
	}

	var roots []*node
	for _, g := range glist {
		var main wsInfo
		found := false
		for _, ws := range g.wss {
			if isMain(ws) {
				main = ws
				found = true
				break
			}
		}
		if !found {
			main = g.wss[0]
		}
		name := repoName(main)
		repo := &node{kind: "repo", label: name, wsID: main.ID, expanded: true}
		checkout, root := "", ""
		if main.Worktree != nil {
			checkout, root = main.Worktree.CheckoutPath, main.Worktree.RepoRoot
		} else if ps := byWs[main.ID]; len(ps) > 0 {
			// herdr reported no worktree metadata (e.g. the space was
			// created before its git discovery ran); the first pane's cwd
			// still tells us which checkout this is.
			checkout = gitTopLevel(ps[0].Cwd)
			root = checkout
		}
		if checkout != "" {
			repo.checkout = checkout
			repo.branch = gitBranch(checkout)
			repo.ticket = ticketFrom(repo.branch)
			repo.ghSlug = slugFor(root)
		}
		repo.children = append(repo.children, paneNodes(main.ID)...)

		others := []wsInfo{}
		for _, ws := range g.wss {
			if ws.ID != main.ID {
				others = append(others, ws)
			}
		}
		// Worktrees sort oldest-first by checkout creation time (which tracks
		// PR order in practice), with the workspace number as tiebreaker.
		type wsWithTime struct {
			ws        wsInfo
			createdAt time.Time
		}
		byAge := make([]wsWithTime, len(others))
		for i, ws := range others {
			byAge[i] = wsWithTime{ws: ws}
			if ws.Worktree != nil && ws.Worktree.CheckoutPath != "" {
				byAge[i].createdAt = worktreeCreatedAt(ws.Worktree.CheckoutPath)
			}
		}
		sort.SliceStable(byAge, func(a, b int) bool {
			if !byAge[a].createdAt.Equal(byAge[b].createdAt) {
				return byAge[a].createdAt.Before(byAge[b].createdAt)
			}
			return byAge[a].ws.Number < byAge[b].ws.Number
		})
		for i, w := range byAge {
			others[i] = w.ws
		}
		for _, ws := range others {
			// A worktree row is named after the branch checked out in it:
			// the folder is just the slug of whatever branch it was created
			// for, and a later checkout leaves it stale (herdr's sidebar
			// shows the same branch under the folder label).
			wt := &node{kind: "worktree", label: ws.Label, folder: ws.Label, wsID: ws.ID, expanded: true}
			if ws.Worktree != nil {
				wt.checkout = ws.Worktree.CheckoutPath
				wt.branch = gitBranch(ws.Worktree.CheckoutPath)
				wt.ticket = ticketFrom(wt.branch, ws.Label)
				wt.ghSlug = slugFor(ws.Worktree.RepoRoot)
			}
			if wt.branch != "" {
				wt.label = wt.branch
			}
			wt.children = paneNodes(ws.ID)
			repo.children = append(repo.children, wt)
		}
		roots = append(roots, repo)
	}
	for _, r := range roots {
		aggregateStatus(r)
	}
	stampOrder(roots)
	return roots
}

// currentWorkspaceNode is the repo/worktree row of the workspace asgoto was
// opened from (herdr reports it as focused), so the cursor starts there and
// the list opens scrolled to where you are. Nil when none is focused.
func currentWorkspaceNode(wss []wsInfo, nodes []*node) *node {
	for _, ws := range wss {
		if !ws.Focused {
			continue
		}
		for _, n := range nodes {
			if n.kind != "pane" && n.wsID == ws.ID {
				return n
			}
		}
	}
	return nil
}

// flatten returns every node in tree order plus parallel slices of lowercased
// labels and extra match text (branch and worktree folder), fed to the fuzzy
// matcher in one shot per keystroke. A node's extra entry is "" when it adds
// nothing over the label (no branch/folder, or identical), so the matcher
// skips it.
func flatten(roots []*node) ([]*node, []string, []string) {
	var nodes []*node
	var labels []string
	var branches []string
	var walk func(n *node)
	walk = func(n *node) {
		nodes = append(nodes, n)
		label := strings.ToLower(n.label)
		labels = append(labels, label)
		var extra []string
		for _, s := range []string{n.branch, n.folder} {
			if s = strings.ToLower(s); s != "" && s != label {
				extra = append(extra, s)
			}
		}
		branches = append(branches, strings.Join(extra, " "))
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return nodes, labels, branches
}

// ---- key bindings (bubbles/key) ----

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Toggle key.Binding
	Sort   key.Binding
	Cancel key.Binding
	Filter key.Binding
}

// ShortHelp leaves the arrows out: at the popup's 55% width the line would be
// cut before the quit keys, and moving with the arrows needs no hint.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Select, k.Toggle, k.Sort, k.Cancel}
}
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

func defaultKeys() keyMap {
	return keyMap{
		Up:     key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/^p", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓/^n", "down")),
		Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Toggle: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "panes")),
		Sort:   key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^s", "sort")),
		Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc/q", "quit")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
	}
}

// ---- bubbletea model ----

type rowItem struct {
	n     *node
	depth int
	match bool
	score int   // fuzzy score (only meaningful when match)
	idx   []int // matched character positions, for highlighting
}

type model struct {
	width, height int // terminal size, from the last WindowSizeMsg
	roots         []*node
	allNodes      []*node            // flattened, parallel to lowerLabels/lowerBranches/lowerMetas
	lowerLabels   []string           // lowercased labels for the fuzzy matcher
	lowerBranches []string           // lowercased branch + worktree folder ("" when same as label); extra match text
	lowerMetas    []string           // ticket + PR number per node ("" when none); extra match text
	slugNodes     map[string][]*node // nodes with a branch, grouped by GitHub slug
	prPending     int                // in-flight gh fetches; cache is saved when it reaches 0
	cache         prCache            // loaded at startup, merged as repoPRsMsg arrive
	initCmds      []tea.Cmd          // PR fetches to fan out from Init
	rows          []rowItem
	cursor        int
	showPanes     bool // panes are hidden by default; ctrl+t toggles them
	prioritySort  bool // order every level by agent status (see sortTree); ctrl+s toggles it
	ti            textinput.Model
	vp            viewport.Model
	help          help.Model
	keys          keyMap
	action        []string // herdr CLI args to run after quit (nil = no action)
	focusPane     string   // pane to focus after quit via the socket API (pane.focus); "" = none
}

var (
	stPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stSel    = lipgloss.NewStyle().Background(lipgloss.Color("8")).Bold(true)
	// a filter match: asgitlog's look, also over the selected row's background
	stMatch    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Underline(true)
	stSelMatch = stSel.Foreground(lipgloss.Color("13")).Underline(true)
	// stDev colors the "(dev)" marker shown in the prompt for non-release builds.
	stDev = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)

	// Active-order label on the prompt line (see sortLabel).
	stDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stCount = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	stSortOn  = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stSortOff = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Ticket / PR prefix: ticket in teal, PR number colored by state.
	stTicket   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))   // teal
	stPROpen   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // green
	stPRDraft  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))   // dim
	stPRMerged = lipgloss.NewStyle().Foreground(lipgloss.Color("135")) // purple
	stPRClosed = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))   // red

	// Gutter status dots, mirroring herdr's sidebar state_dot.
	stDotBlocked = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	stDotWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	stDotDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))  // teal
	stDotIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	stDotNone    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // dim

	// Right-aligned column: listening ports on process rows (teal), the git
	// branch on repo rows, and the folder on worktree rows whose branch moved (dim).
	stPorts  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow; not pink, so ports never read as a delta
	stBranch = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // dim
	// Git hints use the shell prompt's palette (same 256-color codes as the
	// zsh prompt and the Claude Code statusline): delta pink bold, staged
	// green, unstaged yellow, untracked dim gray.
	stDelta     = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	stStaged    = lipgloss.NewStyle().Foreground(lipgloss.Color("84"))
	stUnstaged  = lipgloss.NewStyle().Foreground(lipgloss.Color("228"))
	stUntracked = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// statusDot renders the 1-rune left-gutter indicator for an aggregated agent
// status, mirroring herdr's status_icon in both of its styles (see
// statusSymbols): "dots" (blocked/working/done filled, idle hollow,
// none/unknown dim) or "symbols" (× ◐ ✓ for the first three).
func statusDot(s string) string {
	glyph := func(dot, symbol string) string {
		if statusSymbols {
			return symbol
		}
		return dot
	}
	switch s {
	case "blocked":
		return stDotBlocked.Render(glyph("●", "×"))
	case "working":
		return stDotWorking.Render(glyph("●", "◐"))
	case "done":
		return stDotDone.Render(glyph("●", "✓"))
	case "idle":
		return stDotIdle.Render("○")
	default: // "unknown", "" (shell / no agent)
		return stDotNone.Render("·")
	}
}

// statusSymbols mirrors herdr's `ui.status_indicators = "symbols"`; false is
// its default, "dots". Set once in main by applyHerdrConfig.
var statusSymbols bool

// herdrConfigPath resolves herdr's config.toml the way herdr does
// (config_path in src/config/io.rs): HERDR_CONFIG_PATH, then
// $XDG_CONFIG_HOME/herdr, then ~/.config/herdr.
func herdrConfigPath() string {
	if p := os.Getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "herdr", "config.toml")
}

var (
	reTomlTable  = regexp.MustCompile(`^\s*\[\s*([^\]]+?)\s*\]\s*(#.*)?$`)
	reTomlString = regexp.MustCompile(`^\s*([A-Za-z0-9_.\s-]+?)\s*=\s*["']([^"']*)["']`)
)

// herdrConfigString reads one string value (`table.key`) out of herdr's
// config.toml. A line scan instead of a TOML dependency: the keys asgoto needs
// are plain strings, written either under their [table] or as a dotted
// top-level key. "" when absent.
func herdrConfigString(config, table, key string) string {
	current := ""
	for _, line := range strings.Split(config, "\n") {
		if m := reTomlTable.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		m := reTomlString.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.Join(strings.Fields(m[1]), "")
		if (current == table && name == key) || (current == "" && name == table+"."+key) {
			return m[2]
		}
	}
	return ""
}

// applyHerdrConfig mirrors the two herdr settings the status gutter depends
// on: `ui.status_indicators` (glyph style; anything but "symbols" is herdr's
// default, "dots") and `theme.name`. Only dracula's palette is mirrored
// (Palette::dracula in herdr's src/app/state.rs: red, yellow, teal, green,
// overlay0); every other theme keeps the terminal's ANSI colors.
func applyHerdrConfig(config string) {
	statusSymbols = herdrConfigString(config, "ui", "status_indicators") == "symbols"
	if strings.ToLower(herdrConfigString(config, "theme", "name")) == "dracula" {
		stDotBlocked = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555"))
		stDotWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c"))
		stDotDone = lipgloss.NewStyle().Foreground(lipgloss.Color("#8be9fd"))
		stDotIdle = lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b"))
		stDotNone = lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4"))
	}
}

func prStyle(state string) lipgloss.Style {
	switch state {
	case "draft":
		return stPRDraft
	case "merged":
		return stPRMerged
	case "closed":
		return stPRClosed
	default: // "open"
		return stPROpen
	}
}

// layoutPrefixes stamps per-sibling-group column widths for the ticket / PR
// prefix so sibling rows align. Widths consider all siblings (not just
// visible rows) to keep alignment stable while filtering. Nodes with neither
// ticket nor PR keep 0 = no prefix at all. Re-run whenever PR annotations
// change.
func layoutPrefixes(roots []*node) {
	var layout func(siblings []*node)
	layout = func(siblings []*node) {
		tktW, prW := 0, 0
		for _, n := range siblings {
			if len(n.ticket) > tktW {
				tktW = len(n.ticket)
			}
			if n.pr != nil {
				if w := len(fmt.Sprintf("#%d", n.pr.Number)); w > prW {
					prW = w
				}
			}
		}
		for _, n := range siblings {
			if n.ticket != "" || n.pr != nil {
				n.tktW, n.prW = tktW, prW
			} else {
				n.tktW, n.prW = 0, 0
			}
			layout(n.children)
		}
	}
	layout(roots)
}

// prPrefixPlain builds the "TICKET #123  " prefix without styling, for the
// selected row (nested ANSI inside the selection background misrenders).
// Empty when the node has neither ticket nor PR; either column pads with
// spaces when a sibling has it and this row doesn't.
func prPrefixPlain(n *node) string {
	if n.tktW == 0 && n.prW == 0 {
		return ""
	}
	s := ""
	if n.tktW > 0 {
		s = fmt.Sprintf("%-*s", n.tktW, n.ticket)
	}
	if n.prW > 0 {
		pr := ""
		if n.pr != nil {
			pr = fmt.Sprintf("#%d", n.pr.Number)
		}
		if s != "" {
			s += " "
		}
		s += fmt.Sprintf("%-*s", n.prW, pr)
	}
	return s + "  "
}

// prPrefix is the styled variant used on unselected rows.
func prPrefix(n *node) string {
	if n.tktW == 0 && n.prW == 0 {
		return ""
	}
	s := ""
	if n.tktW > 0 {
		s = stTicket.Render(fmt.Sprintf("%-*s", n.tktW, n.ticket))
	}
	if n.prW > 0 {
		if s != "" {
			s += " "
		}
		if n.pr != nil {
			pr := fmt.Sprintf("#%d", n.pr.Number)
			s += prStyle(n.pr.State).Render(pr) + strings.Repeat(" ", n.prW-len(pr))
		} else {
			s += strings.Repeat(" ", n.prW)
		}
	}
	return s + "  "
}

// promptText builds the textinput prompt. Release builds (version stamped from a
// vX.Y.Z tag) show "asgoto ❯ "; non-release builds (`dev` / `local-<sha>`) insert
// an orange "(dev)" marker so it's obvious you're not on a published version.
func promptText() string {
	if strings.HasPrefix(version, "v") {
		return stPrompt.Render("asgoto ❯ ")
	}
	return stPrompt.Render("asgoto (") + stDev.Render("dev") + stPrompt.Render(") ❯ ")
}

// sortLabel names the active order on the prompt line's right edge: unlike
// ctrl+t, the list alone does not tell which one is on. Dim for the default
// order, prompt-colored when priority sort is on.
func sortLabel(byPriority bool) string {
	if byPriority {
		return stSortOn.Render("sort: priority")
	}
	return stSortOff.Render("sort: spaces")
}

// sortLabelW is the widest sortLabel. The label sits on the counter edge of
// the frame; the constant is kept for the narrow-popup cutoff, where typing
// never runs under the label.
const sortLabelW = len("sort: priority")

// refreshMetas rebuilds the ticket / PR-number search corpus, parallel to
// allNodes. This is what lets a query like "1234" find the row showing
// "#1234", or a ticket that only came from a PR title. Rebuilt whenever PR
// annotations change (they arrive async from gh).
func (m *model) refreshMetas() {
	m.lowerMetas = m.lowerMetas[:0]
	for _, n := range m.allNodes {
		meta := strings.ToLower(n.ticket)
		if n.pr != nil {
			if meta != "" {
				meta += " "
			}
			meta += fmt.Sprintf("#%d", n.pr.Number)
		}
		if len(n.ports) > 0 {
			if meta != "" {
				meta += " "
			}
			meta += portsText(n)
		}
		m.lowerMetas = append(m.lowerMetas, meta)
	}
}

func (m *model) persisted() persisted {
	return persisted{ShowPanes: m.showPanes, PrioritySort: m.prioritySort}
}

// resort applies the current sort mode and rebuilds everything that is
// parallel to the tree order (allNodes and the three match corpora).
func (m *model) resort() {
	sortTree(m.roots, m.prioritySort)
	m.allNodes, m.lowerLabels, m.lowerBranches = flatten(m.roots)
	m.refreshMetas()
}

// kindBonus biases the ranking so repo/worktree names outrank panes on ties,
// keeping the "type h -> herdr" feel even though fuzzy does the real scoring.
func kindBonus(kind string) int {
	switch kind {
	case "repo":
		return 8
	case "worktree":
		return 4
	default:
		return 0
	}
}

func (m *model) applyFilter() {
	q := strings.ToLower(m.ti.Value())
	filtering := q != ""

	type hit struct {
		score int
		idx   []int
	}
	hits := map[*node]hit{}
	if filtering {
		for _, mt := range findTight(q, m.lowerLabels) {
			hits[m.allNodes[mt.Index]] = hit{mt.Score, mt.MatchedIndexes}
		}
		// Branches and ticket/PR metadata are matched separately so queries
		// like "feat/x" find a worktree whose label is the "feat-x" folder
		// slug, and "1234" finds the row showing PR #1234. These hits carry
		// no MatchedIndexes: those indexes point into the branch/meta text,
		// not the rendered label, so there is nothing to highlight.
		for _, corpus := range [][]string{m.lowerBranches, m.lowerMetas} {
			for _, mt := range findTight(q, corpus) {
				n := m.allNodes[mt.Index]
				if h, ok := hits[n]; !ok || mt.Score > h.score {
					hits[n] = hit{mt.Score, nil}
				}
			}
		}
	}

	// Process rows (panes running a command) are always listed; plain panes
	// only when toggled on.
	visible := func(n *node) bool { return m.showPanes || n.kind != "pane" || n.proc != "" }

	var subtree func(n *node) bool
	subtree = func(n *node) bool {
		if !visible(n) {
			return false
		}
		if !filtering {
			return true
		}
		if _, ok := hits[n]; ok {
			return true
		}
		for _, c := range n.children {
			if subtree(c) {
				return true
			}
		}
		return false
	}

	m.rows = m.rows[:0]
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		h, ok := hits[n]
		m.rows = append(m.rows, rowItem{n: n, depth: depth, match: ok, score: h.score, idx: h.idx})
		if n.expanded || filtering {
			for _, c := range n.children {
				if subtree(c) {
					walk(c, depth+1)
				}
			}
		}
	}
	for _, r := range m.roots {
		if subtree(r) {
			walk(r, 0)
		}
	}

	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *model) parentOf(target *node) *node {
	for _, n := range m.allNodes {
		for _, c := range n.children {
			if c == target {
				return n
			}
		}
	}
	return nil
}

// keepCursorOn tries to keep the cursor on the same node after rows change; if
// that node is gone (e.g. a pane we just hid), it falls back to its parent.
func (m *model) keepCursorOn(target *node) {
	if target == nil {
		return
	}
	candidates := []*node{target}
	if p := m.parentOf(target); p != nil {
		candidates = append(candidates, p)
	}
	for _, want := range candidates {
		for i, r := range m.rows {
			if r.n == want {
				m.cursor = i
				return
			}
		}
	}
}

func (m *model) selectBestMatch() {
	best := -1
	var bestScore int
	for i, r := range m.rows {
		if !r.match {
			continue
		}
		sc := r.score + kindBonus(r.n.kind)
		if best == -1 || sc > bestScore {
			best, bestScore = i, sc
		}
	}
	if best >= 0 {
		m.cursor = best
	} else {
		m.cursor = 0
	}
}

func rowLine(r rowItem, selected bool, width int) string {
	indent := strings.Repeat("  ", r.depth)
	// The status dot sits in its own gutter to the left, outside the selection
	// highlight.
	dot := statusDot(r.n.status) + " "
	if selected {
		// Plain text inside the highlight, except the filter's matches, which
		// stay marked where the cursor is. Every piece carries the background
		// itself: nested ANSI on a background renders inconsistently across
		// terminals. Constant 2-col gutter keeps content aligned whether or not
		// the row is selected.
		var idx []int
		if r.match {
			idx = r.idx
		}
		left := "▌ " + indent + prPrefixPlain(r.n)
		leftW := lipgloss.Width(left + r.n.label)
		right := rightColumn(rightSegs(r.n), width-2-rightMargin-leftW, false)
		// Pad to the full row width so the highlight spans the line, not just
		// the text (the gutter takes 2 columns).
		if pad := width - 2 - leftW - lipgloss.Width(right); pad > 0 {
			right += strings.Repeat(" ", pad)
		}
		return dot + stSel.Render(left) + highlight(r.n.label, idx, true) + stSel.Render(right)
	}
	name := r.n.label
	if r.match {
		name = highlight(r.n.label, r.idx, false)
	}
	left := "  " + indent + prPrefix(r.n) + name
	return dot + left + rightColumn(rightSegs(r.n), width-2-rightMargin-lipgloss.Width(left), true)
}

// rightMargin keeps the right-aligned column off the popup's edge.
const rightMargin = 1

// rightMaxW caps the right-aligned column so it never crowds out the row's
// own label.
const rightMaxW = 36

// seg is one styled piece of a row's right column.
type seg struct {
	text string
	st   lipgloss.Style
}

// rightSegs is what a row shows in its right-aligned column. Process rows:
// their listening ports. Repo and worktree rows, in the shell prompt's
// order: the name, then the ahead/behind hint (when not in sync), then the
// working-tree counters ("+n !n ?n", the shell prompt's symbols, each
// hidden at zero). The name is the checked-out branch on repo rows
// (always, so a main checkout sitting on a feature branch is visible at a
// glance); on worktree rows, whose label already is the branch, it is the
// folder name, only when the branch is known and the folder is not its
// slug ("feat/x" -> "feat-x" adds nothing), i.e. the worktree was created
// for one branch and now has another checked out.
func rightSegs(n *node) []seg {
	var segs []seg
	// slot pads text left-aligned to the column's width (so the ↑/+/!/?
	// symbols align vertically) and emits nothing when no row has the hint.
	slot := func(text string, w int, st lipgloss.Style) {
		if w > 0 {
			segs = append(segs, seg{text + strings.Repeat(" ", w-lipgloss.Width(text)), st})
		}
	}
	if n.kind == "pane" {
		if t := portsText(n); t != "" {
			// Same blank hint slots as repo/worktree rows, so the ports
			// end on the column the names end on.
			segs = append(segs, seg{t, stPorts})
			slot("", n.deltaW, stBranch)
			slot("", n.stagedW, stBranch)
			slot("", n.unstagW, stBranch)
			slot("", n.untrkW, stBranch)
			return segs
		}
		return nil
	}
	switch {
	case n.kind == "repo" && n.branch != "":
		segs = append(segs, seg{n.branch, stBranch})
	case n.kind == "worktree" && n.branch != "" && strings.ReplaceAll(n.branch, "/", "-") != n.folder:
		segs = append(segs, seg{n.folder, stBranch})
	}
	slot(deltaText(n), n.deltaW, stDelta)
	slot(countText('+', n.staged), n.stagedW, stStaged)
	slot(countText('!', n.unstaged), n.unstagW, stUnstaged)
	slot(countText('?', n.untrack), n.untrkW, stUntracked)
	return segs
}

// rightText is the right column as plain text (segments space-joined,
// alignment padding trimmed), for -dump and tests.
func rightText(n *node) string {
	segs := rightSegs(n)
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.text
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// rightColumn renders the segments right-aligned within the room left on the
// row (avail columns after the gutter, the label and the margin). It keeps at
// least two columns of separation and truncates with an ellipsis (dropping
// the per-segment styling, since the cut may fall inside one); when there is
// no room it renders nothing rather than wrapping the row. styled=false
// renders plain text (for the selected row, whose highlight covers the line).
func rightColumn(segs []seg, avail int, styled bool) string {
	if len(segs) == 0 {
		return ""
	}
	room := avail - 2
	if room > rightMaxW {
		room = rightMaxW
	}
	if room < 4 {
		return ""
	}
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.text
	}
	plainText := strings.Join(parts, " ")
	if w := lipgloss.Width(plainText); w > room {
		text := truncate(plainText, room)
		pad := strings.Repeat(" ", avail-lipgloss.Width(text))
		if styled {
			return pad + segs[0].st.Render(text)
		}
		return pad + text
	}
	pad := strings.Repeat(" ", avail-lipgloss.Width(plainText))
	if !styled {
		return pad + plainText
	}
	for i, s := range segs {
		parts[i] = s.st.Render(s.text)
	}
	return pad + strings.Join(parts, " ")
}

// truncate shortens s to at most max runes, ending in "…" when cut.
func truncate(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max-1]) + "…"
}

// highlight styles the fuzzy-matched characters within a label. The selected
// row keeps its background under them, so a match stays visible where the
// cursor is.
func highlight(label string, idx []int, selected bool) string {
	plain, match := lipgloss.NewStyle(), stMatch
	if selected {
		plain, match = stSel, stSelMatch
	}
	set := make(map[int]bool, len(idx))
	for _, i := range idx {
		set[i] = true
	}
	var b, run strings.Builder
	on := false
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if on {
			b.WriteString(match.Render(run.String()))
		} else {
			b.WriteString(plain.Render(run.String()))
		}
		run.Reset()
	}
	for i, r := range label { // i is a byte offset, like the matcher's
		if set[i] != on {
			flush()
			on = set[i]
		}
		run.WriteRune(r)
	}
	flush()
	return b.String()
}

func (m *model) renderContent() {
	var b strings.Builder
	for i, r := range m.rows {
		b.WriteString(rowLine(r, i == m.cursor, m.vp.Width()))
		if i < len(m.rows)-1 {
			b.WriteString("\n")
		}
	}
	m.vp.SetContent(b.String())
	m.ensureVisible()
}

func (m *model) ensureVisible() {
	h := m.vp.Height()
	if h <= 0 {
		return
	}
	if m.cursor < m.vp.YOffset() {
		m.vp.SetYOffset(m.cursor)
	} else if m.cursor >= m.vp.YOffset()+h {
		m.vp.SetYOffset(m.cursor - h + 1)
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(append([]tea.Cmd{textinput.Blink}, m.initCmds...)...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.SetWidth(m.innerW())
		m.vp.SetHeight(max(1, msg.Height-frameRows-1)) // the frame's own lines + help
		m.help.SetWidth(max(0, msg.Width-4))
		// Input inside the frame's padding (2 = cursor cell + a gap).
		m.ti.SetWidth(max(1, m.innerW()-2-lipgloss.Width(m.ti.Prompt)-2))
		m.renderContent()
		return m, nil

	case deltaMsg:
		dn := deltaNodes(m.allNodes)
		annotateDeltas(dn, msg.byCheckout)
		layoutHints(m.allNodes)
		m.renderContent()
		// Keep only the checkouts still listed, so the cache doesn't grow
		// with removed worktrees.
		hints := make(map[string]gitDelta, len(dn))
		for _, n := range dn {
			if d, ok := msg.byCheckout[n.checkout]; ok {
				hints[n.checkout] = d
			} else if d, ok := m.cache.Hints[n.checkout]; ok {
				hints[n.checkout] = d
			}
		}
		m.cache.Hints = hints
		return m, savePRCacheCmd(m.cache)

	case portsMsg:
		annotatePorts(procRows(m.allNodes), msg.byPane)
		// Ports join the search corpus; re-filter, keeping the cursor.
		m.refreshMetas()
		var cur *node
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			cur = m.rows[m.cursor].n
		}
		m.applyFilter()
		m.keepCursorOn(cur)
		m.renderContent()
		return m, nil

	case repoPRsMsg:
		if !msg.err {
			annotatePRs(m.slugNodes[msg.slug], msg.byBranch)
			m.cache.Repos[msg.slug] = repoPRs{FetchedAt: time.Now(), Branches: msg.byBranch}
			layoutPrefixes(m.roots)
			// PR numbers are searchable, so a fresh annotation can change the
			// results of the query being typed; re-filter, keeping the cursor
			// on the same node.
			m.refreshMetas()
			var cur *node
			if m.cursor >= 0 && m.cursor < len(m.rows) {
				cur = m.rows[m.cursor].n
			}
			m.applyFilter()
			m.keepCursorOn(cur)
			m.renderContent()
		}
		m.prPending--
		if m.prPending <= 0 {
			return m, savePRCacheCmd(m.cache)
		}
		return m, nil

	case tea.MouseWheelMsg:
		// There is no preview to scroll: the wheel walks the cursor.
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
				m.renderContent()
			}
		case tea.MouseWheelDown:
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.renderContent()
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		// A left click on a row moves the cursor; it never selects, so a stray
		// click cannot switch spaces (same reasoning as asgotopr).
		if msg.Button != tea.MouseLeft || msg.X < 1 || msg.X > m.innerW() ||
			msg.Y < listY || msg.Y >= listY+m.vp.Height() {
			return m, nil
		}
		if i := msg.Y - listY + m.vp.YOffset(); i >= 0 && i < len(m.rows) && i != m.cursor {
			m.cursor = i
			m.renderContent()
		}
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case msg.String() == "q" && m.ti.Value() == "":
			// q quits only while the filter is empty; otherwise it is text.
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Select):
			if m.cursor >= 0 && m.cursor < len(m.rows) {
				n := m.rows[m.cursor].n
				if n.kind == "pane" {
					m.focusPane = n.paneID
				} else {
					m.action = []string{"workspace", "focus", n.wsID}
				}
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.renderContent()
			}
			return m, nil
		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.renderContent()
			}
			return m, nil
		case key.Matches(msg, m.keys.Toggle):
			var cur *node
			if m.cursor >= 0 && m.cursor < len(m.rows) {
				cur = m.rows[m.cursor].n
			}
			m.showPanes = !m.showPanes
			m.applyFilter()
			m.keepCursorOn(cur)
			m.renderContent()
			return m, saveStateCmd(m.persisted())
		case key.Matches(msg, m.keys.Sort):
			var cur *node
			if m.cursor >= 0 && m.cursor < len(m.rows) {
				cur = m.rows[m.cursor].n
			}
			m.prioritySort = !m.prioritySort
			m.resort()
			m.applyFilter()
			m.keepCursorOn(cur)
			m.renderContent()
			return m, saveStateCmd(m.persisted())
		}

		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		m.applyFilter()
		if m.ti.Value() != "" {
			m.selectBestMatch()
		}
		m.renderContent()
		return m, cmd

	default:
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		return m, cmd
	}
}

// View declares the screen: alt screen and cell-motion mouse reports.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// The screen is one rounded frame of sections split by shared edges, the
// layout asgitlog introduced and the other pickers share: the filter input
// (the top border over it carries the matches/total counter and the active
// order), the tree, and the help. There is no context line: nothing here needs
// one. asgoto has no preview either, so the tree takes the whole main section
// and its bottom edge carries the list position.
const (
	mainY     = 2 // the edge over the main section
	listY     = mainY + 1
	frameRows = 5 // top border, input, two edges, bottom border
)

// innerW is the width inside the frame's sides.
func (m model) innerW() int { return max(20, m.width-2) }

// hline draws a horizontal border w cells wide between the corners l and r,
// with an optional (already styled) text set into it near the right end.
func hline(w int, l, r, right string) string {
	inner := max(0, w-2)
	if right != "" {
		right = " " + right + " "
	}
	if 2+ansi.StringWidth(right) > inner {
		right = ""
	}
	fill := inner - ansi.StringWidth(right)
	tail := 0
	if right != "" {
		tail = min(1, fill)
	}
	return stDim.Render(l+strings.Repeat("─", fill-tail)) + right + stDim.Render(strings.Repeat("─", tail)+r)
}

// fit truncates or pads s to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// framed sets a line, with a cell of padding, between the frame's sides.
func framed(w int, l string) string {
	side := stDim.Render("│")
	return side + fit(" "+l, w-2) + side
}

// render stacks the four sections in one frame; the tests assert on it.
func (m model) render() string {
	w, side := m.width, stDim.Render("│")
	out := []string{
		hline(w, "╭", "╮", m.counter()),
		framed(w, m.ti.View()),
		hline(w, "├", "┤", ""),
	}
	lines := strings.Split(m.vp.View(), "\n")
	for i := 0; i < m.vp.Height(); i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, side+fit(l, m.innerW())+side)
	}
	pos := ""
	if total := len(m.rows); total > m.vp.Height() {
		pos = stDim.Render(strconv.Itoa(min(total, m.vp.YOffset()+m.vp.Height())) + "/" + strconv.Itoa(total))
	}
	out = append(out, hline(w, "├", "┤", pos),
		framed(w, ansi.Truncate(m.help.View(m.keys), max(0, w-4), "…")),
		hline(w, "╰", "╯", ""))
	return strings.Join(out, "\n")
}

// counter is the rows listed out of every row of the current mode, with the
// active order next to it; the order is dropped on a popup too narrow for it.
func (m model) counter() string {
	total := 0
	for _, n := range m.allNodes {
		// Same rule as applyFilter: process rows are always listed, plain
		// panes only when toggled on.
		if m.showPanes || n.kind != "pane" || n.proc != "" {
			total++
		}
	}
	s := stCount.Render(strconv.Itoa(len(m.rows)) + "/" + strconv.Itoa(total))
	if m.width >= 2*sortLabelW+16 {
		s += " " + sortLabel(m.prioritySort)
	}
	return s
}

// version is the release tag; overridden at build time via
// -ldflags "-X main.version=vX.Y.Z" (see scripts/release.sh and CI).
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Println(version)
		return
	}
	dump := len(os.Args) > 1 && os.Args[1] == "-dump"

	var ws wsResp
	var pn paneResp
	if err := loadJSON([]string{"workspace", "list"}, &ws); err != nil {
		fmt.Fprintln(os.Stderr, "workspace list:", err)
		os.Exit(1)
	}
	if err := loadJSON([]string{"pane", "list"}, &pn); err != nil {
		fmt.Fprintln(os.Stderr, "pane list:", err)
		os.Exit(1)
	}
	// Only feeds the priority order's tiebreaker, so a failure (older herdr)
	// degrades to ordering by status alone.
	var ag agentResp
	_ = loadJSON([]string{"agent", "list"}, &ag)
	seqs := make(map[string]uint64, len(ag.Result.Agents))
	for _, a := range ag.Result.Agents {
		seqs[a.PaneID] = a.Seq
	}
	if data, err := os.ReadFile(herdrConfigPath()); err == nil {
		applyHerdrConfig(string(data))
	}
	state := loadState()
	roots := buildTree(ws.Result.Workspaces, pn.Result.Panes, seqs)
	sortTree(roots, state.PrioritySort)
	allNodes, lowerLabels, lowerBranches := flatten(roots)

	// Annotate PR info from the disk cache (instant), then schedule a gh
	// refresh per repo whose cache entry is missing or no longer fresh. The
	// fetches run from Init, after the TUI is already on screen.
	slugNodes := map[string][]*node{}
	for _, n := range allNodes {
		if n.ghSlug != "" && n.branch != "" {
			slugNodes[n.ghSlug] = append(slugNodes[n.ghSlug], n)
		}
	}
	cache := loadPRCache()
	for slug, nodes := range slugNodes {
		if entry, ok := cache.Repos[slug]; ok {
			annotatePRs(nodes, entry.Branches)
		}
	}
	layoutPrefixes(roots)
	var initCmds []tea.Cmd
	for slug, nodes := range slugNodes {
		if entry, ok := cache.Repos[slug]; ok && time.Since(entry.FetchedAt) < prCacheFresh {
			continue
		}
		// One --head lookup per unique local branch; main/master checkouts are
		// skipped (a PR with that head would be someone else's release train).
		seen := map[string]bool{}
		var branches []string
		for _, n := range nodes {
			if n.branch == "main" || n.branch == "master" || seen[n.branch] {
				continue
			}
			seen[n.branch] = true
			branches = append(branches, n.branch)
		}
		if len(branches) == 0 {
			continue
		}
		initCmds = append(initCmds, fetchRepoPRsCmd(slug, branches, cache.Repos[slug].Branches))
	}

	prPending := len(initCmds)
	paneIDs := make([]string, 0, len(pn.Result.Panes))
	for _, p := range pn.Result.Panes {
		paneIDs = append(paneIDs, p.ID)
	}
	// Process rows are resolved before the first paint (cheap, and they add
	// rows); their ports arrive async (lsof is the slow part) and only fill the
	// right column.
	annotateProcs(roots, fetchProcInfos(paneIDs))
	allNodes, lowerLabels, lowerBranches = flatten(roots)
	if procs := procRows(allNodes); len(procs) > 0 {
		if dump {
			annotatePorts(procs, fetchPorts(fetchPortsCmdPids(procs)))
		} else {
			initCmds = append(initCmds, fetchPortsCmd(procs))
		}
	}
	if dn := deltaNodes(allNodes); len(dn) > 0 {
		if dump {
			annotateDeltas(dn, fetchDeltas(dn))
		} else {
			// Cached hints paint first (and size the columns); git
			// revalidates them right after.
			annotateDeltas(dn, cache.Hints)
			initCmds = append(initCmds, fetchDeltasCmd(dn))
		}
		layoutHints(allNodes)
	}

	if dump {
		var walk func(n *node, d int)
		walk = func(n *node, d int) {
			prefix := ""
			if n.ticket != "" {
				prefix += n.ticket + " "
			}
			if n.pr != nil {
				prefix += fmt.Sprintf("#%d(%s) ", n.pr.Number, n.pr.State)
			}
			id := n.wsID
			if n.kind == "pane" {
				id = n.paneID
			}
			branch := ""
			if n.branch != "" {
				branch = " " + n.branch
			}
			if n.folder != "" && n.folder != n.label {
				branch += " folder=" + n.folder
			}
			if d := deltaText(n); d != "" {
				branch += " " + d
			}
			if c := countsText(n); c != "" {
				branch += " " + c
			}
			proc := ""
			if n.proc != "" {
				proc = "\t[proc " + portsText(n) + "]"
			}
			fmt.Printf("%s%s%s\t(%s %s %s%s)%s\n", strings.Repeat("  ", d), prefix, n.label, n.kind, n.status, id, branch, proc)
			for _, c := range n.children {
				walk(c, d+1)
			}
		}
		for _, r := range roots {
			walk(r, 0)
		}
		return
	}

	ti := textinput.New()
	ti.Prompt = promptText()
	tiStyles := ti.Styles()
	tiStyles.Focused.Prompt = lipgloss.NewStyle() // colors are already baked into the prompt
	tiStyles.Blurred.Prompt = lipgloss.NewStyle()
	ti.SetStyles(tiStyles)
	ti.Focus()

	m := model{
		roots:         roots,
		allNodes:      allNodes,
		lowerLabels:   lowerLabels,
		lowerBranches: lowerBranches,
		slugNodes:     slugNodes,
		prPending:     prPending,
		cache:         cache,
		initCmds:      initCmds,
		showPanes:     state.ShowPanes,
		prioritySort:  state.PrioritySort,
		ti:            ti,
		width:         82, // until the first WindowSizeMsg: the frame around an 80x20 tree
		height:        26,
		vp:            viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		help:          help.New(),
		keys:          defaultKeys(),
	}
	m.resort()
	m.applyFilter()
	m.keepCursorOn(currentWorkspaceNode(ws.Result.Workspaces, allNodes))
	m.renderContent()

	// The alt screen is declared per frame by View().
	res, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	final := res.(model)
	if final.focusPane != "" {
		if err := focusPane(final.focusPane); err != nil {
			// Standalone run (no socket env) or an older server: the CLI's
			// agent focus still lands on agent panes.
			exec.Command(herdrBin(), "agent", "focus", final.focusPane).Run()
		}
	}
	if final.action != nil {
		exec.Command(herdrBin(), final.action...).Run()
	}
}

// focusPane focuses an arbitrary pane (shell, process or agent) through
// herdr's socket API (`pane.focus`, newline-delimited JSON on
// HERDR_SOCKET_PATH). The CLI has no equivalent: `pane focus` is
// direction-only and `agent focus <paneID>` rejects non-agent panes since
// herdr 0.9 (agent_not_found), which used to leave shell/process rows dead.
func focusPane(paneID string) error {
	sock := os.Getenv("HERDR_SOCKET_PATH")
	if sock == "" {
		return fmt.Errorf("HERDR_SOCKET_PATH not set")
	}
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	req, err := paneFocusRequest(paneID)
	if err != nil {
		return err
	}
	if _, err := conn.Write(req); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// paneFocusRequest encodes one `pane.focus` request line for the socket API.
func paneFocusRequest(paneID string) ([]byte, error) {
	req := struct {
		ID     string            `json:"id"`
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}{ID: "asgoto:pane.focus", Method: "pane.focus", Params: map[string]string{"pane_id": paneID}}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

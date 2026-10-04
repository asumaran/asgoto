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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

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
	// Tokens are the sidebar tokens plugins reported for the space. "desc"
	// (from the asmeta plugin) says what the branch is about: its PR title,
	// else its Jira summary.
	Tokens map[string]string `json:"tokens"`
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
	desc     string // repo/worktree: what the branch is about (the space's "desc" token); extra search text, shown under the row when only it explains a match
	checkout string // repo/worktree: path of the checkout, for the async ahead/behind lookup ("" when unknown)
	cwd      string // pane: its working directory as herdr reports it; repo without worktree metadata: its first pane's ("" when unknown)
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
	prTicket bool   // ticket came from the PR title, so a new PR replaces or clears it
	ghSlug   string // "owner/repo" of the GitHub origin; "" when non-GitHub/unknown
	pr       *prRef // PR for this branch, from the shared PR cache; nil until known or when none
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
	children []*node
}

// ---- persisted UI state ----

// persisted is what asgoto remembers: one file per setting in its state
// directory (setting.go), as in every tool of the family.
type persisted struct {
	ShowPanes    bool `json:"show_panes"`
	PrioritySort bool `json:"priority_sort"`
}

func stateDir() string { return stateDirFor("asgoto") }

func loadState() persisted {
	panes, order := loadSetting(stateDir(), "panes"), loadSetting(stateDir(), "order")
	if panes == "" && order == "" {
		// Before each setting had its file, both lived in state.json.
		var old persisted
		if data, err := os.ReadFile(filepath.Join(stateDir(), "state.json")); err == nil && json.Unmarshal(data, &old) == nil {
			return old
		}
	}
	return persisted{ShowPanes: panes == "shown", PrioritySort: order == "priority"}
}

func saveStateCmd(s persisted) tea.Cmd {
	return func() tea.Msg {
		panes, order := "hidden", "spaces"
		if s.ShowPanes {
			panes = "shown"
		}
		if s.PrioritySort {
			order = "priority"
		}
		saveSetting(stateDir(), "panes", panes)
		saveSetting(stateDir(), "order", order)
		return nil
	}
}

// ---- GitHub PR info (the shared PR cache, prshare.go) ----

// prRef is the PR shown next to a branch. Ticket is extracted from the PR
// title and used only when the branch/label carry no ticket themselves. The
// title itself is search text.
type prRef struct {
	Number int
	State  string // "open" | "draft" | "merged" | "closed"
	Ticket string
	Title  string
}

func prRefFrom(p sharedPR) prRef {
	return prRef{Number: p.Number, State: p.State, Ticket: ticketFrom(p.Title), Title: p.Title}
}

// prCache is asgoto's own disk cache: the git hints (ahead/behind,
// staged/unstaged/untracked) per checkout path, so the hint columns are sized
// from the first paint instead of shifting when the refresh lands. PRs live
// in the shared cache, which asmeta writes.
type prCache struct {
	Hints map[string]gitDelta `json:"hints,omitempty"`
}

// prFresh is how recent a branch's entry in the shared cache must be for
// asgoto not to ask asmeta for a refresh.
const prFresh = 60 * time.Second

func prCacheFile() string {
	return filepath.Join(stateDir(), "prcache.json")
}

func loadPRCache() prCache {
	var c prCache
	readJSONFile(prCacheFile(), &c)
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
		writeFileAtomic(prCacheFile(), data)
		return nil
	}
}

// prNodes are the nodes whose branch can have a PR.
func prNodes(all []*node) []*node {
	var out []*node
	for _, n := range all {
		if n.ghSlug != "" && !prSkipBranch(n.branch) {
			out = append(out, n)
		}
	}
	return out
}

// annotatePRs applies the shared cache to the nodes: each gets its branch's
// PR or none, and the ticket of the PR title when branch/label yielded none
// (replaced or cleared when the PR changes).
func annotatePRs(nodes []*node, prs sharedPRs) {
	for _, n := range nodes {
		if n.prTicket {
			n.ticket, n.prTicket = "", false
		}
		p, _, _ := prs.branch(n.ghSlug, n.branch)
		if p == nil {
			n.pr = nil
			continue
		}
		r := prRefFrom(*p)
		n.pr = &r
		if n.ticket == "" && r.Ticket != "" {
			n.ticket, n.prTicket = r.Ticket, true
		}
	}
}

// prsStale: some branch was never checked, or not in the last prFresh.
func prsStale(nodes []*node, prs sharedPRs, now time.Time) bool {
	for _, n := range nodes {
		if _, checked, at := prs.branch(n.ghSlug, n.branch); !checked || now.Sub(at) >= prFresh {
			return true
		}
	}
	return false
}

// refreshPRsCmd asks asmeta to refresh the shared cache. The answer lands in
// the file, which watchPRsCmd picks up; without asmeta it fails, silently,
// and there is no PR column.
func refreshPRsCmd() tea.Cmd {
	return func() tea.Msg {
		_ = herdrDo("plugin", "action", "invoke", "asumaran.asmeta.refresh-prs")
		return nil
	}
}

// sharedPRsMsg carries the shared cache after the file changed; prWatchMsg
// keeps watching when it did not.
type sharedPRsMsg struct {
	prs   sharedPRs
	mtime time.Time
}

type prWatchMsg struct{ mtime time.Time }

// prWatchEvery is how often the open popup looks at the file's mtime: one
// stat, so a refresh asmeta finishes while the popup is open shows at once.
const prWatchEvery = 500 * time.Millisecond

func sharedPRsMtime() time.Time {
	if fi, err := os.Stat(sharedPRFile()); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

func watchPRsCmd(seen time.Time) tea.Cmd {
	return tea.Tick(prWatchEvery, func(time.Time) tea.Msg {
		if mt := sharedPRsMtime(); mt.After(seen) {
			return sharedPRsMsg{prs: loadSharedPRs(), mtime: mt}
		}
		return prWatchMsg{mtime: seen}
	})
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

// loadJSON reads the answer of a herdr command into out (herdrcli.go).
func loadJSON(args []string, out any) error {
	data, err := herdrRun(args...)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
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
				wsID: wsID, paneID: p.ID, cwd: p.Cwd, status: paneStatus(p), hasAgent: p.Agent != "",
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
		repo := &node{kind: "repo", label: name, wsID: main.ID}
		checkout, root := "", ""
		if main.Worktree != nil {
			checkout, root = main.Worktree.CheckoutPath, main.Worktree.RepoRoot
		} else if ps := byWs[main.ID]; len(ps) > 0 {
			// herdr reported no worktree metadata (e.g. the space was
			// created before its git discovery ran); the first pane's cwd
			// still tells us which checkout this is.
			checkout = gitTopLevel(ps[0].Cwd)
			root = checkout
			repo.cwd = ps[0].Cwd // what ctrl+y copies when this is no git checkout
		}
		repo.desc = main.Tokens["desc"]
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
			wt := &node{kind: "worktree", label: ws.Label, folder: ws.Label, wsID: ws.ID, desc: ws.Tokens["desc"]}
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

// nodeDir is the directory a row stands for, what ctrl+y copies: the checkout
// of a repo or worktree, the cwd of a pane (and of a space that is no git
// checkout). "" when herdr reported none.
func nodeDir(n *node) string {
	if n.checkout != "" {
		return n.checkout
	}
	return n.cwd
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

// flatten returns every node in tree order plus parallel slices of the
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
		labels = append(labels, n.label)
		var extra []string
		for _, s := range []string{n.branch, n.folder} {
			if s != "" && !strings.EqualFold(s, n.label) {
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
	Nav    listNav
	Select key.Binding
	Toggle key.Binding
	Copy   key.Binding
	Quit   key.Binding
	Filter key.Binding
	Help   key.Binding
}

// ShortHelp is the help line: the tool's own actions, the panel's key and the
// quit keys. The list's keys (listnav.go) and the copy key are in the panel:
// at the popup's 55% width the line would be cut before the quit keys.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Select, k.Toggle, k.Help, k.Quit}
}

// FullHelp is the panel's list of keys, one column per group: the filter, the
// list, the tool's actions, the panel and quit.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Filter},
		{k.Nav.Up, k.Nav.PageUp, k.Nav.Top},
		{k.Select, k.Toggle, k.Copy},
		{k.Help, k.Quit},
	}
}

func defaultKeys() keyMap {
	return keyMap{
		Nav:    defaultListNav(),
		Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Toggle: key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("^a", "panes")),
		Copy:   key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("^y", "copy the path")),
		Quit:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/q", "quit")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
		Help:   helpBinding(true),
	}
}

// ---- prose search (description, PR title) ----

// The names (label, branch, ticket) are matched fuzzily, the way they are
// typed: abbreviated. A sentence is different: fuzzy over a few dozen letters
// finds almost any word ("locales" in "Allow x-front-platform ... allowlists").
// So a term finds prose only as the start of one of its words, the way one
// remembers a sentence ("cabec" finds "Cabeceras"). A term written with a
// leading ' must occur as typed anywhere, as it does in the other fields.

// proseScore is what a prose match scores: below a good fuzzy match of a
// name, so a row named after the query still wins the cursor.
const proseScore = 50

// termHit is what one term of the query found in one row: the fuzzy score
// and label offsets (from findFields) and, per prose field, the offsets of
// its match there (nil = no match).
type termHit struct {
	score int
	label []int
	prose [2][]int // 0: description, 1: PR title
}

// findWords matches one term against the prose fields (fields[f][i] is field
// f of row i), keyed by row. A row is a hit when the term starts a word of
// any of them.
func findWords(q string, fields ...[]string) map[int][2][]int {
	hits := map[int][2][]int{}
	terms := queryTerms(q, true)
	if len(terms) == 0 {
		return hits
	}
	t := terms[0]
	for f, corpus := range fields {
		for i, s := range corpus {
			if s == "" {
				continue
			}
			if idx := wordMatch(s, t); idx != nil {
				h := hits[i]
				h[f] = idx
				hits[i] = h
			}
		}
	}
	return hits
}

// wordMatch returns the byte offsets of t in s, nil when it is not there. A
// fuzzy term must start a word, ignoring case; a literal one (') may sit
// anywhere.
func wordMatch(s string, t qterm) []int {
	lower := strings.ToLower(s)
	text := strings.ToLower(t.text)
	if len(lower) != len(s) {
		return nil // lowercasing moved the bytes: the offsets would not point into s
	}
	at := -1
	if !t.fuzzy {
		at = strings.Index(lower, text)
	} else {
		for i := 0; i < len(lower); {
			if strings.HasPrefix(lower[i:], text) && (i == 0 || !wordRune(lastRune(lower[:i]))) {
				at = i
				break
			}
			_, size := utf8.DecodeRuneInString(lower[i:])
			i += size
		}
	}
	if at < 0 {
		return nil
	}
	idx := make([]int, 0, len(text))
	for i := at; i < at+len(text); i++ {
		idx = append(idx, i)
	}
	return idx
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// mergeTerm joins what one term found by name (fuzzy) and in the prose (by
// word). A row found both ways keeps its fuzzy score.
func mergeTerm(byName map[int]fieldsHit, byWord map[int][2][]int) map[int]termHit {
	out := make(map[int]termHit, len(byName)+len(byWord))
	for i, fh := range byName {
		out[i] = termHit{score: fh.Score, label: fh.Any[0]}
	}
	for i, prose := range byWord {
		th, ok := out[i]
		if !ok {
			th.score = proseScore
		}
		th.prose = prose
		out[i] = th
	}
	return out
}

// ---- bubbletea model ----

type rowItem struct {
	n        *node
	depth    int
	match    bool
	score    int    // fuzzy score (only meaningful when match)
	idx      []int  // matched character positions, for highlighting
	extra    string // prose shown under the row (description or PR title) when only it explains the match; "" = none
	extraIdx []int  // matched positions in extra
}

// lines is how many lines the row takes in the list: one, or two when prose
// is shown under it.
func (r rowItem) lines() int {
	if r.extra != "" {
		return 2
	}
	return 1
}

type model struct {
	width, height int // terminal size, from the last WindowSizeMsg
	roots         []*node
	allNodes      []*node   // flattened, parallel to labels/branches/metas
	labels        []string  // the labels as shown, for the matcher (it folds case; its offsets are bytes into them)
	branches      []string  // branch + worktree folder ("" when same as label); extra match text
	metas         []string  // ticket + PR number per node ("" when none); extra match text
	descs         []string  // what the branch is about per node ("" when none); prose match text
	prTitles      []string  // PR title per node ("" when none); prose match text
	prNodes       []*node   // nodes whose branch can have a PR
	cache         prCache   // loaded at startup, merged as deltaMsg arrive
	initCmds      []tea.Cmd // PR fetches to fan out from Init
	rows          []rowItem
	cursor        int
	showPanes     bool // panes are hidden by default; ctrl+a toggles them (the family's "list more" key)
	prioritySort  bool // order every level by agent status (see sortTree); ctrl+s toggles it
	ti            textinput.Model
	vp            viewport.Model
	help          help.Model
	keys          keyMap
	flash         flash    // confirmation on the help line (flash.go)
	panel         panel    // options and keys, over the frame while it is open (panel.go)
	action        []string // herdr CLI args to run after quit (nil = no action)
	focusPane     string   // pane to focus after quit via the socket API (pane.focus); "" = none
}

var (
	// stDev colors the "(dev)" marker shown in the prompt for non-release builds.

	// Active-order label on the prompt line (see sortLabel).
	stDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stCount = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	stSortOn  = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stSortOff = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stError   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true) // the shared foot line and empty list use it

	// Ticket / PR prefix: ticket in teal, PR number colored by state.
	stTicket   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))  // teal
	stPROpen   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	stPRDraft  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // dim
	stPRMerged = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))  // purple (ANSI, so it follows the theme)
	stPRClosed = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red

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

// sortLabel names the active order on the edge over the input: unlike
// ctrl+a, the list alone does not tell which one is on. Dim for the default
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

// refreshMetas rebuilds the ticket / PR-number search corpus and the prose
// ones (description, PR title), parallel to allNodes. This is what lets a query like "1234" find the row showing
// "#1234", or a ticket that only came from a PR title. Rebuilt whenever PR
// annotations change (they arrive async from gh).
func (m *model) refreshMetas() {
	m.metas = m.metas[:0]
	for _, n := range m.allNodes {
		meta := n.ticket
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
		m.metas = append(m.metas, meta)
	}
	m.descs, m.prTitles = m.descs[:0], m.prTitles[:0]
	for _, n := range m.allNodes {
		title := ""
		if n.pr != nil {
			title = n.pr.Title
		}
		m.descs = append(m.descs, n.desc)
		m.prTitles = append(m.prTitles, title)
	}
}

func (m *model) persisted() persisted {
	return persisted{ShowPanes: m.showPanes, PrioritySort: m.prioritySort}
}

// resort applies the current sort mode and rebuilds everything that is
// parallel to the tree order (allNodes and the three match corpora).
func (m *model) resort() {
	sortTree(m.roots, m.prioritySort)
	m.allNodes, m.labels, m.branches = flatten(m.roots)
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
	// One set of hits per term of the query (see queryTerms): branches and
	// ticket/PR metadata are searched next to the label, so "feat/x" finds a
	// worktree whose label is the "feat-x" folder slug and "1234" the row
	// showing PR #1234. Only label matches are highlighted: the other offsets
	// point into text the row does not show. The prose (description, PR
	// title) is matched by word instead (findWords): fuzzy over a sentence
	// finds almost anything. When a term matched the prose and not the label,
	// the prose is shown under the row with its matches marked, so the row
	// says why it is listed.
	var perTerm []map[int]termHit
	for _, tok := range strings.Fields(m.ti.Value()) {
		if len(queryTerms(tok, true)) > 0 {
			perTerm = append(perTerm, mergeTerm(findFields(tok, m.labels, m.branches, m.metas), findWords(tok, m.descs, m.prTitles)))
		}
	}
	filtering := len(perTerm) > 0

	type hit struct {
		score    int
		idx      []int
		extra    string // prose to show under the row; "" when the label explains the match
		extraIdx []int
	}
	hits := map[*node]hit{}
	if filtering {
		index := make(map[*node]int, len(m.allNodes))
		for i, n := range m.allNodes {
			index[n] = i
		}
		// The tree is part of what a row says: "herdr fix" finds the fix
		// worktree of the herdr repo. A node is a hit when it matches a term
		// itself and every other term matches it or one of its ancestors.
		var mark func(n *node, above []bool)
		mark = func(n *node, above []bool) {
			covered := slices.Clone(above)
			own, all, h := false, true, hit{}
			var proseIdx [2][]int // per prose field: the matches of every term
			show := false
			for t, found := range perTerm {
				if th, ok := found[index[n]]; ok {
					own, covered[t] = true, true
					h.score += th.score
					h.idx = mergeIdx(h.idx, th.label)
					for f := range proseIdx {
						proseIdx[f] = mergeIdx(proseIdx[f], th.prose[f])
					}
					show = show || (th.label == nil && (th.prose[0] != nil || th.prose[1] != nil))
				}
				all = all && covered[t]
			}
			if show {
				// The description when it matched, else the PR title.
				h.extra, h.extraIdx = n.desc, proseIdx[0]
				if proseIdx[0] == nil {
					h.extra, h.extraIdx = m.prTitles[index[n]], proseIdx[1]
				}
			}
			if own && all {
				hits[n] = h
			}
			for _, c := range n.children {
				mark(c, covered)
			}
		}
		for _, r := range m.roots {
			mark(r, make([]bool, len(perTerm)))
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
		m.rows = append(m.rows, rowItem{n: n, depth: depth, match: ok, score: h.score, idx: h.idx, extra: h.extra, extraIdx: h.extraIdx})
		for _, c := range n.children {
			if subtree(c) {
				walk(c, depth+1)
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
	// A label longer than the row is cut with an ellipsis, as every list of
	// the family does, instead of being chopped by the frame.
	label, idx := fitLabel(r.n.label, r.idx, width-2-rightMargin-lipgloss.Width("  "+indent+prPrefixPlain(r.n)))
	if selected {
		// Plain text inside the highlight, except the filter's matches, which
		// stay marked where the cursor is. Every piece carries the background
		// itself: nested ANSI on a background renders inconsistently across
		// terminals. Constant 2-col gutter keeps content aligned whether or not
		// the row is selected.
		if !r.match {
			idx = nil
		}
		left := "▌ " + indent + prPrefixPlain(r.n)
		leftW := lipgloss.Width(left + label)
		right := rightColumn(rightSegs(r.n), width-2-rightMargin-leftW, false)
		// Pad to the full row width so the highlight spans the line, not just
		// the text (the gutter takes 2 columns).
		if pad := width - 2 - leftW - lipgloss.Width(right); pad > 0 {
			right += strings.Repeat(" ", pad)
		}
		return dot + stSel.Render(left) + highlight(label, idx, stSel) + stSel.Render(right)
	}
	name := label
	if r.match {
		name = highlight(label, idx, lipgloss.NewStyle())
	}
	left := "  " + indent + prPrefix(r.n) + name
	return dot + left + rightColumn(rightSegs(r.n), width-2-rightMargin-lipgloss.Width(left), true)
}

// extraLine is the line under a row whose prose explains the match: the
// description or the PR title, dim, starting under the label, with the
// matches marked. It belongs to the row, so it takes the selection highlight
// with it.
func extraLine(r rowItem, selected bool, width int) string {
	pad := "  " + strings.Repeat("  ", r.depth) + strings.Repeat(" ", lipgloss.Width(prPrefixPlain(r.n)))
	desc, idx := fitLabel(r.extra, r.extraIdx, width-2-rightMargin-lipgloss.Width(pad))
	if selected {
		line := pad + desc
		fill := ""
		if n := width - 2 - lipgloss.Width(line); n > 0 {
			fill = strings.Repeat(" ", n)
		}
		return "  " + stSel.Render(pad) + highlight(desc, idx, stSel) + stSel.Render(fill)
	}
	return "  " + pad + highlight(desc, idx, stDim)
}

// fitLabel cuts label to room cells and keeps the match offsets (bytes into
// the label) that are still in what is left.
func fitLabel(label string, idx []int, room int) (string, []int) {
	if room < 1 || lipgloss.Width(label) <= room {
		return label, idx
	}
	cut := truncate(label, room)
	kept := len(strings.TrimSuffix(cut, "…"))
	var in []int
	for _, i := range idx {
		if i < kept {
			in = append(in, i)
		}
	}
	return cut, in
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

func (m *model) renderContent() {
	var b strings.Builder
	for i, r := range m.rows {
		b.WriteString(rowLine(r, i == m.cursor, m.vp.Width()))
		if r.extra != "" {
			b.WriteString("\n" + extraLine(r, i == m.cursor, m.vp.Width()))
		}
		if i < len(m.rows)-1 {
			b.WriteString("\n")
		}
	}
	m.vp.SetContent(b.String())
	m.ensureVisible()
}

// lineOf is the list line row i starts on: rows showing their description
// take two lines.
func (m *model) lineOf(i int) int {
	line := 0
	for _, r := range m.rows[:i] {
		line += r.lines()
	}
	return line
}

// rowAt is the row on list line line, false past the last row.
func (m *model) rowAt(line int) (int, bool) {
	for i, r := range m.rows {
		if line < r.lines() {
			return i, true
		}
		line -= r.lines()
	}
	return 0, false
}

func (m *model) ensureVisible() {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		m.vp.SetYOffset(0)
		return
	}
	top := m.lineOf(m.cursor)
	m.vp.SetYOffset(scrollTo(m.vp.YOffset(), m.vp.Height(), m.lineOf(len(m.rows)), top+m.rows[m.cursor].lines()-1, top))
}

func (m model) Init() tea.Cmd {
	return tea.Batch(append([]tea.Cmd{textinput.Blink}, m.initCmds...)...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.SetWidth(m.innerW())
		m.help.SetWidth(max(0, msg.Width-4))
		sizeInput(&m.ti, m.width-4)
		m.fitBody()
		return m, nil

	case flashMsg:
		return m, m.flash.set(string(msg))

	case flashErrMsg:
		return m, m.flash.fail(string(msg))

	case clearFlashMsg:
		m.flash.clear(msg)
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

	case prWatchMsg:
		return m, watchPRsCmd(msg.mtime)

	case sharedPRsMsg:
		annotatePRs(m.prNodes, msg.prs)
		layoutPrefixes(m.roots)
		// PR numbers are searchable, so a fresh annotation can change the
		// results of the query being typed; re-filter, keeping the cursor on
		// the same node.
		m.refreshMetas()
		var cur *node
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			cur = m.rows[m.cursor].n
		}
		m.applyFilter()
		m.keepCursorOn(cur)
		m.renderContent()
		return m, watchPRsCmd(msg.mtime)

	case tea.MouseWheelMsg:
		// There is no preview to scroll: the wheel walks the cursor.
		if m.panel.open {
			return m, nil
		}
		if k, ok := wheelKey(msg); ok {
			if to := m.keys.Nav.move(k, m.cursor, len(m.rows), m.vp.Height(), nil); to != m.cursor {
				m.cursor = to
				m.renderContent()
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		// A left click on a row moves the cursor; it never selects, so a stray
		// click cannot switch spaces (same reasoning as asgotopr).
		if m.panel.open || msg.Button != tea.MouseLeft || !inList(msg.X, msg.Y, listY, m.innerW(), m.vp.Height()) {
			return m, nil
		}
		line, ok := rowUnder(msg.Y, listY, m.vp.YOffset(), m.lineOf(len(m.rows)))
		if i, in := m.rowAt(line); ok && in && i != m.cursor {
			m.cursor = i
			m.renderContent()
		}
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case m.panel.open:
			// The panel takes every key: esc closes it before it quits.
			if a := m.panel.update(msg, m.options()); a.id != "" {
				return m, m.setOption(a.id, a.value)
			}
			return m, nil
		case isHelpKey(msg):
			m.panel.toggle()
			return m, nil
		case msg.String() == "q" && m.ti.Value() == "":
			// q quits only while the filter is empty; otherwise it is text.
			return m, tea.Quit
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Select):
			if m.cursor < 0 || m.cursor >= len(m.rows) {
				return m, nil // nothing under the cursor: the popup stays, as in every tool
			}
			n := m.rows[m.cursor].n
			if n.kind == "pane" {
				m.focusPane = n.paneID
			} else {
				m.action = []string{"workspace", "focus", n.wsID}
			}
			return m, tea.Quit
		case m.keys.Nav.matches(msg):
			if to := m.keys.Nav.move(msg, m.cursor, len(m.rows), m.vp.Height(), nil); to != m.cursor {
				m.cursor = to
				m.renderContent()
			}
			return m, nil
		case key.Matches(msg, m.keys.Toggle):
			return m, m.setOption("panes", nextValue(m.options(), "panes"))
		case key.Matches(msg, m.keys.Copy):
			path := ""
			if m.cursor >= 0 && m.cursor < len(m.rows) {
				path = nodeDir(m.rows[m.cursor].n)
			}
			return m, copyCmd("asgoto", homeRel(path), path)
		}

		return m.toInput(msg)

	case tea.PasteMsg:
		if m.panel.open {
			return m, nil // nothing is typed under the panel
		}
		return m.toInput(msg)

	default:
		// Whatever else the input takes (its own paste, the cursor's blink).
		return m.toInput(msg)
	}
}

// toInput hands a message to the filter input and, when that changed the
// query, filters again: a key, a paste from the terminal (tea.PasteMsg) or the
// input's own ctrl+v all come through here, so the tree never lags behind
// what the input shows. A message that leaves the query alone moves nothing:
// the cursor stays on the row it was on.
func (m model) toInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cur *node
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		cur = m.rows[m.cursor].n
	}
	cmd, changed := typeInto(&m.ti, msg)
	if !changed {
		return m, cmd
	}
	m.applyFilter()
	if hasTerms(m.ti.Value()) {
		m.selectBestMatch()
	} else {
		// Clearing the query rebuilt the rows; stay on the same node instead
		// of whatever now sits at the old cursor index.
		m.keepCursorOn(cur)
	}
	m.renderContent()
	return m, cmd
}

// View declares the screen: alt screen and cell-motion mouse reports.
func (m model) View() tea.View { return popupView(m.render(), true) }

// The screen is the family's frame (frame.go): the filter input under a top
// border that carries the active order, the tree, and the help line. There is
// no context line: nothing here needs one. asgoto has no preview either, so
// the tree takes the whole main section (no splitMain) and the edge under it
// carries the matches/total counter.

// innerW is the width inside the frame's sides.
func (m model) innerW() int { return max(20, m.width-2) }

// render stacks the four sections in one frame; the tests assert on it.
func (m model) render() string {
	w, side := m.width, stDim.Render("│")
	out := append(frameHead(w, withDevMark(m.status()), m.ti.View()), hline(w, "├", "┤", "", ""))
	lines := strings.Split(m.vp.View(), "\n")
	if len(m.rows) == 0 {
		// Nothing to list: say why, as every tool of the family does.
		lines = []string{emptyList("", m.ti.Value(), "No spaces", m.innerW())}
	}
	for i := 0; i < m.vp.Height(); i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, side+fit(l, m.innerW())+side)
	}
	out = append(out, hline(w, "├", "┤", "", m.counter()), framed(w, footLine(m.flash, "", "", m.help, m.keys, w-4)), hline(w, "╰", "╯", "", ""))
	if m.panel.open {
		keys := keyLines(m.help, m.keys, w-10)
		out = overlay(out, panelLines(m.options(), m.panel.cursor, keys, w-4, len(out)-2), w)
	}
	return strings.Join(out, "\n")
}

// options is what the panel offers. The order is chosen there and nowhere
// else; the panes keep ctrl+a, the family's "list more" key.
func (m *model) options() []option {
	cur := func(on bool) int {
		if on {
			return 1
		}
		return 0
	}
	return []option{
		{id: "order", label: "Order", values: []string{"spaces", "priority"}, cur: cur(m.prioritySort)},
		{id: "panes", label: "Panes", values: []string{"hidden", "shown"}, cur: cur(m.showPanes), key: "^a"},
	}
}

// setOption changes a setting and remembers it, with the cursor kept on its
// node. The keys and the panel both come through here.
func (m *model) setOption(id string, v int) tea.Cmd {
	var cur *node
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		cur = m.rows[m.cursor].n
	}
	switch id {
	case "order":
		m.prioritySort = v == 1
		m.resort()
	case "panes":
		m.showPanes = v == 1
	default:
		return nil
	}
	m.applyFilter()
	m.keepCursorOn(cur)
	m.renderContent()
	return saveStateCmd(m.persisted())
}

// fitBody gives the tree the lines the frame and the help line leave.
func (m *model) fitBody() {
	m.vp.SetHeight(max(1, m.height-frameRows-1))
	m.renderContent()
}

// counter is, for the edge under the list, the rows listed out of every row
// of the current mode. With a query it counts the matches, as the rest of the
// family and -dump -query do: the parents kept for context are not results.
func (m model) counter() string {
	shown := 0
	for _, r := range m.rows {
		if r.match {
			shown++
		}
	}
	if shown == 0 {
		shown = len(m.rows) // no query: nothing is a match, every row counts
	}
	total := 0
	for _, n := range m.allNodes {
		// Same rule as applyFilter: process rows are always listed, plain
		// panes only when toggled on.
		if m.showPanes || n.kind != "pane" || n.proc != "" {
			total++
		}
	}
	return stCount.Render(strconv.Itoa(shown) + "/" + strconv.Itoa(total))
}

// status is the active order, for the edge over the input; it is dropped on a
// popup too narrow for it.
func (m model) status() string {
	if m.width < 2*sortLabelW+16 {
		return ""
	}
	return sortLabel(m.prioritySort)
}

// version is the release tag; overridden at build time via
// -ldflags "-X main.version=vX.Y.Z" (see scripts/release.sh and CI).
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the embedded version")
	dumpFlag := flag.Bool("dump", false, "print the tree (no TUI)")
	query := flag.String("query", "", "with -dump: print the rows the filter lists for this text, and their scores")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: asgoto [flags]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	dump := *dumpFlag

	var ws wsResp
	var pn paneResp
	if err := loadJSON([]string{"workspace", "list"}, &ws); err != nil {
		fatal("asgoto", "herdr workspace list: "+err.Error())
	}
	if err := loadJSON([]string{"pane", "list"}, &pn); err != nil {
		fatal("asgoto", "herdr pane list: "+err.Error())
	}
	// Only feeds the priority order's tiebreaker, so a failure (older herdr)
	// degrades to ordering by status alone.
	var ag agentResp
	_ = loadJSON([]string{"agent", "list"}, &ag)
	seqs := make(map[string]uint64, len(ag.Result.Agents))
	for _, a := range ag.Result.Agents {
		seqs[a.PaneID] = a.Seq
	}
	loadHerdrConfig()
	state := loadState()
	roots := buildTree(ws.Result.Workspaces, pn.Result.Panes, seqs)
	sortTree(roots, state.PrioritySort)
	allNodes, labels, branches := flatten(roots)

	// PRs come from the shared cache asmeta writes (instant). When some
	// branch is missing from it or stale, asmeta is asked for a refresh; the
	// open popup watches the file and repaints when it changes.
	pnodes := prNodes(allNodes)
	cache := loadPRCache()
	prMtime := sharedPRsMtime()
	prs := loadSharedPRs()
	annotatePRs(pnodes, prs)
	layoutPrefixes(roots)
	var initCmds []tea.Cmd
	if !dump {
		initCmds = append(initCmds, watchPRsCmd(prMtime))
		if prsStale(pnodes, prs, time.Now()) {
			initCmds = append(initCmds, refreshPRsCmd())
		}
	}

	paneIDs := make([]string, 0, len(pn.Result.Panes))
	for _, p := range pn.Result.Panes {
		paneIDs = append(paneIDs, p.ID)
	}
	// Process rows are resolved before the first paint (cheap, and they add
	// rows); their ports arrive async (lsof is the slow part) and only fill the
	// right column.
	annotateProcs(roots, fetchProcInfos(paneIDs))
	allNodes, labels, branches = flatten(roots)
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

	ti := newFilterInput("asgoto", "Search repos, worktrees and panes…")

	m := model{
		roots:        roots,
		allNodes:     allNodes,
		labels:       labels,
		branches:     branches,
		prNodes:      pnodes,
		cache:        cache,
		initCmds:     initCmds,
		showPanes:    state.ShowPanes,
		prioritySort: state.PrioritySort,
		ti:           ti,
		width:        82, // until the first WindowSizeMsg: the frame around an 80x20 tree
		height:       26,
		vp:           viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		help:         help.New(),
		keys:         defaultKeys(),
	}
	m.resort()
	if dump {
		runDump(os.Stdout, &m, *query)
		return
	}
	m.applyFilter()
	m.keepCursorOn(currentWorkspaceNode(ws.Result.Workspaces, allNodes))
	m.renderContent()

	// The alt screen is declared per frame by View().
	res, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "asgoto:", err)
		os.Exit(1)
	}
	final := res.(model)
	if err := runAction(final.focusPane, final.action); err != nil {
		fmt.Fprintln(os.Stderr, "asgoto:", err)
		os.Exit(1)
	}
}

// runAction does what enter chose, once the TUI is gone: quitting is what
// closes the popup, and the focus has to move after that.
func runAction(pane string, action []string) error {
	if pane != "" && focusPane(pane) != nil {
		// Standalone run (no socket env) or an older server: the CLI's
		// agent focus still lands on agent panes.
		if err := herdrDo("agent", "focus", pane); err != nil {
			return fmt.Errorf("focus pane %s: %w", pane, err)
		}
	}
	if action != nil {
		if err := herdrDo(action...); err != nil {
			return fmt.Errorf("herdr %s: %w", strings.Join(action, " "), err)
		}
	}
	return nil
}

// runDump prints the tree the popup is built from, without a TTY: every node,
// the panes the popup hides included. With a query it prints instead the rows
// the filter lists for it, in the popup's current mode (see queryDump).
func runDump(w io.Writer, m *model, query string) {
	if query != "" {
		queryDump(w, m, query)
		return
	}
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		fmt.Fprintln(w, dumpLine(n, depth))
		for _, c := range n.children {
			walk(c, depth+1)
		}
	}
	for _, r := range m.roots {
		walk(r, 0)
	}
}

// queryDump runs the popup's own filter (applyFilter, then the cursor rule of
// selectBestMatch) and prints the rows it lists: ">" marks the match the
// cursor lands on, "*" the other matches, each with its score; the rows
// without a mark are the parents kept for context. Whether plain panes are
// listed follows the persisted ctrl+a state, as in the popup.
func queryDump(w io.Writer, m *model, query string) {
	m.ti.SetValue(query)
	m.applyFilter()
	m.selectBestMatch()
	matches := 0
	for _, r := range m.rows {
		if r.match {
			matches++
		}
	}
	panes := "plain panes hidden"
	if m.showPanes {
		panes = "plain panes listed"
	}
	fmt.Fprintf(w, "query %q: %d listed, %d matched (%s, %s)\n", query, len(m.rows), matches, ansi.Strip(sortLabel(m.prioritySort)), panes)
	for i, r := range m.rows {
		mark, score := " ", "-"
		if r.match {
			mark, score = "*", strconv.Itoa(r.score)
			if i == m.cursor {
				mark = ">"
			}
		}
		fmt.Fprintf(w, "%s %5s  %s\n", mark, score, dumpLine(r.n, r.depth))
		if r.extra != "" {
			fmt.Fprintf(w, "%s  %s  %s\n", strings.Repeat(" ", 7), strings.Repeat("  ", r.depth+1), r.extra)
		}
	}
}

// dumpLine is one node as text: what the row shows to the left of its label
// (ticket, PR), the label, then in parentheses the kind, the aggregated
// status, the id `enter` focuses and what the right column shows (branch,
// stale folder, ahead/behind, counters); process rows end with their ports.
func dumpLine(n *node, depth int) string {
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
	extra := ""
	if n.branch != "" {
		extra = " " + n.branch
	}
	if n.folder != "" && n.folder != n.label {
		extra += " folder=" + n.folder
	}
	if n.desc != "" {
		extra += fmt.Sprintf(" desc=%q", n.desc)
	}
	if d := deltaText(n); d != "" {
		extra += " " + d
	}
	if c := countsText(n); c != "" {
		extra += " " + c
	}
	proc := ""
	if n.proc != "" {
		proc = "\t[proc " + portsText(n) + "]"
	}
	return fmt.Sprintf("%s%s%s\t(%s %s %s%s)%s", strings.Repeat("  ", depth), prefix, n.label, n.kind, n.status, id, extra, proc)
}

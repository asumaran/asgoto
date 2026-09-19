package main

import (
	"sort"
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTicketFrom(t *testing.T) {
	cases := []struct {
		branch, label, want string
	}{
		{"feat-FED-2030-stargate-oci-pipeline", "", "FED-2030"},
		{"FED-2035-stargate-stg-oci-validation", "", "FED-2035"},
		{"fed-2031", "", "FED-2031"},
		{"cronus/plat-1193-e2e-encryption-proof", "", "PLAT-1193"},
		{"", "fed-2031", "FED-2031"},                    // label fallback
		{"feat-observability-wiring", "some-label", ""}, // no ticket anywhere
		{"feat-digital-lhciFixClosedServerConnection", "", ""},
		{"e2e-tests", "", ""},      // digit inside the key: not a ticket
		{"v1-2-migration", "", ""}, // single letter before the dash: not a ticket
		{"main", "", ""},
		{"feat-FED-2030-FED-2031-x", "", "FED-2030"}, // first match wins
	}
	for _, c := range cases {
		if got := ticketFrom(c.branch, c.label); got != c.want {
			t.Errorf("ticketFrom(%q, %q) = %q, want %q", c.branch, c.label, got, c.want)
		}
	}
}

func TestGithubSlugFromURL(t *testing.T) {
	cases := []struct{ url, want string }{
		{"git@github.com:masmovil/monorepo-front.git", "masmovil/monorepo-front"},
		{"git@github.com:asumaran/herdr-goto", "asumaran/herdr-goto"},
		{"ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"https://github.com/owner/repo/", "owner/repo"},
		{"git@gitlab.com:owner/repo.git", ""}, // non-GitHub host
		{"https://github.com/owner", ""},      // no repo segment
		{"", ""},
	}
	for _, c := range cases {
		if got := githubSlugFromURL(c.url); got != c.want {
			t.Errorf("githubSlugFromURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestOriginURL(t *testing.T) {
	config := `[core]
	repositoryformatversion = 0
[remote "upstream"]
	url = git@github.com:other/upstream.git
[remote "origin"]
	url = git@github.com:owner/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
`
	if got := originURL(config); got != "git@github.com:owner/repo.git" {
		t.Errorf("originURL = %q, want origin url", got)
	}
	if got := originURL("[core]\n\tbare = false\n"); got != "" {
		t.Errorf("originURL without origin = %q, want empty", got)
	}
}

func TestLayoutPrefixesAndPlainPrefix(t *testing.T) {
	repo := &node{kind: "repo", label: "monorepo-front", children: []*node{
		{kind: "worktree", label: "feat-stargate", ticket: "FED-2030", pr: &prRef{Number: 1274, State: "open"}},
		{kind: "worktree", label: "stg-validation", ticket: "FED-2035"},
		{kind: "worktree", label: "webvitals-faro", pr: &prRef{Number: 6449, State: "draft"}},
		{kind: "worktree", label: "observability-wiring"},
	}}
	layoutPrefixes([]*node{repo})

	want := []string{
		"FED-2030 #1274  ", // ticket + PR
		"FED-2035        ", // ticket, PR column padded
		"         #6449  ", // PR without ticket, ticket column padded
		"",                 // neither: no prefix at all
	}
	for i, c := range repo.children {
		if got := prPrefixPlain(c); got != want[i] {
			t.Errorf("child %d (%s): prefix %q, want %q", i, c.label, got, want[i])
		}
	}
	if got := prPrefixPlain(repo); got != "" {
		t.Errorf("repo without ticket/PR: prefix %q, want empty", got)
	}
}

// TestSearchByTicketAndPRNumber covers that digits are plain search text and
// that the ticket / PR-number corpus is matched: typing a PR number or a
// ticket key finds the row that displays it, even when neither appears in the
// label or branch.
func TestSearchByTicketAndPRNumber(t *testing.T) {
	byPR := &node{kind: "worktree", label: "webvitals-faro", pr: &prRef{Number: 6449, State: "open"}}
	byTicket := &node{kind: "worktree", label: "stg-validation", ticket: "FED-2035"}
	repo := &node{kind: "repo", label: "monorepo-front", expanded: true, children: []*node{byPR, byTicket}}
	roots := []*node{repo}

	m := model{roots: roots, ti: textinput.New()}
	m.allNodes, m.lowerLabels, m.lowerBranches = flatten(roots)
	m.refreshMetas()

	matched := func(query string) map[*node]bool {
		m.ti.SetValue(query)
		m.applyFilter()
		out := map[*node]bool{}
		for _, r := range m.rows {
			if r.match {
				out[r.n] = true
			}
		}
		return out
	}

	if got := matched("6449"); !got[byPR] || got[byTicket] {
		t.Errorf("query 6449: matched %v, want only the PR #6449 node", got)
	}
	if got := matched("2035"); !got[byTicket] {
		t.Errorf("query 2035: matched %v, want the FED-2035 node", got)
	}
}

// TestSearchByWorktreeFolder covers that a worktree labelled by its branch is
// still found by the folder it was created as (the name herdr shows in the
// sidebar and the one the user typed to create it).
func TestSearchByWorktreeFolder(t *testing.T) {
	moved := &node{kind: "worktree", label: "as-foo-bar-test", branch: "as-foo-bar-test", folder: "foo"}
	other := &node{kind: "worktree", label: "feat/x", branch: "feat/x", folder: "feat-x"}
	repo := &node{kind: "repo", label: "monorepo-front", branch: "master", expanded: true, children: []*node{moved, other}}
	roots := []*node{repo}

	m := model{roots: roots, ti: textinput.New()}
	m.allNodes, m.lowerLabels, m.lowerBranches = flatten(roots)
	m.refreshMetas()
	m.ti.SetValue("foo")
	m.applyFilter()
	got := map[*node]bool{}
	for _, r := range m.rows {
		if r.match {
			got[r.n] = true
		}
	}
	if !got[moved] || got[other] {
		t.Errorf("query foo: matched %v, want only the worktree whose folder is foo", got)
	}
}

func TestGhPRRef(t *testing.T) {
	cases := []struct {
		in   ghPR
		want prRef
	}{
		{ghPR{Number: 1, State: "OPEN"}, prRef{Number: 1, State: "open"}},
		{ghPR{Number: 2, State: "OPEN", IsDraft: true}, prRef{Number: 2, State: "draft"}},
		{ghPR{Number: 3, State: "MERGED"}, prRef{Number: 3, State: "merged"}},
		{ghPR{Number: 4, State: "CLOSED"}, prRef{Number: 4, State: "closed"}},
		{ghPR{Number: 5, State: "OPEN", Title: "FED-2040: wire observability"}, prRef{Number: 5, State: "open", Ticket: "FED-2040"}},
	}
	for _, c := range cases {
		if got := c.in.ref(); got != c.want {
			t.Errorf("ref(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestShortCmd(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"sleep", "600"}, "sleep 600"},
		{[]string{"/Users/me/.asdf/installs/nodejs/24/bin/node", "-e", "x()"}, "node -e x()"},
		{[]string{"/usr/local/bin/node", "/Users/me/.local/share/pnpm/pnpm", "nx", "dev", "app"}, "pnpm nx dev app"},
		{[]string{"python3", "manage.py", "runserver"}, "manage.py runserver"},
		{[]string{"claude", "--resume", "abc"}, "claude --resume abc"},
	}
	for _, c := range cases {
		if got := shortCmd(c.argv); got != c.want {
			t.Errorf("shortCmd(%v) = %q, want %q", c.argv, got, c.want)
		}
	}
}

func TestProcLabel(t *testing.T) {
	mk := func(shell, group int, procs ...struct {
		pid   int
		argv0 string
		argv  []string
	}) procInfoResp {
		var r procInfoResp
		r.Result.ProcessInfo.ShellPID = shell
		r.Result.ProcessInfo.GroupID = group
		for _, p := range procs {
			r.Result.ProcessInfo.Processes = append(r.Result.ProcessInfo.Processes, struct {
				PID   int      `json:"pid"`
				Name  string   `json:"name"`
				Argv0 string   `json:"argv0"`
				Argv  []string `json:"argv"`
			}{PID: p.pid, Name: "node", Argv0: p.argv0, Argv: p.argv})
		}
		return r
	}
	type proc = struct {
		pid   int
		argv0 string
		argv  []string
	}
	// Shell at its prompt: the group leader is the shell itself.
	if got := procLabel(mk(10, 10, proc{10, "zsh", []string{"-zsh"}})); got != "" {
		t.Errorf("prompt: got %q, want empty", got)
	}
	// Foreground job: the leader is the pane's command, its children are noise.
	r := mk(10, 20, proc{25, "caffeinate", []string{"caffeinate", "-i"}}, proc{20, "sleep", []string{"sleep", "600"}})
	if got := procLabel(r); got != "sleep 600" {
		t.Errorf("job: got %q, want %q", got, "sleep 600")
	}
	// argv unreadable: argv0 beats the bare executable name.
	r = mk(10, 30, proc{30, "npm exec foo@latest", nil})
	if got := procLabel(r); got != "npm exec foo@latest" {
		t.Errorf("no argv: got %q, want argv0", got)
	}
}

func TestAnnotateProcs(t *testing.T) {
	repo := &node{kind: "repo", children: []*node{
		{kind: "pane", label: "shell  app", paneID: "w1:p1"},
		{kind: "pane", label: "claude  [idle]  app", paneID: "w1:p2", hasAgent: true},
		{kind: "pane", label: "shell  app", paneID: "w1:p3"},
	}}
	annotateProcs([]*node{repo}, map[string]procEntry{
		"w1:p1": {label: "pnpm dev", pids: []int{100, 101}},
		"w1:p2": {label: "claude"},
		"w1:p3": {},
	})
	if c := repo.children[0]; c.proc != "pnpm dev" || c.label != "pnpm dev" || !equalInts(c.pids, []int{100, 101}) {
		t.Errorf("process pane: proc %q label %q pids %v", c.proc, c.label, c.pids)
	}
	procs := procRows(repo.children)
	if len(procs) != 1 || procs[0] != repo.children[0] {
		t.Errorf("procRows: %v", procs)
	}
	annotatePorts(procs, map[string][]int{"w1:p1": {4200}})
	if got := portsText(repo.children[0]); got != ":4200" {
		t.Errorf("ports: got %q, want :4200", got)
	}
	if c := repo.children[1]; c.proc != "" || c.label != "claude  [idle]  app" {
		t.Errorf("agent pane: proc %q label %q, want untouched", c.proc, c.label)
	}
	if c := repo.children[2]; c.proc != "" || c.label != "shell  app" {
		t.Errorf("prompt pane: proc %q label %q, want untouched", c.proc, c.label)
	}
}

func TestParseLsof(t *testing.T) {
	out := "p963\nf13\nn*:63904\nf15\nn*:63904\np1081\nf9\nn127.0.0.1:7000\nf11\nn[::1]:5000\n"
	got := parseLsof(out)
	if want := []int{63904}; !equalInts(got[963], want) {
		t.Errorf("pid 963: %v, want %v (deduped)", got[963], want)
	}
	if want := []int{7000, 5000}; !equalInts(got[1081], want) {
		t.Errorf("pid 1081: %v, want %v", got[1081], want)
	}
}

func TestDescendants(t *testing.T) {
	// 10 -> 11 -> 12 (detached group), 20 unrelated, 13 also under 10.
	parents := map[int]int{11: 10, 12: 11, 13: 10, 20: 1, 10: 1}
	got := descendants([]int{10, 11}, parents)
	sort.Ints(got)
	if want := []int{10, 11, 12, 13}; !equalInts(got, want) {
		t.Errorf("descendants: %v, want %v", got, want)
	}
	if got := parsePS(" 10     1\n 11    10\nbad\n"); got[11] != 10 || len(got) != 2 {
		t.Errorf("parsePS: %v", got)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRightText(t *testing.T) {
	cases := []struct {
		n    *node
		want string
	}{
		{&node{kind: "worktree", label: "feat/stargate", branch: "feat/stargate", folder: "stg-validation"}, "stg-validation"},
		{&node{kind: "worktree", label: "feat/x", branch: "feat/x", folder: "feat-x"}, ""}, // only slugging differs
		{&node{kind: "worktree", label: "feat-x", branch: "feat-x", folder: "feat-x"}, ""},
		{&node{kind: "worktree", label: "feat-x", folder: "feat-x"}, ""}, // detached / unknown branch: folder is the label
		{&node{kind: "repo", label: "app", branch: "main"}, "main"},
		{&node{kind: "repo", label: "app", branch: "feat/hotfix"}, "feat/hotfix"},
		{&node{kind: "repo", label: "app"}, ""},
		{&node{kind: "repo", label: "app", branch: "main", ahead: 2}, "main ↑2"},
		{&node{kind: "repo", label: "app", branch: "main", ahead: 1, behind: 3}, "main ↑1↓3"},
		{&node{kind: "repo", label: "app", branch: "main", unstaged: 1}, "main !1"},
		{&node{kind: "repo", label: "app", branch: "main", ahead: 2, staged: 1, unstaged: 2, untrack: 3}, "main ↑2 +1 !2 ?3"},
		{&node{kind: "worktree", label: "feat/x", branch: "feat/x", folder: "feat-x", behind: 1}, "↓1"},
		{&node{kind: "worktree", label: "feat/x", branch: "feat/x", folder: "feat-x", untrack: 2}, "?2"},
		{&node{kind: "worktree", label: "as-foo", branch: "as-foo", folder: "foo", ahead: 4, staged: 1}, "foo ↑4 +1"},
		{&node{kind: "pane", ports: []int{3000, 3001}}, ":3000 :3001"},
		{&node{kind: "pane", ahead: 2, unstaged: 1}, ""}, // panes never carry git hints
	}
	for _, c := range cases {
		layoutHints([]*node{c.n}) // the delta column only exists once sized
		if got := rightText(c.n); got != c.want {
			t.Errorf("%s %q/%q/%q: got %q, want %q", c.n.kind, c.n.label, c.n.branch, c.n.folder, got, c.want)
		}
	}
}

// TestLayoutHintsAlignsNames covers that both hint columns are shared (rows
// without a delta pad the delta column, rows with fewer/no counts pad the
// counts column, pane rows included), so the names end on the same column
// whatever each row's hints are.
func TestLayoutHintsAlignsNames(t *testing.T) {
	a := &node{kind: "repo", label: "a", branch: "main", ahead: 12, behind: 3}
	b := &node{kind: "repo", label: "b", branch: "main", staged: 1, unstaged: 2}
	c := &node{kind: "worktree", label: "feat/x", branch: "feat/x", folder: "x-old"}
	p := &node{kind: "pane", ports: []int{3000}}
	nodes := []*node{a, b, c, p}
	layoutHints(nodes)
	if a.deltaW != 5 || b.deltaW != 5 || c.deltaW != 5 || p.deltaW != 5 {
		t.Fatalf("deltaW: a=%d b=%d c=%d pane=%d, want 5 everywhere", a.deltaW, b.deltaW, c.deltaW, p.deltaW)
	}
	if a.stagedW != 2 || a.unstagW != 2 || a.untrkW != 0 {
		t.Fatalf("count widths: staged=%d unstaged=%d untracked=%d, want 2/2/0", a.stagedW, a.unstagW, a.untrkW)
	}
	join := func(n *node) string {
		segs := rightSegs(n)
		parts := make([]string, len(segs))
		for i, s := range segs {
			parts[i] = s.text
		}
		return strings.Join(parts, " ")
	}
	if got := join(a); got != "main ↑12↓3      " {
		t.Errorf("a: %q", got)
	}
	if got := join(b); got != "main       +1 !2" {
		t.Errorf("b: %q", got)
	}
	if got := join(c); got != "x-old            " {
		t.Errorf("c: %q", got)
	}
	if got := join(p); got != ":3000            " {
		t.Errorf("pane: %q, want the ports followed by the blank hint slots", got)
	}
}

// TestLayoutHintsAlignsCounters covers that each working-tree counter gets
// its own column: a row's "?1" pads to another row's "?10" so the symbols
// stack vertically, and a missing counter leaves its slot blank instead of
// shifting the following ones.
func TestLayoutHintsAlignsCounters(t *testing.T) {
	a := &node{kind: "repo", label: "a", branch: "main", staged: 1, unstaged: 4, untrack: 1}
	b := &node{kind: "repo", label: "b", branch: "main", untrack: 10}
	nodes := []*node{a, b}
	layoutHints(nodes)
	join := func(n *node) string {
		segs := rightSegs(n)
		parts := make([]string, len(segs))
		for i, s := range segs {
			parts[i] = s.text
		}
		return strings.Join(parts, " ")
	}
	if got := join(a); got != "main +1 !4 ?1 " {
		t.Errorf("a: %q", got)
	}
	if got := join(b); got != "main       ?10" {
		t.Errorf("b: %q", got)
	}
}

func TestRightColumn(t *testing.T) {
	one := func(s string) []seg { return []seg{{s, stPorts}} }
	if got := rightColumn(nil, 40, false); got != "" {
		t.Errorf("empty: got %q", got)
	}
	if got := rightColumn(one(":4200"), 20, false); got != "               :4200" {
		t.Errorf("right-align: got %q", got)
	}
	if got := rightColumn(one(":4200"), 5, false); got != "" {
		t.Errorf("no room: got %q, want empty", got)
	}
	if got := rightColumn(one(":4200 :4201 :4202 :4203"), 12, false); got != "  :4200 :42…" {
		t.Errorf("truncate: got %q", got)
	}
	two := []seg{{"↑2", stDelta}, {"main", stBranch}}
	if got := rightColumn(two, 12, false); got != "     ↑2 main" {
		t.Errorf("two segments: got %q", got)
	}
	if got := rightColumn(two, 8, false); got != "  ↑2 ma…" {
		t.Errorf("two segments truncated: got %q", got)
	}
}

func TestParseStatus(t *testing.T) {
	out := "## main...origin/main [ahead 1, behind 3]\n M unstaged.go\nM  staged.go\nMM both.go\n?? new.go\n"
	d, ok := parseStatus(out)
	if !ok || d.Ahead != 1 || d.Behind != 3 {
		t.Errorf("delta: got %+v ok=%v", d, ok)
	}
	if d.Staged != 2 || d.Unstaged != 2 || d.Untracked != 1 {
		t.Errorf("counts: got %+v, want staged=2 unstaged=2 untracked=1", d)
	}
	if d, ok := parseStatus("## main...origin/main\n"); !ok || d != (gitDelta{}) {
		t.Errorf("clean in-sync checkout: got %+v ok=%v", d, ok)
	}
	if _, ok := parseStatus(""); ok {
		t.Error("empty output should not parse")
	}
	if _, ok := parseStatus("not a status header\n"); ok {
		t.Error("output without the ## header should not parse")
	}
	n := &node{ahead: 2, behind: 1}
	if got := deltaText(n); got != "↑2↓1" {
		t.Errorf("deltaText: got %q", got)
	}
	if got := deltaText(&node{}); got != "" {
		t.Errorf("deltaText in sync: got %q", got)
	}
	if got := countsText(&node{staged: 1, unstaged: 2, untrack: 3}); got != "+1 !2 ?3" {
		t.Errorf("countsText: got %q", got)
	}
	if got := countsText(&node{}); got != "" {
		t.Errorf("countsText clean: got %q", got)
	}
}

// TestAnnotateDeltasByCheckout covers that hints are keyed by checkout path
// (the cache key), so a cached entry applies to whichever workspace holds
// that checkout, and checkouts absent from the map keep what they had.
func TestAnnotateDeltasByCheckout(t *testing.T) {
	a := &node{kind: "repo", wsID: "w1", checkout: "/r/a"}
	b := &node{kind: "worktree", wsID: "w2", checkout: "/r/b", ahead: 9}
	annotateDeltas([]*node{a, b}, map[string]gitDelta{"/r/a": {Ahead: 2, Unstaged: 1}})
	if a.ahead != 2 || a.unstaged != 1 {
		t.Errorf("a: ahead=%d unstaged=%d, want 2/1", a.ahead, a.unstaged)
	}
	if b.ahead != 9 {
		t.Errorf("b: ahead=%d, want the previous 9 kept", b.ahead)
	}
}

func TestCurrentWorkspaceNode(t *testing.T) {
	repo := &node{kind: "repo", wsID: "w1"}
	wt := &node{kind: "worktree", wsID: "w2"}
	pane := &node{kind: "pane", wsID: "w2", paneID: "w2:p1"}
	nodes := []*node{repo, wt, pane}
	wss := []wsInfo{{ID: "w1"}, {ID: "w2", Focused: true}}
	if got := currentWorkspaceNode(wss, nodes); got != wt {
		t.Errorf("focused w2: got %v, want the worktree row (not its pane)", got)
	}
	if got := currentWorkspaceNode([]wsInfo{{ID: "w1"}}, nodes); got != nil {
		t.Errorf("none focused: got %v, want nil", got)
	}
}

func TestPaneFocusRequest(t *testing.T) {
	got, err := paneFocusRequest("w45:pF")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"goto:pane.focus","method":"pane.focus","params":{"pane_id":"w45:pF"}}` + "\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSortTreePriority covers the ctrl+s order: every level sorts by its
// aggregated agent status (blocked > done > working > idle > none), the most
// recent state change breaks ties, a repo's own panes stay above its
// worktrees, and turning it off restores the built order.
func TestSortTreePriority(t *testing.T) {
	pane := func(label, status string, seq uint64) *node {
		return &node{kind: "pane", label: label, status: status, seq: seq, hasAgent: status != ""}
	}
	wt := func(label string, panes ...*node) *node {
		return &node{kind: "worktree", label: label, children: panes}
	}
	shell := pane("shell", "", 0)
	idleWt := wt("idle-wt", pane("claude", "idle", 9))
	blockedWt := wt("blocked-wt", pane("claude", "working", 50), pane("claude", "blocked", 3))
	quiet := &node{kind: "repo", label: "quiet", children: []*node{shell, idleWt, blockedWt}}
	oldDone := &node{kind: "repo", label: "old-done", children: []*node{pane("claude", "done", 5)}}
	newDone := &node{kind: "repo", label: "new-done", children: []*node{pane("claude", "done", 8)}}
	empty := &node{kind: "repo", label: "empty"}
	roots := []*node{empty, oldDone, quiet, newDone}
	for _, r := range roots {
		aggregateStatus(r)
	}
	stampOrder(roots)

	labels := func(nodes []*node) string {
		var out []string
		for _, n := range nodes {
			out = append(out, n.label)
		}
		return strings.Join(out, " ")
	}

	if quiet.status != "blocked" || quiet.seq != 3 {
		t.Errorf("aggregate: got %s seq %d, want blocked seq 3 (seq follows the winning status)", quiet.status, quiet.seq)
	}

	sortTree(roots, true)
	if got, want := labels(roots), "quiet new-done old-done empty"; got != want {
		t.Errorf("priority roots: got %q, want %q", got, want)
	}
	if got, want := labels(quiet.children), "shell blocked-wt idle-wt"; got != want {
		t.Errorf("priority children: got %q, want %q (own panes stay above worktrees)", got, want)
	}
	if got, want := blockedWt.children[0].status, "blocked"; got != want {
		t.Errorf("priority panes: first is %q, want %q", got, want)
	}

	sortTree(roots, false)
	if got, want := labels(roots), "empty old-done quiet new-done"; got != want {
		t.Errorf("default roots: got %q, want %q", got, want)
	}
	if got, want := labels(quiet.children), "shell idle-wt blocked-wt"; got != want {
		t.Errorf("default children: got %q, want %q", got, want)
	}
}

// TestResortKeepsCorporaParallel covers that toggling the order rebuilds the
// match corpora with the tree: a stale corpus would make a query hit the
// wrong row.
func TestResortKeepsCorporaParallel(t *testing.T) {
	idle := &node{kind: "repo", label: "alpha", status: "idle", ticket: "FED-1"}
	blocked := &node{kind: "repo", label: "beta", status: "blocked", ticket: "FED-2"}
	roots := []*node{idle, blocked}
	stampOrder(roots)

	m := model{roots: roots, ti: textinput.New(), keys: defaultKeys(), prioritySort: true}
	m.resort()
	if m.allNodes[0] != blocked {
		t.Fatalf("priority order: first node is %q, want beta", m.allNodes[0].label)
	}
	m.ti.SetValue("fed-1")
	m.applyFilter()
	for _, r := range m.rows {
		if r.match && r.n != idle {
			t.Errorf("query fed-1 matched %q, want alpha", r.n.label)
		}
	}
	if len(m.rows) != 1 || m.rows[0].n != idle {
		t.Errorf("query fed-1: rows %v, want only alpha", m.rows)
	}
}

// TestViewSortLabel covers the active-order label: set into the frame's top border, next to
// the counter, naming the mode, and dropped when the popup is too narrow for it.
func TestViewSortLabel(t *testing.T) {
	m := model{ti: textinput.New(), vp: viewport.New(viewport.WithWidth(58), viewport.WithHeight(5)), help: help.New(), keys: defaultKeys(), width: 60, height: 11}
	m.ti.Prompt = "goto > "
	m.ti.SetValue("herdr")

	// lipgloss v2 always emits ANSI, so the text is compared stripped.
	line := func(y int) string { return strings.Split(ansi.Strip(m.render()), "\n")[y] }

	if edge := line(0); !strings.HasSuffix(edge, " 0/0 sort: spaces ─╮") {
		t.Errorf("default: counter edge %q, want it to end in the label", edge)
	}
	if prompt := line(1); !strings.HasPrefix(prompt, "│ goto > herdr") || strings.Contains(prompt, "sort:") {
		t.Errorf("prompt line %q must hold the input only", prompt)
	}
	m.prioritySort = true
	if edge := line(0); !strings.Contains(edge, "sort: priority") {
		t.Errorf("priority: counter edge %q", edge)
	}
	m.width = 30
	m.vp.SetWidth(28)
	if edge := line(0); strings.Contains(edge, "sort:") || !strings.Contains(edge, "0/0") {
		t.Errorf("narrow: counter edge %q, want the count without the label", edge)
	}
}

// TestFrameGeometry pins the single-frame layout: exactly height lines, each
// exactly width cells, sections where the click math expects them.
func TestFrameGeometry(t *testing.T) {
	m := model{ti: textinput.New(), vp: viewport.New(viewport.WithWidth(98), viewport.WithHeight(5)), help: help.New(), keys: defaultKeys(), width: 100, height: 11}
	lines := strings.Split(m.render(), "\n")
	if len(lines) != m.height {
		t.Errorf("%d lines, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, m.width, ansi.Strip(l))
		}
	}
	plain := strings.Split(ansi.Strip(m.render()), "\n")
	if !strings.HasPrefix(plain[0], "╭") || !strings.HasPrefix(plain[len(plain)-1], "╰") ||
		!strings.HasPrefix(plain[mainY], "├") || !strings.HasPrefix(plain[listY], "│") {
		t.Errorf("frame sections misplaced:\n%s", strings.Join(plain, "\n"))
	}
	if help := plain[len(plain)-2]; !strings.Contains(help, "type filter") || !strings.Contains(help, "esc/q quit") {
		t.Errorf("help line = %q", help)
	}
}

// TestHerdrConfigString covers reading herdr's config.toml without a TOML
// parser: only the key under its own table (or dotted at the top level)
// counts.
func TestHerdrConfigString(t *testing.T) {
	cases := []struct{ name, config, want string }{
		{"empty", "", ""},
		{"under ui", "onboarding = false\n[ui]\npane_gaps = true\nstatus_indicators = \"symbols\"\n[theme]\nname = \"dracula\"\n", "symbols"},
		{"dotted top-level", "ui.status_indicators = 'symbols'\n[theme]\n", "symbols"},
		{"other table", "[theme]\nstatus_indicators = \"symbols\"\n", ""},
		{"subtable of ui", "[ui]\n[ui.toast]\nstatus_indicators = \"symbols\"\n", ""},
		{"commented out", "[ui]\n# status_indicators = \"symbols\"\n", ""},
		{"inline table key", "[ui]\ntab_bar_right = [\n  { status_indicators = \"symbols\" },\n]\n", ""},
	}
	for _, c := range cases {
		if got := herdrConfigString(c.config, "ui", "status_indicators"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestApplyHerdrConfig covers the two settings the gutter mirrors: the glyph
// style, and dracula's palette (any other theme keeps the ANSI colors).
func TestApplyHerdrConfig(t *testing.T) {
	prevSymbols := statusSymbols
	prev := []lipgloss.Style{stDotBlocked, stDotWorking, stDotDone, stDotIdle, stDotNone}
	defer func() {
		statusSymbols = prevSymbols
		stDotBlocked, stDotWorking, stDotDone, stDotIdle, stDotNone = prev[0], prev[1], prev[2], prev[3], prev[4]
	}()
	ansi := stDotBlocked.GetForeground()

	applyHerdrConfig("[ui]\nstatus_indicators = \"emoji\"\n[theme]\nname = \"nord\"\n")
	if statusSymbols || stDotBlocked.GetForeground() != ansi {
		t.Errorf("unknown style / other theme: symbols=%v fg=%v, want dots and ANSI", statusSymbols, stDotBlocked.GetForeground())
	}
	applyHerdrConfig("[ui]\nstatus_indicators = \"symbols\"\n[theme]\nname = \"Dracula\"\n")
	if want := lipgloss.Color("#ff5555"); !statusSymbols || stDotBlocked.GetForeground() != want {
		t.Errorf("symbols + dracula: symbols=%v fg=%v, want true and %v", statusSymbols, stDotBlocked.GetForeground(), want)
	}
}

func TestStatusDotStyles(t *testing.T) {
	defer func(prev bool) { statusSymbols = prev }(statusSymbols)
	for _, c := range []struct {
		symbols bool
		want    string // blocked working done idle none
	}{
		{false, "● ● ● ○ ·"},
		{true, "× ◐ ✓ ○ ·"},
	} {
		statusSymbols = c.symbols
		var got []string
		for _, s := range []string{"blocked", "working", "done", "idle", ""} {
			got = append(got, ansi.Strip(statusDot(s)))
		}
		if g := strings.Join(got, " "); g != c.want {
			t.Errorf("symbols=%v: got %q, want %q", c.symbols, g, c.want)
		}
	}
}

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

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
	want := `{"id":"asgoto:pane.focus","method":"pane.focus","params":{"pane_id":"w45:pF"}}` + "\n"
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

// TestQueryTermsFollowTheTree covers a query of several terms: they match in
// any order, and a term an ancestor matches counts for the rows under it.
func TestQueryTermsFollowTheTree(t *testing.T) {
	fix := &node{kind: "worktree", label: "fix-login"}
	docs := &node{kind: "worktree", label: "docs"}
	herdr := &node{kind: "repo", label: "herdr", children: []*node{fix, docs}, expanded: true}
	other := &node{kind: "repo", label: "shop", children: []*node{{kind: "worktree", label: "fix-cart"}}, expanded: true}
	roots := []*node{herdr, other}
	stampOrder(roots)
	m := model{roots: roots, ti: textinput.New(), keys: defaultKeys()}
	m.resort()

	for _, q := range []string{"herdr fix", "fix herdr"} {
		m.ti.SetValue(q)
		m.applyFilter()
		var matched []string
		for _, r := range m.rows {
			if r.match {
				matched = append(matched, r.n.label)
			}
		}
		if len(matched) != 1 || matched[0] != "fix-login" {
			t.Errorf("%q matched %v, want only fix-login (under herdr)", q, matched)
		}
		if len(m.rows) != 2 || m.rows[0].n != herdr || m.rows[1].n != fix {
			t.Errorf("%q: rows must be herdr and its fix-login worktree", q)
		}
	}
	m.ti.SetValue("'")
	m.applyFilter()
	if len(m.rows) != 5 {
		t.Errorf("a bare prefix is not a term yet: %d rows, want the whole tree (5)", len(m.rows))
	}
}

// TestViewSortLabel covers the active-order label: set into the frame's top
// border, naming the mode, and dropped when the popup is too narrow for it.
// The counter sits on the edge under the list.
func TestViewSortLabel(t *testing.T) {
	m := model{ti: textinput.New(), vp: viewport.New(viewport.WithWidth(58), viewport.WithHeight(5)), help: help.New(), keys: defaultKeys(), width: 60, height: 11}
	m.ti.Prompt = "asgoto > "
	m.ti.SetValue("herdr")

	// lipgloss v2 always emits ANSI, so the text is compared stripped.
	line := func(y int) string { return strings.Split(ansi.Strip(m.render()), "\n")[y] }

	if edge := line(0); !strings.HasSuffix(edge, "─ sort: spaces (dev) ─╮") { // tests run an unstamped build
		t.Errorf("default: top border %q, want it to end in the label", edge)
	}
	if edge := line(m.height - 3); !strings.HasSuffix(edge, "─ 0/0 ─┤") {
		t.Errorf("default: edge under the list %q, want the counter", edge)
	}
	if prompt := line(1); !strings.HasPrefix(prompt, "│ asgoto > herdr") || strings.Contains(prompt, "sort:") {
		t.Errorf("prompt line %q must hold the input only", prompt)
	}
	m.prioritySort = true
	if edge := line(0); !strings.Contains(edge, "sort: priority") {
		t.Errorf("priority: counter edge %q", edge)
	}
	m.width = 30
	m.vp.SetWidth(28)
	if edge := line(0); strings.Contains(edge, "sort:") {
		t.Errorf("narrow: top border %q, want it without the label", edge)
	}
	if edge := line(m.height - 3); !strings.Contains(edge, "0/0") {
		t.Errorf("narrow: edge under the list %q, want the counter", edge)
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
		!strings.HasPrefix(plain[mainY(false)], "├") || !strings.HasPrefix(plain[listY(false)], "│") {
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

// copyModel is a sized model over one repo (a checkout under home), its
// worktree, the worktree's agent pane and a space herdr knows no directory
// of, with the panes listed.
func copyModel(t *testing.T, home string) model {
	t.Helper()
	pane := &node{kind: "pane", label: "claude", paneID: "w2:p1", cwd: filepath.Join(home, "wt/shop/fix/src"), hasAgent: true}
	fix := &node{kind: "worktree", label: "fix", wsID: "w2", checkout: filepath.Join(home, "wt/shop/fix"), expanded: true, children: []*node{pane}}
	shop := &node{kind: "repo", label: "shop", wsID: "w1", checkout: filepath.Join(home, "Developer/shop"), expanded: true, children: []*node{fix}}
	bare := &node{kind: "repo", label: "scratch", wsID: "w3", expanded: true}
	roots := []*node{shop, bare}
	stampOrder(roots)
	m := model{roots: roots, showPanes: true, ti: textinput.New(), vp: viewport.New(viewport.WithWidth(98), viewport.WithHeight(7)),
		help: help.New(), keys: defaultKeys(), width: 100, height: 13}
	m.resort()
	m.applyFilter()
	m.renderContent()
	return m
}

// clipboardStub points ASGOTO_CLIPBOARD at a script that logs its stdin and
// returns the log's path.
func clipboardStub(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "clip")
	stub := filepath.Join(t.TempDir(), "clipboard")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat > "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASGOTO_CLIPBOARD", stub)
	return log
}

func footOf(m model) string {
	plain := strings.Split(ansi.Strip(m.View().Content), "\n")
	return plain[len(plain)-2]
}

// TestCopyKeyCopiesThePath covers ctrl+y on every kind of row: the clipboard
// gets the absolute directory, the help line confirms it with the home
// shortened, and the key never reaches the filter.
func TestCopyKeyCopiesThePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	log := clipboardStub(t)
	m := copyModel(t, home)

	cases := []struct{ kind, want, label string }{
		{"repo", filepath.Join(home, "Developer/shop"), "~/Developer/shop"},
		{"worktree", filepath.Join(home, "wt/shop/fix"), "~/wt/shop/fix"},
		{"pane", filepath.Join(home, "wt/shop/fix/src"), "~/wt/shop/fix/src"},
	}
	for i, c := range cases {
		m.cursor = i
		if got := m.rows[i].n.kind; got != c.kind {
			t.Fatalf("row %d is a %s, want a %s", i, got, c.kind)
		}
		res, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatalf("%s: ctrl+y returned no command", c.kind)
		}
		res, _ = res.(model).Update(cmd())
		m = res.(model)
		if got, _ := os.ReadFile(log); string(got) != c.want {
			t.Errorf("%s: the clipboard got %q, want %q", c.kind, got, c.want)
		}
		if help := footOf(m); !strings.Contains(help, "copied "+c.label) {
			t.Errorf("%s: help line = %q, want the confirmation with the home shortened", c.kind, help)
		}
		if m.ti.Value() != "" {
			t.Errorf("%s: ctrl+y leaked into the filter: %q", c.kind, m.ti.Value())
		}
	}
	res, _ := m.Update(clearFlashMsg(m.flash.seq))
	if help := footOf(res.(model)); !strings.Contains(help, "type filter") {
		t.Errorf("after the timer the help is back: %q", help)
	}
}

// TestCopyKeyWithoutADirectory covers a row herdr reported no directory for:
// nothing is copied and the help line says so.
func TestCopyKeyWithoutADirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	log := clipboardStub(t)
	m := copyModel(t, home)
	m.cursor = len(m.rows) - 1 // the space without a checkout
	res, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	res, _ = res.(model).Update(cmd())
	if help := footOf(res.(model)); !strings.Contains(help, "nothing to copy") {
		t.Errorf("help line = %q, want \"nothing to copy\"", help)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("the clipboard command ran without a path")
	}
}

// TestNodeDir covers what ctrl+y copies per kind, and the space that is no
// git checkout, which falls back to its first pane's cwd.
func TestNodeDir(t *testing.T) {
	cases := []struct {
		n    *node
		want string
	}{
		{&node{kind: "repo", checkout: "/r/shop"}, "/r/shop"},
		{&node{kind: "worktree", checkout: "/wt/shop/fix"}, "/wt/shop/fix"},
		{&node{kind: "pane", cwd: "/wt/shop/fix/src"}, "/wt/shop/fix/src"},
		{&node{kind: "repo", cwd: "/tmp/notes"}, "/tmp/notes"},
		{&node{kind: "repo"}, ""},
	}
	for _, c := range cases {
		if got := nodeDir(c.n); got != c.want {
			t.Errorf("nodeDir(%s %+v) = %q, want %q", c.n.kind, c.n, got, c.want)
		}
	}
	roots := buildTree([]wsInfo{{ID: "w1", Label: "notes", Number: 1}},
		[]paneInfo{{ID: "w1:p1", WsID: "w1", Cwd: "/nonexistent/notes"}}, nil)
	if got := nodeDir(roots[0]); got != "/nonexistent/notes" {
		t.Errorf("space without worktree metadata: %q, want its first pane's cwd", got)
	}
	if got := nodeDir(roots[0].children[0]); got != "/nonexistent/notes" {
		t.Errorf("pane: %q, want its cwd", got)
	}
}

// TestPanel: f1 lays the options and the keys over a frame that keeps its
// size, takes every key while it is open (the order is set there), and esc
// closes it before it quits. `?` is text for the filter.
func TestPanel(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	m := copyModel(t, t.TempDir())
	m.ti = newFilterInput("asgoto", "Search…") // focused, as main builds it
	res, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = res.(model)
	press := func(keys ...tea.KeyPressMsg) {
		for _, k := range keys {
			res, _ := m.Update(k)
			m = res.(model)
		}
	}
	if foot := footOf(m); !strings.Contains(foot, "f1 options") || strings.Contains(foot, "sort") {
		t.Errorf("the help line offers the panel and no sort key: %q", foot)
	}
	tree := m.vp.Height()
	press(tea.KeyPressMsg{Code: tea.KeyF1})
	plain := strings.Split(ansi.Strip(m.render()), "\n")
	if len(plain) != m.height || m.vp.Height() != tree {
		t.Fatalf("the panel changed the frame: %d lines (tree %d), want %d (tree %d)", len(plain), m.vp.Height(), m.height, tree)
	}
	all := strings.Join(plain, "\n")
	for _, want := range []string{"╭─ options ", "▌ Order", "‹spaces›", "Panes", "^a", "Keys", "copy the path", "esc close"} {
		if !strings.Contains(all, want) {
			t.Errorf("the panel lacks %q:\n%s", want, all)
		}
	}
	for i, l := range plain {
		if ansi.StringWidth(l) != 100 {
			t.Errorf("line %d is %d cells wide, want 100", i, ansi.StringWidth(l))
		}
	}
	press(tea.KeyPressMsg{Code: 'z', Text: "z"}, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.ti.Value() != "" || !m.prioritySort {
		t.Errorf("space on Order sets the priority order and nothing reaches the filter: %q %v", m.ti.Value(), m.prioritySort)
	}
	res, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = res.(model)
	if m.panel.open || cmd != nil {
		t.Errorf("esc closes the panel and nothing else: open=%v cmd=%v", m.panel.open, cmd)
	}
	press(tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.ti.Value() != "?" || m.panel.open {
		t.Errorf("? is text: filter %q, panel open %v", m.ti.Value(), m.panel.open)
	}
	// ctrl+s is no longer the order's key.
	press(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !m.prioritySort {
		t.Errorf("ctrl+s should not touch the order any more")
	}
}

// TestCopyKeyIsInTheFullHelpOnly keeps the help line short enough for the
// popup: the copy key is listed in the panel only.
func TestCopyKeyIsInTheFullHelpOnly(t *testing.T) {
	h, keys := help.New(), defaultKeys()
	h.SetWidth(200)
	if short := ansi.Strip(h.View(keys)); strings.Contains(short, "copy the path") {
		t.Errorf("folded help lists the copy key: %q", short)
	}
	h.ShowAll = true
	if full := ansi.Strip(h.View(keys)); !strings.Contains(full, "^y") || !strings.Contains(full, "copy the path") {
		t.Errorf("expanded help misses the copy key:\n%s", full)
	}
}

// dumpModel is a model without a TUI over a small tree, the way main builds
// it for -dump.
func dumpModel() *model {
	pane := &node{kind: "pane", label: "claude  [working]  fix-login", paneID: "w2:p1", status: "working", hasAgent: true}
	server := &node{kind: "pane", label: "npm run dev", paneID: "w1:p2", proc: "npm run dev", ports: []int{3000, 9229}}
	fix := &node{kind: "worktree", label: "fix/FED-12-login", branch: "fix/FED-12-login", folder: "fix-login", ticket: "FED-12",
		pr: &prRef{Number: 77, State: "draft"}, wsID: "w2", status: "working", ahead: 2, unstaged: 3, expanded: true, children: []*node{pane}}
	shop := &node{kind: "repo", label: "shop", branch: "main", wsID: "w1", status: "working", expanded: true, children: []*node{server, fix}}
	docs := &node{kind: "repo", label: "docs", branch: "main", wsID: "w3", expanded: true}
	roots := []*node{shop, docs}
	stampOrder(roots)
	m := &model{roots: roots, ti: textinput.New(), keys: defaultKeys()}
	m.resort()
	return m
}

// TestRunDump covers -dump: one line per node, indented by depth, with the
// kind, the status, the id, and what the row shows around its label; panes
// the popup hides are printed too.
func TestRunDump(t *testing.T) {
	var out bytes.Buffer
	runDump(&out, dumpModel(), "")
	want := strings.Join([]string{
		"shop\t(repo working w1 main)",
		"  npm run dev\t(pane  w1:p2)\t[proc :3000 :9229]",
		"  FED-12 #77(draft) fix/FED-12-login\t(worktree working w2 fix/FED-12-login folder=fix-login " + deltaText(&node{ahead: 2}) + " !3)",
		"    claude  [working]  fix-login\t(pane working w2:p1)",
		"docs\t(repo  w3 main)",
	}, "\n") + "\n"
	if out.String() != want {
		t.Errorf("dump:\n%s\nwant:\n%s", out.String(), want)
	}
}

// TestQueryDump covers -dump -query: the rows applyFilter lists, the parents
// kept for context unmarked, the matches with their score, and ">" on the one
// the cursor would land on.
func TestQueryDump(t *testing.T) {
	var out bytes.Buffer
	runDump(&out, dumpModel(), "login")
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want the header, the repo and the worktree:\n%s", len(lines), out.String())
	}
	if want := `query "login": 2 listed, 1 matched (sort: spaces, plain panes hidden)`; lines[0] != want {
		t.Errorf("header %q, want %q", lines[0], want)
	}
	if !strings.HasPrefix(lines[1], "      -  shop\t(repo") {
		t.Errorf("context row %q, want it unmarked and without a score", lines[1])
	}
	f := strings.Fields(lines[2])
	if len(f) < 3 || f[0] != ">" || f[1] == "-" || !strings.Contains(lines[2], "  FED-12 #77(draft) fix/FED-12-login\t(worktree") {
		t.Errorf("match row %q, want \">\", a score and the indented node", lines[2])
	}

	// A metadata hit (the PR number) and the agent pane, once panes are listed.
	m := dumpModel()
	m.showPanes = true
	out.Reset()
	runDump(&out, m, "fix")
	if got := out.String(); !strings.Contains(got, "plain panes listed") || strings.Count(got, "\n*")+strings.Count(got, "\n>") != 2 {
		t.Errorf("with panes listed \"fix\" matches the worktree and its pane:\n%s", got)
	}
	out.Reset()
	runDump(&out, dumpModel(), "zzzz")
	if got := out.String(); got != "query \"zzzz\": 0 listed, 0 matched (sort: spaces, plain panes hidden)\n" {
		t.Errorf("no match: %q", got)
	}
}

// TestHerdrError covers the message of a failed herdr read: herdr's own
// words when it answered with its JSON error, the exec error otherwise.
func TestHerdrError(t *testing.T) {
	exit := errors.New("exit status 1")
	reply := []byte(`{"id":"cli:workspace:list","error":{"code":"server_not_running","message":"no herdr server is running"}}` + "\n")
	if got := herdrError(exit, reply, nil).Error(); got != "no herdr server is running" {
		t.Errorf("JSON error on stdout: %q", got)
	}
	if got := herdrError(exit, nil, reply).Error(); got != "no herdr server is running" {
		t.Errorf("JSON error on stderr: %q", got)
	}
	if got := herdrError(exit, []byte("boom"), nil); got != exit {
		t.Errorf("anything else: %v, want the exec error", got)
	}
}

// TestLoadJSONReportsAMissingHerdr covers the CLI not being there at all: an
// error, straight away.
func TestLoadJSONReportsAMissingHerdr(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", filepath.Join(t.TempDir(), "no-herdr"))
	var ws wsResp
	if err := loadJSON([]string{"workspace", "list"}, &ws); err == nil {
		t.Error("loadJSON with no herdr binary returned no error")
	}
}

// TestStateIsOneFilePerSetting: the panes and the order are remembered like
// the settings of every tool of the family, and a state.json from before is
// still read until something is saved.
func TestStateIsOneFilePerSetting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	if got := loadState(); got != (persisted{}) {
		t.Errorf("nothing saved = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"show_panes":true,"priority_sort":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadState(); !got.ShowPanes || !got.PrioritySort {
		t.Errorf("the old state.json should still count: %+v", got)
	}
	saveStateCmd(persisted{ShowPanes: true})()
	if got := loadState(); got != (persisted{ShowPanes: true}) {
		t.Errorf("after a save the files win: %+v", got)
	}
	if loadSetting(dir, "panes") != "shown" || loadSetting(dir, "order") != "spaces" {
		t.Errorf("files = %q %q", loadSetting(dir, "panes"), loadSetting(dir, "order"))
	}
}

// TestEmptyTreeSaysWhy: a query that matches nothing says so in the list, as
// in every tool of the family (emptyList in listnav.go).
func TestEmptyTreeSaysWhy(t *testing.T) {
	m := copyModel(t, t.TempDir())
	m.ti = newFilterInput("asgoto", "Search…")
	for _, r := range "zzzzqq" {
		res, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = res.(model)
	}
	plain := strings.Split(ansi.Strip(m.render()), "\n")
	if len(m.rows) != 0 || !strings.HasPrefix(plain[3], "│ No matches") || len(plain) != m.height {
		t.Errorf("rows=%d, first list line %q, %d lines", len(m.rows), plain[3], len(plain))
	}
}

// TestPasteFilters: a paste changes the query without a key press, and the
// tree must follow it (toInput). A key that leaves the query alone must not
// move the cursor, and clearing the query keeps the cursor on its node.
func TestPasteFilters(t *testing.T) {
	m := copyModel(t, t.TempDir())
	m.ti = newFilterInput("asgoto", "Search…")
	step := func(msg tea.Msg) {
		res, _ := m.Update(msg)
		m = res.(model)
	}
	step(tea.PasteMsg{Content: "zzzzqq"})
	if m.ti.Value() != "zzzzqq" || len(m.rows) != 0 {
		t.Fatalf("a paste should filter: query %q, %d rows", m.ti.Value(), len(m.rows))
	}
	for range "zzzzqq" {
		step(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	step(tea.KeyPressMsg{Code: tea.KeyDown})
	at := m.rows[m.cursor].n
	step(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.rows[m.cursor].n != at {
		t.Errorf("a key that does not edit the query moved the cursor off %q", at.label)
	}
	step(tea.KeyPressMsg{Code: 'f', Text: "f"})
	on := m.rows[m.cursor].n
	step(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.rows[m.cursor].n != on {
		t.Errorf("clearing the query should stay on %q, the cursor is on %q", on.label, m.rows[m.cursor].n.label)
	}
}

// TestEnterOnAnEmptyTreeStays: with nothing under the cursor enter does
// nothing, as in every tool of the family; it used to close the popup.
func TestEnterOnAnEmptyTreeStays(t *testing.T) {
	m := copyModel(t, t.TempDir())
	m.ti = newFilterInput("asgoto", "Search…")
	res, _ := m.Update(tea.PasteMsg{Content: "zzzzqq"})
	m = res.(model)
	res, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := res.(model); cmd != nil || got.action != nil || got.focusPane != "" {
		t.Errorf("enter on an empty tree: cmd=%v action=%v pane=%q", cmd, got.action, got.focusPane)
	}
}

// TestMain sandboxes the state dir: tests must never touch the real one, even
// one that forgets to set it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "asgoto-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	os.Setenv("XDG_CACHE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

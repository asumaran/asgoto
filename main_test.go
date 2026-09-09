package main

import (
	"sort"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
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
		{&node{kind: "worktree", label: "stg-validation", branch: "feat/stargate"}, "feat/stargate"},
		{&node{kind: "worktree", label: "feat-x", branch: "feat/x"}, ""}, // only slugging differs
		{&node{kind: "worktree", label: "feat-x", branch: "feat-x"}, ""},
		{&node{kind: "repo", label: "app", branch: "main"}, ""},
		{&node{kind: "pane", ports: []int{3000, 3001}}, ":3000 :3001"},
	}
	for _, c := range cases {
		if got, _ := rightText(c.n); got != c.want {
			t.Errorf("%s %q/%q: got %q, want %q", c.n.kind, c.n.label, c.n.branch, got, c.want)
		}
	}
}

func TestRightColumn(t *testing.T) {
	if got := rightColumn("", 40, nil); got != "" {
		t.Errorf("empty: got %q", got)
	}
	if got := rightColumn(":4200", 20, nil); got != "               :4200" {
		t.Errorf("right-align: got %q", got)
	}
	if got := rightColumn(":4200", 5, nil); got != "" {
		t.Errorf("no room: got %q, want empty", got)
	}
	if got := rightColumn(":4200 :4201 :4202 :4203", 12, nil); got != "  :4200 :42…" {
		t.Errorf("truncate: got %q", got)
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

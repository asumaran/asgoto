package main

import (
	"sort"
	"testing"
)

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

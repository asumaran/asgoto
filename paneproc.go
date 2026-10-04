package main

// The foreground processes herdr panes run, read for the whole family:
// `pane process-info` resolved to a short label and the pids of the pane's
// process group, and the TCP ports those pids (and their descendants)
// listen on, from one lsof + one ps.

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

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

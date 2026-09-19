#!/usr/bin/env python3
"""End-to-end TUI check for asgoto without a real terminal.

Spawns the binary on a pty, answers the terminal queries bubbletea sends,
replays keystrokes and asserts on frames rendered with pyte and on what was
asked of herdr. Everything runs in a throwaway sandbox: a fake HOME, a herdr
stub (HERDR_BIN_PATH) that serves a synthetic session and logs every call, no
socket (HERDR_SOCKET_PATH unset) and a `gh` stub first on PATH. It never talks
to a herdr server or to GitHub.

Usage: scripts/pty-check.py ./asgoto   (needs python3 + pyte)
"""
NAME, ROWS, COLS = "asgoto", 16, 110
import atexit, fcntl, json, os, pty, select, shutil, signal, struct, subprocess, sys, tempfile, termios, time
import pyte

BIN = os.path.abspath(sys.argv[1])
SANDBOX = os.path.realpath(tempfile.mkdtemp(prefix="%s-pty-" % NAME))
home = os.path.join(SANDBOX, "home")
os.makedirs(home)

def write(path, text, mode=None):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)
    if mode: os.chmod(path, mode)
    return path

QUERIES = [(b"\x1b]11;?", b"\x1b]11;rgb:0000/0000/0000\x1b\\"), (b"\x1b]10;?", b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\"),
           (b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?62c")]

failures = []
def check(cond, msg):
    print(("  ok   " if cond else "  FAIL ") + msg)
    if not cond: failures.append(msg)

class Session:
    """One run of the binary on a pty."""
    def __init__(self, env, args=(), cwd=None):
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.proc = subprocess.Popen([BIN, *args], stdin=slave, stdout=slave, stderr=slave, env=env,
                                     close_fds=True, cwd=cwd or SANDBOX)
        os.close(slave)
        # A failed assertion must not leave the binary running on a dead pty.
        atexit.register(lambda p=self.proc: p.poll() is None and p.kill())
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.raw = bytearray()
        self.answered = 0

    def pump(self, seconds):
        end = time.time() + seconds
        while True:
            left = end - time.time()
            if left <= 0: break
            r, _, _ = select.select([self.master], [], [], left)
            if not r: continue
            try:
                data = os.read(self.master, 65536)
            except OSError:
                break
            if not data: break
            self.raw.extend(data); self.stream.feed(data)
            tail = bytes(self.raw[self.answered:])
            for q, reply in QUERIES:
                for _ in range(tail.count(q)):
                    os.write(self.master, reply)
            self.answered = len(self.raw)

    def repaint(self):
        # The v2 renderer updates the screen with scroll regions and SU, which
        # pyte ignores; a resize forces a full redraw it can follow.
        for cols in (COLS - 1, COLS):
            fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, cols, 0, 0))
            self.screen.resize(ROWS, cols)
            if self.proc.poll() is None: os.kill(self.proc.pid, signal.SIGWINCH)
            self.pump(0.3)

    def frame(self):
        return [line.rstrip() for line in self.screen.display]

    def send(self, b, wait=0.4):
        os.write(self.master, b); self.pump(wait); self.repaint()
        return self.frame()

    def start(self, marker):
        for _ in range(50):
            self.pump(0.1)
            if marker in "\n".join(self.frame()): break
        self.pump(0.5); self.repaint()
        return self.frame()

    def finish(self):
        try:
            self.proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            return None
        self.pump(0.2)
        return self.proc.returncode

def dump(title, f):
    print("--- %s ---" % title)
    for i, l in enumerate(f): print("%2d|%s" % (i, l))

def done():
    shutil.rmtree(SANDBOX, ignore_errors=True)
    print("\n%d failure(s)" % len(failures))
    sys.exit(1 if failures else 0)

CTRL_A, CTRL_S, CTRL_T, ESC, ENTER, TAB, DOWN, UP = b"\x01", b"\x13", b"\x14", b"\x1b", b"\r", b"\t", b"\x1b[B", b"\x1b[A"

# ---------- sandbox: synthetic herdr session, herdr + gh stubs ----------
def ws(wid, label, number, repo, path, linked=False, focused=False):
    return {"workspace_id": wid, "label": label, "number": number, "focused": focused,
            "worktree": {"repo_key": "/r/%s/.git" % repo, "repo_name": repo, "repo_root": "/r/" + repo,
                         "checkout_path": path, "is_linked_worktree": linked}}
workspaces = [
    ws("w1", "shop", 1, "shop", "/r/shop"),
    ws("w2", "fix-checkout-form", 2, "shop", "/wt/shop/fix-checkout-form", linked=True),
    ws("w3", "dotfiles", 3, "dotfiles", "/r/dotfiles", focused=True),
]
panes = [
    {"pane_id": "w2:p1", "workspace_id": "w2", "agent": "claude", "agent_status": "working", "cwd": "/wt/shop/fix-checkout-form"},
    {"pane_id": "w3:p1", "workspace_id": "w3", "agent": "", "agent_status": "", "cwd": "/r/dotfiles"},
]
data = os.path.join(SANDBOX, "data")
write(os.path.join(data, "workspace-list.json"), json.dumps({"result": {"workspaces": workspaces}}))
write(os.path.join(data, "pane-list.json"), json.dumps({"result": {"panes": panes}}))
write(os.path.join(data, "agent-list.json"), json.dumps({"result": {"agents": [{"pane_id": "w2:p1", "state_change_seq": 7}]}}))
calls_log = os.path.join(SANDBOX, "calls.log")
herdr = write(os.path.join(SANDBOX, "herdr"), """#!/bin/sh
printf '%%s\\n' "$*" >> "%s"
f="%s/$1-$2.json"
if [ -f "$f" ]; then cat "$f"; else printf '{}'; fi
""" % (calls_log, data), 0o755)
stub_bin = os.path.join(SANDBOX, "bin")
write(os.path.join(stub_bin, "gh"), "#!/bin/sh\nprintf '[]'\n", 0o755)

def session():
    env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor", HOME=home, HERDR_BIN_PATH=herdr,
               XDG_CONFIG_HOME=os.path.join(home, ".config"), PATH=stub_bin + os.pathsep + os.environ["PATH"])
    for k in ("HERDR_SOCKET_PATH", "HERDR_PLUGIN_STATE_DIR", "HERDR_CONFIG_PATH"):
        env.pop(k, None)
    if os.path.exists(calls_log): os.remove(calls_log)
    return Session(env)

def actions():
    time.sleep(0.3)
    if not os.path.exists(calls_log): return []
    reads = ("workspace list", "pane list", "agent list", "pane process-info")
    return [c for c in open(calls_log).read().splitlines() if not c.startswith(reads)]

# One frame: top border with the counter and the active order, input, main
# edge, tree, bottom edge, help, border. There is no context line.
def rows(f): return [l[1:-1].rstrip() for l in f[3:-3] if l[1:-1].strip()]
def prompt(f): return f[1].strip("│ ").rstrip()
def counter(f): return f[0].strip("╭╮─ ")

print("== asgoto pty driver (%dx%d) ==" % (COLS, ROWS))

# ---------- run 1: tree, cursor on the current space, filter, select ----------
s = session()
f = s.start("asgoto (dev) ❯"); dump("open", f)
check(prompt(f) == "asgoto (dev) ❯", "prompt line is clean: %r" % f[1])
check(f[0].startswith("╭") and f[-1].startswith("╰") and f[2].startswith("├"),
      "one frame: input right under the top border, no title line")
check(counter(f) == "3/3 sort: spaces", "counter and active order on the top border: %r" % counter(f))
check("type filter" in f[-2] and "esc/q quit" in f[-2], "help shows the filter hint and the quit keys: %r" % f[-2])
check(b"\x1b[?1049h" in s.raw, "program entered the alt screen")
r = rows(f)
check(len(r) == 3 and "shop" in r[0] and "fix-checkout-form" in r[1] and "dotfiles" in r[2], "repos and their worktrees form the tree: %r" % r)
check("▌" in r[2], "the cursor starts on the space asgoto was opened from: %r" % r[2])
f = s.send(b"checkout", 0.6); dump("filtered", f)
r = rows(f)
check(len(r) == 2 and "shop" in r[0] and "▌" in r[1] and "fix-checkout-form" in r[1],
      "the filter keeps the ancestor and lands on the match: %r" % r)
check(counter(f).startswith("2/3"), "the counter follows the filter: %r" % counter(f))
s.send(ENTER, 0.3)
check(s.finish() == 0, "clean exit after enter")
check(actions() == ["workspace focus w2"], "enter focuses the workspace: %r" % actions())

# ---------- run 2: ctrl+t lists panes, enter on one focuses it ----------
s = session()
s.start("asgoto (dev) ❯")
f = s.send(CTRL_T, 0.6); dump("panes", f)
r = rows(f)
check(any("claude" in x and "working" in x for x in r), "ctrl+t lists the agent pane with its status: %r" % r)
f = s.send(b"claude", 0.6)
s.send(ENTER, 0.3)
check(s.finish() == 0, "clean exit after enter on a pane")
check(actions() == ["agent focus w2:p1"], "without a socket the pane is focused through the CLI: %r" % actions())

# ---------- run 3: mouse: a click moves the cursor, the wheel walks it, q quits ----------
s = session()
s.start("asgoto (dev) ❯")
f = s.send(b"\x1b[<0;6;4M\x1b[<0;6;4m", 0.5)   # SGR press+release on the first tree line
r = rows(f)
check("▌" in r[0] and s.proc.poll() is None, "a click moves the cursor without selecting: %r" % r)
f = s.send(b"\x1b[<65;6;4M", 0.5)               # wheel down
check("▌" in rows(f)[1], "the wheel walks the cursor: %r" % rows(f))
os.write(s.master, b"q"); s.pump(0.4)
check(s.finish() == 0 and actions() == [], "q quits with an empty filter without touching herdr: %r" % actions())

# ---------- run 4: ctrl+s flips the sort label, esc does nothing ----------
s = session()
s.start("asgoto (dev) ❯")
f = s.send(CTRL_S, 0.5)
check(counter(f).endswith("sort: priority"), "ctrl+s switches to the priority order: %r" % counter(f))
s.send(CTRL_S, 0.3)   # the choice is persisted; put it back
os.write(s.master, ESC); s.pump(0.4)
check(s.finish() == 0 and actions() == [], "esc quits without touching herdr: %r" % actions())

done()

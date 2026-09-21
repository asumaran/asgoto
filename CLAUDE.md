# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgoto` is a small tree-style switcher across herdr repos, worktrees and
panes, used as a replacement for herdr's native "goto" navigator. It runs as a
herdr plugin pane (session-modal popup): open, pick a target, exit. It
talks to herdr through its CLI (`workspace list` / `pane list` to read,
`workspace focus` to act on repos/worktrees, and the socket API's
`pane.focus` to focus a pane, see below).

Distributed as a herdr plugin (`herdr plugin install asumaran/asgoto`; the
manifest's `[[build]]` runs `scripts/fetch-binary.sh`, which downloads the
release binary matching the manifest version and falls back to `go build`).
Each GitHub Release attaches the `asgoto-<os>-<arch>` assets (macOS and Linux, arm64 and amd64) that
`fetch-binary.sh` depends on. There is no published library.

## Stack & layout

- Go (single module `asgoto`, see `go.mod`). Single static binary, no
  runtime deps.
- TUI: Bubble Tea v2 + bubbles v2 (`textinput`, `viewport`, `key`, `help`),
  lipgloss v2 for styling, `sahilm/fuzzy` for fuzzy matching/scoring. The charm
  v2 modules are imported under their canonical `charm.land/<name>/v2` paths.
  `View()` returns a `tea.View` (the alt screen is declared there, not as a
  program option) built from `render()`, which is what the tests assert on.
  lipgloss v2 always emits ANSI, so tests compare `ansi.Strip`ped text.
- `main.go` — the whole program: herdr CLI JSON shapes, tree building, the
  filter-that-keeps-ancestors, rendering, and `main()`. It's intentionally one
  file; keep it that way unless it clearly outgrows it.
- `match.go` — `findTight`/`tighten`, the fuzzy matcher with one correction: it is
  greedy (first candidate for each rune, left to right), so a query that
  occurs in one piece could still match scattered letters before it. When the
  query occurs whole, that occurrence is the match, for the highlight and the
  score. The same file in every
  tool of the family.
  The files shared with the rest of the family (listed here) are the
  exception to the single file.
- `text.go` — `truncate`, `padRight`, `padLeft`: fitting text, styled or not,
  into cells. The same file in every tool of the family.
- `statedir.go` — `stateDirFor`: the state dir herdr injects, or a fixed path
  under the config home when the tool runs on its own. The same file in every
  tool of the family that keeps state.
- `setting.go` — `loadSetting`, `saveSetting`: a setting the tool remembers,
  one plain-text file each in the state dir. Every option of the panel is
  kept this way, per tool. The same file in every tool of the family that
  needs it.
- `listmouse.go` — `inList`, `rowUnder`, `wheelKey`: the mouse over the list.
  The wheel goes through the same code as the arrows; a click moves the
  cursor and never opens anything. The same file in every tool of the family.
- `prompt.go` — the filter input: its prompt (with the tool's name only outside
  herdr's popup), the placeholder, the `(dev)` mark on the edge over the
  input. The same
  file in every tool of the family.
- `helpfoot.go` — the help line at the foot, cut to the width, and the key
  that opens the panel. The same file in every tool of the family.
- `panel.go` — the panel `f1` opens over the frame: options to change in
  place and every key under them (`option`, `panel`, `panelLines`,
  `overlay`). The same file in every tool of the family.
- `listnav.go` — `listNav`: the keys that move the cursor through a list and
  where each one takes it, group headers skipped. `scrollTo` keeps the
  cursor in view, with the header of its group when there is one. The same file in every tool
  of the family.
- `highlight.go` — `highlight`/`highlightFrom`, `matchOver`, `onSel`,
  `selPad` and the `stSel`/`stMatch` styles: how a match and the selected row
  look. The same file in every tool of the family.
- `flash.go`: `flash`, `flashMsg`, `clearFlashMsg`: a confirmation that takes
  the help line for a moment. The same file in every tool of the family.
- `clipboard.go`: `copyCmd`: feeds a text to the system clipboard and reports
  it with a `flashMsg`; `ASGOTO_CLIPBOARD` replaces the command. The same file
  in every tool of the family.
- `herdr-plugin.toml` — the herdr plugin manifest (id `asumaran.asgoto`): a
  `[[build]]` (runs `scripts/fetch-binary.sh` on install), the `picker` popup
  pane, and the `open` action that opens it (keybind entry point).
- `scripts/` — `release.sh` (see below) plus two plugin pieces:
  `open-pane.sh`, the `open` action's command (plugin commands are argv without
  shell, so the wrapper resolves `HERDR_BIN_PATH` at runtime; honors optional
  `ASGOTO_POPUP_WIDTH`/`ASGOTO_POPUP_HEIGHT` env overrides over the manifest's
  popup size — used by the demo recording, not by normal installs), and
  `fetch-binary.sh`, the `[[build]]` command (downloads the release binary
  matching the manifest's `version`, falls back to `go build -ldflags
  "-X main.version=v<version>-source"`, aborts the install if neither works;
  `ASGOTO_BUILD_FROM_SOURCE=1` skips the download and always compiles).
- `scripts/demo/` — the demo scenario (`scenario.sh` + `keys.json`) that
  `asdemo record` (asumaran/asdemokit, the recording tool shared by
  the herdr plugins) uses to re-record the README GIF (`docs/demo.gif`); see
  `scripts/demo/README.md`. Uses a disposable herdr session (`asgotodemo`),
  never the user's default session.
- `docs/DESIGN.md` — implementation-level design notes (tree building, filter,
  right column, caches). Read it before changing that code.
- The compiled binary (`asgoto`, `asgoto-<os>-<arch>`) is **never committed**
  (`.gitignore`); it is built locally or in CI.

## Build & run

```bash
go build -o asgoto .     # local build in the repo
./asgoto -dump           # print the built tree (no TUI) for debugging without a TTY
./asgoto -dump -query x  # the rows the filter lists for x, matches with scores
./asgoto -version        # print the embedded version
go vet ./... && go test ./...
scripts/pty-check.py ./asgoto   # end-to-end TUI check on a pty (python3 + pyte)
```

`version` in `main.go` defaults to `"dev"` and is stamped at build time via
`-ldflags "-X main.version=<tag>"` (CI does this from the release tag;
`fetch-binary.sh`'s source fallback stamps `v<version>-source`).

## Testing

For end-to-end verification without a TTY, `scripts/pty-check.py ./asgoto`
(python3 + `pyte`) spawns the binary on a pty, answers the terminal queries,
replays keystrokes and asserts on pyte-rendered frames, in a throwaway sandbox
(a herdr stub as `HERDR_BIN_PATH` serving a synthetic session and logging
every call, no socket, a `gh` stub first on `PATH`, a clipboard stub as
`ASGOTO_CLIPBOARD` logging what `ctrl+y` feeds it). It also runs `-dump` and
`-dump -query` without a pty against the same stubs. The v2 renderer repaints with scroll regions, which pyte ignores, so the
driver forces a full redraw (pty resize + SIGWINCH) before reading a frame.

## How it's wired into herdr

The plugin requires herdr >= 0.7.5. The manifest declares the `picker` popup
pane (55% x 50%) and the `open` action; herdr has no `plugin_pane` keybind
type, so the key binds the action, which runs `scripts/open-pane.sh` ->
`herdr plugin pane open`:

```toml
[[keys.command]]
key = ["prefix+f", "ctrl+alt+f"]
type = "plugin_action"
command = "asumaran.asgoto.open"
```

- Install: `herdr plugin install asumaran/asgoto` (clones, runs `[[build]]`
  = `scripts/fetch-binary.sh`: release download first, `go build` fallback, so
  a Go toolchain is only needed where no release asset exists).
- Local dev: `herdr plugin link "$PWD"` from the checkout registers the
  working copy. `plugin link` does **not** run build commands — run `go build -o asgoto .`
  yourself (not `fetch-binary.sh`, which would fetch the released build over
  your local changes); the pane runs `./asgoto` from the plugin root.
- Runtime state (one file per setting, `panes` and `order`, through
  `setting.go`, and `prcache.json`; a `state.json` from before is still read
  until a setting is saved) lives in
  `HERDR_PLUGIN_STATE_DIR` (herdr injects it; never store state in the plugin
  checkout). When run standalone (outside herdr, e.g. `./asgoto -dump`),
  `statedir.go` falls back to the same directory
  (`~/.local/state/herdr/plugins/asumaran.asgoto/`).

## Releasing

`scripts/release.sh <X.Y.Z>` does everything: gates on a clean tree and green
`go vet`/`go build`/`go test`, syncs `version` in `herdr-plugin.toml`,
with `--demo` re-records `docs/demo.gif` with `asdemo record` (opt-in: it takes
over a herdr session; a failed recording aborts before anything is committed),
writes the
`CHANGELOG.md` entry and release notes from commit subjects, commits
(`chore(release): vX.Y.Z`), tags, pushes and publishes the GitHub release. CI
(`.github/workflows/release.yml`) builds the platforms the manifest declares (macOS and Linux, arm64 and amd64)
and attaches them; those assets are what `fetch-binary.sh` downloads on
installs, so they must keep being published. `release.sh` and `release.yml`
are the same files in every plugin of the family: they read the repository
name and the manifest instead of naming the tool.

Releasing never touches this machine's linked plugin. After a release, offer
to run `scripts/fetch-binary.sh` to install the published build over `./asgoto`;
never do it as a side effect. `go build -o asgoto .` switches back to a dev build.

## Behaviour / decisions

Full notes in `docs/DESIGN.md`; `README.md` only describes what the user sees
and does (keep implementation detail out of it).
Non-negotiables that are not obvious from the code:

- **Layout**: one rounded frame of sections split by shared edges, the layout
  asgitlog introduced and every picker of the family follows (the helpers live
  in `main.go` here): the filter input (the border over it carries the
  active order), the main section (the tree, full width: asgoto has no
  preview; its bottom edge carries the matches/total counter), and the help. A context line on top is only for what the rest of
  the screen cannot say (asgitlog: repo and branch); a title is not context,
  so there is none here. The list starts on screen row `listY`, one cell in
  from the left side, which is what the click-to-row math uses. Errors and
  notices take the help line.
- **Moving through the list** is the same in every tool of the family and
  comes from `listnav.go` (the same file in each repo): arrows or
  `ctrl+p`/`ctrl+n` a row, `pgup`/`pgdn` a page, `alt+↑`/`alt+↓` or
  `home`/`end` the ends. `home`/`end` are taken from the filter input's caret
  on purpose (`←`/`→` and `ctrl+e` still move it). The preview scrolls with
  `shift+↑`/`shift+↓` only. The keys are listed in the panel.
- **The filter input** comes from `prompt.go` (the same file in every tool of
  the family). Inside herdr's popup the prompt is the arrow alone, because the
  pane's title (`[[panes]] title` in the manifest, the tool's name) already
  says which tool it is, and a placeholder says what the filter searches. Run
  on its own the prompt carries the tool's name. A build that is not a release
  says `(dev)` on the edge over the input, never inside the
  prompt.
  herdr sets `HERDR_PLUGIN_ENTRYPOINT_ID` for a plugin pane; that is how the
  two cases are told apart.
- **Help and options**: the line at the foot shows the tool's own actions,
  the panel's key and the quit keys (`helpfoot.go`). `f1` opens the panel (`panel.go`, the same file in
  every tool of the family): the options on top, to change with `←`/`→` or
  `space`, and every key in columns under them, laid out by bubbles' `help`
  from `FullHelp()`. The panel is spliced over the middle of the frame, which
  keeps its size; while it is open it takes every key and the mouse, and `esc`
  closes it before it does anything else. `?` is not a help key: the filter
  has the focus, so it is text. Moving, scrolling and resizing are listed in
  the panel only, so the help line stays short enough for a narrow popup. A
  message (error, notice) takes the help line's place.
  This tool's options are the order and the panes: `options()` lists them as things stand and
  `setOption` is the one place that changes a setting, for the panel and for
  the keys that kept a shortcut. A setting that is chosen once has no key of
  its own; the panel is where it lives.
- **Copying**: `ctrl+y` copies the directory of the row under the cursor
  (`nodeDir`: the checkout of a repo or worktree, a pane's `cwd`, and the
  first pane's `cwd` for a space that is no git checkout) with `copyCmd`
  (`clipboard.go`). The clipboard gets the absolute path and the help line
  flashes `copied <path with ~>` or `nothing to copy` (`flash.go`, shown by
  `footMsg`, which has nothing else to show here); both files are the same in
  every tool of the family. The key is listed in the panel only.
  `ASGOTO_CLIPBOARD` replaces the clipboard command,
  which is how the tests and the pty check log it.
- **`-dump` / `-query`**: flags are parsed with `flag` (`-version`, `-dump`,
  `-query`). `-dump` runs the same `main()` path as the popup up to the model
  (herdr reads, `buildTree`, sort, PR cache, process rows; ports and git
  hints are fetched synchronously) and hands it to `runDump`, which prints
  every node with `dumpLine`. `-dump -query x` goes through `queryDump`: it
  sets the input, calls `applyFilter` and `selectBestMatch` on that model and
  prints `m.rows`, so there is no second filter to keep in sync. It follows
  the persisted panes and order settings. `loadJSON` bounds each herdr read
  (`herdrTimeout`) and reports herdr's own JSON error message, so an
  unreachable server is an error and exit 1, never a hang. Read-only.
- **Filter matches** look the same in every tool of the family and come from
  one place, `highlight.go` (the same file in each repo; it also owns `stSel`
  and `stMatch`): a match is the match color plus an underline on top of the
  style the text already has, and the selected row shows them too. That row
  is never one big `stSel.Render` around styled text, because the reset that
  ends a match would cut the background: every piece is rendered over `stSel`
  (`highlight(s, idx, stSel)`) and `selPad` fills the rest. Do not write a
  local highlighter.
- **Mouse**: a left click on a row moves the cursor and never selects, so a
  stray click cannot switch spaces; the wheel walks the cursor a row at a time
  (there is no preview to scroll). `q` quits only while the filter is empty.
- Digits are plain search text. The old "1-9 jumps to a numbered repo" mode
  was removed on purpose (it conflicted with searching by PR/ticket number);
  do not reintroduce it.
- Worktree rows are labelled by the checked-out branch, never by the folder
  (the folder is a stale slug after a checkout). The folder stays searchable.
- The right column mirrors the zsh prompt and the Claude Code statusline
  (dotfiles-bash: modules/zsh/zshrc.template,
  modules/claude-code/statusline-command.sh): same order, symbols and
  256-color palette. Keep the three in sync.
- Enter on a repo/worktree never changes the focused pane inside it, and
  switching never autofocuses the agent pane.
- Process rows are resolved synchronously before the first paint (they add
  rows); ports, git hints and PR data arrive async and only fill columns
  that are already sized from `prcache.json`.
- No breadcrumb line under the prompt (removed as redundant with the tree).

## Commits & branches

- Conventional Commits: `type(scope): description` (feat, fix, chore, docs,
  style, refactor, test, perf).
- Never mention AI tooling in commits, PRs, or any repo-visible text.
- Default branch is `main`. Don't commit, tag, or push unless explicitly asked
  (releasing is an explicit, separate request).

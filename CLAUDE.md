# CLAUDE.md

Guidance for working in this repository.

## What this is

`herdr-goto` is a small tree-style switcher across herdr repos, worktrees and
panes, used as a replacement for herdr's native "goto" navigator. It runs as a
herdr plugin pane (session-modal popup): open, pick a target, exit. It
talks to herdr through its CLI (`workspace list` / `pane list` to read,
`workspace focus` to act on repos/worktrees, and the socket API's
`pane.focus` to focus a pane, see below).

Distributed as a herdr plugin (`herdr plugin install asumaran/herdr-goto`; the
manifest's `[[build]]` runs `scripts/fetch-binary.sh`, which downloads the
release binary matching the manifest version and falls back to `go build`).
Each GitHub Release attaches the `goto-darwin-arm64` asset that
`fetch-binary.sh` depends on. There is no published library.

## Stack & layout

- Go (single module `herdr-goto`, see `go.mod`). Single static binary, no
  runtime deps.
- TUI: Bubble Tea + bubbles (`textinput`, `viewport`, `key`, `help`),
  `lipgloss` for styling, `sahilm/fuzzy` for fuzzy matching/scoring.
- `main.go` — the whole program: herdr CLI JSON shapes, tree building, the
  filter-that-keeps-ancestors, rendering, and `main()`. It's intentionally one
  file; keep it that way unless it clearly outgrows it.
- `herdr-plugin.toml` — the herdr plugin manifest (id `asumaran.goto`): a
  `[[build]]` (runs `scripts/fetch-binary.sh` on install), the `goto` popup
  pane, and the `open` action that opens it (keybind entry point).
- `scripts/` — `release.sh` (see below) plus two plugin pieces:
  `open-pane.sh`, the `open` action's command (plugin commands are argv without
  shell, so the wrapper resolves `HERDR_BIN_PATH` at runtime; honors optional
  `GOTO_POPUP_WIDTH`/`GOTO_POPUP_HEIGHT` env overrides over the manifest's
  popup size — used by the demo recording, not by normal installs), and
  `fetch-binary.sh`, the `[[build]]` command (downloads the release binary
  matching the manifest's `version`, falls back to `go build -ldflags
  "-X main.version=v<version>-source"`, aborts the install if neither works;
  `GOTO_BUILD_FROM_SOURCE=1` skips the download and always compiles).
- `scripts/demo/` — the demo scenario (`scenario.sh` + `keys.json`) that
  `herdr-demo record` (asumaran/herdr-demokit, the recording tool shared by
  the herdr plugins) uses to re-record the README GIF (`docs/demo.gif`); see
  `scripts/demo/README.md`. Uses a disposable herdr session (`gotodemo`),
  never the user's default session.
- `docs/DESIGN.md` — implementation-level design notes (tree building, filter,
  right column, caches). Read it before changing that code.
- The compiled binary (`goto`, `goto-darwin-arm64`) is **never committed**
  (`.gitignore`); it is built locally or in CI.

## Build & run

```bash
go build -o goto .     # local build in the repo
./goto -dump           # print the built tree (no TUI) for debugging without a TTY
./goto -version        # print the embedded version
go vet ./... && go test ./...
```

`version` in `main.go` defaults to `"dev"` and is stamped at build time via
`-ldflags "-X main.version=<tag>"` (CI does this from the release tag;
`fetch-binary.sh`'s source fallback stamps `v<version>-source`).

## How it's wired into herdr

The plugin requires herdr >= 0.7.5. The manifest declares the `goto` popup
pane (55% x 50%) and the `open` action; herdr has no `plugin_pane` keybind
type, so the key binds the action, which runs `scripts/open-pane.sh` ->
`herdr plugin pane open`:

```toml
[[keys.command]]
key = ["prefix+f", "ctrl+alt+f"]
type = "plugin_action"
command = "asumaran.goto.open"
```

- Install: `herdr plugin install asumaran/herdr-goto` (clones, runs `[[build]]`
  = `scripts/fetch-binary.sh`: release download first, `go build` fallback, so
  a Go toolchain is only needed off `darwin/arm64`).
- Local dev: `herdr plugin link ~/Developer/herdr-goto` registers the working
  copy. `plugin link` does **not** run build commands — run `go build -o goto .`
  yourself (not `fetch-binary.sh`, which would fetch the released build over
  your local changes); the pane runs `./goto` from the plugin root.
- Runtime state (`state.json`, `prcache.json`) lives in
  `HERDR_PLUGIN_STATE_DIR` (herdr injects it; never store state in the plugin
  checkout). When run standalone (outside herdr, e.g. `./goto -dump`),
  `main.go` falls back to `~/.config/herdr/goto-tui/`.

## Releasing

`scripts/release.sh <X.Y.Z>` does everything: gates on a clean tree and green
`go vet`/`go build`/`go test`, syncs `version` in `herdr-plugin.toml`,
re-records `docs/demo.gif` with `herdr-demo record` (`--no-demo` skips it; a
failed recording aborts before anything is committed), writes the
`CHANGELOG.md` entry and release notes from commit subjects, commits
(`chore(release): vX.Y.Z`), tags, pushes and publishes the GitHub release. CI
(`.github/workflows/release.yml`) builds `goto-darwin-arm64` and attaches it;
that asset is what `fetch-binary.sh` downloads on installs, so it must keep
being published. Only `darwin/arm64` is built; other platforms need a matrix
in `release.yml` (`fetch-binary.sh` already resolves the asset from `uname`).

Releasing never touches this machine's linked plugin. After a release, offer
to run `scripts/fetch-binary.sh` to install the published build over `./goto`;
never do it as a side effect. `go build -o goto .` switches back to a dev build.

## Behaviour / decisions

Full notes in `docs/DESIGN.md`; `README.md` only describes what the user sees
and does (keep implementation detail out of it).
Non-negotiables that are not obvious from the code:

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

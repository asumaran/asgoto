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

`scripts/release.sh <X.Y.Z>` cuts and publishes a release. It gates on a clean
tree + green `go vet`/`go build`/`go test`, syncs `version` in
`herdr-plugin.toml` to the tag, re-records the README demo GIF
(`docs/demo.gif`) with `herdr-demo record` so it always shows the released UI
(the manifest sync happens first so the recorded popup carries the new
version; resolved via `HERDR_DEMO_BIN`, PATH, then
`~/Developer/herdr-demokit/bin`; `--no-demo` skips it and a failed recording
aborts the release before anything is committed), generates the
`CHANGELOG.md` entry and GitHub release notes from commit subjects since the
last tag, commits (`chore(release): vX.Y.Z`, the refreshed GIF included),
tags, pushes, and publishes the GitHub release. CI
(`.github/workflows/release.yml`) then builds the binary (stamping the version
from the tag) and attaches `goto-darwin-arm64` — the asset `fetch-binary.sh`
downloads on plugin installs, so it must keep being published.

Releasing does not touch this machine's linked plugin: it runs whatever
`./goto` sits in the working copy. After cutting a release, offer to install
the published build locally by running `scripts/fetch-binary.sh` (downloads
the release binary matching the manifest version over `./goto`); never do it
as a side effect of releasing. `go build -o goto .` switches back to a dev
build when working on the code.

The release workflow builds only `darwin/arm64` (the development machine). To
support more platforms, add a build matrix in `release.yml` and upload one
`goto-<os>-<arch>` asset per target; `fetch-binary.sh` already resolves the
asset name from `uname`.

## Behaviour / decisions

- Tree = two levels by default: repo (== main checkout) -> worktrees. Panes are
  hidden by default; `ctrl+t` toggles them, persisted in `state.json`.
- Repos ordered by lowest workspace `number`. Worktrees inside a repo sort
  oldest-first by checkout creation time (directory birth time, which tracks PR
  order in practice), workspace `number` as tiebreaker.
  Grouping key: `worktree.repo_key` (falls back to checkout_path, then a pane's
  cwd, then workspace id).
- Search: fuzzy with scoring + a small kind bonus (repo +8, worktree +4) so
  repo/worktree names outrank panes. Besides the label, the branch, the Jira
  ticket and the PR number are matched (typing "1234" finds the row showing
  #1234). Matches keep ancestors visible. Digits are plain search text; the
  old "1-9 jumps to a numbered repo" mode was removed on purpose — do not
  reintroduce it.
- Process rows: a pane whose foreground process group leader is not the
  shell (`pane process-info`, argv compressed by `shortCmd`) is relabelled
  with the command and stays visible while plain panes are hidden (`visible`
  in `applyFilter`). Its listening TCP ports (one `lsof -iTCP -sTCP:LISTEN`
  matched against the group's pids and their descendants via one `ps -axo
  pid,ppid`, since dev servers detach into their own group) render
  right-aligned (`rightColumn`, capped at `rightMaxW`) and join the search
  corpus. Agent panes and shells at the prompt are untouched. Process rows are
  resolved synchronously in `main` before the first paint (cheap, and they add
  rows, so a late arrival would shift the list); ports are fetched async from
  Init (`fetchPortsCmd`, lsof is ~80ms) and only fill the right column.
  Errors degrade to no rows / no ports. `-dump` runs it
  synchronously and prints `[proc :port]`.
- Labels: repo rows = repo name; worktree rows = the branch checked out in
  the worktree (`node.branch`), falling back to the folder (`node.folder`,
  herdr's workspace label) when the branch is unknown/detached. The folder is
  the slug of the branch the worktree was created for and goes stale after a
  checkout, which is why it is not the label; it stays in the search corpus
  (`flatten` joins branch + folder as the extra match text).
- Right column (`rightSegs`, styled segments; `rightText` is the plain join
  for -dump/tests): ports on process rows. Repo/worktree rows, in the shell
  prompt's order: the name (the branch on repo rows, always; on worktree
  rows the folder, only when it is not the branch's `/` -> `-` slug), then
  the ahead/behind hint (`deltaText`, "↑n↓n", empty when in sync), then the
  working-tree counters (`countsText`/`countSegs`, "+n !n ?n"
  staged/unstaged/untracked, each hidden at zero) — the same order, symbols
  and 256-color palette as the zsh prompt's `git_prompt_segment` and the
  Claude Code statusline (dotfiles-bash: modules/zsh/zshrc.template,
  modules/claude-code/statusline-command.sh): delta pink bold (212), staged
  green (84), unstaged yellow (228), untracked dim (245); keep the three in
  sync. Every hint is its own fixed-width column, sized by the row with the
  widest value and absent when no row has it (`layoutHints`, `node.deltaW`
  / `stagedW` / `unstagW` / `untrkW`; rows with a shorter/absent value pad
  the slot, left-aligned so the ↑/+/!/? symbols stack vertically), so the
  names right-align on one column and each counter aligns with its own kind
  across the list; the widths are stamped on pane rows too, whose blank
  slots make ports end on the column the names end on. Ports are yellow-3
  and deltas pink so `:3000` and `↑3` never read as the same thing. Hints come from one `git status
  --porcelain=v1 -b --untracked-files=normal` per checkout (`fetchDeltas` ->
  `parseStatus`: ahead/behind from the `## ` header, counts from the entry
  lines; 4 at a time: git status is
  multithreaded and ~1s CPU on a large repo), async from Init
  (`fetchDeltasCmd`, `deltaMsg`) since they only fill the right column;
  `-dump` runs it synchronously. They are cached per checkout path in
  `prcache.json` (`prCache.Hints`, no freshness gate: always revalidated)
  so the cached values paint and size the columns from the first frame;
  `deltaMsg` rewrites `Hints` with only the checkouts still listed and
  saves. `savePRCacheCmd` marshals synchronously so the async write never
  races a later mutation of the maps. A `rightMargin` of 1 column keeps the
  column off the popup edge. The breadcrumb line under the prompt was
  removed as redundant with the tree.
- Workspaces without `worktree` metadata resolve their checkout from the
  first pane's cwd (`gitTopLevel`) so repo rows still get branch + PR data.
- Initial cursor: the repo/worktree row of the focused workspace
  (`currentWorkspaceNode`, `workspace list` `.focused`); top of the list when
  none is focused.
- Enter on repo/worktree -> `workspace focus` (does not change the focused pane
  inside it). Enter on pane -> `focusPane`: a `pane.focus` request over the
  socket API (`HERDR_SOCKET_PATH`, one JSON line each way). Not the CLI:
  `herdr pane focus` is direction-only and `herdr agent focus <paneID>`
  answers agent_not_found for shell/process panes since herdr 0.9, so it is
  only the fallback when the socket env is missing. No autofocus on switch.
- Rows are prefixed with the Jira ticket (`KEY-123` regex over branch, then
  label, then PR title as fallback) and the branch's PR number colored by
  state (open green, draft dim, merged purple, closed red). Columns align per
  sibling group; rows with neither ticket nor PR get no prefix. PR data comes
  from one async `gh pr list` per unique GitHub repo, fired after the TUI is
  on screen, and cached in `prcache.json` next to `state.json`
  (stale-while-revalidate; entries fresher than 60s skip the refresh).
  Missing `gh` or non-GitHub remotes degrade silently to no PR info.

## Commits & branches

- Conventional Commits: `type(scope): description` (feat, fix, chore, docs,
  style, refactor, test, perf).
- Never mention AI tooling in commits, PRs, or any repo-visible text.
- Default branch is `main`. Don't commit, tag, or push unless explicitly asked
  (releasing is an explicit, separate request).

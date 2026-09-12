# Design notes

Implementation-level decisions behind the TUI, kept here so `CLAUDE.md` stays
short. The user-facing summary lives in `README.md` ("Behaviour / decisions");
this file names the functions, fields and caches involved. Read it before
touching the tree building, the filter, the right column or the caches in
`main.go`.


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


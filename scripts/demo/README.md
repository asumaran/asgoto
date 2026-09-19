# Demo recording

Scenario for re-recording the README demo GIF (`docs/demo.gif`) with
[asdemokit](https://github.com/asumaran/asdemokit):

```bash
asdemo record            # from the repo root; writes docs/demo.gif
asdemo doctor            # check the toolchain first
```

`scripts/release.sh` runs the recording automatically on every release (after
syncing the manifest version, so the popup shows the released version) and
commits the refreshed GIF with the release; `--no-demo` skips it.

- `scenario.sh` — what the demo session looks like: the isolated herdr
  session (`asgotodemo`), the personal repos/worktrees it is populated with,
  the bottom splits, a dev server (`npm run dev` in the shopnest worktree
  split, so the popup lists it as a process row with its port), the
  popup-size overrides (60% x 60% via
  `ASGOTO_POPUP_WIDTH`/`ASGOTO_POPUP_HEIGHT`, honored by `scripts/open-pane.sh`),
  plus `demo_build` (stamps `./asgoto` with the manifest version so the popup
  shows the release prompt, no `(dev)` marker) and `demo_teardown` (restores
  the dev build).
- `keys.json` — the keystrokes replayed once the client is attached:
  `prefix+f` -> popup -> type `price` -> enter -> `prefix+f` -> type `asdev`
  -> enter. The kit appends the detach.

The kit boots the session, records it headless with asciinema, trims the
cast to the UI frames and renders the GIF with agg (Berkeley Mono, dracula,
170x44). The user's default herdr session is never touched. Requirements,
knobs (`DEMO_COLS`/`DEMO_ROWS`, `--keep-cast`, `--keep-session`) and the
scenario contract are documented in the kit's README.

Besides the kit's toolchain, this scenario needs the `asumaran.asgoto` plugin
registered in herdr with the `prefix+f` `plugin_action` keybind, `gh`
authenticated (live PR info on the demo repos), the repos/worktrees listed
in `scenario.sh` to exist, `node_modules` installed in the dev-server
worktree and its port (`DEV_SERVER_PORT` in `scenario.sh`) free.

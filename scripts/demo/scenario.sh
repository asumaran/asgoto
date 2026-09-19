# shellcheck shell=bash
# scenario.sh — demo session for the README GIF, run by `asdemo record`
# (asumaran/asdemokit). Sourced by the kit; the helpers used below
# (demo_*) come from it.

DEMO_SESSION="asgotodemo"
DEMO_OUT="docs/demo.gif"
# The initial workspace is the linked plugin's checkout, so the demo starts on
# "asgoto main" whatever directory the recording runs from.
DEMO_START_CWD="$HOME/Developer/asgoto"
# The demo popup opens bigger than the manifest's 55% x 50% so it reads well
# in the GIF; open-pane.sh picks these up from the session server's env.
DEMO_SESSION_ENV=(ASGOTO_POPUP_WIDTH=60% ASGOTO_POPUP_HEIGHT=60%)

# Repos/worktrees shown in the demo (personal projects only). Main checkouts
# get their own workspace; linked worktrees are opened with the owning repo
# as --cwd context so herdr groups them under it.
REPOS=(
  "$HOME/Developer/shopnest"
  "$HOME/Developer/worktree-cli"
  "$HOME/Developer/asdev"
  "$HOME/Developer/asreviewer"
  "$HOME/Developer/aspage"
)
WORKTREES=(
  "$HOME/Developer/shopnest:$HOME/wt/shopnest/feat-cart-has-product"
  "$HOME/Developer/shopnest:$HOME/wt/shopnest/fix-SHOP-4-product-card-accessibility"
  "$HOME/Developer/shopnest:$HOME/wt/shopnest/test-format-price-util"
)
# Workspaces the demo visits get a bottom split so herdr's pane dividers are
# visible, like a real working layout.
SPLITS=(
  "$HOME/Developer/asgoto"
  "$HOME/wt/shopnest/test-format-price-util"
  "$HOME/Developer/asdev"
)
# The shopnest worktree split runs a dev server so the popup lists it as a
# process row with its listening port. Needs node_modules in that worktree
# and the port free (pinned off 3000/3001, usually taken by real dev
# servers on this machine; next would otherwise silently pick another port
# and the wait below would never see it).
DEV_SERVER_CWD="$HOME/wt/shopnest/test-format-price-util"
DEV_SERVER_PORT=3002
DEV_SERVER_CMD="npm run dev -- --port $DEV_SERVER_PORT"

# Build ./asgoto stamped with the manifest version so the popup prompt shows the
# release look ("asgoto ❯", no "(dev)" marker). demo_teardown restores the plain
# dev build for the linked plugin afterwards.
demo_build() {
  local version
  version="$(sed -n 's/^version = "\(.*\)"/\1/p' herdr-plugin.toml)"
  go build -ldflags "-X main.version=v${version}" -o asgoto .
}

demo_teardown() {
  go build -o asgoto . 2>/dev/null || true
}

demo_setup() {
  local repo pair target pane dev_pane="" i
  # The launch workspace exists but has no worktree metadata yet; adopt it.
  demo_adopt_repo "$(demo_first_workspace)" "$DEMO_START_CWD"
  for repo in "${REPOS[@]}"; do demo_open_repo "$repo" >/dev/null; done
  for pair in "${WORKTREES[@]}"; do demo_open_worktree "${pair%%:*}" "${pair#*:}"; done
  for target in "${SPLITS[@]}"; do
    pane="$(demo_split_below "$target")"
    [[ "$target" == "$DEV_SERVER_CWD" ]] && dev_pane="$pane"
  done

  lsof -nP -iTCP:"$DEV_SERVER_PORT" -sTCP:LISTEN >/dev/null 2>&1 &&
    die "port $DEV_SERVER_PORT is already taken; the demo dev server needs it"
  demo_run_in_pane "$dev_pane" "$DEV_SERVER_CMD"
  for i in $(seq 1 60); do
    lsof -nP -iTCP:"$DEV_SERVER_PORT" -sTCP:LISTEN >/dev/null 2>&1 && return 0
    sleep 1
  done
  die "dev server never bound port $DEV_SERVER_PORT"
}

# shellcheck shell=bash
# scenario.sh — demo session for the README GIF, run by `herdr-demo record`
# (asumaran/herdr-demokit). Sourced by the kit; the helpers used below
# (demo_*) come from it.

DEMO_SESSION="gotodemo"
DEMO_OUT="docs/demo.gif"
# The initial workspace is the linked plugin's checkout, so the demo starts on
# "herdr-goto main" whatever directory the recording runs from.
DEMO_START_CWD="$HOME/Developer/herdr-goto"
# The demo popup opens bigger than the manifest's 45% x 50% so it reads well
# in the GIF; open-pane.sh picks these up from the session server's env.
DEMO_SESSION_ENV=(GOTO_POPUP_WIDTH=60% GOTO_POPUP_HEIGHT=60%)

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
  "$HOME/Developer/herdr-goto"
  "$HOME/wt/shopnest/test-format-price-util"
  "$HOME/Developer/asdev"
)

# Build ./goto stamped with the manifest version so the popup prompt shows the
# release look ("goto ❯", no "(dev)" marker). demo_teardown restores the plain
# dev build for the linked plugin afterwards.
demo_build() {
  local version
  version="$(sed -n 's/^version = "\(.*\)"/\1/p' herdr-plugin.toml)"
  go build -ldflags "-X main.version=v${version}" -o goto .
}

demo_teardown() {
  go build -o goto . 2>/dev/null || true
}

demo_setup() {
  local repo pair target
  # The launch workspace exists but has no worktree metadata yet; adopt it.
  demo_adopt_repo "$(demo_first_workspace)" "$DEMO_START_CWD"
  for repo in "${REPOS[@]}"; do demo_open_repo "$repo" >/dev/null; done
  for pair in "${WORKTREES[@]}"; do demo_open_worktree "${pair%%:*}" "${pair#*:}"; done
  for target in "${SPLITS[@]}"; do demo_split_below "$target" >/dev/null; done
}

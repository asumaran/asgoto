#!/bin/sh
# Entry point for the "open" plugin action: opens the asgoto pane (placement and
# size come from the [[panes]] entry in herdr-plugin.toml). ASGOTO_POPUP_WIDTH /
# ASGOTO_POPUP_HEIGHT (e.g. "60%") override the manifest size — the demo
# recording uses that; normal use should rely on the manifest. Plugin commands
# are argv arrays with no shell expansion, so this wrapper exists to resolve
# HERDR_BIN_PATH and the overrides at runtime.
set -eu
exec "${HERDR_BIN_PATH:-herdr}" plugin pane open --plugin asumaran.asgoto --entrypoint picker \
  ${ASGOTO_POPUP_WIDTH:+--width "$ASGOTO_POPUP_WIDTH"} \
  ${ASGOTO_POPUP_HEIGHT:+--height "$ASGOTO_POPUP_HEIGHT"}

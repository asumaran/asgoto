#!/usr/bin/env bash
#
# release.sh — cut a new release, gated on a clean tree and a green
# vet+build+test.
#
# Releases are created from a tag: a GitHub Actions workflow then compiles the
# binaries (stamping the version via -ldflags) and attaches them as release
# assets. This script makes sure we never tag a half-finished or broken state:
#
#   1. the working tree must be clean (the tag == exactly what is committed)
#   2. `go vet ./...`, `go build` and `go test ./...` must pass
#
# The CHANGELOG entry and the GitHub release notes are generated automatically
# from the commit subjects since the previous tag — nothing to write by hand.
# `--demo` also re-records the README demo GIF (docs/demo.gif) with asdemokit's
# `asdemo record`, after the manifest version is synced so the recorded popup
# carries the new version; the refreshed GIF rides the release commit. It needs
# the scenario in scripts/demo and takes over a herdr session while it records,
# which is why it is opt-in.
#
# This file is the same in every plugin of the family: it reads what it needs
# from the repository it runs in.
#
# Usage:
#   scripts/release.sh 0.2.0            # release version 0.2.0
#   scripts/release.sh 0.2.0 --no-push  # do everything locally, skip push
#   scripts/release.sh 0.2.0 --demo     # also re-record docs/demo.gif
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SELF="$ROOT/scripts/release.sh" # $0 may be relative to where the script was called from
cd "$ROOT"

DO_PUSH=true
DO_DEMO=false
VERSION=""
for arg in "$@"; do
  case "$arg" in
    --no-push) DO_PUSH=false ;;
    --demo)    DO_DEMO=true ;;
    -h|--help) sed -n '2,28p' "$SELF" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*)        echo "unknown argument: $arg" >&2; exit 2 ;;
    *)         VERSION="$arg" ;;
  esac
done

if [ -z "$VERSION" ]; then
  echo "error: version required, e.g. scripts/release.sh 0.2.0" >&2
  exit 2
fi
if ! printf '%s' "$VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "error: '$VERSION' is not a valid X.Y.Z version" >&2
  exit 2
fi

tag="v${VERSION}"

# --- preconditions ----------------------------------------------------------
if [ -n "$(git status --porcelain)" ]; then
  echo "error: working tree is dirty; commit or stash before releasing." >&2
  exit 1
fi
if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
  echo "error: tag ${tag} already exists." >&2
  exit 1
fi
if $DO_DEMO && [ ! -f scripts/demo/scenario.sh ]; then
  echo "error: --demo needs scripts/demo/scenario.sh, which this repository does not have." >&2
  exit 1
fi

# --- quality gate -----------------------------------------------------------
echo "==> go vet ./..."
go vet ./...
echo "==> go build ./..."
go build ./...
echo "==> go test ./..."
go test ./...

# --- sync the manifest ------------------------------------------------------
# Before the recording, so the recorded popup carries the version being
# released. Through a temp file: `sed -i` is not portable between BSD and GNU.
sed -E "s/^version = \".*\"/version = \"${VERSION}\"/" herdr-plugin.toml > herdr-plugin.toml.tmp
mv herdr-plugin.toml.tmp herdr-plugin.toml

# --- re-record the README demo GIF (opt-in) ---------------------------------
if $DO_DEMO; then
  demo_bin="${ASDEMO_BIN:-}"
  if [ -z "$demo_bin" ]; then
    demo_bin="$(command -v asdemo || true)"
  fi
  if [ -z "$demo_bin" ]; then
    git checkout -- herdr-plugin.toml
    echo "error: asdemo not found (github.com/asumaran/asdemokit); put it on PATH or set ASDEMO_BIN." >&2
    exit 1
  fi
  echo "==> Re-recording docs/demo.gif (${demo_bin})..."
  if ! "$demo_bin" record; then
    git checkout -- herdr-plugin.toml
    git checkout -- docs/demo.gif 2>/dev/null || true
    echo "error: demo recording failed; nothing committed. Fix the demo environment or release without --demo." >&2
    exit 1
  fi
fi

# --- generate changelog + release notes -------------------------------------
prev_tag="$(git tag --list 'v*' --sort=-version:refname | head -n1 || true)"
date_str="$(date +%Y-%m-%d)"

if [ -n "$prev_tag" ]; then
  log_range="${prev_tag}..HEAD"
  echo "==> Collecting commits ${prev_tag}..HEAD..."
else
  log_range="HEAD"
  echo "==> Collecting all commits (no previous tag)..."
fi

commits="$(git log --no-merges --pretty='format:* %s (%h)' "$log_range" \
  | grep -vE '^\* chore\(release\): ' || true)"
if [ -z "$commits" ]; then
  commits="* No changes since ${prev_tag:-the start}."
fi

# Prepend the new section to CHANGELOG.md.
{
  printf '## %s (%s)\n\n%s\n\n' "$tag" "$date_str" "$commits"
  cat CHANGELOG.md 2>/dev/null || true
} > CHANGELOG.md.tmp
mv CHANGELOG.md.tmp CHANGELOG.md

# Release notes: the same commit list plus a compare link to the previous tag.
origin_url="$(git config --get remote.origin.url || true)"
repo_slug="$(printf '%s' "$origin_url" | sed -E 's#^git@[^:]+:##; s#^https?://[^/]+/##; s#\.git$##')"
notes_file="$(mktemp)"
trap 'rm -f "$notes_file"' EXIT
printf '%s\n' "$commits" > "$notes_file"
if [ -n "$prev_tag" ] && [ -n "$repo_slug" ]; then
  printf '\n**Full Changelog**: https://github.com/%s/compare/%s...%s\n' \
    "$repo_slug" "$prev_tag" "$tag" >> "$notes_file"
fi

# --- apply ------------------------------------------------------------------
git add CHANGELOG.md herdr-plugin.toml
if $DO_DEMO; then
  git add docs/demo.gif # rides the release commit
fi
git commit -m "chore(release): ${tag}"
# -m keeps the tag creatable without an editor: tag.gpgsign turns every tag
# into an annotated one, and an annotated tag without a message needs a tty.
git tag -m "$tag" "$tag"

if $DO_PUSH; then
  git push origin HEAD
  git push origin "$tag"
  # The build workflow triggers on a *published GitHub release*, not on a tag
  # push, so create the release. Notes come from the generated commit list.
  gh release create "$tag" --verify-tag --notes-file "$notes_file" --title "$tag"
  echo "released ${tag}: pushed branch + tag and published the GitHub release. CI will attach the binaries."
else
  echo "released ${tag} locally (tag created, not pushed)."
  echo "The CHANGELOG entry is already committed; finish with:"
  echo "  git push origin HEAD && git push origin ${tag} && gh release create ${tag} --verify-tag --generate-notes --title ${tag}"
fi

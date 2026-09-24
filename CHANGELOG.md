## v0.22.1 (2026-09-24)

* refactor(ui): take the shared frame without the context line (de39f3b)

## v0.22.0 (2026-09-21)

* docs: describe the new shared files and flashes (3a33d40)
* refactor: share the herdr, gh and cache helpers (311696b)
* docs(readme): the counter counts matches (14a87f6)
* fix(list): count matches, report a failed action (214fd1f)
* docs: match the docs to the shared helpers (c7f42ec)
* fix(ui): filter on paste, keep enter off no row (5554dd0)
* docs(claude): follow the family's order and names (fc9c0da)
* style(ui): color a merged PR with ANSI magenta (6ba175c)
* feat(list): say why the tree is empty (dd2c8d5)

## v0.21.2 (2026-09-20)

* docs(readme): follow the family's sections (a15089c)
* refactor(list): keep the cursor in view with scrollTo (110d2c8)

## v0.21.1 (2026-09-20)

* refactor(state): keep one file per setting (d788f8f)

## v0.21.0 (2026-09-20)

* feat(ui): open an options and keys panel with f1 (10f6455)

## v0.20.0 (2026-09-20)

* feat: copy the path with ctrl+y and add -query (fab82bd)

## v0.19.0 (2026-09-20)

* refactor(state): share one state dir with the shell (0639b88)
* feat(keys): show panes with ctrl+a (daa57b7)
* feat(search): match query terms in any order (fd4c58e)

## v0.18.0 (2026-09-19)

* feat(ui): move the counter under the list (da8d812)

## v0.17.2 (2026-09-19)

* fix: shorten only paths inside the home dir (c8f099c)

## v0.17.1 (2026-09-19)

* refactor: share the last duplicated helpers (f91b6fa)

## v0.17.0 (2026-09-19)

* feat(ui): placeholder in the filter, name only standalone (9be85cb)
* feat(ui): page the list, jump to its ends, expand the help (596d7e3)
* fix(filter): stop scoring short texts higher (7acd847)
* refactor(ui): one highlight implementation for the family (7b65be1)
* fix(filter): match the query where it occurs whole (8111d66)

## v0.16.0 (2026-09-19)

* feat(ui): mark matches like asgitlog, selected row too (5ac9f42)
* docs(dev): link the plugin from the checkout with $PWD (8096cc5)
* ci: spend less time on CI and on releases (7bc0a15)

## v0.15.0 (2026-09-19)

* feat: support linux and share the release process (6c3473f)

## v0.14.0 (2026-09-19)

* refactor: rename herdr-goto to asgoto (6a270c0)

## v0.13.1 (2026-09-19)

* fix(ui): fit the quit keys in the help line (f13e7bb)

## v0.13.0 (2026-09-18)

* feat(ui): adopt the family's single-frame layout and the mouse (ea5fd8d)
* ci: run on macos only (6ed398c)

## v0.12.0 (2026-09-18)

* ci: run gofmt, vet and tests on push (ac4bfb0)
* test(tui): add a pty end-to-end check (cc07e2f)
* refactor(ui): migrate to bubble tea v2 (0a7ae1d)
* docs(readme): keep the readme user-facing (cf90844)
* feat(tui): match herdr's status indicators (dc06b38)
* feat(tui): add priority sort toggle (7de1ae1)
* docs(claude): move design notes to docs/DESIGN.md (4611847)

## v0.11.0 (2026-09-12)

* chore(scripts): re-record the demo GIF on every release (5f8d2c5)
* feat(tui): match git hints to the shell prompt (c07171b)
* chore(plugin): raise popup height to 90% (2c2c567)

## v0.10.1 (2026-09-11)

* chore(demo): refresh the README GIF with yellow ports and aligned columns (7e44893)
* fix(tui): focus shell and process panes via pane.focus (01d1cae)

## v0.10.0 (2026-09-11)

* chore(demo): refresh the README GIF with branch labels and git hints (d7951ab)
* feat(tui): label worktrees by branch, add git hints (b5983a2)

## v0.9.1 (2026-09-09)

* fix(procs): use argv0 when argv is unreadable (34f7edb)
* chore(demo): refresh the README GIF (a6f0ab3)

## v0.9.0 (2026-09-09)

* fix(release): pass a tag message so signed tags work non-interactively (2431a44)
* chore(demo): show a running dev server with its port in the README GIF (1aab9c4)
* chore(plugin): widen popup to 55% (9417f8f)
* feat(tui): list running processes with their ports under each space (19b451d)
* chore(demo): record the README GIF with herdr-demokit (8e2b9d4)
* docs: document popup size overrides and post-release install flow in CLAUDE.md (9a944b1)

## v0.8.0 (2026-07-28)

* docs: sync behaviour section with current sorting and PR prefixes (7c447d7)
* docs: embed the demo GIF in the README (175007c)
* feat(demo): add scripted README demo recording (a98caf8)
* feat(plugin): allow popup size overrides via env in open-pane.sh (4dcfa73)
* fix(ui): show ctrl+p/ctrl+n in the help footer (7fee16e)
* docs: retire the legacy install channel (ec973ca)
* chore(scripts): drop legacy fixed-path install scripts (b404173)
* docs: document the herdr plugin channel and wiring (4c11d1d)
* feat(plugin): package as a herdr plugin (a901f5a)
* feat(state): store runtime state in HERDR_PLUGIN_STATE_DIR when present (b82d1bc)

## v0.7.0 (2026-07-08)

* feat(ui): replace 1-9 digit jump with ticket and PR number search (c8e233e)

## v0.6.0 (2026-07-08)

* feat(ui): sort worktrees oldest-first by checkout creation time (4ed71ae)

## v0.5.1 (2026-07-05)

* fix(ui): align unnumbered repos and their children with numbered ones (5049446)

## v0.5.0 (2026-07-04)

* feat(ui): prefix rows with Jira ticket and PR number colored by state (32062c9)

## v0.4.0 (2026-07-02)

* feat(search): match git branch names in addition to labels (3a1277f)

## v0.3.0 (2026-06-30)

* feat(ui): show agent status dots in the sidebar gutter (67ecd61)

## v0.2.0 (2026-06-30)

* feat: mark dev builds in the prompt (a8fa20a)
* chore: add MIT license, qualify module path, gofmt (308b62f)

## v0.1.0 (2026-06-30)

* feat: herdr-goto tree switcher with automated releases (fb5ba6b)


# Changelog

All notable changes to Yon are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Yon adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.0] - 2026-06-06

### Added
- **Image & PDF response previews** ([#16]) — when a response is an image
  (`image/*`, or detected from its magic bytes), Yon renders it inline, scaled to
  fit, with the pixel dimensions in the response meta line; **Raw** still shows the
  exact bytes. A **PDF** response (`application/pdf` or a `%PDF-` signature) shows a
  panel with **Save…** and **Open** (Open hands the file to your OS viewer). No PDF
  renderer or third-party dependency is added. An explicit textual `Content-Type` is
  always trusted, so a text body that merely starts with `BM`/`GIF8`/`%PDF-` is never
  mistaken for a binary.

### Changed
- **Tunnels…** moved from the *Collection* menu to the **View** menu ([#15]), next
  to Variables and Request Log — it opens a window, like the other View items.

## [1.1.1] - 2026-06-05

### Fixed
- The macOS app menu now reads **Yon** (the bundle executable is named `Yon`),
  not a lowercase `yon` / *About yon* ([#8]).

### Changed
- App identifier rebranded from `com.ultramcu.yon` to **`th.co.asr.yon`** now that
  the project lives under asrth ([#11]). (Existing installs start with fresh
  settings once; signing/notarization is unaffected.)

### Docs
- README refreshed with the full feature list — SSH jump host, captures +
  assertions, the variables inspector, per-request overrides, New Request from
  cURL, and the request log — plus a Highlights section. Added **CONTRIBUTING.md**:
  open an approved issue first, then branch → PR.

## [1.1.0] - 2026-06-05

### Added
- **Variables panel: copy values** ([#1]) — each variable shows as a `KEY : VALUE`
  row; **double-click a row to copy its value** to the clipboard (secrets are
  masked on screen but the real value is copied).
- **Request log: detail view** ([#2]) — the bottom request log is now a list, and
  **double-clicking a row opens a detail window** with the full request (method,
  URL, headers, body sent) and response (status, headers, body), JSON
  pretty-printed.

### Changed
- Sidebar **Save / Save As… / New Folder / New Request** now render as proper
  buttons (they previously looked like flat text).
- The **About** box links to the new home **github.com/asrth/yon** and shows
  *Powered by ADVANCED SOFTWARE AND ROBOTICS CO., LTD.*
- The app name shows as **Yon** in the macOS menu bar in every build mode.

## [1.0.1] - 2026-06-05

### Changed
- macOS builds are now **code-signed (Developer ID) and notarized** by Apple, so
  the `.dmg` opens without the Gatekeeper *“cannot verify”* warning.

## [1.0.0] - 2026-06-05

First stable release. New home: the project moved to
**https://github.com/asrth/yon** — the in-app update check now points there.

### Added
- **Response captures + assertions** ([#27]) — extract values from a response
  into `{{variables}}` for later requests (chaining), and assert on status /
  JSON path / header / time, all declarative (no scripting).
- **Combined request log** ([#30]) — a copyable, resizable dock at the bottom
  logging every send across all tabs.
- **Variables inspector** ([#29]) — a toggleable right-side panel showing the
  active environment, collection, and captured (runtime) variables; secrets
  masked.
- **cURL as a tab** ([#28]) — the equivalent curl command moved from a dialog to
  a live tab in the request editor.

### Changed
- Repository moved from `ultramcu/yon` (now archived) to `asrth/yon`; the update
  checker and download links point to the new home.

## [0.14.0] - 2026-06-05

### Added
- **Per-request overrides** ([#26]) — a new **Options** tab in the request editor
  lets a Request override the global *timeout*, *Allow insecure TLS*, and *Follow
  redirects* settings; left on *Default*, each inherits the global Setting.
  Overrides also flow into *Copy as cURL* (`--max-time` / `-k` / `-L`). Requests
  with no overrides are unchanged on disk.

## [0.13.2] - 2026-06-04

### Fixed
- **Secret variable names containing `=` or surrounding spaces now persist
  correctly** ([#25]). Such names were mangled when written to `.env` (the value
  loaded back empty); `.env` keys are now quoted/escaped when needed, while
  ordinary names stay unquoted and existing files are unaffected.

## [0.13.1] - 2026-06-04

### Added
- **Duplicate an Environment** ([#23]) — a *Duplicate* button in the Environment
  manager clones the selected environment (its variables, including secrets, and
  its SSH jump host) under a unique name, ready to edit.

### Fixed
- **Secret variables with the same name no longer collide across Environments**
  ([#24]). A secret's value was stored in the shared `.env` keyed by the bare
  variable name, so two Environments that each defined a same-named secret shared
  one slot — the later save silently overwrote the earlier value. Secrets are now
  namespaced per environment (existing `.env` files keep working and migrate on
  the next save).

## [0.13.0] - 2026-06-04

### Added
- **New Request from cURL** ([#22]) — *File ▸ New Request from cURL…* opens a
  paste box; Yon parses the `curl` command into a Request (the inverse of *Copy
  as cURL*) and opens it. Supports method (`-X`), URL, headers (`-H`), body
  (`-d` / `--data*` / `--data-urlencode`), `-G` query upload, Basic (`-u`) and
  Bearer auth, and maps `Content-Type` to the body type; noise flags (`-L`,
  `-k`, `--compressed`, …) are tolerated. Pure Go — no shell-out, no filesystem
  access, no new dependencies.

## [0.12.0] - 2026-06-04

### Added
- **SSH jump host per Environment** ([#21]). An Environment can define an SSH
  jump host; when it is active, Yon dials every Request through one in-process
  SSH Tunnel (`golang.org/x/crypto/ssh`) — the URL is never rewritten.
  - Environment manager gains an *SSH jump host* section: host / port / user,
    private-key (path + passphrase) or password auth, and a *Skip host key
    check* toggle. Passphrase and password are stored as Secrets in the
    gitignored `.env`, never in the committed Collection.
  - **Tunnels window** (`Collection ▸ Tunnels…`): a live table of every Tunnel
    (state, refcount, uptime, last error) with a Disconnect button; the next
    send reconnects automatically.
  - **Footer indicator** beside the version: a coloured dot and the active
    environment's name when a jump host is configured; click it to open the
    Tunnels window.
  - **Host-key verification (trust on first use):** an unknown host shows a
    fingerprint confirmation, and a changed key on a known host is hard-rejected.
    Yon reads `~/.ssh/known_hosts` read-only and never writes to it.

### Notes
- Jump-host fields are resolved against the active Environment's variables; an
  incomplete config (an unresolved `{{var}}`) never dials.
- The HTTP proxy is bypassed while a jump host is active.
- No external `ssh` binary is required — Yon stays a single download.

## [0.11.2] - 2026-06-04

### Added
- **Reorder requests by drag & drop** — drag a request in the sidebar to
  reorder it, drop it onto a folder to move it in, or onto a top-level row to
  move it out. (Drag is paused while a sidebar filter is active.)
- The app **version** now shows at the far left of the bottom status bar.

## [0.11.1] - 2026-06-04

### Added
- **Filter requests** — a sidebar search box filters requests as you type
  (name, method, or URL). Folders containing a match expand; non-matching
  folders are hidden. Clearing the box restores the full list.

## [0.11.0] - 2026-06-04

### Added
- **Folders** — organize a collection's requests into one level of folders,
  collapse/expand to hide them. New Folder button; right-click a request →
  *Move to folder…*; right-click a folder → *Rename* / *Delete* (deleting a
  folder keeps its requests, moving them back to top level). Collapse state is
  saved with the collection.
- **Duplicate a request** — right-click → *Duplicate* (an independent copy
  inserted right after the original).

### Changed
- The response **Headers** moved into a tab, so the **Body** now uses the full
  pane (Body | Headers tabs).

## [0.10.5] - 2026-06-04

### Fixed
- **Clicking a request in the sidebar opens it again** — fixes a v0.10.4
  regression where left-click no longer opened a request as a tab (right-click →
  Delete was unaffected).

### Internal
- Release builds retry asset uploads (absorbing transient GitHub API timeouts)
  and no longer use a deprecated GitHub Action.

## [0.10.4] - 2026-06-03

### Added
- **Delete a request** — right-click a request in the sidebar → *Delete* (with
  confirmation). Open tabs and selection stay consistent.

### Fixed
- **Session restore** now works when you quit by closing the window (not just
  Cmd/Ctrl+Q): reopening Yon brings back your last collection and its open tabs.

## [0.10.3] - 2026-06-03

### Fixed
- **Rename Collection** prefills the dialog with the current name (previously
  blank for name-less files, which felt like creating a new collection); the
  confirm button reads *Rename*.
- The **status bar** shows just the path of a resolved URL even when the
  `{{server}}` value has no scheme (e.g. `host:port/usage` → `/usage`).

### Internal
- Releases stay a **draft until all platform binaries are uploaded**, then
  auto-publish — so the in-app *Check for Updates* never offers a version whose
  download isn't ready yet.

## [0.10.2] - 2026-06-03

### Added
- **Rename a collection** — *Collection ▸ Rename Collection…* sets the
  collection's own name (window title and sidebar header), independent of the
  file name. Clearing it falls back to the file name.

## [0.10.1] - 2026-06-03

### Added
- **Save shortcuts** — Cmd/Ctrl+S saves the collection, Cmd/Ctrl+Shift+S is
  Save As (they work even while a text field is focused).
- **Check for Updates** shows a "Checking for updates…" progress indicator.

### Fixed
- **Secret variables** resolve to their value (they were resolving to empty —
  e.g. a blank `token=` in the built request / cURL).
- The **status bar** shows the real path with `{{variables}}` resolved, not the
  literal `{{key}}`.
- The **macOS build** registers the `.yon` file type, so double-clicking a
  collection opens it in Yon.

## [0.10.0] - 2026-06-03

### Added
- **URL ⇄ Params live sync** — pasting a URL with a `?query` fills the **Params**
  table; editing the table rewrites the URL; unchecking a row drops it from the
  URL while keeping it in the table. Special characters round-trip safely and
  `{{templates}}` stay readable.
- **Double-click to open** a `.yon` collection from Finder / Explorer / your
  file manager (register the file type with the scripts in `packaging/`).

### Fixed
- **Copy as cURL** expands `{{variables}}` from the active environment and no
  longer mangles unresolved templates into `%7B%7B…`.
- The collection header no longer shows *Untitled* for a name-less `.yon` opened
  from disk — it falls back to the file name.

## [0.9.4] - 2026-06-03

### Changed
- The **About Yon** dialog shows the Thai name **(โยน)** under the Yon title.

## [0.9.3] - 2026-06-03

### Changed
- After an in-app update downloads, Yon asks before installing: **Open & Quit**
  opens the downloaded installer and quits Yon, or **Later** just reveals the
  file (you can't replace a running app while it's open).

## [0.9.2] - 2026-06-03

### Changed
- The **macOS download is now a drag-to-install `.dmg`** that mounts showing
  Yon.app next to an Applications shortcut (still a universal binary).

## [0.9.1] - 2026-06-03

### Fixed
- **Update check on macOS** reported "development build" because the released
  macOS binary didn't carry its version. The version is now embedded from
  `FyneApp.toml` so every build/platform reports it correctly.

## [0.8.0] - 2026-06-03

### Added
- **Environments & variables** — define `{{variables}}` in named environments
  (Local / Prod…) and at the collection level, switch the active environment
  from a toolbar selector, and reference them in the URL, query params, headers,
  body and auth.
- **Secrets stay out of committed files** — secret values live in a gitignored
  `.env`, blanked in committed environment files, never in the `.yon`.
- Dynamic values: `{{$uuid}}`, `{{$timestamp}}`, `{{$isoTimestamp}}`,
  `{{$randomInt}}`.

### Changed
- **Redesigned request tabs** — browser-style cards with coloured method chips,
  a dirty dot, per-tab close, and horizontal overflow scrolling. Cmd/Ctrl+W
  closes the active tab; the method dropdown is wider.

## [0.7.0] - 2026-06-03

### Added
- **More HTTP methods** — PATCH, HEAD, OPTIONS, plus any custom verb you type.
- **XML support** — a dedicated XML request body (auto `application/xml` + a
  Format button) and a response viewer that pretty-prints and syntax-highlights
  XML and HTML (in addition to JSON), preserving comments, namespaces and
  mixed-content whitespace.
- Test server gained `/xml`, `/html` and `/soap` endpoints with matching
  requests in `testserver.yon`.

## [0.6.0] - 2026-06-02

### Added
- **Import Collection (JSON)** — *File ▸ Import Collection (JSON)…* brings in
  requests from a Collection v2.1 JSON export; anything Yon can't represent is
  reported in a summary.

### Fixed
- Transparent app-icon corners (no more white corners on the rounded icon).
- **macOS menus** — *About* and *Settings…* live in the application (Yon) menu;
  no more duplicate app menu.
- Dev/un-packaged builds no longer claim to be out of date in the update check.

## [0.5.0] - 2026-06-02

### Added
- **About Yon** — a *Yon ▸ About Yon* dialog showing the app icon, version,
  slogan, a short description, a source link, and the licence.

## [0.4.0] - 2026-06-02

### Added
- **Update checker (opt-in)** — *Check for Updates…* queries GitHub for a newer
  release and, if one exists, downloads the build for your OS to Downloads and
  reveals it. A Settings toggle adds an **off-by-default** startup check, keeping
  the offline / no-telemetry promise.

## [0.3.0] - 2026-06-01

### Added
- **Text search in responses (Cmd/Ctrl+F)** — find a string in the response body
  with match highlighting, an *n/m* counter, prev/next navigation, and Esc to
  close. Works in the main and pop-out windows.
- **Pop-out response window** (⤢) — view a large response in its own resizable
  window; drag-select & copy, *Save Output As…*, and Cmd/Ctrl+F search.
- **Edit menu** — Copy / Paste / Find… from the menu bar.

### Internal
- CI: bumped `actions/checkout` → v5 and `actions/setup-go` → v6 (Node 24).

## [0.2.0] - 2026-06-01

### Added
- **Native OS file dialogs** — Open / Save As / Save-response use the native
  Finder / Explorer / GTK dialog (falls back to the in-app dialog if
  unavailable).
- **Copy as cURL** — generate the equivalent `curl` command for any request.
- **Save / Save As buttons** in the sidebar toolbar.
- Bundled HTTP **test server** (`testserver/`) and a ready-made collection.

## [0.1.0] - 2026-05-31

### Added
- Initial release of Yon — an offline desktop client for testing HTTP APIs.
  Build requests (method, URL, params, headers, auth, body), send them, and view
  the response; save and reopen Collections as human-readable `.yon` files.
  Native builds for macOS (universal), Windows, and Linux.

[1.1.1]: https://github.com/asrth/yon/releases/tag/v1.1.1
[1.1.0]: https://github.com/asrth/yon/releases/tag/v1.1.0
[1.0.1]: https://github.com/asrth/yon/releases/tag/v1.0.1
[1.0.0]: https://github.com/asrth/yon/releases/tag/v1.0.0
[0.14.0]: https://github.com/ultramcu/yon/releases/tag/v0.14.0
[0.13.2]: https://github.com/ultramcu/yon/releases/tag/v0.13.2
[0.13.1]: https://github.com/ultramcu/yon/releases/tag/v0.13.1
[0.13.0]: https://github.com/ultramcu/yon/releases/tag/v0.13.0
[0.12.0]: https://github.com/ultramcu/yon/releases/tag/v0.12.0
[0.11.2]: https://github.com/ultramcu/yon/releases/tag/v0.11.2
[0.11.1]: https://github.com/ultramcu/yon/releases/tag/v0.11.1
[0.11.0]: https://github.com/ultramcu/yon/releases/tag/v0.11.0
[0.10.5]: https://github.com/ultramcu/yon/releases/tag/v0.10.5
[0.10.4]: https://github.com/ultramcu/yon/releases/tag/v0.10.4
[0.10.3]: https://github.com/ultramcu/yon/releases/tag/v0.10.3
[0.10.2]: https://github.com/ultramcu/yon/releases/tag/v0.10.2
[0.10.1]: https://github.com/ultramcu/yon/releases/tag/v0.10.1
[0.10.0]: https://github.com/ultramcu/yon/releases/tag/v0.10.0
[0.9.4]: https://github.com/ultramcu/yon/releases/tag/v0.9.4
[0.9.3]: https://github.com/ultramcu/yon/releases/tag/v0.9.3
[0.9.2]: https://github.com/ultramcu/yon/releases/tag/v0.9.2
[0.9.1]: https://github.com/ultramcu/yon/releases/tag/v0.9.1
[0.8.0]: https://github.com/ultramcu/yon/releases/tag/v0.8.0
[0.7.0]: https://github.com/ultramcu/yon/releases/tag/v0.7.0
[0.6.0]: https://github.com/ultramcu/yon/releases/tag/v0.6.0
[0.5.0]: https://github.com/ultramcu/yon/releases/tag/v0.5.0
[0.4.0]: https://github.com/ultramcu/yon/releases/tag/v0.4.0
[0.3.0]: https://github.com/ultramcu/yon/releases/tag/v0.3.0
[0.2.0]: https://github.com/ultramcu/yon/releases/tag/v0.2.0
[0.1.0]: https://github.com/ultramcu/yon/releases/tag/v0.1.0
[#1]: https://github.com/asrth/yon/issues/1
[#2]: https://github.com/asrth/yon/issues/2
[#8]: https://github.com/asrth/yon/issues/8
[#11]: https://github.com/asrth/yon/issues/11
[#15]: https://github.com/asrth/yon/issues/15
[#16]: https://github.com/asrth/yon/issues/16

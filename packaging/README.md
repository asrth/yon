# Packaging & file association

Yon already opens any `.yon` collection passed on the command line
(`yon mycollection.yon`). The files here register the `.yon` type with each OS so
that **double-clicking a collection in the file manager opens it in Yon**.

How the path reaches the app differs per platform:

| OS | Double-click delivers the path via… | Handled in |
|---|---|---|
| **macOS** | a `kAEOpenDocuments` Apple Event (not the command line) | `internal/ui/openfiles_darwin.{go,m}` |
| **Windows** | the command line (`yon.exe "%1"`) | `main.go` → `App.Run` |
| **Linux** | the command line (`.desktop` `Exec=yon %F`) | `main.go` → `App.Run` |

## macOS

```sh
sh packaging/macos/package.sh
```

Builds `Yon.app` with `fyne package`, then injects `CFBundleDocumentTypes` and an
exported UTI (`th.co.asr.yon.collection`, conforming to `public.json`) into the
bundle's `Info.plist`. Move the app to `/Applications` (or `open Yon.app` once) so
Launch Services records the association. Then double-clicking a `.yon` file opens
it; the in-app handler turns the Apple Event into a normal `OpenPath` call.

## Windows

Ship `register-filetype.ps1` alongside `yon.exe` in the release zip. After
extracting, the user runs:

```powershell
powershell -ExecutionPolicy Bypass -File register-filetype.ps1
```

This writes a per-user (`HKCU`) association mapping `.yon` → a `Yon.Collection`
ProgID whose open command is `"<path>\yon.exe" "%1"`. No admin rights required.

## Linux

```sh
sh packaging/linux/install-filetype.sh
```

Installs `application-x-yon.xml` (defines `application/x-yon`, glob `*.yon`) and
`yon.desktop` (`Exec=yon %F`) under `~/.local/share`, then refreshes the MIME and
desktop databases. Make sure `yon` is on `PATH` (e.g. `~/.local/bin/yon`).

## Cutting a release

The release is kept a **draft** until every platform's asset is uploaded, then
auto-published — so an in-app update check never sees a version it can't yet
download. Steps:

1. Bump `FyneApp.toml` (Version + Build), commit `Release vX.Y.Z`, push `main`.
2. Create the **draft** release with notes (this does *not* create the tag yet):
   ```sh
   gh release create vX.Y.Z --draft --target main --title "Yon vX.Y.Z" --notes-file notes.md
   ```
3. Push the tag to trigger the build:
   ```sh
   git tag vX.Y.Z && git push origin vX.Y.Z
   ```
4. `release.yml` builds all three platforms and uploads each asset to the draft
   with `gh release upload --clobber` (retried up to 3× for transient
   `api.github.com` timeouts); the `publish` job flips the release to public once
   all builds succeed. If any platform fails, the release stays a draft — just
   re-run the failed job (`gh run rerun <id> --failed`); `--clobber` makes the
   retry idempotent, then publish runs.

## Known limitations (verify on a real install)

These can't be exercised by `go test` — they need a packaged, OS-registered app —
so confirm them when cutting a release:

- **macOS cold-launch race:** the handler is registered before the run loop and
  again from `OnStarted`; if the open-document event is delivered in the narrow
  window before `OnStarted` re-registers, that first double-click may be missed
  (warm launches are reliable). There is no event buffering.
- **macOS alias descriptors:** the handler coerces each Apple Event item to
  `typeFileURL`. Modern Finder sends file-URL items; if an older path delivers an
  alias the coercion can return nil and the item is skipped (an `NSLog` records
  it). Re-check on the oldest supported macOS.
- **Linux/macOS JSON subclassing:** `.yon` is declared a subclass of JSON
  (`public.json` / `application/json`). A user's default JSON handler can appear
  in "Open with"; on some desktops it could shadow Yon. Drop the subclass if that
  becomes a problem.

## macOS code signing & notarization

Release builds are **ad-hoc signed** by default — this fixes the *"app is
damaged"* error on Apple Silicon, but macOS still shows a *"cannot verify…"*
Gatekeeper prompt (users click **Open Anyway** in System Settings ▸ Privacy &
Security, or run `xattr -dr com.apple.quarantine /Applications/Yon.app`).

To ship a **notarized** build with no Gatekeeper warning, add these repository
secrets (Settings ▸ Secrets and variables ▸ Actions) — the release workflow then
signs with the hardened runtime and notarizes + staples the `.dmg` automatically
(`packaging/macos/entitlements.plist` is used for signing):

| Secret | Value |
|---|---|
| `MACOS_CERTIFICATE` | base64 of your **Developer ID Application** `.p12` |
| `MACOS_CERTIFICATE_PWD` | password for that `.p12` |
| `MACOS_SIGN_IDENTITY` | e.g. `Developer ID Application: Your Name (TEAMID)` |
| `APPLE_ID` | Apple ID email used for `notarytool` |
| `APPLE_TEAM_ID` | 10-character Apple Team ID |
| `APPLE_APP_PASSWORD` | an app-specific password for that Apple ID |

Requires a paid **Apple Developer** membership. Without the secrets the workflow
falls back to ad-hoc signing, so releases keep working either way.

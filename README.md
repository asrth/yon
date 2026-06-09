<p align="center">
  <img src="assets/logo/yon-icon-512.png" alt="Yon" width="128" height="128">
</p>

<h1 align="center">Yon</h1>

<p align="center"><b>Throw a request. Catch a response.</b></p>

<p align="center">
A fast, lightweight, <b>open-source</b> desktop client for testing HTTP APIs.
A single native binary for macOS, Windows, and Linux — no account, no cloud,
no telemetry. Your work stays in plain files on your own disk.
</p>

<p align="center">
  <img src="assets/screenshot.png" alt="Yon — a GET request and its pretty-printed JSON response, with find-in-response highlighting" width="900">
</p>

---

## Why Yon

Yon is small on purpose. It's a tiny native app that opens instantly, never asks
you to sign in, and keeps everything offline in human-readable files you can commit
to git. Free and open source under the MIT licence — read it, fork it, ship it. It
does the everyday job — fire a request, read the response — and gets out of your way.

The name is Thai: **โยน (yon)** = *to throw*. You throw a request; you catch a response.

## Features

> **Highlights:** chain requests with **captures + assertions**, reach private APIs
> through an **SSH jump host**, inspect every call in the **request log**, see what's
> in scope in the **variables inspector**, and go both ways with **cURL** — all
> offline, in plain `.yon` files.

- **Methods:** GET · POST · PUT · DELETE · PATCH · HEAD · OPTIONS — or type any custom verb
- **Organize your requests:** group them into **folders** you can collapse to cut
  clutter, **drag & drop** to reorder or move between folders, and **filter** the
  sidebar by name / method / URL as you type. Right-click a request to **Duplicate**,
  **Move to folder…**, or **Delete**; rename folders and the collection itself
- **Request editor in tabs:** Params · Headers · Auth · Body. **Query parameters** and
  **headers** are toggleable key/value tables — the Params table and the URL bar
  **stay in sync both ways**: paste a URL with a `?query` and the pairs fill the table
  automatically, edit the table and the URL updates, and unchecking a row drops it from
  the URL while keeping it in the table
- **Auth:** None · Basic · Bearer — set a default on the collection and inherit or
  override it per request
- **Environments & variables:** `{{variables}}` in named environments (Local / Prod…)
  and collection-level variables, switched from a toolbar selector and substituted in
  the URL, params, headers, body and auth. Mark a variable **secret** to keep its value
  out of committed files (stored in a gitignored `.env`); plus dynamic
  `{{$uuid}}` / `{{$timestamp}}`
- **SSH jump host (tunnel):** give an environment an **SSH jump host** and Yon dials
  every request *through* a live, in-process SSH tunnel — reach APIs only visible from
  a bastion, with the request URL never rewritten. Key or password auth, host-key
  **trust-on-first-use**, a live **Tunnels** view (Collection ▸ Tunnels…) and a footer
  status dot; jump-host secrets stay in the gitignored `.env`
- **Chain requests — captures & assertions:** **capture** a value from a response
  (JSON path or header) into a `{{variable}}` the next request uses, and **assert** on
  the response (status · JSON path · header · response time) — declarative, no
  scripting. Pass/fail shows in the response's **Tests** tab
- **Variables inspector:** a toggleable side panel listing every variable in scope —
  environment, collection, and captured runtime values — as `KEY : VALUE`;
  double-click a row to copy its value (secrets masked on screen)
- **Per-request overrides:** override the global timeout, *Allow insecure TLS*, and
  *Follow redirects* for a single request from its **Options** tab
- **Body:** None · JSON (auto `Content-Type` + pretty) · XML (auto `Content-Type` + format) · Text
- **Response in tabs — Body & Headers:** the body uses the full pane, with a **Pretty**
  (indented and syntax-coloured for JSON, XML and HTML) / **Raw** toggle and one-click
  **Copy**; huge bodies render fast (virtualized) and are capped on screen with a
  *Save Output As…* escape hatch. The footer shows status · time · size and the app version
- **Find in response (⌘F / Ctrl+F):** search the body with match highlighting, a
  match counter, next/previous navigation, and Esc to close
- **Pop-out window** (⤢): open a response in its own resizable window — drag-select
  and copy text, **Save Output As…** for the full body, or search with highlighting
- **cURL both ways:** **Copy as cURL** turns any request into a ready-to-run `curl`
  command (a live tab, `{{variables}}` resolved); **New Request from cURL** does the
  inverse — paste a `curl` command and Yon builds the request (method, URL, headers,
  body, auth)
- **Request log:** a combined, copyable log of every send across all tabs — **double-click
  a row** to open a detail window with the full request and response (headers + body)
- **Send / Cancel** any in-flight request — or press **Enter** in the URL bar to send
- **Collections** saved as plain-JSON `.yon` files — one window per collection, one
  tab per request; **⌘S / Ctrl+S** to Save and **⌘⇧S / Ctrl+Shift+S** for Save As,
  plus native open dialogs and **Open Recent**. Your open collections and tabs are
  restored on the next launch. **Double-click a `.yon` file** in Finder/Explorer/your
  file manager to open it in Yon once the file type is registered (see
  [`packaging/`](packaging/README.md))
- **Import Collection (JSON):** bring in requests from a Collection v2.1 JSON export —
  anything Yon can't represent is reported
- **Edit menu:** Copy · Paste · Find…
- **Themes:** Dark Pro / Warm / System, switchable in Settings
- **Connection settings:** request timeout, follow redirects, system proxy support,
  and an *Allow insecure TLS* toggle for self-signed dev APIs
- **Updates:** a manual *Check for Updates…* (plus an opt-in startup check, **off by
  default**) that finds a newer GitHub release and downloads the build for your OS to
  your Downloads folder — no telemetry, it only reaches the network when you ask

## Install

Download a prebuilt build from the [latest release](https://github.com/asrth/yon/releases/latest):

| Platform | Download |
|---|---|
| **macOS** (universal — Apple Silicon + Intel) | `Yon-*-macos.dmg` — open it and drag **Yon** into **Applications** |
| **Windows** | `Yon-*-windows.zip` |
| **Linux** | `Yon-*-linux.tar.xz` |

> macOS builds aren't notarized by Apple. If you see **“Yon” Not Opened — Apple
> could not verify it is free of malware**, open **System Settings ▸ Privacy &
> Security**, scroll down and click **Open Anyway**, then open Yon again. Or run
> this once in Terminal: `xattr -dr com.apple.quarantine /Applications/Yon.app`.

Or run / build from source (below).

## Run / build from source

Requires **Go 1.26+** and a C toolchain (Fyne uses cgo/OpenGL).

```sh
git clone https://github.com/asrth/yon
cd yon
go run .
```

Build a binary:

```sh
go build -o yon .
```

Open a collection on launch by passing its `.yon` file (one window per file; this
takes precedence over restoring the previous session):

```sh
yon mycollection.yon
yon api/users.yon api/billing.yon
```

Package a native app bundle with the embedded icon (optional, needs the Fyne CLI):

```sh
go run fyne.io/tools/cmd/fyne package -os darwin   # or windows / linux
```

### Double-click to open

To make a double-clicked `.yon` file launch Yon and load that collection, register
the file type with your OS — see [`packaging/`](packaging/README.md):

```sh
sh packaging/macos/package.sh                                          # macOS
powershell -ExecutionPolicy Bypass -File packaging/windows/register-filetype.ps1   # Windows
sh packaging/linux/install-filetype.sh                                 # Linux
```

## The `.yon` file

A collection is one human-readable JSON file you can keep in git:

```json
{
  "version": 1,
  "name": "My API",
  "auth": { "kind": "bearer", "token": "..." },
  "requests": [
    {
      "name": "List users",
      "method": "GET",
      "url": "https://api.example.com/users",
      "params": [{ "key": "page", "value": "1", "enabled": true }],
      "auth": { "kind": "inherit" }
    }
  ]
}
```

## Architecture

The core is UI-free and fully testable; Fyne lives only in the front end.

| Package | Role |
|---|---|
| `internal/model` | data types + auth resolution (no Fyne, no net) |
| `internal/yonner` | builds and sends the HTTP request (the engine) |
| `internal/store` | reads/writes `.yon` files |
| `internal/ui` + `main.go` | the Fyne desktop UI (the only Fyne importers) |

## Roadmap (post-v1)

Import from `.http` files · **form-data & multipart** bodies ·
[**OAuth 2.0** auth](https://github.com/asrth/yon/issues/30) · pre-request &
post-response **scripting** · WebSocket / SSE.

(Import from **OpenAPI / Swagger** shipped in v1.4.0.)

## License

MIT © 2026 **ADVANCED SOFTWARE AND ROBOTICS CO., LTD.**
Contact: [contact@asr.co.th](mailto:contact@asr.co.th)

# testserver

A tiny stdlib-only HTTP server that exercises every feature of **Yon**, plus
`testserver.yon` — a ready-made collection pointing at it. It includes a
**Tests & Chaining** folder that demonstrates the newer features: response
**assertions** (Tests tab), **capturing** a value into a variable, **chaining**
it into a follow-up request (`{{userId}}`), and a **per-request override**
(don't follow a redirect).

## Run

From the repo root:

```sh
go run ./testserver          # listens on http://localhost:7878
```

Then in Yon: **File → Open** → `testserver/testserver.yon` (or `yon testserver/testserver.yon`).

## Collection layout

`testserver.yon` keeps **every** Yon feature exercisable, with requests grouped
into one folder per area (no loose top-level requests):

- **Methods & Echo** — GET / POST / PUT / DELETE / PATCH / Headers
- **Auth (Bearer / Basic)**
- **Status, Redirect & Overrides** — status codes, 302 redirect, per-request "don't follow"
- **Body & Formats** — JSON / XML / HTML / SOAP (Pretty + highlighting)
- **Large & Slow** — >256 KB truncation, slow endpoint for Cancel / timeout
- **Media — image & PDF (#16)** — inline image preview, PDF panel, magic-byte sniffing
- **Tests & Chaining** — assertions, capture, chaining a captured `{{userId}}`

> Convention: when a **new feature** lands, add its endpoint(s) here **and** a
> request (in the right folder) to `testserver.yon`, so this stays a complete,
> always-current manual-test harness.

## Endpoints

| Endpoint | Purpose |
|---|---|
| `/get` `/post` `/put` `/delete` | echo method, query, headers, body as JSON |
| `/headers` | echo request headers |
| `/basic-auth/{user}/{pass}` | requires Basic auth (`alice` / `secret`) |
| `/bearer` | requires `Authorization: Bearer yon-demo-token` |
| `/status/{code}` | returns that status code |
| `/redirect` | 302 → `/get` (tests follow-redirect) |
| `/large` | ~600 KB JSON (tests the 256 KB display truncation) |
| `/slow?seconds=N` | sleeps N s, honouring cancellation (tests Cancel / timeout) |
| `/json` | nested JSON sample (tests Pretty syntax colouring) |
| `/xml` `/html` `/soap` | XML / HTML / SOAP documents (tests Pretty formatting + highlighting) |
| `/image/png` `/image/jpeg` `/image/gif` | a 240×160 gradient image (tests the inline image preview, #16) |
| `/image/octet` | a PNG served as `application/octet-stream` (tests magic-byte sniffing) |
| `/pdf` | a minimal valid one-page PDF (tests the PDF Save…/Open panel, #16) |
| `/pdf/octet` | the same PDF as `application/octet-stream` (tests magic-byte sniffing) |
| `/text-bm` | `text/plain` body starting with `"BM"` — must stay **text**, not a broken image (the #16 detection regression) |

The bundled collection groups the last five under a **Media — image & PDF (#16)**
folder so you can click through the previews. The `/text-bm` request also carries
an assertion checking its `Content-Type` is `text/plain`.

## Demo credentials

- Bearer token: `yon-demo-token` (set as the collection-level default auth)
- Basic: `alice` / `secret`

## Tests

`main_test.go` mounts the routes on an `httptest` server and verifies every
endpoint (echo, Basic/Bearer auth, status codes, redirect, the >256 KB body,
slow-endpoint cancellation, the XML/HTML/SOAP documents, the decodable
image/PDF bodies, and the `"BM"`-prefixed text contract) — run with
`go test ./testserver`.

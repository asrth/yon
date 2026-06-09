// Command testserver is a tiny stdlib-only HTTP server that exercises every
// feature of Yon: the four methods, query/header echo, Basic and Bearer auth,
// redirects, arbitrary status codes, a >256 KB body (display truncation), and a
// slow endpoint (cancel/timeout). Run it and load testserver.yon in Yon.
//
//	go run ./testserver        # listens on :7878
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const addr = ":7878"

// Demo credentials the bundled testserver.yon collection uses.
const (
	demoBearer   = "yon-demo-token"
	demoUser     = "alice"
	demoPassword = "secret"

	// OAuth 2.0 (#30) client_credentials demo: the client authenticates to
	// /oauth/token with these and receives demoOAuthToken, which /oauth/protected
	// then requires as a Bearer.
	demoOAuthClientID     = "yon-client"
	demoOAuthClientSecret = "yon-secret"
	demoOAuthToken        = "yon-oauth-access-token"
)

func main() {
	log.Printf("testserver listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, newMux()))
}

// newMux builds the routing table. Extracted from main so tests can mount it on
// an httptest server.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()

	for _, p := range []string{"/get", "/post", "/put", "/delete", "/anything", "/headers"} {
		mux.HandleFunc(p, echo)
	}
	mux.HandleFunc("/basic-auth/{user}/{pass}", basicAuth)
	mux.HandleFunc("/bearer", bearerAuth)
	// OAuth 2.0 (#30): a client_credentials token endpoint + a Bearer-protected
	// resource, so the OAuth flow is exercisable end-to-end from testserver.yon.
	mux.HandleFunc("/oauth/token", oauthToken)
	mux.HandleFunc("/oauth/protected", oauthProtected)
	// form-data bodies (#48): echoes a urlencoded or multipart/form-data POST.
	mux.HandleFunc("/form", formEcho)
	mux.HandleFunc("/status/{code}", status)
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/get", http.StatusFound)
	})
	mux.HandleFunc("/large", large)
	mux.HandleFunc("/slow", slow)
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, sampleJSON())
	})
	mux.HandleFunc("/xml", xmlEcho)
	mux.HandleFunc("/html", htmlPage)
	mux.HandleFunc("/soap", soapEnvelope)

	// Image & PDF preview endpoints (issue #16). The images and PDF are generated
	// at runtime so the testserver stays stdlib-only with no checked-in binaries.
	mux.HandleFunc("/image/png", imageHandler("png", "image/png"))
	mux.HandleFunc("/image/jpeg", imageHandler("jpeg", "image/jpeg"))
	mux.HandleFunc("/image/gif", imageHandler("gif", "image/gif"))
	// A PNG served as application/octet-stream — exercises magic-byte sniffing
	// (the server gives no useful type, Yon detects the image from its bytes).
	mux.HandleFunc("/image/octet", imageHandler("png", "application/octet-stream"))
	mux.HandleFunc("/pdf", pdfHandler("application/pdf"))
	mux.HandleFunc("/pdf/octet", pdfHandler("application/octet-stream"))
	// A plain-text body that begins with "BM" (BMP's signature). It MUST render as
	// text, not a broken image — the regression behind the #16 detection fix:
	// an explicit textual Content-Type is trusted and never magic-sniffed.
	mux.HandleFunc("/text-bm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Yon-Testserver", "1")
		_, _ = io.WriteString(w, "BMW recall notice: please return your vehicle.\n"+
			"This is plain text that begins with \"BM\" and must NOT be shown as an image.")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"service": "yon-testserver",
			"endpoints": []string{
				"/get", "/post", "/put", "/delete", "/headers",
				"/basic-auth/{user}/{pass}", "/bearer", "/status/{code}",
				"/oauth/token", "/oauth/protected", "/form",
				"/redirect", "/large", "/slow?seconds=N", "/json",
				"/xml", "/html", "/soap",
				"/image/png", "/image/jpeg", "/image/gif", "/image/octet",
				"/pdf", "/pdf/octet", "/text-bm",
			},
			"credentials": map[string]string{
				"bearer": demoBearer, "basicUser": demoUser, "basicPass": demoPassword,
				"oauthClientId": demoOAuthClientID, "oauthClientSecret": demoOAuthClientSecret,
			},
		})
	})
	return mux
}

// echo reflects the request back as JSON.
func echo(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	writeJSON(w, http.StatusOK, map[string]any{
		"method":  r.Method,
		"path":    r.URL.Path,
		"query":   flatten(r.URL.Query()),
		"headers": flatten(r.Header),
		"body":    string(body),
	})
}

func basicAuth(w http.ResponseWriter, r *http.Request) {
	user := r.PathValue("user")
	pass := r.PathValue("pass")
	u, p, ok := r.BasicAuth()
	if !ok || u != user || p != pass {
		w.Header().Set("WWW-Authenticate", `Basic realm="yon"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"authenticated": false,
			"hint":          fmt.Sprintf("send Basic auth %s / %s", user, pass),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "user": u})
}

func bearerAuth(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if token != demoBearer || !strings.HasPrefix(auth, "Bearer ") {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"authenticated": false,
			"hint":          "send Bearer token " + demoBearer,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "token": token})
}

// oauthToken is a minimal OAuth 2.0 token endpoint (#30) for the
// client_credentials grant. It authenticates the client by either an HTTP Basic
// Authorization header or client_id/client_secret in the form body (so both of
// Yon's client-auth styles are exercised), and on success issues demoOAuthToken.
func oauthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": "invalid_request", "error_description": "POST required",
		})
		return
	}
	_ = r.ParseForm()
	if g := r.PostForm.Get("grant_type"); g != "client_credentials" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":             "unsupported_grant_type",
			"error_description": "testserver issues tokens for client_credentials only (got " + g + ")",
		})
		return
	}

	// Client auth: prefer the Basic header, fall back to body credentials.
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != demoOAuthClientID || secret != demoOAuthClientSecret {
		w.Header().Set("WWW-Authenticate", `Basic realm="yon-oauth"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":             "invalid_client",
			"error_description": fmt.Sprintf("send client_credentials %s / %s (Basic header or body)", demoOAuthClientID, demoOAuthClientSecret),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": demoOAuthToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        r.PostForm.Get("scope"),
	})
}

// oauthProtected is a resource that requires the Bearer token minted by
// oauthToken (#30) — the destination of the OAuth2 request in testserver.yon.
func oauthProtected(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if !strings.HasPrefix(auth, "Bearer ") || token != demoOAuthToken {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"authenticated": false,
			"hint":          "obtain a token from /oauth/token (client_credentials) and send it as a Bearer",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"via":           "oauth2 client_credentials",
		"token":         token,
	})
}

// formEcho echoes a posted form. It accepts POST with either an
// application/x-www-form-urlencoded body or a multipart/form-data body (text
// parts plus file parts). The response reports the request Content-Type, the
// flattened text fields (first value per key), a list of any uploaded files
// (field name, filename, size), and the number of text fields.
func formEcho(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": "POST only",
			"hint":  "POST application/x-www-form-urlencoded or multipart/form-data",
		})
		return
	}

	ct := r.Header.Get("Content-Type")
	fields := map[string]string{}
	files := []map[string]any{}

	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "parse multipart: " + err.Error()})
			return
		}
		if r.MultipartForm != nil {
			for k, vs := range r.MultipartForm.Value {
				if len(vs) > 0 {
					fields[k] = vs[0]
				}
			}
			for field, headers := range r.MultipartForm.File {
				for _, fh := range headers {
					files = append(files, map[string]any{
						"field":    field,
						"filename": fh.Filename,
						"size":     fh.Size,
					})
				}
			}
		}
	} else {
		if err := r.ParseForm(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "parse form: " + err.Error()})
			return
		}
		for k, vs := range r.PostForm {
			if len(vs) > 0 {
				fields[k] = vs[0]
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"contentType": ct,
		"fields":      fields,
		"files":       files,
		"fieldCount":  len(fields),
	})
}

func status(w http.ResponseWriter, r *http.Request) {
	code, err := strconv.Atoi(r.PathValue("code"))
	if err != nil || code < 100 || code > 599 {
		code = http.StatusBadRequest
	}
	writeJSON(w, code, map[string]any{"status": code, "text": http.StatusText(code)})
}

func large(w http.ResponseWriter, r *http.Request) {
	items := make([]map[string]any, 4000)
	for i := range items {
		items[i] = map[string]any{
			"id":    i,
			"name":  fmt.Sprintf("item-%05d", i),
			"value": i * 7,
			"note":  "lorem ipsum dolor sit amet consectetur adipiscing elit",
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "items": items})
}

func slow(w http.ResponseWriter, r *http.Request) {
	secs := 5
	if v := r.URL.Query().Get("seconds"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			secs = n
		}
	}
	select {
	case <-time.After(time.Duration(secs) * time.Second):
		writeJSON(w, http.StatusOK, map[string]any{"sleptSeconds": secs})
	case <-r.Context().Done():
		// Client cancelled (Yon's Cancel button or timeout) — just stop.
	}
}

// flatten collapses a header/query multimap to single values (first wins).
func flatten(m map[string][]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// xmlEcho returns an XML document as application/xml. When the request carries a
// body (e.g. an XML request body sent from Yon) it echoes that body back so the
// payload round-trips; otherwise it returns a sample catalog document. The sample
// is minified so Yon's Pretty view (and the Format button) have something to
// re-indent, and it includes a comment, attributes, an xml:lang attribute, nested
// elements and UTF-8 text.
func xmlEcho(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("X-Yon-Testserver", "1")
	w.WriteHeader(http.StatusOK)
	if len(bytes.TrimSpace(body)) > 0 {
		_, _ = w.Write(body)
		return
	}
	_, _ = io.WriteString(w, sampleXML)
}

// htmlPage returns a small HTML document as text/html (exercises Yon's HTML
// syntax highlighting).
func htmlPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Yon-Testserver", "1")
	_, _ = io.WriteString(w, sampleHTML)
}

// soapEnvelope returns a SOAP 1.1 envelope as text/xml (exercises namespace
// prefixes — soap:, m: — in Yon's XML formatter and highlighter).
func soapEnvelope(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("X-Yon-Testserver", "1")
	_, _ = io.WriteString(w, sampleSOAP)
}

// imageHandler returns a handler that writes a freshly-rendered gradient image in
// the given format ("png" / "jpeg" / "gif") under contentType. Pass an
// image content-type to test the authoritative path, or "application/octet-stream"
// to test magic-byte sniffing of a mislabelled image.
func imageHandler(format, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Yon-Testserver", "1")
		_, _ = w.Write(sampleImage(format))
	}
}

// sampleImage renders a 240×160 gradient test image encoded as PNG, JPEG or GIF.
func sampleImage(format string) []byte {
	const w, h = 240, 160
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / w),
				G: uint8(y * 255 / h),
				B: uint8((x + y) * 255 / (w + h)),
				A: 0xff,
			})
		}
	}
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	case "gif":
		_ = gif.Encode(&buf, img, nil)
	default:
		_ = png.Encode(&buf, img)
	}
	return buf.Bytes()
}

// pdfHandler returns a handler that writes a minimal valid one-page PDF under
// contentType (application/pdf, or application/octet-stream to test sniffing).
func pdfHandler(contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Yon-Testserver", "1")
		_, _ = w.Write(minimalPDF())
	}
}

// minimalPDF builds a tiny but structurally valid one-page PDF (header, five
// objects, a cross-reference table with correct byte offsets, trailer) so a real
// OS viewer opens it from Yon's PDF panel. Generated rather than checked in to
// keep the testserver dependency- and binary-free.
func minimalPDF() []byte {
	stream := "BT /F1 24 Tf 36 96 Td (Yon testserver PDF) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 320 144] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xrefStart)
	return buf.Bytes()
}

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?><!-- Yon testserver sample catalog --><catalog><book id="bk101" xml:lang="en"><author>Ada Lovelace</author><title>Notes on the Analytical Engine</title><price currency="GBP">9.75</price><tags><tag>history</tag><tag>computing</tag></tags></book><book id="bk102" xml:lang="th"><author>สมชาย ใจดี</author><title>HTTP ฉบับโยน</title><price currency="THB">350</price></book></catalog>`

const sampleHTML = `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>Yon testserver</title></head><body><h1>Hello from Yon</h1><p>Throw a request. <strong>Catch a response.</strong></p><ul><li>offline</li><li>fast</li></ul></body></html>`

const sampleSOAP = `<?xml version="1.0"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Header/><soap:Body><m:GetPriceResponse xmlns:m="https://yon.example/prices"><m:Price currency="USD">42.00</m:Price></m:GetPriceResponse></soap:Body></soap:Envelope>`

func sampleJSON() map[string]any {
	return map[string]any{
		"id":      42,
		"name":    "Yon",
		"active":  true,
		"score":   9.75,
		"tags":    []string{"http", "offline", "fast"},
		"nested":  map[string]any{"a": 1, "b": []int{1, 2, 3}, "c": nil},
		"message": "Throw a request. Catch a response.",
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Yon-Testserver", "1")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

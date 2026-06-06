package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// newTestServer mounts the real mux on an httptest server and returns it plus a
// client that does NOT follow redirects (so the /redirect 302 is observable).
func newTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(newMux())
	t.Cleanup(srv.Close)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return srv, client
}

func getJSON(t *testing.T, c *http.Client, url string) (int, map[string]any) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	return resp.StatusCode, m
}

func TestEcho_ReflectsMethodAndQuery(t *testing.T) {
	srv, c := newTestServer(t)
	code, m := getJSON(t, c, srv.URL+"/get?page=1&q=hello")
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if m["method"] != "GET" || m["path"] != "/get" {
		t.Fatalf("method/path wrong: %v", m)
	}
	q, _ := m["query"].(map[string]any)
	if q["page"] != "1" || q["q"] != "hello" {
		t.Fatalf("query not echoed: %v", q)
	}
}

func TestBasicAuth(t *testing.T) {
	srv, c := newTestServer(t)
	url := srv.URL + "/basic-auth/alice/secret"

	// wrong creds → 401
	if code, m := getJSON(t, c, url); code != 401 || m["authenticated"] != false {
		t.Fatalf("no-creds: code=%d m=%v, want 401 + authenticated:false", code, m)
	}

	// correct creds → 200
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.SetBasicAuth(demoUser, demoPassword)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	if resp.StatusCode != 200 || m["authenticated"] != true || m["user"] != demoUser {
		t.Fatalf("good creds: code=%d m=%v", resp.StatusCode, m)
	}
}

func TestBearerAuth(t *testing.T) {
	srv, c := newTestServer(t)
	url := srv.URL + "/bearer"

	if code, m := getJSON(t, c, url); code != 401 || m["authenticated"] != false {
		t.Fatalf("no-token: code=%d m=%v, want 401", code, m)
	}

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+demoBearer)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	if resp.StatusCode != 200 || m["authenticated"] != true {
		t.Fatalf("good token: code=%d m=%v", resp.StatusCode, m)
	}
}

func TestStatusCodes(t *testing.T) {
	srv, c := newTestServer(t)
	for _, want := range []int{200, 404, 500, 503} {
		code, _ := getJSON(t, c, srv.URL+"/status/"+strconv.Itoa(want))
		if code != want {
			t.Fatalf("/status/%d returned %d", want, code)
		}
	}
}

func TestRedirect(t *testing.T) {
	srv, c := newTestServer(t)
	resp, err := c.Get(srv.URL + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/get" {
		t.Fatalf("Location = %q, want /get", loc)
	}
}

func TestLargeBodyExceeds256KB(t *testing.T) {
	srv, c := newTestServer(t)
	resp, err := c.Get(srv.URL + "/large")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) <= 256*1024 {
		t.Fatalf("/large body = %d bytes, want > 256 KB", len(body))
	}
	if !json.Valid(body) {
		t.Fatal("/large body is not valid JSON")
	}
}

func TestSlow_RespectsCancellation(t *testing.T) {
	srv, _ := newTestServer(t)

	// seconds=0 returns promptly
	c := &http.Client{}
	resp, err := c.Get(srv.URL + "/slow?seconds=0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("slow?seconds=0 = %d, want 200", resp.StatusCode)
	}

	// a cancelled context aborts a long sleep quickly (handler honours r.Context)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/slow?seconds=30", nil)
	start := time.Now()
	if _, err := c.Do(req); err == nil {
		t.Fatal("expected a context-cancel error for a 30s sleep")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %v — handler didn't honour ctx", elapsed)
	}
}

func TestJSONSample(t *testing.T) {
	srv, c := newTestServer(t)
	code, m := getJSON(t, c, srv.URL+"/json")
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	if m["name"] != "Yon" {
		t.Fatalf("sample JSON missing fields: %v", m)
	}
}

func TestXML_ReturnsSampleDocument(t *testing.T) {
	srv, c := newTestServer(t)
	resp, err := c.Get(srv.URL + "/xml")
	if err != nil {
		t.Fatalf("GET /xml: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("Content-Type = %q, want application/xml", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"<catalog>", "<book", `xml:lang="en"`, "<!--"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("sample XML missing %q:\n%s", want, body)
		}
	}
}

func TestXML_EchoesRequestBody(t *testing.T) {
	srv, c := newTestServer(t)
	const sent = `<note><to>Yon</to></note>`
	resp, err := c.Post(srv.URL+"/xml", "application/xml", strings.NewReader(sent))
	if err != nil {
		t.Fatalf("POST /xml: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != sent {
		t.Fatalf("echoed body = %q, want %q", body, sent)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("Content-Type = %q, want application/xml", ct)
	}
}

func TestHTML_ReturnsHTML(t *testing.T) {
	srv, c := newTestServer(t)
	resp, err := c.Get(srv.URL + "/html")
	if err != nil {
		t.Fatalf("GET /html: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<html") {
		t.Errorf("HTML body missing <html>:\n%s", body)
	}
}

func TestSOAP_ReturnsNamespacedEnvelope(t *testing.T) {
	srv, c := newTestServer(t)
	resp, err := c.Get(srv.URL + "/soap")
	if err != nil {
		t.Fatalf("GET /soap: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/xml") {
		t.Fatalf("Content-Type = %q, want text/xml", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"soap:Envelope", "xmlns:soap=", "m:GetPriceResponse"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("SOAP body missing %q:\n%s", want, body)
		}
	}
}

// TestImageEndpoints_DecodeWithCorrectType confirms each image endpoint serves a
// real, decodable image under the expected Content-Type (the authoritative path
// Yon's classifyBody uses), at the advertised 240×160 size.
func TestImageEndpoints_DecodeWithCorrectType(t *testing.T) {
	srv, c := newTestServer(t)
	cases := []struct {
		path, wantCT, wantFormat string
	}{
		{"/image/png", "image/png", "png"},
		{"/image/jpeg", "image/jpeg", "jpeg"},
		{"/image/gif", "image/gif", "gif"},
	}
	for _, tc := range cases {
		resp, err := c.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if ct := resp.Header.Get("Content-Type"); ct != tc.wantCT {
			t.Errorf("%s Content-Type = %q, want %q", tc.path, ct, tc.wantCT)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			t.Errorf("%s: image.DecodeConfig: %v", tc.path, err)
			continue
		}
		if format != tc.wantFormat {
			t.Errorf("%s decoded as %q, want %q", tc.path, format, tc.wantFormat)
		}
		if cfg.Width != 240 || cfg.Height != 160 {
			t.Errorf("%s = %dx%d, want 240x160", tc.path, cfg.Width, cfg.Height)
		}
	}
}

// TestImageOctet_IsDecodablePNGUnderGenericType serves a PNG as
// application/octet-stream — the magic-byte-sniffing path. The body must still be
// a decodable PNG even though the type gives nothing away.
func TestImageOctet_IsDecodablePNGUnderGenericType(t *testing.T) {
	srv, c := newTestServer(t)
	resp, err := c.Get(srv.URL + "/image/octet")
	if err != nil {
		t.Fatalf("GET /image/octet: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(body)); err != nil || format != "png" {
		t.Errorf("octet body decode: format=%q err=%v, want png/nil", format, err)
	}
}

// TestPDFEndpoints_ValidPDF checks both PDF endpoints return a structurally sane
// PDF: the %PDF- header, an %%EOF trailer, and a startxref. (A malformed PDF
// would not open from Yon's PDF panel.)
func TestPDFEndpoints_ValidPDF(t *testing.T) {
	srv, c := newTestServer(t)
	cases := []struct{ path, wantCT string }{
		{"/pdf", "application/pdf"},
		{"/pdf/octet", "application/octet-stream"},
	}
	for _, tc := range cases {
		resp, err := c.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if ct := resp.Header.Get("Content-Type"); ct != tc.wantCT {
			t.Errorf("%s Content-Type = %q, want %q", tc.path, ct, tc.wantCT)
		}
		if !bytes.HasPrefix(body, []byte("%PDF-")) {
			t.Errorf("%s missing %%PDF- header", tc.path)
		}
		if !bytes.Contains(body, []byte("startxref")) || !bytes.Contains(body, []byte("%%EOF")) {
			t.Errorf("%s missing startxref/%%%%EOF trailer", tc.path)
		}
	}
}

// TestTextBM_StaysTextNotImage pins the #16 regression at the wire level: the
// body begins with "BM" (BMP's signature) but is served as text/plain, so Yon
// must keep it text. Here we assert the server contract the fix relies on.
func TestTextBM_StaysTextNotImage(t *testing.T) {
	srv, c := newTestServer(t)
	resp, err := c.Get(srv.URL + "/text-bm")
	if err != nil {
		t.Fatalf("GET /text-bm: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if !bytes.HasPrefix(body, []byte("BM")) {
		t.Errorf("body should start with the BMP-signature prefix \"BM\"; got %q", body[:min(8, len(body))])
	}
}

func TestPatch_MethodReflected(t *testing.T) {
	srv, c := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/anything", strings.NewReader("{}"))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("PATCH /anything: %v", err)
	}
	defer resp.Body.Close()
	var m map[string]any
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &m)
	if m["method"] != "PATCH" {
		t.Fatalf("method = %v, want PATCH", m["method"])
	}
}

package main

// End-to-end BLIND tests for issue #48 (form-data bodies). They exercise the
// whole path the user sees: a model.Request carrying a BodyForm / BodyMultipart
// body is run through the real yonner.Send engine against the real testserver
// mux mounted on an httptest server, and the JSON the /form endpoint echoes back
// is decoded and asserted. They are written from the published contract (the
// /form response shape, model.Body/FormField, yonner.Send/DefaultOptions) — the
// engine's body-building code (internal/yonner/formbody.go) is never read, so a
// pass proves the engine-built body is correctly received on the wire.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/yonner"
)

// feBTSend runs req through the real engine against the testserver and returns
// the decoded /form JSON response plus the HTTP status.
func feBTSend(t *testing.T, srvURL string, req model.Request) (int, map[string]any) {
	t.Helper()
	resp, err := yonner.Send(context.Background(), req, model.Collection{}, yonner.DefaultOptions())
	if err != nil {
		t.Fatalf("yonner.Send: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		t.Fatalf("decode /form JSON: %v\nbody: %s", err, resp.Body)
	}
	return resp.Status, m
}

// Test_feBT_FormURLEncoded_RoundTrip sends a BodyForm request with two enabled
// fields and one disabled field; the engine must build an
// application/x-www-form-urlencoded body that the server echoes back with only
// the enabled fields present.
func Test_feBT_FormURLEncoded_RoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)

	req := model.Request{
		Method: model.MethodPost,
		URL:    srv.URL + "/form",
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				{Key: "name", Value: "yon", Enabled: true},
				{Key: "lang", Value: "go", Enabled: true},
				{Key: "skip", Value: "x", Enabled: false},
			},
		},
	}

	status, m := feBTSend(t, srv.URL, req)
	if status != 200 {
		t.Fatalf("status = %d, want 200 (body: %v)", status, m)
	}

	ct, _ := m["contentType"].(string)
	if !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		t.Fatalf("contentType = %q, want application/x-www-form-urlencoded prefix", ct)
	}

	fields, ok := m["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields not an object: %v", m["fields"])
	}
	if fields["name"] != "yon" {
		t.Errorf("fields[name] = %v, want yon", fields["name"])
	}
	if fields["lang"] != "go" {
		t.Errorf("fields[lang] = %v, want go", fields["lang"])
	}
	if v, present := fields["skip"]; present {
		t.Errorf("disabled field skip was sent: %v", v)
	}

	// fieldCount is a JSON number → float64 after decoding.
	if fc, _ := m["fieldCount"].(float64); fc != 2 {
		t.Errorf("fieldCount = %v, want 2", m["fieldCount"])
	}
}

// Test_feBT_Multipart_TextAndFile_RoundTrip sends a BodyMultipart request with a
// text field and a file field whose Value is a real temp-file path. The engine
// must build a multipart/form-data body — reading the file at send time — that
// the server echoes back with the text field in fields and the upload in files
// (with the right field name, filename basename, and byte size).
func Test_feBT_Multipart_TextAndFile_RoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)

	contents := []byte("hello from feBT multipart upload\n")
	path := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	req := model.Request{
		Method: model.MethodPost,
		URL:    srv.URL + "/form",
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				{Key: "title", Value: "hello", Enabled: true},
				{Key: "upload", Value: path, Enabled: true, IsFile: true},
			},
		},
	}

	status, m := feBTSend(t, srv.URL, req)
	if status != 200 {
		t.Fatalf("status = %d, want 200 (body: %v)", status, m)
	}

	ct, _ := m["contentType"].(string)
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Fatalf("contentType = %q, want multipart/form-data prefix", ct)
	}

	fields, ok := m["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields not an object: %v", m["fields"])
	}
	if fields["title"] != "hello" {
		t.Errorf("fields[title] = %v, want hello", fields["title"])
	}

	files, ok := m["files"].([]any)
	if !ok {
		t.Fatalf("files not an array: %v", m["files"])
	}
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1 (%v)", len(files), files)
	}

	file, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("files[0] not an object: %v", files[0])
	}
	if file["field"] != "upload" {
		t.Errorf("files[0].field = %v, want upload", file["field"])
	}
	if want := filepath.Base(path); file["filename"] != want {
		t.Errorf("files[0].filename = %v, want %q", file["filename"], want)
	}
	// size is a JSON number → float64 after decoding.
	if size, _ := file["size"].(float64); int(size) != len(contents) {
		t.Errorf("files[0].size = %v, want %d", file["size"], len(contents))
	}
}

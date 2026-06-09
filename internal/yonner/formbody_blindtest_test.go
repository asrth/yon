package yonner_test

// BLIND tests for issue #48 (form-data bodies). Written purely from the
// documented contract for yonner.BuildWith + the public model types; the
// implementation file internal/yonner/formbody.go was NOT read.
//
// Contract under test:
//   - BodyForm produces an application/x-www-form-urlencoded body from the
//     ENABLED, non-empty-Key Fields, in order, each key/value url.QueryEscape'd
//     and joined by '&'. Content-Type is set to
//     "application/x-www-form-urlencoded" unless the user already set one.
//   - BodyMultipart produces a multipart/form-data body: text fields become
//     form fields; IsFile fields become file parts whose content is read from
//     the file at the (resolved) Value path, with part filename = Filename or
//     the path basename. Content-Type is "multipart/form-data; boundary=..."
//     unless the user already set one. Order preserved; disabled/empty-Key
//     skipped. A file that cannot be opened makes BuildWith return an error.

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/yonner"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// fbBTBuild builds the request through the public BuildWith API with a
// background context and an empty collection.
func fbBTBuild(t *testing.T, req model.Request, opts yonner.Options) (*http.Request, error) {
	t.Helper()
	return yonner.BuildWith(context.Background(), req, model.Collection{}, opts)
}

// fbBTReadBody drains httpReq.Body to a string.
func fbBTReadBody(t *testing.T, hr *http.Request) string {
	t.Helper()
	if hr.Body == nil {
		t.Fatalf("request body is nil, want a non-nil body")
	}
	b, err := io.ReadAll(hr.Body)
	if err != nil {
		t.Fatalf("reading request body: %v", err)
	}
	return string(b)
}

// fbBTField is a small constructor for an enabled text form field.
func fbBTField(key, value string) model.FormField {
	return model.FormField{Key: key, Value: value, Enabled: true}
}

// fbBTWriteTemp writes content to a fresh temp file named name under a per-test
// temp dir and returns the absolute path.
func fbBTWriteTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file %q: %v", path, err)
	}
	return path
}

// fbBTMediaType parses the Content-Type header and returns the media type plus
// the boundary parameter (for multipart).
func fbBTMediaType(t *testing.T, hr *http.Request) (string, map[string]string) {
	t.Helper()
	ct := hr.Header.Get("Content-Type")
	if ct == "" {
		t.Fatalf("Content-Type header is empty")
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("parsing Content-Type %q: %v", ct, err)
	}
	return mt, params
}

// fbBTPart is a flattened multipart part for assertion.
type fbBTPart struct {
	FormName string
	FileName string
	Content  string
}

// fbBTParseMultipart parses the request as multipart/form-data using the
// boundary from its Content-Type header and returns the parts in order.
func fbBTParseMultipart(t *testing.T, hr *http.Request) []fbBTPart {
	t.Helper()
	mt, params := fbBTMediaType(t, hr)
	if mt != "multipart/form-data" {
		t.Fatalf("media type: got %q, want multipart/form-data", mt)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatalf("multipart Content-Type missing boundary: %q", hr.Header.Get("Content-Type"))
	}
	mr := multipart.NewReader(hr.Body, boundary)
	var parts []fbBTPart
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading multipart part: %v", err)
		}
		b, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("reading part content: %v", err)
		}
		parts = append(parts, fbBTPart{
			FormName: p.FormName(),
			FileName: p.FileName(),
			Content:  string(b),
		})
		_ = p.Close()
	}
	return parts
}

// ===========================================================================
// BodyForm tests
// ===========================================================================

// Happy path: enabled fields, in order, url-encoded, joined by '&', with the
// auto Content-Type set.
func Test_fbBTForm_HappyPathOrderAndContentType(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				fbBTField("first", "1"),
				fbBTField("second", "2"),
				fbBTField("third", "3"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}

	if got := hr.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type: got %q, want %q", got, "application/x-www-form-urlencoded")
	}

	got := fbBTReadBody(t, hr)
	want := "first=1&second=2&third=3"
	if got != want {
		t.Errorf("form body: got %q, want %q", got, want)
	}
}

// Special characters in keys and values are url.QueryEscape'd.
func Test_fbBTForm_URLEncodesSpecialChars(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				fbBTField("a b", "x y"),      // spaces
				fbBTField("k&v", "1=2"),      // reserved chars
				fbBTField("emoji", "café/ü"), // non-ASCII
				fbBTField("plus+", "a+b"),    // literal plus
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}

	got := fbBTReadBody(t, hr)

	// Expected is exactly each key/value QueryEscape'd, joined by '&', in order.
	var sb strings.Builder
	for i, f := range req.Body.Fields {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(url.QueryEscape(f.Key))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(f.Value))
	}
	want := sb.String()
	if got != want {
		t.Errorf("encoded form body: got %q, want %q", got, want)
	}

	// And it must round-trip back to the original key/value pairs.
	vals, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", got, err)
	}
	for _, f := range req.Body.Fields {
		if v := vals.Get(f.Key); v != f.Value {
			t.Errorf("decoded %q: got %q, want %q", f.Key, v, f.Value)
		}
	}
}

// Disabled fields and empty-Key fields are omitted; remaining order preserved.
func Test_fbBTForm_DisabledAndEmptyKeySkipped(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				fbBTField("keep1", "a"),
				{Key: "off", Value: "b", Enabled: false}, // disabled -> skip
				{Key: "", Value: "c", Enabled: true},     // empty key -> skip
				fbBTField("keep2", "d"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	got := fbBTReadBody(t, hr)
	want := "keep1=a&keep2=d"
	if got != want {
		t.Errorf("form body: got %q, want %q", got, want)
	}
}

// Empty values are kept (the key is non-empty and the field is enabled).
func Test_fbBTForm_EmptyValueKept(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				fbBTField("k1", ""),
				fbBTField("k2", "v"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	got := fbBTReadBody(t, hr)
	want := "k1=&k2=v"
	if got != want {
		t.Errorf("form body: got %q, want %q", got, want)
	}
}

// A user-supplied Content-Type header wins over the auto value.
func Test_fbBTForm_UserContentTypeWins(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Headers: []model.Param{
			{Key: "Content-Type", Value: "text/plain", Enabled: true},
		},
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				fbBTField("a", "1"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	if got := hr.Header.Get("Content-Type"); got != "text/plain" {
		t.Errorf("user Content-Type should win: got %q, want %q", got, "text/plain")
	}
	// The body is still the urlencoded payload.
	if got := fbBTReadBody(t, hr); got != "a=1" {
		t.Errorf("form body: got %q, want %q", got, "a=1")
	}
}

// opts.Resolve expands {{vars}} in both keys and values before encoding.
func Test_fbBTForm_ResolveExpandsVars(t *testing.T) {
	resolve := func(s string) string {
		r := strings.NewReplacer("{{k}}", "name", "{{v}}", "Ada Lovelace")
		return r.Replace(s)
	}
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				fbBTField("{{k}}", "{{v}}"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{Resolve: resolve})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	got := fbBTReadBody(t, hr)
	want := "name=" + url.QueryEscape("Ada Lovelace")
	if got != want {
		t.Errorf("resolved form body: got %q, want %q", got, want)
	}
}

// ===========================================================================
// BodyMultipart tests
// ===========================================================================

// Happy path: text fields + a file field. The file part's content comes from
// the file, and its default filename is the path basename.
func Test_fbBTMultipart_TextAndFileDefaultFilename(t *testing.T) {
	filePath := fbBTWriteTemp(t, "upload.txt", "file-contents-here")

	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				fbBTField("field1", "value1"),
				{Key: "doc", Value: filePath, Enabled: true, IsFile: true}, // no Filename -> basename
				fbBTField("field2", "value2"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}

	mt, _ := fbBTMediaType(t, hr)
	if mt != "multipart/form-data" {
		t.Fatalf("media type: got %q, want multipart/form-data", mt)
	}

	parts := fbBTParseMultipart(t, hr)
	if len(parts) != 3 {
		t.Fatalf("part count: got %d, want 3 (%#v)", len(parts), parts)
	}

	// Order preserved.
	if parts[0].FormName != "field1" || parts[0].Content != "value1" {
		t.Errorf("part[0]: got name=%q content=%q, want field1/value1", parts[0].FormName, parts[0].Content)
	}
	if parts[1].FormName != "doc" {
		t.Errorf("part[1] name: got %q, want doc", parts[1].FormName)
	}
	if parts[1].Content != "file-contents-here" {
		t.Errorf("part[1] content: got %q, want %q", parts[1].Content, "file-contents-here")
	}
	if parts[1].FileName != "upload.txt" {
		t.Errorf("part[1] filename: got %q, want %q (basename default)", parts[1].FileName, "upload.txt")
	}
	if parts[2].FormName != "field2" || parts[2].Content != "value2" {
		t.Errorf("part[2]: got name=%q content=%q, want field2/value2", parts[2].FormName, parts[2].Content)
	}
}

// An explicit Filename overrides the path basename.
func Test_fbBTMultipart_ExplicitFilename(t *testing.T) {
	filePath := fbBTWriteTemp(t, "ondisk.bin", "BINARY")

	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				{Key: "attachment", Value: filePath, Enabled: true, IsFile: true, Filename: "renamed.dat"},
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	parts := fbBTParseMultipart(t, hr)
	if len(parts) != 1 {
		t.Fatalf("part count: got %d, want 1", len(parts))
	}
	if parts[0].FormName != "attachment" {
		t.Errorf("form name: got %q, want attachment", parts[0].FormName)
	}
	if parts[0].FileName != "renamed.dat" {
		t.Errorf("filename: got %q, want %q (explicit Filename)", parts[0].FileName, "renamed.dat")
	}
	if parts[0].Content != "BINARY" {
		t.Errorf("content: got %q, want %q", parts[0].Content, "BINARY")
	}
}

// Disabled and empty-Key fields are skipped; order of the rest preserved.
func Test_fbBTMultipart_DisabledAndEmptyKeySkipped(t *testing.T) {
	filePath := fbBTWriteTemp(t, "keep.txt", "FILE")

	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				fbBTField("a", "1"),
				{Key: "off", Value: "2", Enabled: false},                        // disabled
				{Key: "", Value: "3", Enabled: true},                            // empty key
				{Key: "file", Value: filePath, Enabled: true, IsFile: true},     // kept file
				{Key: "offfile", Value: filePath, Enabled: false, IsFile: true}, // disabled file
				fbBTField("b", "4"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	parts := fbBTParseMultipart(t, hr)

	var gotNames []string
	for _, p := range parts {
		gotNames = append(gotNames, p.FormName)
	}
	wantNames := []string{"a", "file", "b"}
	if strings.Join(gotNames, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("part names: got %v, want %v", gotNames, wantNames)
	}
	// The kept file part carries the file content.
	if parts[1].Content != "FILE" {
		t.Errorf("file part content: got %q, want %q", parts[1].Content, "FILE")
	}
}

// A file path that cannot be opened makes BuildWith return an error.
func Test_fbBTMultipart_MissingFileError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")

	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				fbBTField("ok", "1"),
				{Key: "bad", Value: missing, Enabled: true, IsFile: true},
			},
		},
	}
	_, err := fbBTBuild(t, req, yonner.Options{})
	if err == nil {
		t.Fatalf("BuildWith should return an error for an unopenable file path, got nil")
	}
}

// User-supplied Content-Type wins over the auto multipart value.
func Test_fbBTMultipart_UserContentTypeWins(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Headers: []model.Param{
			{Key: "Content-Type", Value: "application/custom", Enabled: true},
		},
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				fbBTField("a", "1"),
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	if got := hr.Header.Get("Content-Type"); got != "application/custom" {
		t.Errorf("user Content-Type should win: got %q, want %q", got, "application/custom")
	}
}

// opts.Resolve expands {{vars}} in a file field's Value (the path) and in text
// field keys/values.
func Test_fbBTMultipart_ResolveExpandsFilePathAndFields(t *testing.T) {
	filePath := fbBTWriteTemp(t, "resolved.txt", "RESOLVED-FILE")

	resolve := func(s string) string {
		r := strings.NewReplacer("{{path}}", filePath, "{{v}}", "expanded")
		return r.Replace(s)
	}
	req := model.Request{
		Method: model.MethodPost,
		URL:    "http://example.com/",
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				fbBTField("text", "{{v}}"),
				{Key: "file", Value: "{{path}}", Enabled: true, IsFile: true},
			},
		},
	}
	hr, err := fbBTBuild(t, req, yonner.Options{Resolve: resolve})
	if err != nil {
		t.Fatalf("BuildWith error: %v", err)
	}
	parts := fbBTParseMultipart(t, hr)
	if len(parts) != 2 {
		t.Fatalf("part count: got %d, want 2", len(parts))
	}
	if parts[0].FormName != "text" || parts[0].Content != "expanded" {
		t.Errorf("text part: got name=%q content=%q, want text/expanded", parts[0].FormName, parts[0].Content)
	}
	if parts[1].Content != "RESOLVED-FILE" {
		t.Errorf("file part content: got %q, want %q", parts[1].Content, "RESOLVED-FILE")
	}
	// Default filename is the basename of the resolved path.
	if parts[1].FileName != "resolved.txt" {
		t.Errorf("file part filename: got %q, want %q", parts[1].FileName, "resolved.txt")
	}
}

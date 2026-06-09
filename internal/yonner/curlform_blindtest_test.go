package yonner

// Blind tests for form-data bodies in the curl interop lane (issue #48).
// Written purely from the published contract (the public ToCurl / FromCurl
// signatures plus the model.Body / model.FormField types), with NO knowledge of
// the form/multipart implementation in curl.go or fromcurl.go. They pin:
//   - ToCurl BodyForm  -> one --data-urlencode 'k=v' per enabled non-empty-key
//     field, in order, with no Content-Type header emitted for it;
//   - ToCurl BodyMultipart -> -F 'k=v' for text fields and -F 'k=@path' for file
//     fields, appending ;filename=NAME when set, in order, enabled non-empty-key
//     only;
//   - FromCurl -F / --form parsing into a BodyMultipart with text/file Fields,
//     -F precedence over -d, and a round-trip-ish ToCurl re-emit of the -F args.
// Fixtures use the cfBT prefix and a nil/identity resolver (Options{}).

import (
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// cfBTIndexOf returns the byte index of sub in s, or -1 when absent. Kept local
// so the file leans only on contract symbols and the stdlib.
func cfBTIndexOf(s, sub string) int { return strings.Index(s, sub) }

// cfBTContains is a readable wrapper used by the assertions below.
func cfBTContains(s, sub string) bool { return strings.Contains(s, sub) }

// ---------------------------------------------------------------------------
// A. ToCurl, BodyForm: --data-urlencode per enabled non-empty-key field, in
//    order, and no Content-Type header emitted for the form body.
// ---------------------------------------------------------------------------

func TestBT_cfBT_ToCurl_FormDataUrlencodeInOrder(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "https://api.example.com/cfBT/form",
		Auth:   model.Auth{Kind: model.AuthNone},
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				{Key: "cfBTalpha", Value: "one", Enabled: true},
				{Key: "cfBTbeta", Value: "two", Enabled: true},
				{Key: "cfBTgamma", Value: "three", Enabled: true},
			},
		},
	}
	got := ToCurl(req, model.NewCollection(""), Options{})

	wantArgs := []string{
		"--data-urlencode 'cfBTalpha=one'",
		"--data-urlencode 'cfBTbeta=two'",
		"--data-urlencode 'cfBTgamma=three'",
	}
	prev := -1
	for _, a := range wantArgs {
		i := cfBTIndexOf(got, a)
		if i < 0 {
			t.Fatalf("curl missing %q.\n got: %s", a, got)
		}
		if i < prev {
			t.Errorf("arg %q out of order.\n got: %s", a, got)
		}
		prev = i
	}

	// A form body must NOT carry an explicit Content-Type header in the curl
	// string (the contract says no Content-Type header is emitted for it).
	if cfBTContains(got, "Content-Type") {
		t.Errorf("form curl should not emit a Content-Type header.\n got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// B. ToCurl, BodyForm: disabled and empty-Key fields are skipped.
// ---------------------------------------------------------------------------

func TestBT_cfBT_ToCurl_FormSkipsDisabledAndEmptyKey(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "https://api.example.com/cfBT/form",
		Auth:   model.Auth{Kind: model.AuthNone},
		Body: model.Body{
			Type: model.BodyForm,
			Fields: []model.FormField{
				{Key: "cfBTkeep", Value: "yes", Enabled: true},
				{Key: "cfBTskip", Value: "no", Enabled: false},
				{Key: "", Value: "orphan", Enabled: true},
				{Key: "cfBTlast", Value: "ok", Enabled: true},
			},
		},
	}
	got := ToCurl(req, model.NewCollection(""), Options{})

	if !cfBTContains(got, "--data-urlencode 'cfBTkeep=yes'") {
		t.Errorf("enabled field cfBTkeep missing.\n got: %s", got)
	}
	if !cfBTContains(got, "--data-urlencode 'cfBTlast=ok'") {
		t.Errorf("enabled field cfBTlast missing.\n got: %s", got)
	}
	if cfBTContains(got, "cfBTskip") {
		t.Errorf("disabled field cfBTskip should be skipped.\n got: %s", got)
	}
	if cfBTContains(got, "orphan") {
		t.Errorf("empty-key field should be skipped.\n got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// C. ToCurl, BodyMultipart: -F text + -F file (@path) + ;filename, in order,
//    enabled non-empty-key only.
// ---------------------------------------------------------------------------

func TestBT_cfBT_ToCurl_MultipartTextAndFile(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "https://api.example.com/cfBT/upload",
		Auth:   model.Auth{Kind: model.AuthNone},
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				{Key: "cfBTfield", Value: "hello", Enabled: true},
				{Key: "cfBTplain", Value: "/tmp/cfBT_a.bin", Enabled: true, IsFile: true},
				{Key: "cfBTnamed", Value: "/tmp/cfBT_b.png", Enabled: true, IsFile: true, Filename: "pretty.png"},
			},
		},
	}
	got := ToCurl(req, model.NewCollection(""), Options{})

	wantArgs := []string{
		"-F 'cfBTfield=hello'",
		"-F 'cfBTplain=@/tmp/cfBT_a.bin'",
		"-F 'cfBTnamed=@/tmp/cfBT_b.png;filename=pretty.png'",
	}
	prev := -1
	for _, a := range wantArgs {
		i := cfBTIndexOf(got, a)
		if i < 0 {
			t.Fatalf("curl missing %q.\n got: %s", a, got)
		}
		if i < prev {
			t.Errorf("arg %q out of order.\n got: %s", a, got)
		}
		prev = i
	}
}

func TestBT_cfBT_ToCurl_MultipartSkipsDisabledAndEmptyKey(t *testing.T) {
	req := model.Request{
		Method: model.MethodPost,
		URL:    "https://api.example.com/cfBT/upload",
		Auth:   model.Auth{Kind: model.AuthNone},
		Body: model.Body{
			Type: model.BodyMultipart,
			Fields: []model.FormField{
				{Key: "cfBTkeep", Value: "yes", Enabled: true},
				{Key: "cfBTskip", Value: "no", Enabled: false},
				{Key: "", Value: "/tmp/cfBT_orphan.bin", Enabled: true, IsFile: true},
			},
		},
	}
	got := ToCurl(req, model.NewCollection(""), Options{})

	if !cfBTContains(got, "-F 'cfBTkeep=yes'") {
		t.Errorf("enabled multipart field cfBTkeep missing.\n got: %s", got)
	}
	if cfBTContains(got, "cfBTskip") {
		t.Errorf("disabled multipart field cfBTskip should be skipped.\n got: %s", got)
	}
	if cfBTContains(got, "cfBT_orphan") {
		t.Errorf("empty-key multipart field should be skipped.\n got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// D. FromCurl: -F flags parse into a BodyMultipart with text + file fields,
//    IsFile/Value/Filename set correctly, Enabled=true, in order.
// ---------------------------------------------------------------------------

func TestBT_cfBT_FromCurl_MultipartFields(t *testing.T) {
	in := `curl https://api/cfBT -F cfBTa=1 -F cfBTb=@/tmp/cfBTx.png;filename=cfBTp.png`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Body.Type != model.BodyMultipart {
		t.Fatalf("Body.Type = %q, want %q", got.Body.Type, model.BodyMultipart)
	}
	if len(got.Body.Fields) != 2 {
		t.Fatalf("Fields = %#v, want 2 fields", got.Body.Fields)
	}

	text := got.Body.Fields[0]
	if text.Key != "cfBTa" || text.Value != "1" || text.IsFile || !text.Enabled || text.Filename != "" {
		t.Errorf("text field = %#v, want {Key:cfBTa Value:1 Enabled:true IsFile:false Filename:\"\"}", text)
	}

	file := got.Body.Fields[1]
	if file.Key != "cfBTb" {
		t.Errorf("file field Key = %q, want cfBTb", file.Key)
	}
	if !file.IsFile {
		t.Errorf("file field IsFile = false, want true (key=value with @ is a file part)")
	}
	if file.Value != "/tmp/cfBTx.png" {
		t.Errorf("file field Value = %q, want /tmp/cfBTx.png (the @path without the @)", file.Value)
	}
	if file.Filename != "cfBTp.png" {
		t.Errorf("file field Filename = %q, want cfBTp.png (split from ;filename=)", file.Filename)
	}
	if !file.Enabled {
		t.Errorf("file field Enabled = false, want true")
	}
}

// FromCurl must also accept the long --form spelling.
func TestBT_cfBT_FromCurl_LongFormFlag(t *testing.T) {
	in := `curl https://api/cfBT --form cfBTa=1 --form cfBTb=2`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Body.Type != model.BodyMultipart {
		t.Fatalf("Body.Type = %q, want %q", got.Body.Type, model.BodyMultipart)
	}
	if len(got.Body.Fields) != 2 {
		t.Fatalf("Fields = %#v, want 2", got.Body.Fields)
	}
	if got.Body.Fields[0].Key != "cfBTa" || got.Body.Fields[0].Value != "1" {
		t.Errorf("field[0] = %#v, want cfBTa=1", got.Body.Fields[0])
	}
	if got.Body.Fields[1].Key != "cfBTb" || got.Body.Fields[1].Value != "2" {
		t.Errorf("field[1] = %#v, want cfBTb=2", got.Body.Fields[1])
	}
}

// ---------------------------------------------------------------------------
// E. FromCurl: any -F present makes the body multipart, taking precedence over
//    -d data (the -d content must not become the body).
// ---------------------------------------------------------------------------

func TestBT_cfBT_FromCurl_FormPrecedenceOverData(t *testing.T) {
	in := `curl https://api/cfBT -d cfBTignored=1 -F cfBTreal=2`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Body.Type != model.BodyMultipart {
		t.Fatalf("Body.Type = %q, want %q (-F wins over -d)", got.Body.Type, model.BodyMultipart)
	}
	// The -d datum must not have been promoted to the text Content.
	if cfBTContains(got.Body.Content, "cfBTignored") {
		t.Errorf("Body.Content = %q, want -d data not used when -F present", got.Body.Content)
	}
	found := false
	for _, f := range got.Body.Fields {
		if f.Key == "cfBTreal" && f.Value == "2" {
			found = true
		}
		if f.Key == "cfBTignored" {
			t.Errorf("-d datum leaked into multipart Fields: %#v", f)
		}
	}
	if !found {
		t.Errorf("Fields = %#v, want the -F field cfBTreal=2", got.Body.Fields)
	}
}

// FromCurl with only -d (no -F) still yields a text body (unchanged behaviour).
func TestBT_cfBT_FromCurl_DataWithoutFormStaysText(t *testing.T) {
	in := `curl https://api/cfBT -d cfBTk=v`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Body.Type != model.BodyText {
		t.Errorf("Body.Type = %q, want text (no -F present)", got.Body.Type)
	}
	if got.Body.Content != "cfBTk=v" {
		t.Errorf("Body.Content = %q, want cfBTk=v", got.Body.Content)
	}
	if len(got.Body.Fields) != 0 {
		t.Errorf("Fields = %#v, want none for a -d-only body", got.Body.Fields)
	}
}

// ---------------------------------------------------------------------------
// F. Round-trip-ish: FromCurl of a -F curl string yields fields whose ToCurl
//    re-emits the same -F args (text + file + ;filename).
// ---------------------------------------------------------------------------

func TestBT_cfBT_RoundTrip_FormFromCurlToCurl(t *testing.T) {
	in := `curl https://api/cfBT -F cfBTtext=hello -F cfBTfile=@/tmp/cfBTr.png;filename=cfBTout.png`
	parsed, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if parsed.Body.Type != model.BodyMultipart {
		t.Fatalf("Body.Type = %q, want multipart", parsed.Body.Type)
	}

	got := ToCurl(parsed, model.NewCollection(""), Options{})
	wantArgs := []string{
		"-F 'cfBTtext=hello'",
		"-F 'cfBTfile=@/tmp/cfBTr.png;filename=cfBTout.png'",
	}
	for _, a := range wantArgs {
		if !cfBTContains(got, a) {
			t.Errorf("round-trip curl missing %q.\n got: %s", a, got)
		}
	}
}

package yonner

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ultramcu/yon/internal/model"
)

// buildBodyReader turns a model.Body into the io.Reader sent as the request body
// plus the Content-Type the body type implies (empty string = no automatic
// Content-Type; the caller only applies a non-empty value when the user has not
// set Content-Type explicitly). resolve expands {{variable}} templates in the
// outgoing text just before the body is built; a nil-safe identity is the
// caller's responsibility (Options.resolve already handles nil).
//
// A nil reader means "no body" (BodyNone, or an empty Content for the text-ish
// types) so http.NewRequest sends no body at all.
func buildBodyReader(b model.Body, resolve func(string) string) (io.Reader, string, error) {
	switch b.Type {
	case model.BodyNone, "":
		return nil, "", nil

	case model.BodyForm:
		// application/x-www-form-urlencoded: build from the enabled Fields in
		// order (Yon preserves field order, unlike url.Values.Encode which
		// sorts). IsFile/Filename are ignored here.
		var pairs []string
		for _, f := range b.Fields {
			if !f.Enabled || f.Key == "" {
				continue
			}
			pairs = append(pairs,
				url.QueryEscape(resolve(f.Key))+"="+url.QueryEscape(resolve(f.Value)))
		}
		return strings.NewReader(strings.Join(pairs, "&")), "application/x-www-form-urlencoded", nil

	case model.BodyMultipart:
		// multipart/form-data: text parts via WriteField, file parts read from
		// the resolved Value path. FormDataContentType() carries the boundary.
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, f := range b.Fields {
			if !f.Enabled || f.Key == "" {
				continue
			}
			if !f.IsFile {
				if err := w.WriteField(resolve(f.Key), resolve(f.Value)); err != nil {
					return nil, "", err
				}
				continue
			}
			path := resolve(f.Value)
			file, err := os.Open(path)
			if err != nil {
				return nil, "", fmt.Errorf("yonner: multipart file %q: %w", path, err)
			}
			filename := resolve(f.Filename)
			if filename == "" {
				filename = filepath.Base(path)
			}
			part, err := w.CreateFormFile(resolve(f.Key), filename)
			if err != nil {
				file.Close()
				return nil, "", err
			}
			if _, err := io.Copy(part, file); err != nil {
				file.Close()
				return nil, "", err
			}
			file.Close()
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return bytes.NewReader(buf.Bytes()), w.FormDataContentType(), nil

	default: // BodyJSON, BodyXML, BodyText
		if b.Content == "" {
			return nil, "", nil
		}
		content := resolve(b.Content)
		ct := ""
		switch b.Type {
		case model.BodyJSON:
			ct = "application/json"
		case model.BodyXML:
			ct = "application/xml"
		}
		return strings.NewReader(content), ct, nil
	}
}

package importer

// Blind test for exampleJSON, written from the CONTRACT ONLY (see
// openapi_types.go for the schema type). The generator under test is treated as
// a black box; these tests assert the documented behaviour, decoding the
// produced JSON and asserting values/shape rather than matching whitespace.
//
// Contract recap:
//
//	exampleJSON(s *schema) string
//	  - nil           -> ""
//	  - Example != nil -> that value
//	  - object        -> object with each property in PropOrder (else sorted keys),
//	                      all properties included
//	  - array         -> one-element array of the item example
//	  - string        -> Enum[0] if present, else a format-aware sample
//	                      (date-time/date/email/uuid) else "string"
//	  - integer/number -> Enum[0] else 0
//	  - boolean       -> false
//	  - cyclic schemas must not hang (guarded); output deterministic + valid JSON.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// decode parses out as JSON, failing the test if it is not valid JSON.
func exBTdecode(t *testing.T, out string) any {
	t.Helper()
	if !json.Valid([]byte(out)) {
		t.Fatalf("output is not valid JSON: %q", out)
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("json.Unmarshal(%q) error: %v", out, err)
	}
	return v
}

// 1. nil -> "".
func TestExampleJSON_Nil(t *testing.T) {
	if got := exampleJSON(nil); got != "" {
		t.Fatalf("exampleJSON(nil) = %q, want \"\"", got)
	}
}

// 2. Primitives.
func TestExampleJSON_Primitives(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		v := exBTdecode(t, exampleJSON(&schema{Type: "string"}))
		s, ok := v.(string)
		if !ok || s != "string" {
			t.Fatalf("string primitive decoded to %#v, want \"string\"", v)
		}
	})
	t.Run("integer", func(t *testing.T) {
		v := exBTdecode(t, exampleJSON(&schema{Type: "integer"}))
		n, ok := v.(float64)
		if !ok || n != 0 {
			t.Fatalf("integer primitive decoded to %#v, want 0", v)
		}
	})
	t.Run("number", func(t *testing.T) {
		v := exBTdecode(t, exampleJSON(&schema{Type: "number"}))
		n, ok := v.(float64)
		if !ok || n != 0 {
			t.Fatalf("number primitive decoded to %#v, want 0", v)
		}
	})
	t.Run("boolean", func(t *testing.T) {
		v := exBTdecode(t, exampleJSON(&schema{Type: "boolean"}))
		b, ok := v.(bool)
		if !ok || b != false {
			t.Fatalf("boolean primitive decoded to %#v, want false", v)
		}
	})
}

// 3. Enum wins for strings.
func TestExampleJSON_EnumWins(t *testing.T) {
	exBT := &schema{Type: "string", Enum: []any{"active", "closed"}}
	v := exBTdecode(t, exampleJSON(exBT))
	if s, ok := v.(string); !ok || s != "active" {
		t.Fatalf("enum string decoded to %#v, want \"active\"", v)
	}
}

// 4. Example wins over everything else.
func TestExampleJSON_ExampleWins(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		exBT := &schema{Type: "string", Example: "hello"}
		v := exBTdecode(t, exampleJSON(exBT))
		if s, ok := v.(string); !ok || s != "hello" {
			t.Fatalf("example string decoded to %#v, want \"hello\"", v)
		}
	})
	t.Run("integer", func(t *testing.T) {
		exBT := &schema{Type: "integer", Example: 42}
		v := exBTdecode(t, exampleJSON(exBT))
		if n, ok := v.(float64); !ok || n != 42 {
			t.Fatalf("example integer decoded to %#v, want 42", v)
		}
	})
}

// 5. Object with property order: id before name in the raw output.
func TestExampleJSON_ObjectPropOrder(t *testing.T) {
	exBT := &schema{
		Type:      "object",
		PropOrder: []string{"id", "name"},
		Properties: map[string]*schema{
			"id":   {Type: "integer"},
			"name": {Type: "string"},
		},
	}
	out := exampleJSON(exBT)
	v := exBTdecode(t, out)

	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("object decoded to %#v, want a JSON object", v)
	}
	if n, ok := m["id"].(float64); !ok || n != 0 {
		t.Fatalf("m[\"id\"] = %#v, want 0", m["id"])
	}
	if s, ok := m["name"].(string); !ok || s != "string" {
		t.Fatalf("m[\"name\"] = %#v, want \"string\"", m["name"])
	}
	if len(m) != 2 {
		t.Fatalf("object has %d properties, want 2 (all included): %#v", len(m), m)
	}

	idIdx := strings.Index(out, `"id"`)
	nameIdx := strings.Index(out, `"name"`)
	if idIdx < 0 || nameIdx < 0 {
		t.Fatalf("expected both \"id\" and \"name\" keys in output: %q", out)
	}
	if idIdx > nameIdx {
		t.Fatalf("expected \"id\" before \"name\" in output (PropOrder), got: %q", out)
	}
}

// 6. Array -> one-element array of the item example.
func TestExampleJSON_Array(t *testing.T) {
	exBT := &schema{Type: "array", Items: &schema{Type: "string"}}
	v := exBTdecode(t, exampleJSON(exBT))
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("array decoded to %#v, want a JSON array", v)
	}
	if len(arr) != 1 {
		t.Fatalf("array has %d elements, want 1: %#v", len(arr), arr)
	}
	if s, ok := arr[0].(string); !ok || s != "string" {
		t.Fatalf("array[0] = %#v, want \"string\"", arr[0])
	}
}

// 7. Nested object with an array-of-objects property.
func TestExampleJSON_NestedObjectArray(t *testing.T) {
	exBT := &schema{
		Type:      "object",
		PropOrder: []string{"items"},
		Properties: map[string]*schema{
			"items": {
				Type: "array",
				Items: &schema{
					Type:      "object",
					PropOrder: []string{"id", "label"},
					Properties: map[string]*schema{
						"id":    {Type: "integer"},
						"label": {Type: "string"},
					},
				},
			},
		},
	}
	v := exBTdecode(t, exampleJSON(exBT))

	root, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("root decoded to %#v, want object", v)
	}
	arr, ok := root["items"].([]any)
	if !ok {
		t.Fatalf("root[\"items\"] = %#v, want array", root["items"])
	}
	if len(arr) != 1 {
		t.Fatalf("nested array has %d elements, want 1: %#v", len(arr), arr)
	}
	elem, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("nested array element = %#v, want object", arr[0])
	}
	if n, ok := elem["id"].(float64); !ok || n != 0 {
		t.Fatalf("nested elem[\"id\"] = %#v, want 0", elem["id"])
	}
	if s, ok := elem["label"].(string); !ok || s != "string" {
		t.Fatalf("nested elem[\"label\"] = %#v, want \"string\"", elem["label"])
	}
}

// 8. Format-aware string (loose): non-empty string, and RFC3339-parseable for
// the date-time format.
func TestExampleJSON_FormatDateTime(t *testing.T) {
	exBT := &schema{Type: "string", Format: "date-time"}
	v := exBTdecode(t, exampleJSON(exBT))
	s, ok := v.(string)
	if !ok {
		t.Fatalf("date-time string decoded to %#v, want a JSON string", v)
	}
	if s == "" {
		t.Fatalf("date-time sample is empty, want a non-empty sample")
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		t.Fatalf("date-time sample %q is not RFC3339-parseable: %v", s, err)
	}
}

// 9. Cycle safety: a self-referential schema must not hang and must produce
// valid JSON. The test itself must complete (no infinite loop); a guarded
// implementation returns promptly.
func TestExampleJSON_CycleSafe(t *testing.T) {
	exBT := &schema{
		Type:      "object",
		PropOrder: []string{"self"},
	}
	exBT.Properties = map[string]*schema{"self": exBT}

	done := make(chan string, 1)
	go func() {
		done <- exampleJSON(exBT)
	}()

	select {
	case out := <-done:
		if !json.Valid([]byte(out)) {
			t.Fatalf("cyclic schema produced invalid JSON: %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exampleJSON did not return on a cyclic schema (hang/unguarded recursion)")
	}
}

// 10. Validity: every produced non-empty output is valid JSON.
func TestExampleJSON_AlwaysValidJSON(t *testing.T) {
	cases := map[string]*schema{
		"string":      {Type: "string"},
		"integer":     {Type: "integer"},
		"number":      {Type: "number"},
		"boolean":     {Type: "boolean"},
		"enum":        {Type: "string", Enum: []any{"active", "closed"}},
		"example":     {Type: "string", Example: "hello"},
		"date-time":   {Type: "string", Format: "date-time"},
		"date":        {Type: "string", Format: "date"},
		"email":       {Type: "string", Format: "email"},
		"uuid":        {Type: "string", Format: "uuid"},
		"array":       {Type: "array", Items: &schema{Type: "integer"}},
		"emptyobject": {Type: "object"},
		"object": {
			Type:       "object",
			PropOrder:  []string{"a", "b"},
			Properties: map[string]*schema{"a": {Type: "integer"}, "b": {Type: "boolean"}},
		},
	}
	for name, s := range cases {
		out := exampleJSON(s)
		if out == "" {
			t.Errorf("%s: produced empty output, want non-empty JSON", name)
			continue
		}
		if !json.Valid([]byte(out)) {
			t.Errorf("%s: output is not valid JSON: %q", name, out)
		}
	}
}

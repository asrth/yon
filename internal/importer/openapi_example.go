package importer

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
)

// exampleJSON renders a pretty-printed (2-space indent) JSON example for a
// request-body schema, honouring example/default/enum/format with a guard
// against cyclic schemas. Returns "" for a nil schema.
//
// LANE: example generator.
func exampleJSON(s *schema) string {
	if s == nil {
		return ""
	}
	var buf bytes.Buffer
	writeExample(&buf, s, "", map[*schema]bool{})
	return buf.String()
}

// indentUnit is the per-level indentation, matching json.MarshalIndent(v, "", "  ").
const indentUnit = "  "

// writeExample emits the example for s into buf. indent is the current line
// prefix; seen tracks the *schema pointers on the active recursion path so a
// schema that (directly or transitively) references itself stops at null
// instead of looping forever.
func writeExample(buf *bytes.Buffer, s *schema, indent string, seen map[*schema]bool) {
	if s == nil {
		buf.WriteString("null")
		return
	}

	// An explicit example/default always wins, rendered verbatim.
	if s.Example != nil {
		writeValue(buf, s.Example, indent)
		return
	}

	// Determine the effective type: an empty/unknown Type that carries
	// Properties is treated as an object.
	typ := s.Type
	if typ == "" && len(s.Properties) > 0 {
		typ = "object"
	}

	switch typ {
	case "object":
		writeObject(buf, s, indent, seen)
	case "array":
		writeArray(buf, s, indent, seen)
	case "string":
		writeValue(buf, stringSample(s), indent)
	case "integer", "number":
		if len(s.Enum) > 0 {
			writeValue(buf, s.Enum[0], indent)
		} else {
			buf.WriteString("0")
		}
	case "boolean":
		buf.WriteString("false")
	default:
		buf.WriteString("null")
	}
}

// writeObject emits a JSON object, one property per line, in PropOrder (falling
// back to sorted Properties keys). All properties are included.
func writeObject(buf *bytes.Buffer, s *schema, indent string, seen map[*schema]bool) {
	// Cycle guard: if we are already inside this schema on the current path,
	// stop and emit null rather than recursing forever.
	if seen[s] {
		buf.WriteString("null")
		return
	}

	keys := propKeys(s)
	if len(keys) == 0 {
		buf.WriteString("{}")
		return
	}

	seen[s] = true
	defer delete(seen, s)

	inner := indent + indentUnit
	buf.WriteString("{\n")
	for i, k := range keys {
		buf.WriteString(inner)
		buf.Write(jsonKey(k))
		buf.WriteString(": ")
		writeExample(buf, s.Properties[k], inner, seen)
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(indent)
	buf.WriteByte('}')
}

// writeArray emits a one-element array holding the example of s.Items, or [] if
// Items is nil.
func writeArray(buf *bytes.Buffer, s *schema, indent string, seen map[*schema]bool) {
	if s.Items == nil {
		buf.WriteString("[]")
		return
	}
	// Cycle guard for self-referential arrays (an array whose item is the array).
	if seen[s] {
		buf.WriteString("[]")
		return
	}
	seen[s] = true
	defer delete(seen, s)

	inner := indent + indentUnit
	buf.WriteString("[\n")
	buf.WriteString(inner)
	writeExample(buf, s.Items, inner, seen)
	buf.WriteByte('\n')
	buf.WriteString(indent)
	buf.WriteByte(']')
}

// propKeys returns the object's property names in PropOrder, falling back to the
// sorted Properties keys when PropOrder is empty. PropOrder entries that are not
// actually present in Properties are skipped so we never dereference a missing
// schema.
func propKeys(s *schema) []string {
	if len(s.PropOrder) > 0 {
		keys := make([]string, 0, len(s.PropOrder))
		for _, k := range s.PropOrder {
			if _, ok := s.Properties[k]; ok {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			return keys
		}
	}
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// stringSample picks a sample value for a string schema: the first enum value
// if present, else a format-aware sample, else "string".
func stringSample(s *schema) string {
	if len(s.Enum) > 0 {
		if str, ok := s.Enum[0].(string); ok {
			return str
		}
		// Non-string enum on a string schema: handled best-effort below.
	}
	switch s.Format {
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "date":
		return "2024-01-01"
	case "email":
		return "user@example.com"
	case "uuid":
		return "00000000-0000-0000-0000-000000000000"
	}
	if len(s.Enum) > 0 {
		// Fall back to the raw (non-string) enum value's JSON form.
		if b, err := json.Marshal(s.Enum[0]); err == nil {
			return string(b)
		}
	}
	return "string"
}

// jsonKey returns a JSON-encoded (quoted, escaped) object key.
func jsonKey(k string) []byte {
	b, err := json.Marshal(k)
	if err != nil {
		// json.Marshal of a string never fails, but stay safe.
		return []byte(strconv.Quote(k))
	}
	return b
}

// writeValue renders an arbitrary Go value as pretty JSON, re-indenting any
// multi-line result (objects/arrays from example values) to sit at the current
// indent level. Determinism is ensured by encoding/json's sorted map keys.
func writeValue(buf *bytes.Buffer, v any, indent string) {
	b, err := json.MarshalIndent(v, indent, indentUnit)
	if err != nil {
		buf.WriteString("null")
		return
	}
	buf.Write(b)
}

package importer

// exampleJSON renders a pretty-printed JSON example for a request-body schema,
// honouring example/default/enum and required, with a guard against cyclic
// schemas. Returns "" for a nil schema.
//
// LANE: example generator. Benign stub (returns an empty object) so the mapper
// lane can build before the real generator lands; overwritten wholesale by the
// example Dev.
func exampleJSON(s *schema) string {
	if s == nil {
		return ""
	}
	return "{}"
}

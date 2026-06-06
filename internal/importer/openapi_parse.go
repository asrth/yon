package importer

// decodeSpec parses raw OpenAPI/Swagger bytes (JSON or YAML; OpenAPI 3.x or
// Swagger 2.0) into the normalized *spec, resolving local $ref. It records
// non-fatal downgrades via report.add and returns an error only when the input
// is not a recognizable spec at all.
//
// LANE: parser. This stub is overwritten wholesale by the parser Dev.
func decodeSpec(data []byte, report *Report) (*spec, error) {
	panic("decodeSpec: not implemented")
}

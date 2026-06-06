package importer

import "github.com/ultramcu/yon/internal/model"

// specToCollection maps a normalized *spec into a Yon model.Collection:
// operations → requests grouped by tag into folders, BaseURL → a {{baseUrl}}
// collection variable, parameters → query/header params, body → a Body with a
// generated example (exampleJSON), and security → Auth. Non-representable bits
// are noted via report.add.
//
// LANE: mapper. This stub is overwritten wholesale by the mapper Dev.
func specToCollection(s *spec, report *Report) model.Collection {
	panic("specToCollection: not implemented")
}

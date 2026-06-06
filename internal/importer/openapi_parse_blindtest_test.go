package importer

// Blind test for issue #27 (OpenAPI/Swagger import), Parser lane.
//
// Written from the CONTRACT ONLY — no Dev implementation was consulted. These
// tests exercise decodeSpec (and the normalized *spec it produces) for OpenAPI
// 3.x and Swagger 2.0, in JSON and YAML, including $ref resolution, security
// schemes, request bodies, and the error path. Fixtures are local with the
// parseBT prefix so they don't collide with other files in the package.

import (
	"strings"
	"testing"
)

// parseBTOpenAPI30JSON is a small but complete OpenAPI 3.0 document: one
// server, one tagged operation with a query parameter and a JSON request body
// referencing a component schema, plus a bearer security scheme.
const parseBTOpenAPI30JSON = `{
  "openapi": "3.0.3",
  "info": { "title": "Pet Store", "version": "1.4.2" },
  "servers": [ { "url": "https://api.example.com/v2" } ],
  "paths": {
    "/pets": {
      "post": {
        "summary": "Create a pet",
        "operationId": "createPet",
        "tags": [ "pets" ],
        "parameters": [
          { "name": "dryRun", "in": "query", "required": true,
            "schema": { "type": "boolean" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/Pet" }
            }
          }
        },
        "security": [ { "bearerAuth": [] } ]
      }
    }
  },
  "components": {
    "schemas": {
      "Pet": {
        "type": "object",
        "required": [ "name" ],
        "properties": {
          "name": { "type": "string", "example": "Rex" },
          "age": { "type": "integer", "format": "int32" }
        }
      }
    },
    "securitySchemes": {
      "bearerAuth": { "type": "http", "scheme": "bearer", "bearerFormat": "JWT" }
    }
  }
}`

func TestDecodeSpecOpenAPI30JSON(t *testing.T) {
	var report Report
	s, err := decodeSpec([]byte(parseBTOpenAPI30JSON), &report)
	if err != nil {
		t.Fatalf("decodeSpec returned error: %v", err)
	}
	if s == nil {
		t.Fatal("decodeSpec returned nil *spec without error")
	}

	if s.Title != "Pet Store" {
		t.Errorf("Title = %q, want %q", s.Title, "Pet Store")
	}
	if !strings.HasPrefix(s.Version, "3") {
		t.Errorf("Version = %q, want one starting with %q", s.Version, "3")
	}
	if s.BaseURL != "https://api.example.com/v2" {
		t.Errorf("BaseURL = %q, want %q", s.BaseURL, "https://api.example.com/v2")
	}

	if len(s.Ops) != 1 {
		t.Fatalf("len(Ops) = %d, want 1", len(s.Ops))
	}
	op := s.Ops[0]
	if op.Method != "POST" {
		t.Errorf("op.Method = %q, want %q", op.Method, "POST")
	}
	if op.Path != "/pets" {
		t.Errorf("op.Path = %q, want %q", op.Path, "/pets")
	}
	// Name = summary || operationId || "METHOD path"; summary is set here.
	if op.Name != "Create a pet" {
		t.Errorf("op.Name = %q, want %q", op.Name, "Create a pet")
	}
	if op.Tag != "pets" {
		t.Errorf("op.Tag = %q, want %q", op.Tag, "pets")
	}

	// Query param: assert the robust parts (name, required, presence).
	if len(op.Query) != 1 {
		t.Fatalf("len(op.Query) = %d, want 1", len(op.Query))
	}
	if op.Query[0].Name != "dryRun" {
		t.Errorf("op.Query[0].Name = %q, want %q", op.Query[0].Name, "dryRun")
	}
	if !op.Query[0].Required {
		t.Error("op.Query[0].Required = false, want true")
	}

	// JSON request body.
	if op.Body == nil {
		t.Fatal("op.Body = nil, want non-nil reqBody")
	}
	if op.Body.ContentType != "application/json" {
		t.Errorf("op.Body.ContentType = %q, want %q", op.Body.ContentType, "application/json")
	}
	if op.Body.Schema == nil {
		t.Error("op.Body.Schema = nil, want non-nil")
	}

	// Security: bearer scheme recognized.
	scheme, ok := s.Schemes["bearerAuth"]
	if !ok {
		t.Fatalf("Schemes missing %q; have %v", "bearerAuth", keysOfParseBT(s.Schemes))
	}
	if scheme.Type != "bearer" {
		t.Errorf("Schemes[bearerAuth].Type = %q, want %q", scheme.Type, "bearer")
	}
	if !op.HasSecurity {
		t.Error("op.HasSecurity = false, want true")
	}
}

// parseBTOpenAPI30YAML is the SAME document as parseBTOpenAPI30JSON, written as
// real YAML (indentation-based), to pin YAML support.
const parseBTOpenAPI30YAML = `openapi: 3.0.3
info:
  title: Pet Store
  version: 1.4.2
servers:
  - url: https://api.example.com/v2
paths:
  /pets:
    post:
      summary: Create a pet
      operationId: createPet
      tags:
        - pets
      parameters:
        - name: dryRun
          in: query
          required: true
          schema:
            type: boolean
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Pet'
      security:
        - bearerAuth: []
components:
  schemas:
    Pet:
      type: object
      required:
        - name
      properties:
        name:
          type: string
          example: Rex
        age:
          type: integer
          format: int32
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
`

func TestDecodeSpecOpenAPI30YAMLEquivalent(t *testing.T) {
	var jsonReport, yamlReport Report
	jsonSpec, err := decodeSpec([]byte(parseBTOpenAPI30JSON), &jsonReport)
	if err != nil {
		t.Fatalf("decodeSpec(JSON) error: %v", err)
	}
	yamlSpec, err := decodeSpec([]byte(parseBTOpenAPI30YAML), &yamlReport)
	if err != nil {
		t.Fatalf("decodeSpec(YAML) error: %v", err)
	}
	if jsonSpec == nil || yamlSpec == nil {
		t.Fatal("decodeSpec returned nil *spec")
	}

	if len(yamlSpec.Ops) != len(jsonSpec.Ops) {
		t.Errorf("YAML Ops count = %d, JSON Ops count = %d; want equal",
			len(yamlSpec.Ops), len(jsonSpec.Ops))
	}
	if yamlSpec.BaseURL != jsonSpec.BaseURL {
		t.Errorf("YAML BaseURL = %q, JSON BaseURL = %q; want equal",
			yamlSpec.BaseURL, jsonSpec.BaseURL)
	}
	if len(yamlSpec.Ops) > 0 && len(jsonSpec.Ops) > 0 {
		if yamlSpec.Ops[0].Name != jsonSpec.Ops[0].Name {
			t.Errorf("YAML first op Name = %q, JSON first op Name = %q; want equal",
				yamlSpec.Ops[0].Name, jsonSpec.Ops[0].Name)
		}
	}
}

// parseBTSwagger20JSON is a Swagger 2.0 document: host + basePath + schemes
// compose the BaseURL; a body parameter references a definition resolved via
// $ref; an apiKey securityDefinition is present.
const parseBTSwagger20JSON = `{
  "swagger": "2.0",
  "info": { "title": "Legacy API", "version": "1.0.0" },
  "host": "host.example.com",
  "basePath": "/v1",
  "schemes": [ "https" ],
  "paths": {
    "/widgets": {
      "post": {
        "summary": "Make a widget",
        "operationId": "makeWidget",
        "tags": [ "widgets" ],
        "parameters": [
          { "name": "body", "in": "body", "required": true,
            "schema": { "$ref": "#/definitions/Widget" } }
        ]
      }
    }
  },
  "definitions": {
    "Widget": {
      "type": "object",
      "required": [ "sku" ],
      "properties": {
        "sku": { "type": "string" },
        "qty": { "type": "integer" }
      }
    }
  },
  "securityDefinitions": {
    "apiKeyAuth": { "type": "apiKey", "in": "header", "name": "X-API-Key" }
  }
}`

func TestDecodeSpecSwagger20JSON(t *testing.T) {
	var report Report
	s, err := decodeSpec([]byte(parseBTSwagger20JSON), &report)
	if err != nil {
		t.Fatalf("decodeSpec returned error: %v", err)
	}
	if s == nil {
		t.Fatal("decodeSpec returned nil *spec without error")
	}

	// host + basePath + schemes -> BaseURL.
	if s.BaseURL != "https://host.example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", s.BaseURL, "https://host.example.com/v1")
	}

	if len(s.Ops) != 1 {
		t.Fatalf("len(Ops) = %d, want 1", len(s.Ops))
	}
	op := s.Ops[0]
	if op.Method != "POST" {
		t.Errorf("op.Method = %q, want %q", op.Method, "POST")
	}
	if op.Path != "/widgets" {
		t.Errorf("op.Path = %q, want %q", op.Path, "/widgets")
	}

	// body parameter -> reqBody with the $ref resolved into the schema.
	if op.Body == nil {
		t.Fatal("op.Body = nil, want non-nil reqBody from body parameter")
	}
	if op.Body.Schema == nil {
		t.Fatal("op.Body.Schema = nil, want resolved schema")
	}
	if len(op.Body.Schema.Properties) == 0 {
		t.Errorf("op.Body.Schema.Properties is empty; want $ref to #/definitions/Widget resolved")
	}

	// apiKey securityDefinition recognized with In and Name preserved.
	scheme, ok := s.Schemes["apiKeyAuth"]
	if !ok {
		t.Fatalf("Schemes missing %q; have %v", "apiKeyAuth", keysOfParseBT(s.Schemes))
	}
	if scheme.Type != "apiKey" {
		t.Errorf("Schemes[apiKeyAuth].Type = %q, want %q", scheme.Type, "apiKey")
	}
	if scheme.In != "header" {
		t.Errorf("Schemes[apiKeyAuth].In = %q, want %q", scheme.In, "header")
	}
	if scheme.Name != "X-API-Key" {
		t.Errorf("Schemes[apiKeyAuth].Name = %q, want %q", scheme.Name, "X-API-Key")
	}
}

// parseBTRefOpenAPI exercises $ref resolution for a property whose schema is a
// component reference: after decoding the referenced schema's fields should be
// populated, not left as a bare unresolved ref.
const parseBTRefOpenAPI = `{
  "openapi": "3.0.0",
  "info": { "title": "Ref API", "version": "3.0.0" },
  "servers": [ { "url": "https://ref.example.com" } ],
  "paths": {
    "/orders": {
      "post": {
        "summary": "Place order",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "customer": { "$ref": "#/components/schemas/Customer" }
                }
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "Customer": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "name": { "type": "string" }
        }
      }
    }
  }
}`

func TestDecodeSpecResolvesPropertyRef(t *testing.T) {
	var report Report
	s, err := decodeSpec([]byte(parseBTRefOpenAPI), &report)
	if err != nil {
		t.Fatalf("decodeSpec returned error: %v", err)
	}
	if s == nil || len(s.Ops) == 0 {
		t.Fatal("decodeSpec produced no operations")
	}
	op := s.Ops[0]
	if op.Body == nil || op.Body.Schema == nil {
		t.Fatal("op.Body or op.Body.Schema is nil; want a resolved request body schema")
	}
	customer, ok := op.Body.Schema.Properties["customer"]
	if !ok {
		t.Fatalf("body schema missing 'customer' property; have %v",
			propKeysParseBT(op.Body.Schema))
	}
	if customer == nil {
		t.Fatal("'customer' property schema is nil; $ref not resolved")
	}
	// $ref to Customer should leave Type/Properties populated, not a bare ref.
	if customer.Type != "object" {
		t.Errorf("resolved customer.Type = %q, want %q", customer.Type, "object")
	}
	if len(customer.Properties) == 0 {
		t.Error("resolved customer.Properties is empty; $ref to #/components/schemas/Customer not resolved")
	}
}

func TestDecodeSpecErrorOnUnknownFormat(t *testing.T) {
	cases := map[string]string{
		"plain object, no version key": `{"foo":1}`,
		"junk":                         `not json at all }{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var report Report
			_, err := decodeSpec([]byte(body), &report)
			if err == nil {
				t.Errorf("decodeSpec(%q) returned nil error; want non-nil", body)
			}
		})
	}
}

// parseBTNoTagsOpenAPI has an operation with no tags; op.Tag must be "" so the
// downstream tag->folder mapping treats it as untagged.
const parseBTNoTagsOpenAPI = `{
  "openapi": "3.0.0",
  "info": { "title": "No Tags", "version": "3.0.0" },
  "servers": [ { "url": "https://nt.example.com" } ],
  "paths": {
    "/ping": {
      "get": {
        "summary": "Ping",
        "operationId": "ping"
      }
    }
  }
}`

func TestDecodeSpecUntaggedOperationHasEmptyTag(t *testing.T) {
	var report Report
	s, err := decodeSpec([]byte(parseBTNoTagsOpenAPI), &report)
	if err != nil {
		t.Fatalf("decodeSpec returned error: %v", err)
	}
	if s == nil || len(s.Ops) != 1 {
		t.Fatalf("want exactly 1 op, got spec=%v", s)
	}
	if s.Ops[0].Tag != "" {
		t.Errorf("op.Tag = %q, want %q (no tags -> empty)", s.Ops[0].Tag, "")
	}
}

// keysOfParseBT returns the keys of a Schemes map for diagnostic messages.
func keysOfParseBT(m map[string]secScheme) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// propKeysParseBT returns the property names of a schema for diagnostics.
func propKeysParseBT(s *schema) []string {
	if s == nil {
		return nil
	}
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		keys = append(keys, k)
	}
	return keys
}

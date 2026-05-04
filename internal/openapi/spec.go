// Package openapi builds the OpenAPI 3.1 document for the mcsm REST
// surface. We hand-author it (rather than reflect on http.ServeMux)
// because the spec value lies in the request/response schemas, which
// reflection can't infer reliably from runtime handlers.
//
// The generated document is what /openapi.json returns; tests verify
// it is well-formed JSON and includes every route registered by the
// router.
package openapi

import (
	"encoding/json"
)

// Doc is the top-level OpenAPI 3.1 document.
type Doc struct {
	OpenAPI    string                  `json:"openapi"`
	Info       Info                    `json:"info"`
	Servers    []Server                `json:"servers,omitempty"`
	Paths      map[string]PathItem     `json:"paths"`
	Components Components              `json:"components"`
	Security   []map[string][]string   `json:"security,omitempty"`
	Tags       []Tag                   `json:"tags,omitempty"`
}

type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
	Summary string `json:"summary,omitempty"`
}

type Server struct {
	URL string `json:"url"`
}

type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Patch  *Operation `json:"patch,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
}

type Operation struct {
	Tags        []string             `json:"tags,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	OperationID string               `json:"operationId,omitempty"`
	Parameters  []Parameter          `json:"parameters,omitempty"`
	RequestBody *RequestBody         `json:"requestBody,omitempty"`
	Responses   map[string]Response  `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"` // path | query | header
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Schema      Schema `json:"schema,omitempty"`
}

type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type MediaType struct {
	Schema Schema `json:"schema,omitempty"`
}

type Schema struct {
	Ref         string             `json:"$ref,omitempty"`
	Type        string             `json:"type,omitempty"`
	Format      string             `json:"format,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Description string             `json:"description,omitempty"`
}

type Components struct {
	Schemas         map[string]*Schema           `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme    `json:"securitySchemes,omitempty"`
}

type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Description  string `json:"description,omitempty"`
}

// JSON returns the canonical JSON encoding of the spec.
func (d *Doc) JSON() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

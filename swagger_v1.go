package main

// -----------------------------------------------------------------------------
// Swagger 1.x (1.0 / 1.1 / 1.2) support.
//
// There is no maintained Go library for Swagger 1.x, so this is a hand-rolled,
// best-effort parser. Swagger 1.x splits an API across a top-level "resource
// listing" plus one "API declaration" per resource. When the spec is loaded
// from a URL we can fetch the sub-declarations; from a single file we can only
// parse what is embedded. JSON only (1.x predates YAML specs).
//
// This is the least-tested path. If your 1.x spec is exotic, expect rough edges.
// -----------------------------------------------------------------------------

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type v1ResourceListing struct {
	SwaggerVersion string      `json:"swaggerVersion"`
	BasePath       string      `json:"basePath"`
	APIs           []v1APIRef  `json:"apis"`
	Models         interface{} `json:"models"`
}

type v1APIRef struct {
	Path        string        `json:"path"`
	Description  string        `json:"description"`
	Operations   []v1Operation `json:"operations"`   // present in embedded/merged docs
}

type v1Declaration struct {
	BasePath     string  `json:"basePath"`
	ResourcePath string  `json:"resourcePath"`
	APIs         []v1API `json:"apis"`
}

type v1API struct {
	Path       string        `json:"path"`
	Operations []v1Operation `json:"operations"`
}

type v1Operation struct {
	Method     string    `json:"method"`     // 1.2
	HTTPMethod string    `json:"httpMethod"` // 1.0/1.1
	Nickname   string    `json:"nickname"`
	Parameters []v1Param `json:"parameters"`
}

func (o v1Operation) method() string {
	if o.Method != "" {
		return strings.ToUpper(o.Method)
	}
	return strings.ToUpper(o.HTTPMethod)
}

type v1Param struct {
	Name      string `json:"name"`
	ParamType string `json:"paramType"` // path | query | header | body | form
	Required  bool   `json:"required"`
	Type      string `json:"type"`
	Format    string `json:"format"`
}

// fetcher fetches a sub-document; injected so we reuse the same HTTP settings.
type fetcher func(rawurl string) ([]byte, error)

func parseV1(data []byte, base *url.URL, fetch fetcher) (Spec, error) {
	var rl v1ResourceListing
	if err := json.Unmarshal(data, &rl); err != nil {
		return Spec{}, fmt.Errorf("unmarshal swagger 1.x: %w", err)
	}

	var spec Spec
	embedded := false
	for _, a := range rl.APIs {
		if len(a.Operations) > 0 {
			embedded = true
			spec.Operations = append(spec.Operations, v1Ops(a.Path, a.Operations)...)
		}
	}
	if embedded {
		if rl.BasePath != "" {
			spec.Servers = []string{rl.BasePath}
		} else if base != nil && base.Scheme != "file" {
			spec.Servers = []string{originOf(base)}
		}
		return spec, nil
	}

	// Not embedded: each apis[].path points to an API declaration we must fetch.
	if fetch == nil || base == nil || base.Scheme == "file" {
		return Spec{}, fmt.Errorf(
			"swagger 1.x spec references separate API declarations; load it from a URL so they can be fetched")
	}

	for _, a := range rl.APIs {
		declURL := resolveV1DeclURL(base, rl.BasePath, a.Path)
		body, err := fetch(declURL)
		if err != nil {
			fmt.Printf("[warn] fetch v1 declaration %s: %v\n", declURL, err)
			continue
		}
		var decl v1Declaration
		if err := json.Unmarshal(body, &decl); err != nil {
			fmt.Printf("[warn] parse v1 declaration %s: %v\n", declURL, err)
			continue
		}
		if decl.BasePath != "" && len(spec.Servers) == 0 {
			spec.Servers = []string{decl.BasePath}
		}
		for _, api := range decl.APIs {
			spec.Operations = append(spec.Operations, v1Ops(api.Path, api.Operations)...)
		}
	}
	if len(spec.Servers) == 0 && base != nil {
		spec.Servers = []string{originOf(base)}
	}
	return spec, nil
}

func v1Ops(path string, ops []v1Operation) []Operation {
	var out []Operation
	for _, o := range ops {
		op := Operation{Method: o.method(), Path: path}
		for _, p := range o.Parameters {
			switch p.ParamType {
			case "body":
				op.Body = &Body{
					ContentType: "application/json",
					Schema:      &Schema{Type: v1Type(p.Type), Format: p.Format},
				}
			case "path", "query", "header":
				op.Params = append(op.Params, Param{
					Name:     p.Name,
					In:       ParamLoc(p.ParamType),
					Required: p.Required,
					Schema:   &Schema{Type: v1Type(p.Type), Format: p.Format},
				})
			case "form":
				// Treat form fields as query params (best effort).
				op.Params = append(op.Params, Param{
					Name:   p.Name,
					In:     InQuery,
					Schema: &Schema{Type: v1Type(p.Type), Format: p.Format},
				})
			}
		}
		out = append(out, op)
	}
	return out
}

func v1Type(t string) string {
	switch strings.ToLower(t) {
	case "integer", "int", "long":
		return "integer"
	case "number", "float", "double":
		return "number"
	case "boolean", "bool":
		return "boolean"
	case "array":
		return "array"
	case "":
		return ""
	default:
		return "string"
	}
}

func resolveV1DeclURL(base *url.URL, listingBasePath, apiPath string) string {
	// If the declaration path is absolute, use it directly.
	if strings.HasPrefix(apiPath, "http://") || strings.HasPrefix(apiPath, "https://") {
		return apiPath
	}
	root := listingBasePath
	if root == "" {
		root = strings.TrimSuffix(base.String(), path0(base))
	}
	return strings.TrimRight(root, "/") + "/" + strings.TrimLeft(apiPath, "/")
}

func path0(u *url.URL) string { return u.Path }

func originOf(u *url.URL) string {
	return u.Scheme + "://" + u.Host
}

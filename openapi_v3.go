package main

// -----------------------------------------------------------------------------
// This is the ONLY file that touches kin-openapi. If a `go build` error mentions
// kin-openapi, it will be here. The library's API has shifted across releases;
// this targets github.com/getkin/kin-openapi v0.127.0. The most likely spots to
// need adjustment on a different version are marked with:  // API-CHURN
// -----------------------------------------------------------------------------

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

// newLoader returns a loader configured to resolve external refs, including
// remote ones fetched over HTTP.
//
// API-CHURN: ReadFromURIs / ReadFromHTTP / ReadFromFile symbol names have
// occasionally changed. If the build fails here, drop the ReadFromURIFunc line;
// local/self-contained specs will still work, only remote-ref fetching is lost.
func newLoader() *openapi3.Loader {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.Context = context.Background()
	loader.ReadFromURIFunc = openapi3.ReadFromURIs(
		openapi3.ReadFromHTTP(http.DefaultClient),
		openapi3.ReadFromFile,
	)
	return loader
}

// parseV3 loads an OpenAPI 3.x document from raw bytes, resolving refs
// (including remote ones) relative to base.
func parseV3(data []byte, base *url.URL) (Spec, error) {
	loader := newLoader()
	doc, err := loader.LoadFromDataWithPath(data, base)
	if err != nil {
		return Spec{}, fmt.Errorf("load openapi3: %w", err)
	}
	return fromV3Doc(doc), nil
}

// parseV2 loads a Swagger 2.0 document, converts it to v3, then resolves refs.
func parseV2(data []byte, base *url.URL) (Spec, error) {
	jsonBytes, err := toJSON(data)
	if err != nil {
		return Spec{}, err
	}
	var doc2 openapi2.T
	if err := json.Unmarshal(jsonBytes, &doc2); err != nil {
		return Spec{}, fmt.Errorf("unmarshal swagger2: %w", err)
	}
	doc3, err := openapi2conv.ToV3(&doc2)
	if err != nil {
		return Spec{}, fmt.Errorf("convert swagger2->openapi3: %w", err)
	}
	loader := newLoader()
	// Populate .Value on internal (and, where possible, remote) refs.
	if err := loader.ResolveRefsIn(doc3, base); err != nil {
		// Non-fatal: many specs still work with partially-resolved refs.
		fmt.Printf("[warn] ref resolution (v2): %v\n", err)
	}
	return fromV3Doc(doc3), nil
}

func fromV3Doc(doc *openapi3.T) Spec {
	var spec Spec
	for _, srv := range doc.Servers {
		if u := expandServer(srv); u != "" {
			spec.Servers = append(spec.Servers, u)
		}
	}

	// API-CHURN: in <=v0.121 this was `for path, item := range doc.Paths` (a map).
	// v0.122+ uses doc.Paths.Map().
	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		for method, op := range item.Operations() {
			o := Operation{Method: method, Path: path}

			// Path-level params apply to every operation, then op-level params.
			var all openapi3.Parameters
			all = append(all, item.Parameters...)
			all = append(all, op.Parameters...)
			for _, pr := range all {
				if pr == nil || pr.Value == nil {
					continue
				}
				pv := pr.Value
				o.Params = append(o.Params, Param{
					Name:     pv.Name,
					In:       ParamLoc(pv.In),
					Required: pv.Required,
					Example:  pv.Example,
					Schema:   convSchema(pv.Schema, 0),
				})
			}

			if op.RequestBody != nil && op.RequestBody.Value != nil {
				o.Body = pickBody(op.RequestBody.Value.Content)
			}
			spec.Operations = append(spec.Operations, o)
		}
	}
	return spec
}

// pickBody prefers a JSON media type, falling back to the first available one.
func pickBody(content openapi3.Content) *Body {
	if len(content) == 0 {
		return nil
	}
	var fallbackCT string
	var fallbackMT *openapi3.MediaType
	for ct, mt := range content {
		if strings.Contains(ct, "json") {
			return &Body{ContentType: ct, Schema: convSchema(mt.Schema, 0), Example: mt.Example}
		}
		if fallbackMT == nil {
			fallbackCT, fallbackMT = ct, mt
		}
	}
	return &Body{ContentType: fallbackCT, Schema: convSchema(fallbackMT.Schema, 0), Example: fallbackMT.Example}
}

// convSchema translates a kin-openapi schema into our internal Schema, guarding
// against cyclic references via a depth cap.
func convSchema(ref *openapi3.SchemaRef, depth int) *Schema {
	if ref == nil || ref.Value == nil || depth > maxDepth {
		return nil
	}
	v := ref.Value
	s := &Schema{
		Type:     typeString(v), // API-CHURN: see helper below
		Format:   v.Format,
		Example:  v.Example,
		Default:  v.Default,
		Enum:     v.Enum,
		Required: v.Required,
	}
	if v.Items != nil {
		s.Items = convSchema(v.Items, depth+1)
	}
	if len(v.Properties) > 0 {
		s.Props = make(map[string]*Schema, len(v.Properties))
		for k, pr := range v.Properties {
			s.Props[k] = convSchema(pr, depth+1)
		}
	}
	return s
}

// typeString extracts the primary type name.
//
// API-CHURN: in v0.120+ Schema.Type is *openapi3.Types ([]string). In older
// versions it was a plain string. For a string-typed version, replace the body
// with:  return v.Type
func typeString(v *openapi3.Schema) string {
	if v.Type == nil {
		return ""
	}
	t := *v.Type // []string
	if len(t) == 0 {
		return ""
	}
	return t[0]
}

// expandServer builds an absolute URL from a server entry, substituting the
// default value for any {variables}.
func expandServer(srv *openapi3.Server) string {
	u := srv.URL
	for name, variable := range srv.Variables {
		if variable != nil {
			u = strings.ReplaceAll(u, "{"+name+"}", variable.Default)
		}
	}
	return u
}

// toJSON accepts JSON or YAML bytes and returns JSON. yaml.v3 decodes mappings
// with string keys, so a subsequent json.Marshal is clean.
func toJSON(data []byte) ([]byte, error) {
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse spec (yaml/json): %w", err)
	}
	return json.Marshal(raw)
}

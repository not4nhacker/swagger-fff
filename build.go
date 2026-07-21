package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var leftoverPathParam = regexp.MustCompile(`\{[^}]+\}`)
var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// buildEndpoints turns each Operation into a ready-to-send Endpoint and assigns
// a unique output filename. Filenames are computed here (single-threaded) so the
// concurrent fetchers never race on names.
func buildEndpoints(base string, spec Spec, extraHeaders map[string]string) ([]Endpoint, error) {
	used := map[string]bool{}
	out := make([]Endpoint, 0, len(spec.Operations))

	for _, op := range spec.Operations {
		ep, err := buildOne(base, op)
		if err != nil {
			fmt.Printf("[warn] skipping %s %s: %v\n", op.Method, op.Path, err)
			continue
		}
		// apply user -H headers last so they win
		for k, v := range extraHeaders {
			ep.Headers[k] = v
		}
		ep.OutFile = uniqueName(op.Method, op.Path, used)
		out = append(out, ep)
	}
	return out, nil
}

func buildOne(base string, op Operation) (Endpoint, error) {
	path := op.Path
	query := url.Values{}
	headers := map[string]string{}
	cookies := map[string]string{}

	for _, p := range op.Params {
		val := paramValue(p)
		switch p.In {
		case InPath:
			path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(val))
		case InQuery:
			query.Add(p.Name, val)
		case InHeader:
			headers[p.Name] = val
		case InCookie:
			cookies[p.Name] = val
		}
	}

	// Any path params that were declared implicitly (not in the param list) get a safe default.
	path = leftoverPathParam.ReplaceAllString(path, "1")

	full := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	if enc := query.Encode(); enc != "" {
		full += "?" + enc
	}
	if _, err := url.Parse(full); err != nil {
		return Endpoint{}, fmt.Errorf("bad url %q: %w", full, err)
	}

	ep := Endpoint{
		Method:  strings.ToUpper(op.Method),
		URL:     full,
		Headers: headers,
		Cookies: cookies,
	}

	if op.Body != nil {
		var payload interface{}
		if op.Body.Example != nil {
			payload = op.Body.Example
		} else {
			payload = genValue(op.Body.Schema, 0)
		}
		b, err := json.Marshal(payload)
		if err == nil {
			ep.Body = b
			ep.ContentType = op.Body.ContentType
			if ep.ContentType == "" {
				ep.ContentType = "application/json"
			}
		}
	}
	return ep, nil
}

// uniqueName -> e.g. "GET__users_id.txt", collisions get _2, _3, ...
func uniqueName(method, path string, used map[string]bool) string {
	p := strings.Trim(path, "/")
	if p == "" {
		p = "root"
	}
	p = strings.ReplaceAll(p, "/", "_")
	p = unsafeFilename.ReplaceAllString(p, "_")
	p = strings.Trim(p, "_")

	base := strings.ToUpper(method) + "__" + p
	name := base + ".txt"
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d.txt", base, i)
	}
	used[name] = true
	return name
}

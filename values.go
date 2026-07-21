package main

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

const maxDepth = 8 // guard against self-referential schemas

// genValue produces a plausible value whose *type* matches the schema.
// Precedence: example > default > first enum > type-based synthesis.
func genValue(s *Schema, depth int) interface{} {
	if s == nil || depth > maxDepth {
		return "test"
	}
	if s.Example != nil {
		return s.Example
	}
	if s.Default != nil {
		return s.Default
	}
	if len(s.Enum) > 0 {
		return s.Enum[0]
	}

	switch s.Type {
	case "integer":
		return 1
	case "number":
		return 1.1
	case "boolean":
		return true
	case "array":
		return []interface{}{genValue(s.Items, depth+1)}
	case "object":
		return genObject(s, depth)
	case "string":
		return stringForFormat(s.Format)
	default:
		// No explicit type. If it looks like an object, treat it as one.
		if len(s.Props) > 0 {
			return genObject(s, depth)
		}
		if s.Items != nil {
			return []interface{}{genValue(s.Items, depth+1)}
		}
		return "test"
	}
}

func genObject(s *Schema, depth int) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range s.Props {
		m[k] = genValue(v, depth+1)
	}
	return m
}

// stringForFormat returns a value that satisfies common string formats so that
// servers validating input are more likely to accept the request.
func stringForFormat(f string) string {
	switch f {
	case "date":
		return "2024-01-01"
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "email":
		return "test@example.com"
	case "uuid":
		return "00000000-0000-0000-0000-000000000000"
	case "uri", "url":
		return "https://example.com"
	case "hostname":
		return "example.com"
	case "ipv4":
		return "127.0.0.1"
	case "ipv6":
		return "::1"
	case "byte":
		return base64.StdEncoding.EncodeToString([]byte("test"))
	case "password":
		return "password"
	default:
		return "test"
	}
}

// scalar renders a value as a single string, suitable for a path segment,
// query value, or header value. Arrays are comma-joined (OpenAPI default).
func scalar(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, scalar(e))
		}
		return strings.Join(parts, ",")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// paramValue picks the string to use for a single parameter.
func paramValue(p Param) string {
	if p.Example != nil {
		return scalar(p.Example)
	}
	return scalar(genValue(p.Schema, 0))
}

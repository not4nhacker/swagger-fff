package main

// This file defines a small, library-independent representation of an API spec.
// Every parser (v1 / v2 / v3) normalizes into these types, so the rest of the
// program (value synthesis, request building, fetching) never touches
// kin-openapi or any other spec library directly.

type ParamLoc string

const (
	InPath   ParamLoc = "path"
	InQuery  ParamLoc = "query"
	InHeader ParamLoc = "header"
	InCookie ParamLoc = "cookie"
)

// Schema is a trimmed-down JSON-schema-ish node: only the bits we need to
// synthesize a plausible, type-correct value.
type Schema struct {
	Type     string
	Format   string
	Example  interface{}
	Default  interface{}
	Enum     []interface{}
	Items    *Schema            // for arrays
	Props    map[string]*Schema // for objects
	Required []string
}

type Param struct {
	Name     string
	In       ParamLoc
	Required bool
	Example  interface{}
	Schema   *Schema
}

type Body struct {
	ContentType string
	Schema      *Schema
	Example     interface{}
}

type Operation struct {
	Method string
	Path   string
	Params []Param
	Body   *Body
}

// Spec is the normalized output of any parser.
type Spec struct {
	Servers    []string // absolute base URLs, e.g. https://api.example.com/v2
	Operations []Operation
}

// Endpoint is a fully-resolved, ready-to-send HTTP request.
type Endpoint struct {
	Method      string
	URL         string
	Headers     map[string]string
	Cookies     map[string]string
	Body        []byte
	ContentType string
	OutFile     string // pre-computed, collision-free output filename
}

package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// versionProbe peeks at the top-level keys to decide which parser to use.
type versionProbe struct {
	OpenAPI        string      `yaml:"openapi" json:"openapi"`
	Swagger        string      `yaml:"swagger" json:"swagger"`
	SwaggerVersion string      `yaml:"swaggerVersion" json:"swaggerVersion"`
	APIs           interface{} `yaml:"apis" json:"apis"`
}

// loadSpec reads the source (URL or file), detects the version, and returns a
// normalized Spec. specFetch is used both for the top-level doc and, for v1,
// for sub-declarations.
func loadSpec(src, baseOverride string, headers map[string]string, insecure bool, timeout time.Duration) (Spec, error) {
	fetch := makeFetcher(headers, insecure, timeout)

	var data []byte
	var base *url.URL
	var err error

	if isURL(src) {
		data, err = fetch(src)
		if err != nil {
			return Spec{}, fmt.Errorf("fetch spec: %w", err)
		}
		base, _ = url.Parse(src)
	} else {
		data, err = os.ReadFile(src)
		if err != nil {
			return Spec{}, fmt.Errorf("read spec file: %w", err)
		}
		abs, _ := filepath.Abs(src)
		base = &url.URL{Scheme: "file", Path: abs}
	}

	var pr versionProbe
	_ = yaml.Unmarshal(data, &pr) // JSON is valid YAML, so this covers both

	switch {
	case strings.HasPrefix(pr.OpenAPI, "3"):
		return parseV3(data, base)
	case pr.Swagger == "2.0" || strings.HasPrefix(pr.Swagger, "2"):
		return parseV2(data, base)
	case strings.HasPrefix(pr.SwaggerVersion, "1") ||
		strings.HasPrefix(pr.Swagger, "1") ||
		(pr.APIs != nil && pr.OpenAPI == ""):
		return parseV1(data, base, fetch)
	default:
		return Spec{}, fmt.Errorf(
			"could not detect spec version (openapi=%q swagger=%q swaggerVersion=%q)",
			pr.OpenAPI, pr.Swagger, pr.SwaggerVersion)
	}
}

func makeFetcher(headers map[string]string, insecure bool, timeout time.Duration) fetcher {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
		},
	}
	return func(rawurl string) ([]byte, error) {
		req, err := http.NewRequest(http.MethodGet, rawurl, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("status %s fetching %s", resp.Status, rawurl)
		}
		return io.ReadAll(resp.Body)
	}
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

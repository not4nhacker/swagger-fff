package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type fetchConfig struct {
	OutDir    string
	Workers   int
	Delay     time.Duration
	Timeout   time.Duration
	Insecure  bool
	StoreReqs bool
}

func newClient(cfg fetchConfig) *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure},
	}
	return &http.Client{
		Timeout:   cfg.Timeout,
		Transport: tr,
		// Do not follow redirects; return the 3xx as-is.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func runFetches(eps []Endpoint, cfg fetchConfig) {
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create output dir: %v\n", err)
		os.Exit(1)
	}
	client := newClient(cfg)

	jobs := make(chan Endpoint)
	var wg sync.WaitGroup
	var done int64
	var mu sync.Mutex

	worker := func() {
		defer wg.Done()
		for ep := range jobs {
			if cfg.Delay > 0 {
				time.Sleep(cfg.Delay)
			}
			status := fetchOne(client, ep, cfg)
			mu.Lock()
			done++
			fmt.Printf("[%d/%d] %-6s %s -> %s\n", done, len(eps), ep.Method, ep.URL, status)
			mu.Unlock()
		}
	}

	n := cfg.Workers
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go worker()
	}
	for _, ep := range eps {
		jobs <- ep
	}
	close(jobs)
	wg.Wait()
}

// fetchOne performs the request and writes the response (and optionally the
// request) to the endpoint's output file. Returns a short status string for logging.
func fetchOne(client *http.Client, ep Endpoint, cfg fetchConfig) string {
	var reqBody io.Reader
	if len(ep.Body) > 0 {
		reqBody = bytes.NewReader(ep.Body)
	}
	req, err := http.NewRequest(ep.Method, ep.URL, reqBody)
	if err != nil {
		writeError(cfg.OutDir, ep, err)
		return "REQUEST-ERROR"
	}
	for k, v := range ep.Headers {
		req.Header.Set(k, v)
	}
	if len(ep.Cookies) > 0 {
		var parts []string
		for k, v := range ep.Cookies {
			parts = append(parts, k+"="+v)
		}
		sort.Strings(parts)
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	if ep.ContentType != "" && len(ep.Body) > 0 {
		req.Header.Set("Content-Type", ep.ContentType)
	}

	resp, err := client.Do(req)
	if err != nil {
		writeError(cfg.OutDir, ep, err)
		return "ERROR"
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var buf bytes.Buffer
	if cfg.StoreReqs {
		writeRequestBlock(&buf, req, ep)
		buf.WriteString("\n### RESPONSE ###\n")
	}
	writeResponseBlock(&buf, resp, body)

	path := filepath.Join(cfg.OutDir, ep.OutFile)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
	}
	return resp.Status
}

func writeRequestBlock(buf *bytes.Buffer, req *http.Request, ep Endpoint) {
	buf.WriteString("### REQUEST ###\n")
	fmt.Fprintf(buf, "%s %s HTTP/1.1\n", req.Method, req.URL.RequestURI())
	fmt.Fprintf(buf, "Host: %s\n", req.URL.Host)
	writeHeaders(buf, req.Header)
	buf.WriteString("\n")
	if len(ep.Body) > 0 {
		buf.Write(ep.Body)
		buf.WriteString("\n")
	}
}

func writeResponseBlock(buf *bytes.Buffer, resp *http.Response, body []byte) {
	// Status line: e.g. "HTTP/1.1 200 OK"
	fmt.Fprintf(buf, "%s %s\n", resp.Proto, resp.Status)
	writeHeaders(buf, resp.Header)
	buf.WriteString("\n")
	buf.Write(body)
}

func writeHeaders(buf *bytes.Buffer, h http.Header) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			fmt.Fprintf(buf, "%s: %s\n", k, v)
		}
	}
}

func writeError(dir string, ep Endpoint, err error) {
	path := filepath.Join(dir, ep.OutFile)
	msg := fmt.Sprintf("REQUEST FAILED\n%s %s\nerror: %v\n", ep.Method, ep.URL, err)
	_ = os.WriteFile(path, []byte(msg), 0o644)
}

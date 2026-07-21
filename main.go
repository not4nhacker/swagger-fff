package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// headerList collects repeated -H flags.
type headerList []string

func (h *headerList) String() string { return strings.Join(*h, ", ") }
func (h *headerList) Set(v string) error {
	*h = append(*h, v)
	return nil
}

func main() {
	var (
		baseURL   = flag.String("base-url", "", "override the server/base URL from the spec")
		outDir    = flag.String("o", "out", "output directory")
		workers   = flag.Int("c", 20, "number of concurrent requests")
		insecure  = flag.Bool("k", false, "skip TLS certificate verification")
		storeReqs = flag.Bool("store-reqs", false, "also store the request that was sent")
		timeout   = flag.Duration("timeout", 10*time.Second, "per-request timeout (e.g. 5s, 500ms)")
		delay     = flag.Duration("delay", 0, "delay before each request (e.g. 200ms)")
	)
	var headers headerList
	flag.Var(&headers, "H", "HTTP header 'Name: value' (repeatable)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "swagger-fff — fetch every endpoint in a Swagger/OpenAPI spec\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  swagger-fff [flags] <spec-url-or-file>\n\nFlags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  swagger-fff -H 'Authorization: Bearer x' -H 'X-Env: staging' https://api.example.com/openapi.json\n")
		fmt.Fprintf(os.Stderr, "  swagger-fff -o results --store-reqs -c 40 ./spec.yaml\n")
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	src := flag.Arg(0)

	hdrMap, err := parseHeaders(headers)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// 1. Load + parse the spec into a normalized form.
	spec, err := loadSpec(src, *baseURL, hdrMap, *insecure, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(spec.Operations) == 0 {
		fmt.Fprintln(os.Stderr, "no operations found in spec")
		os.Exit(1)
	}

	// 2. Decide the base URL to hit.
	base, err := resolveBaseURL(*baseURL, spec.Servers, src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Base URL: %s\n", base)
	fmt.Printf("Operations: %d\n", len(spec.Operations))

	// 3. Build concrete requests (params/bodies filled, filenames assigned).
	eps, err := buildEndpoints(base, spec, hdrMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// 4. Fetch everything concurrently and store responses.
	runFetches(eps, fetchConfig{
		OutDir:    *outDir,
		Workers:   *workers,
		Delay:     *delay,
		Timeout:   *timeout,
		Insecure:  *insecure,
		StoreReqs: *storeReqs,
	})

	fmt.Printf("\nDone. Responses written to %s/\n", *outDir)
}

func parseHeaders(list headerList) (map[string]string, error) {
	m := map[string]string{}
	for _, h := range list {
		idx := strings.Index(h, ":")
		if idx < 0 {
			return nil, fmt.Errorf("invalid header %q (expected 'Name: value')", h)
		}
		name := strings.TrimSpace(h[:idx])
		val := strings.TrimSpace(h[idx+1:])
		if name == "" {
			return nil, fmt.Errorf("invalid header %q (empty name)", h)
		}
		m[name] = val
	}
	return m, nil
}

// resolveBaseURL applies the precedence:
//  1. -base-url flag wins outright
//  2. exactly one server in the spec -> use it
//  3. multiple servers -> prompt the user to choose at runtime
//  4. no servers but spec came from a URL -> use that URL's origin
func resolveBaseURL(override string, servers []string, src string) (string, error) {
	if override != "" {
		return override, nil
	}

	// Make relative servers absolute using the source origin when possible.
	servers = absolutize(servers, src)

	switch len(servers) {
	case 1:
		return servers[0], nil
	case 0:
		if isURL(src) {
			if u, err := url.Parse(src); err == nil {
				return u.Scheme + "://" + u.Host, nil
			}
		}
		return "", fmt.Errorf("spec defines no server URL; pass -base-url")
	default:
		return promptServer(servers)
	}
}

func absolutize(servers []string, src string) []string {
	if !isURL(src) {
		return servers
	}
	u, err := url.Parse(src)
	if err != nil {
		return servers
	}
	origin := u.Scheme + "://" + u.Host
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			out = append(out, s)
		} else {
			out = append(out, strings.TrimRight(origin, "/")+"/"+strings.TrimLeft(s, "/"))
		}
	}
	return out
}

func promptServer(servers []string) (string, error) {
	fmt.Println("Multiple servers found in the spec. Choose one:")
	for i, s := range servers {
		fmt.Printf("  [%d] %s\n", i+1, s)
	}
	fmt.Print("Enter number: ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading choice: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(servers) {
		return "", fmt.Errorf("invalid choice %q", strings.TrimSpace(line))
	}
	return servers[n-1], nil
}

# swagger-fff

A Swagger / OpenAPI fetcher tool inspired by Tomnomnom's awesome [fff](https://github.com/tomnomnom/fff). Given a spec, it parses every path + method, synthesizes type-correct parameters and request bodies, fires all requests concurrently, and writes each raw HTTP response to its own file.

## Installation

You can install this with:
```bash
go install github.com/not4nhacker/swagger-fff
```

Or you can built it from sources (tryharder):
```bash
cd swagger-fff
go mod tidy
go build -o swagger-fff .
```

## Usage

```
swagger-fff [flags] <spec-url-or-file>
```

| Flag           | Default | Meaning                                                        |
|----------------|---------|----------------------------------------------------------------|
| `-H`           | –       | HTTP header `Name: value`. **Repeatable** (`-H a -H b`).       |
| `-base-url`    | –       | Override the server/base URL from the spec.                    |
| `-o`           | `./out`   | Output directory.                                              |
| `-c`           | `20`    | Concurrent requests.                                           |
| `-k`           | `false` | Skip TLS certificate verification.                             |
| `-store-reqs`  | `false` | Also store the request that was sent (prepended to the file).  |
| `-timeout`     | `10s`   | Per-request timeout.                                           |
| `-delay`       | `0`     | Delay before each request (e.g. `200ms`).                      |

The spec argument is auto-detected: anything starting with `http://` / `https://` is fetched; otherwise it's read as a local file.

### Examples

```bash
swagger-fff -H 'Authorization: Bearer TOKEN' -H 'X-Env: staging' \
  https://api.example.com/openapi.json

swagger-fff -o results --store-reqs -c 40 -k ./spec.yaml
```

## Files

| File              | Responsibility                                             |
|-------------------|------------------------------------------------------------|
| `main.go`         | Flags, base-URL resolution (incl. runtime prompt), orchestration |
| `load.go`         | Read source, detect version, dispatch to a parser          |
| `openapi_v3.go`   | OpenAPI 3.x + Swagger 2.0 parsing (**only file using kin-openapi**) |
| `swagger_v1.go`   | Swagger 1.x best-effort parser                             |
| `model.go`        | Library-independent internal types                         |
| `values.go`       | Type-correct value synthesis                               |
| `build.go`        | Turn operations into concrete requests + filenames         |
| `fetch.go`        | Concurrent HTTP + response storage                         |

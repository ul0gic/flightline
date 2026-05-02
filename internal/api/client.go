// Package api wraps the OpenAPI-generated App Store Connect client with auth
// injection, rate-limit backoff, and Apple error-envelope handling.
//
// The generated code lives in internal/api/api.gen.go (produced by oapi-codegen
// from openapi.oas.json — see oapi-codegen.yaml at the project root). Run
// `make gen` to regenerate.
//
// The real client wrapper (auth, retry, error mapping) lands in Phase 1. This
// file exists so `go build ./...` succeeds before `make gen` has run.
package api

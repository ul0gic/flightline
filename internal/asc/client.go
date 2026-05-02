// Package asc is Skipper's hand-rolled App Store Connect API client.
//
// The client wraps stdlib net/http with auth injection (ES256 JWT, IEEE P1363
// raw signature), rate-limit backoff, Apple's errors[] envelope decoding, and
// generic JSON:API envelope handling (Resource[T], Single[T], Collection[T]).
//
// Apple's openapi.oas.json (committed at the project root) is the authoritative
// reference for endpoint shapes; query it via jq during command authoring. This
// package is NOT generated — see .project/issues/closed/ISSUE-001 for why.
//
// Real implementation lands in Phase 1. This file is a package-declaration
// placeholder so go build succeeds against the rest of the scaffold.
package asc

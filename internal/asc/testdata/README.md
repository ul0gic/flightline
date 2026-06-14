# ASC golden fixtures

Captured-and-redacted (or hand-crafted) JSON responses from the App Store
Connect API. Replayed by `internal/asc/*_test.go` via the `fixtureServer`
helper in `fixture_test.go`. Production code never reads these files.

## Why fixtures, not live HTTP

Apple's API is rate-limited, intermittently slow, and gated by per-account
credentials. We replay captured responses in unit tests so the suite is:

- fast (sub-100ms per test on a warm cache)
- deterministic (no flakes from rate limits or transient 5xx)
- runnable on CI without secret rotation
- reviewable in PRs (the JSON shape is the contract; diffs surface drift)

Real-API tests live behind `//go:build integration` and require live ASC
creds in the environment.

## Naming convention

```
testdata/golden/<endpoint>_<scenario>.json
```

- `<endpoint>` is the resource family in snake_case: `apps`, `users`, etc.
- `<scenario>` describes the case: `list`, `get_byBundleId`,
  `list_paginated_page1`, `notFound`.
- For error fixtures: `error_<status>[_<code>].json` — e.g. `error_401.json`,
  `error_429_rate_limit.json`.

Examples currently in this directory:

| File | What it represents |
|---|---|
| `apps_list.json` | 200 response listing 3 apps, single page |
| `apps_list_paginated_page1.json` | 200 page 1 of 2 with `links.next` |
| `apps_list_paginated_page2.json` | 200 page 2 of 2, no further next |
| `apps_get_byBundleId.json` | 200 single-result list filtered by bundleId |
| `apps_get_notFound.json` | 200 empty `data` array — Apple's "no match" shape |
| `whoami_apps_limit1.json` | 200 with 1 app, used by `whoami`'s auth probe |
| `error_401.json` | Apple `errors[]` envelope for unauthorized |
| `error_403.json` | Forbidden / insufficient scope |
| `error_429_rate_limit.json` | Rate limited |
| `error_500.json` | Apple internal server error |

## Cred redaction discipline (non-negotiable)

A fixture **must not** contain any of:

- Real ASC API key IDs (10-char `[A-Z0-9]{10}` tokens like `ABCDEF1234`)
- Real issuer UUIDs (`xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`)
- Real JWTs (`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
- Real vendor numbers (8–10 digit account IDs)
- Real bundle IDs from third-party apps
- Real numeric app IDs from production apps
- `Authorization: Bearer …` headers from request captures (we never serve
  request headers anyway, but if an error body echoes one back, redact it)

Replace with stable test placeholders:

| Real value | Test value |
|---|---|
| Key ID | `TEST123ABC` |
| Issuer ID | `11111111-2222-3333-4444-555555555555` |
| Vendor number | `99999999` |
| Bundle ID | `com.example.testapp`, `com.example.alpha`, … |
| App ID (numeric) | `1234567890`, `1234567891`, … |
| Apple internal IDs in `links` URLs | host stays `api.appstoreconnect.apple.com`, path uses test IDs |

Stable placeholders matter: if every fixture uses the same `bundleId`, tests
that assert on it can be table-driven against a single constant.

## Authoring a new fixture

### Hand-crafted from the OpenAPI spec (preferred for v1)

Most fixtures so far are hand-crafted because the schemas are well-defined
and the surface area is small. Recipe:

```bash
# 1. Pull the schema for the response body.
jq '.components.schemas.AppsResponse' openapi.oas.json

# 2. Pull the schema for an item inside it.
jq '.components.schemas.App' openapi.oas.json

# 3. Build a minimal valid response covering only the fields Flightline reads
#    (see internal/asc/types.go and internal/cmd/apps.go for the shape).

# 4. Save under testdata/golden/<name>.json. jq pretty-print:
jq . > internal/asc/testdata/golden/<name>.json
```

Pretty-print with `jq .` so PR diffs are line-by-line readable.

### Captured-and-redacted from live ASC

When a hand-crafted fixture would miss real-world quirks (e.g. odd
whitespace, unexpected null fields, Apple-internal `id` formats), capture
once and redact:

```bash
# 1. Issue the request with flightline itself or curl + a fresh JWT.
flightline apps list --output json > /tmp/apps_list_raw.json

# 2. Run the redaction pass:
sed -E '
  s/"id":[ ]*"[0-9]{8,}"/"id":"1234567890"/g
  s/"bundleId":[ ]*"com\.[a-z0-9._-]+"/"bundleId":"com.example.testapp"/g
  s/eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/[REDACTED-JWT]/g
  s/[A-Z0-9]{10}/TEST123ABC/g
' /tmp/apps_list_raw.json | jq . > internal/asc/testdata/golden/apps_list.json

# 3. EYEBALL the result before committing.
#    Look for: leftover bundle IDs, leftover key IDs, leftover JWTs,
#    leftover numeric app IDs, vendor numbers, anything personal.
```

If the file shows even one un-scrubbed credential token, **delete and
rewrite by hand**. Once a real key ID lands in git history, rotation is the
only remediation — please don't make us do that.

## Validation

Every fixture file must be valid JSON. CI is enforced by the test suite
(decode failures show up immediately) and by a smoke check at the gate:

```bash
for f in internal/asc/testdata/golden/*.json; do jq -e . "$f" >/dev/null \
  || { echo "invalid JSON: $f"; exit 1; }; done
```

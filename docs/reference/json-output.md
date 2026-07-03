# JSON output

Every Flightline command supports `--output json`. The JSON shape is a stable contract:

- **Adding fields is backward-compatible.** New keys can appear in any release; consumers must tolerate unknown keys.
- **Removing or renaming fields is a breaking change**, tracked by a major version bump. The same applies to rule IDs in diagnostics and to enum-like string values documented here.
- **Table output is not a contract.** Column layout, truncation, and formatting can change any time. If a script parses Flightline output, it parses `--output json`.

Only data goes to stdout; progress and warnings go to stderr, so JSON output is always pipe-clean.

## The envelope philosophy

Commands emit a single top-level JSON object that carries the request parameters alongside the results, so a payload is self-describing: a consumer (or an LLM) can tell what was asked from the output alone, without the command line that produced it. Keys are camelCase. Collections are named arrays inside the envelope (`rows`, `reviews`, `diagnostics`, `submissions`), not bare top-level arrays, so envelopes can grow fields without breaking consumers. Optional fields are omitted when empty rather than emitted as `null`.

## Example: a list command

`flightline reviews list app.tideterm.ios --rating 1..2 --limit 1 --output json`:

```json
{
  "reviews": [
    {
      "id": "a3f8c210-77aa-4d21-9c3e-5b8e01d92f44",
      "type": "customerReviews",
      "attributes": {
        "rating": 2,
        "title": "Tide times wrong after update",
        "body": "Since 2.1 the tide table for my harbor is off by an hour.",
        "reviewerNickname": "harborwatch",
        "createdDate": "2026-06-28T09:12:44-07:00",
        "territory": "USA"
      }
    }
  ]
}
```

List commands that wrap an Apple resource keep Apple's structure: `id` and `type` identify the resource, `attributes` carries the resource fields as Apple names them. Where a related resource is included (here, a developer `response` when one exists), it appears as an optional sibling of `attributes`.

## Example: preflight and lint diagnostics

`flightline lint` and `flightline preflight` share one envelope. `flightline preflight app.tideterm.ios --version 2.1.0 --state-file state.yaml --output json`:

```json
{
  "bundleId": "app.tideterm.ios",
  "version": "2.1.0",
  "sourcePath": "/Users/dev/tideterm/state.yaml",
  "mode": "preflight",
  "diagnostics": [
    {
      "ruleId": "iap.review-screenshot-exists",
      "severity": "error",
      "message": "IAP \"app.tideterm.ios.pro\" has no App Store review screenshot attached",
      "path": "/spec/iap/products/app.tideterm.ios.pro/reviewScreenshot",
      "fixHint": "upload one: `flightline iap review-screenshot upload app.tideterm.ios --product app.tideterm.ios.pro <file>`",
      "reference": "PRD §L3: IAP review-screenshot-exists"
    }
  ],
  "summary": { "error": 1, "warning": 0, "info": 0 }
}
```

Envelope fields:

| Field | Notes |
|-------|-------|
| `bundleId`, `version` | Omitted when unknown (a lint of a file without them) |
| `sourcePath` | Absolute path of the linted file; omitted on live-only preflight |
| `mode` | `"lint"` or `"preflight"` |
| `diagnostics` | Every finding, sorted by rule ID then path |
| `summary` | Counts by severity: `error`, `warning`, `info` |

Each diagnostic has this shape; `ruleId` values are stable identifiers (see the [rule catalog](./preflight-rules.md)):

| Field | Presence | Notes |
|-------|----------|-------|
| `ruleId` | always | Stable rule identifier, or `"schema"` for JSON Schema violations |
| `severity` | always | `"error"`, `"warning"`, or `"info"` |
| `message` | always | What is wrong, in plain language |
| `path` | optional | JSON Pointer into the state file |
| `fixHint` | optional | The remedy |
| `reference` | optional | The guideline or document behind the rule |

## Example: a report command

Report commands carry the fetch parameters in the envelope plus both the raw rows and (where useful) a pre-aggregated summary. `flightline sales app.tideterm.ios --days 1 --output json`:

```json
{
  "bundleId": "app.tideterm.ios",
  "vendorNumber": "87654321",
  "reportType": "SALES",
  "frequency": "DAILY",
  "reportDates": ["2026-07-01"],
  "rowCount": 2,
  "rows": [
    {
      "provider": "APPLE",
      "sku": "tideterm",
      "title": "Tideterm",
      "productTypeIdentifier": "1",
      "units": 9,
      "developerProceeds": 17.91,
      "beginDate": "2026-07-01",
      "endDate": "2026-07-01",
      "countryCode": "US",
      "currencyOfProceeds": "USD",
      "parentIdentifier": "app.tideterm.ios"
    },
    {
      "provider": "APPLE",
      "sku": "tideterm.pro",
      "title": "Tideterm Pro",
      "productTypeIdentifier": "IA1",
      "units": 3,
      "developerProceeds": 20.97,
      "beginDate": "2026-07-01",
      "endDate": "2026-07-01",
      "countryCode": "US",
      "currencyOfProceeds": "USD",
      "parentIdentifier": "app.tideterm.ios"
    }
  ],
  "summary": [
    { "date": "2026-07-01", "units": 12, "developerProceeds": 38.88, "currency": "USD" }
  ]
}
```

`rows` passes through every column of Apple's report as camelCase fields (rows omit columns Apple left blank); `summary` folds them by date and currency, and amounts in different currencies are never summed together. The async `analytics` commands follow the same pattern: `analytics status --output json` returns the request parameters (`bundleId`, `requestId`, `status`, `stateFile`) alongside the observed `reports` and `downloadedSegments` arrays.

## TSV passthrough

`sales`, `finance`, and `subscriptions reports` additionally accept `--output tsv`, which streams Apple's raw (gunzipped) tab-separated wire format to stdout, unfiltered and unparsed. TSV is a passthrough of whatever Apple serves, not a Flightline contract: column names and order are Apple's, and the bundle ID scoping that applies to table and JSON output does not apply. Use it to feed existing report tooling that already speaks Apple's format.

```bash
flightline sales app.tideterm.ios --days 1 --output tsv > today.tsv
```

Other commands reject `--output tsv`.

## Consuming with jq and LLMs

Because the shape is stable, `jq` expressions written against it keep working across releases:

```bash
# Gate a script on auth (jq -e sets the exit code from the expression)
flightline whoami --output json | jq -e .authorized

# Extract one field per line
flightline reviews list app.tideterm.ios --output json \
  | jq -r '.reviews[].attributes.body'

# Count plan changes
flightline plan state.yaml --output json | jq '.changes | length'
```

The envelopes are also deliberately LLM-friendly: self-describing keys, request parameters embedded, no positional data. Piping a payload into a prompt requires no preprocessing:

```bash
flightline preflight app.tideterm.ios --version 2.1.0 --output json \
  | llm "Explain what blocks this submission and in what order to fix it"
```

## See also

- [Preflight rules](./preflight-rules.md), every stable `ruleId`
- [CLI reference](./cli.md), command index; `flightline <group> --help` for flags
- [Observability guide](../guides/observability.md), the report commands in workflow context

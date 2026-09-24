# JSON output

Every Flightline command supports `--output json`. The JSON shape is a stable contract:

- **Adding fields is backward-compatible.** New keys can appear in any release; consumers must tolerate unknown keys.
- **Removing or renaming fields is a breaking change.** The same applies to rule IDs in diagnostics and to enum-like string values documented here. Pre-1.0, breaking changes can land in minor releases (0.5 → 0.6) and are always flagged in a `### Breaking` section of the release notes; they never land in patch releases. 1.0 locks the contract behind a major version bump.
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
  "platform": "IOS",
  "sourcePath": "/Users/dev/tideterm/state.yaml",
  "mode": "preflight",
  "diagnostics": [
    {
      "ruleId": "iap.review-screenshot-exists",
      "severity": "error",
      "message": "IAP \"app.tideterm.ios.pro\" has no App Store review screenshot attached",
      "path": "/spec/iap/products/app.tideterm.ios.pro/reviewScreenshot",
      "fixHint": "upload one: `flightline iap review-screenshot upload app.tideterm.ios --product app.tideterm.ios.pro <file>`",
      "reference": "https://flightline.dev/docs/reference/preflight-rules#iapreview-screenshot-exists"
    }
  ],
  "summary": { "error": 1, "warning": 0, "info": 0 }
}
```

Envelope fields:

| Field | Notes |
|-------|-------|
| `bundleId`, `version` | Omitted when unknown (a lint of a file without them) |
| `platform` | Resolved command platform on preflight; omitted from lint output when the file does not declare it |
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
| `fixHint` | runtime rule findings | A concrete field, value, command, or manual action that resolves the finding |
| `reference` | optional | The guideline or document behind the rule |

## Apply previews

`apply --dry-run` validates every planned dispatch and emits changes in `planned`; `applied` contains no changes. Table output uses status `planned`. Unsupported paths, operations, and local values fail before any mutation or checkpoint update. Preview still requires live reads and credentials; it cannot guarantee Apple will accept a later write.

## Example: apply with a partial failure

`flightline apply state.yaml --confirm --output json` keeps successful changes and reports each failed change with a stable, redacted message:

```json
{
  "bundleId": "app.tideterm.ios",
  "version": "2.1.0",
  "applied": [
    {
      "op": "update",
      "resource": "version",
      "path": "/spec/version/copyright",
      "from": "2025 TideTerm LLC",
      "to": "2026 TideTerm LLC"
    }
  ],
  "errors": [
    {
      "change": {
        "op": "update",
        "resource": "iap",
        "path": "/spec/iap/products/app.tideterm.ios.pro/name",
        "from": "Old name",
        "to": "TideTerm Pro"
      },
      "message": "ASC rejected the in-app purchase update"
    }
  ]
}
```

The command returns a nonzero exit code when `errors` is nonempty. Progress and the joined summary remain on stderr; the JSON payload on stdout is self-contained.

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

`rows` passes through every column of Apple's report as camelCase fields (rows omit columns Apple left blank); `summary` folds them by date and currency, and amounts in different currencies are never summed together. Dates for which Apple has no report appear in `unavailableDates` without aborting the remaining window. Collection fields are always arrays, including when empty; they are never omitted or rendered as `null`. The async `analytics` commands follow the same pattern: `analytics status --output json` returns the request parameters (`bundleId`, `requestId`, `status`, `stateFile`) alongside the observed `reports` and `downloadedSegments` arrays. Additive fields `reportDefinitionsAvailable` and `dataReadiness` distinguish definitions from instance availability: status reports `unchecked`, while `list-instances` reports `available` or `none_available` per selected report. The legacy status `reports_available` means definitions exist.

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

## Asset and legal-declaration results

`previews list` returns a `previews` array with preview type, set ID, and a typed preview containing its checksum and processing state. `review-attachments list` returns an `attachments` array. Both return empty arrays for an empty collection. Successful upload actions report the resource ID and final processing state; a nonzero command result must not be interpreted as processing success merely because byte transfer completed.

`screenshots reorder` returns `bundleId`, `setId`, `screenshotIds`, and `changed`. An identical complete order returns `changed: false`. Rights updates similarly report whether a mutation occurred. `app-eula get` emits agreement text and a complete `territories` array; absence has no EULA ID and an empty territory array. It does not imply that no standard license terms apply.

## Review responses

`reviews responses get|create|delete` results include action, bundle and review IDs, response ID when present, response body/state when returned, and `changed`. A created response may be `PENDING_PUBLISH`; creation is not proof that it is already publicly visible. An absent response has no response ID. Identical-body creation is a no-op; a different body requires a separately confirmed deletion before creation. An uncertain write returns an error and inspection guidance, without retrying the write.

## Release and webhook results

Phased-release results report the observed phase and change outcome. State supports enable and eligible pause/resume only; manual version release is a separate confirmed action. An accepted release request does not prove distribution has completed.

Webhook outputs contain allowlisted configuration and delivery fields. Endpoint output is limited to its origin; secrets, raw payloads, and arbitrary server error text are excluded. Secret rotation is represented only by a boolean outcome. Ping reports request creation, not delivery success. Redelivery requires a new delivery identity and a recognized delivery state; missing or uncertain results return an error without retrying the action.

## Submission assembly and campaign results

`submission-assembly plan|assemble|submit` identifies its `action` and exact ordered `planned` items. Each proposal has `relationship`, `type` and `id`; proposal creation performs no submission write. Assembly reports the draft `submissionId`, whether it was `created`, and newly attached items. Only a confirmed final-submit result sets `submitted: true`; plan and assembly keep it false. An uncertain outcome returns an error with inspection guidance, never a success claim or automatic retry.

`events` returns typed events, localizations and media processing states. The selected event workflow requires one processed media resource per card/detail slot; mixed image/video resources in one slot require inspection rather than automatic selection. Event card/detail uploads have separate checkpoint identities; a committed upload still requires processing completion. Apple does not expose a source checksum for event media, so an occupied slot requires inspection or an explicit deletion before replacement.

`experiments` manages v2 product page experiments and treatment localizations/assets. Creation is app-bound: the response does not claim an association with the selected version until observed. A stopped experiment cannot restart. Event and experiment commands are direct workflows, outside desired-state reconciliation.

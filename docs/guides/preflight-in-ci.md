# Preflight in CI

Flightline's preflight rules catch the clerical mistakes that get releases rejected. Running them in CI means a broken state file fails the pull request and an unsubmittable version fails the release pipeline, before anyone clicks Submit for Review. This guide covers wiring; the rules themselves are documented in the [preflight rules reference](../reference/preflight-rules.md).

## lint vs preflight

Two commands run the rules, at different points in the pipeline:

| Command | Needs credentials? | Input | Run it |
|---------|--------------------|-------|--------|
| `flightline lint state.yaml` | No | Your YAML, offline | On every push and pull request |
| `flightline preflight <bundleId> --version <v>` | Yes | Live ASC state | Before a release, on a protected branch |

`lint` validates the state file against the embedded JSON Schema plus every offline rule (YAML coercion traps, required-but-empty fields, email format, localization completeness, screenshot device coverage). It makes no network calls, so it belongs in the fast path of every PR: no secrets, no rate-limit budget, sub-second.

`preflight` fetches the live App Store version and runs the live rules on top: build attached and valid, IAPs attached to the review submission, review screenshots present, age rating and export compliance answered. It answers "is this version actually submittable right now?" and belongs where release decisions happen.

## Exit codes

Both commands implement the same tri-state contract:

| Exit code | Meaning |
|-----------|---------|
| `0` | Clean: no diagnostics, or info-only |
| `1` | At least one error-severity diagnostic |
| `2` | Warnings only, no errors |

A bare invocation is already a strict CI gate: any nonzero exit fails the step, so warnings block the pipeline along with errors, and the diagnostics (with fix hints) are in the step log.

To fail on errors but let warnings through, accept exit `2` explicitly:

```bash
flightline preflight app.tideterm.ios --version 2.1.0 || [ $? -eq 2 ]
```

The `|| [ $? -eq 2 ]` converts a warnings-only result back into step success; exit `1` still fails.

For finer-grained gates, the JSON summary carries exact counts. `jq -e` exits nonzero when the expression is false, which fails the step:

```bash
# Allow up to 5 warnings, fail beyond that
flightline lint state.yaml --output json | jq -e '.summary.warning <= 5' > /dev/null
```

Related gate: `flightline plan state.yaml --exit-on-changes` exits `2` when the file has drifted from live ASC state, `0` when in sync. Useful as a drift detector on a schedule.

## JSON output for machines

`--output json` emits a stable envelope (see the [JSON output reference](../reference/json-output.md#preflight-and-lint-diagnostics)):

```json
{
  "bundleId": "app.tideterm.ios",
  "version": "2.1.0",
  "mode": "preflight",
  "diagnostics": [
    {
      "ruleId": "version.export-compliance-answered",
      "severity": "error",
      "message": "the build attached to this version has not declared usesNonExemptEncryption",
      "path": "/spec/exportCompliance/usesNonExemptEncryption",
      "fixHint": "answer the export-compliance question in App Store Connect, or `flightline export-compliance set <bundleId> --version <v> --uses-non-exempt-encryption=false`."
    }
  ],
  "summary": { "error": 1, "warning": 0, "info": 0 }
}
```

Useful extractions:

```bash
# Every error, one line each, for a PR comment
flightline lint state.yaml --output json \
  | jq -r '.diagnostics[] | select(.severity == "error") | "\(.ruleId): \(.message)"'

# Machine-readable pass/fail counts
flightline preflight app.tideterm.ios --version 2.1.0 --output json | jq '.summary'
```

## GitHub Actions example

Two jobs: `lint` runs everywhere with no secrets; `preflight` runs on `main` with the API key from repository secrets. Store the raw contents of the `.p8` file as the secret `ASC_PRIVATE_KEY`, and the Key ID and Issuer ID as `ASC_KEY_ID` and `ASC_ISSUER_ID` (the same values as the [environment variables](../getting-started/apple-api-key.md#5-export-the-credentials)).

```yaml
name: preflight

on:
  push:
  pull_request:

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - run: go install github.com/ul0gic/flightline@latest
      - run: flightline lint state.yaml

  preflight:
    if: github.ref == 'refs/heads/main'
    needs: lint
    runs-on: ubuntu-latest
    env:
      APP_STORE_CONNECT_KEY_ID: ${{ secrets.ASC_KEY_ID }}
      APP_STORE_CONNECT_ISSUER_ID: ${{ secrets.ASC_ISSUER_ID }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - run: go install github.com/ul0gic/flightline@latest
      - name: Install API key
        run: |
          mkdir -p ~/.appstoreconnect
          printf '%s' "$ASC_PRIVATE_KEY" > ~/.appstoreconnect/AuthKey_${APP_STORE_CONNECT_KEY_ID}.p8
          chmod 600 ~/.appstoreconnect/AuthKey_${APP_STORE_CONNECT_KEY_ID}.p8
        env:
          ASC_PRIVATE_KEY: ${{ secrets.ASC_PRIVATE_KEY }}
      - run: flightline preflight app.tideterm.ios --version 2.1.0 --state-file state.yaml
```

Notes:

- Both bare invocations are strict: warnings (exit `2`) fail the step. Append `|| [ $? -eq 2 ]` to a run line to gate on errors only.
- The `chmod 600` is not optional: Flightline refuses a key file with wider permissions.
- Flightline never logs the key, the JWT, or credential IDs; error output is redacted. Still, scope the key to the least role that works (App Manager).
- `preflight` defaults `--platform` to `IOS`; pass `--platform MAC_OS` (or `TV_OS`, `VISION_OS`) for other platforms.
- Once tagged releases exist, pin one (`go install github.com/ul0gic/flightline@<tag>`) instead of tracking `@latest` in a gate you depend on.

## Cross-checking with --state-file

Without `--state-file`, preflight fetches live state and checks it: pure "is ASC submittable?". With `--state-file`, the offline rules run against your authored YAML while the live rules consult ASC, so one command catches authoring mistakes and live gaps together:

```bash
flightline preflight app.tideterm.ios --version 2.1.0 --state-file state.yaml
```

This is the right form for a release pipeline that manages state as code: it verifies both that the file you are about to `apply` is well-formed and that the live version has everything Apple's submission flow will demand. For the full authoring loop around it, see [State as Code](./state-as-code.md).

## See also

- [Preflight rules reference](../reference/preflight-rules.md), every rule with mode, severity, and fix hints
- [JSON output contract](../reference/json-output.md), the diagnostic envelope in detail
- [API key setup](../getting-started/apple-api-key.md), roles and credential troubleshooting

# The three-layer model

Flightline combines direct API commands, configuration in YAML, and validation rules. You can use each separately.

```
L3: preflight rules  (internal/lint/)   catches clerical rejection causes
L2: state as code    (internal/state/)  declare, diff, apply
L1: API CLI          (internal/asc/)    supported ASC operations as terminal commands
```

You can use `flightline sales` and `flightline reviews` (L1) without ever touching a `state.yaml`. You can use L2 without running preflight. L3 can catch issues even if you manage writes by hand.

## L1: API CLI

Supported App Store Connect surfaces are reachable from the terminal across both pillars: authoring (read and write) and observation (read). Command groups are backed by a typed HTTP and JSON client. Verbs are conventional: `flightline <resource> <verb> [flags]`.

Authoring examples:

```bash
flightline apps list
flightline versions create app.tideterm.ios --version 1.1 --copyright "..."
flightline builds attach app.tideterm.ios --version 1.1 --build <id>
flightline iap list app.tideterm.ios
flightline rejection app.tideterm.ios --version 1.0
```

Observation examples:

```bash
flightline sales app.tideterm.ios --days 30
flightline finance app.tideterm.ios --month 2026-04
flightline reviews list app.tideterm.ios --rating 1..2
flightline analytics request app.tideterm.ios --access-type ONE_TIME_SNAPSHOT --wait
flightline performance app app.tideterm.ios
```

Every command supports `--output table|json`. The JSON shape is a stable contract, so you can pipe to `jq`, feed it to an LLM, or cron-schedule snapshots.

## L2: state as code

A YAML file describes the supported configuration you want to manage for one app. Three commands drive it:

```bash
flightline fetch app.tideterm.ios > state.yaml   # snapshot live ASC state into YAML
flightline plan state.yaml                         # read-only diff, no writes
flightline apply state.yaml --confirm              # idempotent writes, checkpointed
```

The YAML is human-edited, a JSON Schema is the contract (the `apiVersion` constant locks it to `flightline.dev/v1alpha1`), and the L3 linter enforces it. L2 covers authoring only; observation surfaces stay in L1 because they are queries against live state, not state to declare.

Fetch captures the current configuration. Plan compares it with your edited YAML. Apply writes the supported changes, and a fresh plan verifies the result.

See [State as code](../guides/state-as-code.md) for the walkthrough and the [state-yaml reference](../reference/state-yaml.md) for the schema.

## L3: preflight rules

Fifteen rules check supported schema, consistency and release-readiness requirements. Two commands run them:

```bash
flightline lint state.yaml                          # offline; YAML correctness plus Apple format rules
flightline preflight app.tideterm.ios --version 1.1   # live; reads ASC state, runs all rules
```

Sample rules:

- `iap.attached-to-review-submission`, an IAP being `READY_TO_SUBMIT` is not enough; it must be in the review submission's items.
- `iap.review-screenshot-exists`, the buried review screenshot that is a common rejection cause.
- `version.export-compliance-answered` and `version.age-rating-answered`.
- `localizations.completeness`, every declared locale has every required field.
- `screenshots.required-devices`, 6.9-inch and 6.7-inch present for new submissions.

Passing checks does not guarantee App Review approval. See [Preflight rules](../reference/preflight-rules.md) for the full catalog with modes, severities, and fix hints.

## The lifecycle

The three layers come together in the authoring loop:

```
1. fetch       read live state into state.yaml
2. edit        change the YAML
3. lint        offline schema and format check (L3)
4. plan        diff against live ASC, no writes (L2)
5. preflight   live rule check (L3)
6. apply       idempotent writes (L2)
7a. external TestFlight   flightline testflight beta-review submit
7b. App Store release    submission-assembly plan, assemble --confirm, then submit --confirm
```

Steps 1 through 5 are read-only against ASC and reversible. Step 6 patches ASC but does not submit. The terminal paths are different workflows: beta review gates external TestFlight testing, while production App Store Review requires a separate review submission. Flightline keeps assembly and final submit in separate confirmed commands. Final submit requires fresh preflight and exact membership checks; generic state apply never invokes it.

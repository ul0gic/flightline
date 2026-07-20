# The three-layer model

Flightline is built as three layers. Each one is useful on its own, and they stack: L1 is the foundation, L2 builds desired-state management on top of it, and L3 validates that state before it reaches Apple.

```
L3: preflight rules  (internal/lint/)   catches clerical rejection causes
L2: state as code    (internal/state/)  declare, diff, apply
L1: API CLI          (internal/asc/)    every ASC surface as a terminal command
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
flightline reviews list app.tideterm.ios --rating 1 --rating 2
flightline analytics request app.tideterm.ios --access-type ONE_TIME_SNAPSHOT --wait
flightline performance app app.tideterm.ios
```

Every command supports `--output table|json`. The JSON shape is a stable contract, so you can pipe to `jq`, feed it to an LLM, or cron-schedule snapshots.

## L2: state as code

A single declarative YAML file per app describes the desired state across every authoring surface. Three commands drive it:

```bash
flightline fetch app.tideterm.ios > state.yaml   # snapshot live ASC state into YAML
flightline plan state.yaml                         # read-only diff, no writes
flightline apply state.yaml --confirm              # idempotent writes, checkpointed
```

The YAML is human-edited, a JSON Schema is the contract (the `apiVersion` constant locks it to `flightline.dev/v1alpha1`), and the L3 linter enforces it. L2 covers authoring only; observation surfaces stay in L1 because they are queries against live state, not state to declare.

This is the same shape as Terraform or Pulumi: declarative state, idempotent reconciliation, drift detection, version control as the source of truth. The substrate is Apple's API instead of a cloud, and the failure mode being prevented is App Store rejection rather than a bad cloud rollout, but the discipline is identical.

See [State as code](../guides/state-as-code.md) for the walkthrough and the [state-yaml reference](../reference/state-yaml.md) for the schema.

## L3: preflight rules

A growing rule set that captures the clerical reasons Apple rejects releases. Every rejection eaten in the wild becomes a rule. Two commands run them:

```bash
flightline lint state.yaml                          # offline; YAML correctness plus Apple format rules
flightline preflight app.tideterm.ios --version 1.1   # live; reads ASC state, runs all rules
```

Sample rules:

- `iap.attachedToReviewSubmission`, an IAP being `READY_TO_SUBMIT` is not enough; it must be in the review submission's items.
- `iap.reviewScreenshot.exists`, the buried review screenshot that is a common rejection cause.
- `version.exportCompliance.answered` and `version.ageRating.answered`.
- `localizations.completeness`, every declared locale has every required field.
- `screenshots.requiredDevices`, 6.9-inch and 6.7-inch present for new submissions.

L3 is the highest-value authoring layer; rejection prevention is the actual product on the authoring side. See [Preflight rules](../reference/preflight-rules.md) for the full catalog with modes, severities, and fix hints.

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
7b. App Store release    attach build and IAPs, then Submit for Review manually in ASC
```

Steps 1 through 5 are read-only against ASC and reversible. Step 6 patches ASC but does not submit. The terminal paths are different workflows: beta review gates external TestFlight testing, while production App Store Review requires a separate review submission. Flightline checks that submission but does not press its final Submit for Review action today.

# State as code

Manage selected App Store Connect fields in YAML: fetch current state, edit intent, inspect a plan, apply it, and verify convergence. This is desired state for the surfaces you choose to manage; it is not a complete backup of every Apple resource.

Complete [first run](../getting-started/first-run.md) first. Examples use an existing editable `IOS` version `1.0` of `com.example.app`; substitute your own coordinates.

## 1. Fetch and choose what to manage

```bash
flightline fetch com.example.app --version 1.0 --platform IOS -o state.yaml
```

The file contains version coordinates and supported fetched surfaces. Omitted surfaces are unmanaged and remain untouched. An explicit empty collection can mean clearing membership where that surface supports it; consult the [state schema](../reference/state-yaml.md) before adding `[]`. Omission and an empty list are not interchangeable.

Fetch does not turn every direct command into state support. Events, experiments, webhooks, review responses, and final production submission have separate workflows. Beta group build memberships require an explicit `--include-beta-builds` fetch opt-in; see [TestFlight](./testflight.md).

Inspect the snapshot before committing it. Reviewer contact information and demo credentials may be sensitive. Keep binary assets and relative paths accessible wherever you run apply.

## 2. Edit and lint

Edit one intended field in the existing file, such as `spec.metadata.locales.en-US.promotionalText`. Keep the fetched bundle ID, version, and platform unchanged. Quote version strings and build numbers.

```bash
flightline lint state.yaml
```

Lint runs schema and offline checks without credentials or network calls. Errors exit `1`; warnings alone exit `2`. Review both before continuing. Offline lint cannot establish live editability, approval, or submission readiness.

## 3. Review the live diff

```bash
flightline plan state.yaml
flightline plan state.yaml --output json
```

Plan reads current ASC state without writing. Inspect every proposed change, especially collection membership, asset removals, and pricing. Remove surfaces from the file if you do not intend to manage them.

For drift detection, `flightline plan state.yaml --exit-on-changes` exits `0` for no changes, `2` for changes, and `1` on failure. Those meanings differ from lint's warning code.

A dry run also exercises apply validation and dispatch without mutating requests:

```bash
flightline apply state.yaml --dry-run --output json
```

It requires live access, but sends no POST, PATCH, or DELETE requests. Successful dry-run output is not an applied change or live qualification of a write path.

## 4. Apply reviewed intent

```bash
flightline apply state.yaml --confirm
```

Without `--confirm`, apply does not write. With confirmation, it validates live eligibility and executes supported changes. Apple lifecycle restrictions still apply. An intent file cannot make a noneditable version editable, and a permitted phased-release transition does not permit unrelated metadata edits.

Apply is not an atomic transaction. A failed parent blocks its dependent changes; independent changes may proceed. Inspect the result before retrying. Some changes and external effects cannot be reversed by restoring the previous file.

## 5. Verify convergence

```bash
flightline plan state.yaml
flightline preflight com.example.app --version 1.0 --platform IOS --state-file state.yaml
```

An empty plan confirms no remaining differences for the managed intent observed by that read. Preflight checks supported rules against authored and live data; it does not guarantee Apple approval. Allow for asset processing and inspect any remaining changes rather than repeatedly applying blindly.

## Interrupted work

Apply checkpoints bind the bundle ID, version, platform, and original plan. To resume an interrupted apply with the same intent:

```bash
flightline apply state.yaml --confirm --resume
```

Resume fetches fresh state and checks that the remaining diff agrees with the interrupted plan. A mismatch stops before writing. Inspect the new live state and intent before starting a fresh apply. A successful apply removes its checkpoint.

Upload recovery separately validates the file identity and checksum. Do not replace asset bytes while resuming an upload. See [uploading assets](./uploading-assets.md) for processing and recovery details.

## Submission stays explicit

State apply never performs final production App Review submission or manual version release. External TestFlight beta review is a separate action and does not submit a production version.

Use [submission and release](./submission-and-release.md) after state has converged. Keep reviewer instructions specific: if a purchase requires a trial, login, or another in-app condition, explain the exact steps and supply working demo access. API metadata cannot prove that a reviewer can reach a feature in the running app.

## Related workflows

- [IAP commerce](./iap-commerce.md): products, availability, pricing, and direct purchase actions.
- [Declarations](./declarations.md): rights, EULA, accessibility, and other declarations.
- [Preflight in CI](./preflight-in-ci.md): diagnostics and failure handling.
- [Capabilities](../reference/capabilities.md): direct commands, state support, and limitations.
- [State schema](../reference/state-yaml.md): field names and collection semantics.

# First run

Start with [installation](./install.md) and an [Apple API key](./apple-api-key.md). These commands read App Store Connect and write one local snapshot; they do not change your app.

The guides describe the current source tree. A published binary may lag it: check `flightline --version` and the relevant command's `--help`, or build the source revision you intend to use.

## Inspect your app

```bash
flightline --version
flightline whoami
flightline apps list
flightline versions list com.example.app --platform IOS
flightline versions get com.example.app --version 1.0 --platform IOS
```

Replace `com.example.app` and `1.0` with a bundle ID and version returned by your account. Select an editable version for the later authoring workflow. Use `--output json` on inspection commands when you need machine-readable results.

## Create a state file before linting it

```bash
flightline fetch com.example.app --version 1.0 --platform IOS -o state.yaml
flightline lint state.yaml
flightline plan state.yaml
```

`fetch` creates the file. `lint` validates it offline. `plan` reads live state and reports the changes needed for the fields you manage. Review fetched metadata before committing it: reviewer contact information and demo credentials can be sensitive.

Lint exits `0` for clean or info-only results, `1` for errors, and `2` for warnings only. A diagnostic is a finding to inspect, not evidence that these read-only commands changed anything.

## Choose a workflow

- [State as code](../guides/state-as-code.md): edit, preview, apply, and recover interrupted changes.
- [Submission and release](../guides/submission-and-release.md): assemble a production submission, submit it, and control release separately.
- [TestFlight](../guides/testflight.md): beta metadata, groups, builds, and external review.
- [Capabilities](../reference/capabilities.md): select a workflow and understand its support boundary.
- [Preflight in CI](../guides/preflight-in-ci.md): handle diagnostics and exit codes correctly.

No write is needed to finish this first run. Continue to a workflow guide when you have reviewed the target app, version, and proposed changes.

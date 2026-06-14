# First run

Once you have [installed Flightline](./install.md) and [set up an API key](./apple-api-key.md), these five commands confirm the install works and cover both pillars (authoring and observation). They are all read-only, so nothing here writes to App Store Connect.

```bash
# Verify auth
flightline whoami

# List your apps
flightline apps list

# Inspect a version
flightline versions get com.under5.passdmv --version 1.0

# Diagnose a rejection (if the version is in REJECTED state)
flightline rejection com.under5.passdmv --version 1.0

# Run offline preflight against a state file
flightline lint state.yaml
```

Replace `com.under5.passdmv` with your own bundle ID.

## What each command does

| Command | What it does | Writes? |
|---------|--------------|---------|
| `flightline whoami` | Prints the configured identity and confirms the key authorizes | No |
| `flightline apps list` | Lists the apps your key can see | No |
| `flightline versions get <bundleId> --version <v>` | Shows the state of one App Store version | No |
| `flightline rejection <bundleId> --version <v>` | Composes a rejection report for a version | No |
| `flightline lint state.yaml` | Validates a state file offline against the schema and format rules | No |

## Output formats

Every command supports `--output table` (the default) and `--output json`. JSON is a stable contract: pipe it to `jq` or feed it to an LLM prompt.

```bash
flightline apps list --output json
```

## Next steps

- [State as code: a 5-minute walkthrough](../guides/state-as-code.md), fetch, edit, plan, apply.
- [Preflight rules](../reference/preflight-rules.md), every rejection rule Flightline checks.
- [The three-layer model](../concepts/three-layer-model.md), how L1, L2, and L3 fit together.

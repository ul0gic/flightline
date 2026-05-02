# Skipper

Single-binary Go CLI for App Store Connect. Turns ASC into a structured, declarative surface — read account state, lint a desired-state YAML against it, preflight every Apple rejection rule we know about, and apply changes idempotently — so submissions stop being a clerical landmine.

> **Status:** scaffold-only. Commands land starting Phase 1. See development notes below.

## Why

Apple's App Store Connect has hundreds of fields scattered across a dozen surfaces. Forget any one (export compliance, age rating, IAP attachment, IAP review screenshot, account-deletion attestation, privacy nutrition label) and the release gets bounced. Skipper makes those fields scriptable, reviewable in git, and lintable before you hit submit.

## Prerequisites

- **Go 1.26+** (stable since April 2026)
- An **App Store Connect API key** (`.p8`) — generate at App Store Connect → Users and Access → Integrations → App Store Connect API
- Mac primary; Linux works too if you want to run from CI

## Install

```bash
go install github.com/ul0gic/skipper@latest
```

Homebrew formula and prebuilt binaries land in a later release.

## Setup

Place your `.p8` private key:

```bash
mkdir -p ~/.appstoreconnect
mv ~/Downloads/AuthKey_<KEY_ID>.p8 ~/.appstoreconnect/
chmod 600 ~/.appstoreconnect/AuthKey_<KEY_ID>.p8
```

Set environment variables (e.g., in `~/.zshrc`):

```bash
export APP_STORE_CONNECT_KEY_ID="<your 10-char key id>"
export APP_STORE_CONNECT_ISSUER_ID="<your issuer uuid>"
export APP_STORE_CONNECT_VENDOR_NUMBER="<your vendor number>"
```

Optionally override the `.p8` location with `APP_STORE_CONNECT_KEY_PATH`. Defaults to `~/.appstoreconnect/AuthKey_<KEY_ID>.p8`.

## Usage

```bash
skipper --help
```

Commands land in subsequent phases. Roughly:

```text
skipper whoami                              # verify auth
skipper apps list                           # list apps
skipper versions get <bundle> --version 1.1 # version detail
skipper rejection <bundle> --version 1.1    # diagnose REJECTED state
skipper lint state.yaml                     # offline schema + format check
skipper preflight <bundle> --version 1.1    # live rejection-rule check
skipper apply state.yaml --confirm          # idempotent writes
skipper submit <bundle> --version 1.1       # the only commit-to-Apple step
```

## Output

Every command supports `--output table | json`. JSON is a stable contract for piping and LLM consumers.

## Configuration precedence

1. CLI flags (highest)
2. Environment variables (`SKIPPER_*` and `APP_STORE_CONNECT_*`)
3. Config file (`~/.config/skipper/config.yaml`)
4. Defaults

## Development

```bash
make build    # ./bin/skipper
make test     # go test -race
make vet      # go vet
make lint     # golangci-lint
make verify   # vet + test + lint
make gen      # regenerate API client (codegen pending Phase 1.0)
make fmt      # gofmt + goimports
```

## License

MIT — see [LICENSE](./LICENSE).

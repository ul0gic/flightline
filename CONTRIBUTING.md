# Contributing to Flightline

Thanks for your interest. Flightline is a single-binary Go CLI for App Store Connect. This guide covers how to build, test, and submit changes.

## Prerequisites

- Go 1.26 or newer
- `golangci-lint` for linting

## Build and test

```bash
go build -o ./bin/flightline .   # build the binary
go test ./...                    # run the unit + fixture suite
go vet ./...                     # vet
golangci-lint run                # lint
gofmt -s -w . && goimports -w .  # format
```

Integration tests are gated behind a build tag and require real App Store Connect credentials:

```bash
go test -tags=integration ./...
```

Most contributors do not need these. The standard `go test ./...` runs fully offline against recorded fixtures.

## Before you open a PR

Every change must pass the full gate with zero warnings and zero failures:

```bash
go vet ./... && go test ./... && golangci-lint run
```

If you touched anything user-facing, confirm both output modes still work: every command supports `--output table|json`, and the JSON shape is a stable contract. Adding fields is fine; renaming or removing them is a breaking change.

## Pull request flow

`main` is protected. Push to a branch and open a PR.

1. Branch from `main`.
2. Make your change. Keep commits focused and the history clean; the repo requires linear history, so rebase rather than merge `main` into your branch.
3. Run the gate above.
4. Open the PR. Explain *why* the change is needed, not just what it does.
5. Resolve any review conversations before merge.

## Commit messages

Write them like an engineer explaining intent to the next reader:

- Subject in the imperative: `fix pricing diff on empty tier`, not `fixed bug`.
- Explain *why* in the body when the reason is not obvious from the diff.
- One logical change per commit where practical.

## Code conventions

- The App Store Connect client in `internal/asc/` is hand-rolled, not generated. Edit it directly.
- Writes are idempotent: every `apply`/`update` diffs first and patches only the delta. Keep it that way.
- Surface Apple API errors fully: HTTP status, the JSON `errors[]` payload, and a hint about what to check.
- Comments explain *why*, never *what*. The default is no comment. Keep them rare and short.
- Never commit credentials. `.p8` keys, issuer IDs, and key IDs stay out of the repo and out of logs.

## Reporting bugs

Open an issue with the command you ran, what you expected, what happened, and the output (redact any credentials or IDs). Reproduction steps make a fix far more likely.

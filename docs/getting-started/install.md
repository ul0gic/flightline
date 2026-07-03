# Install

Flightline is a single Go binary. Install it with `go install`:

```bash
go install github.com/ul0gic/flightline@latest
```

Requires Go 1.26 or later. App Store Connect work requires a Mac, so that is where Flightline is meant to run (Apple Silicon and Intel are both supported). The binary itself builds anywhere Go does.

The binary lands in `$GOBIN` (typically `~/go/bin`). Make sure `~/go/bin` is on your `PATH`.

## Verify the install

```bash
flightline --version
flightline --help
```

`go install` is the supported install method — a single static binary, no package manager needed.

## Build from source

To compile from a checkout instead:

```bash
git clone https://github.com/ul0gic/flightline.git
cd flightline
make build
./bin/flightline --version
```

The binary is at `./bin/flightline`. Move it onto your `PATH` if you want it system-wide:

```bash
sudo mv ./bin/flightline /usr/local/bin/flightline
```

## Next steps

1. [Generate an App Store Connect API key](./apple-api-key.md) so Flightline can authenticate.
2. [Run your first commands](./first-run.md) to confirm everything works.

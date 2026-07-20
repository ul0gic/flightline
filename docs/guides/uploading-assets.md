# Uploading assets

`flightline apply` reconciles App Store binary assets declared in state YAML, including version screenshots, IAP review screenshots, and Custom Product Page screenshots. It uses Apple's multipart reserve, PUT, and commit protocol and compares local MD5 checksums with live `sourceFileChecksum` values before uploading.

The dedicated version screenshot and IAP review-screenshot commands remain available when you want an explicit one-off upload without managing the asset in state YAML.

This guide covers the upload workflow. For the state-file fields that describe these assets, see the [state-yaml reference](../reference/state-yaml.md).

## How it fits the apply workflow

Declare asset paths relative to the state file, review the checksum-based plan, and apply normally:

```bash
flightline plan state.yaml
flightline apply state.yaml --confirm
```

Matching checksums are no-ops. For screenshot sets, assets omitted from the managed list are deleted during the confirmed apply. If an upload is interrupted, rerun `flightline apply state.yaml --confirm --resume`; the apply and multipart checkpoints are both validated before work continues.

## Version screenshots

```bash
flightline screenshots upload <bundleId> \
  --version <v> --locale <locale> --device-set <displayType> \
  <file> [<file> ...]
```

Files are positional arguments; you can pass a glob or several paths. Required flags: `--version`, `--locale`, `--device-set`.

```bash
# A whole device set with a glob
flightline screenshots upload app.tideterm.ios \
  --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 \
  ./shots/iphone-67/*.png

# Explicit files for an iPad set
flightline screenshots upload app.tideterm.ios \
  --version 1.0.1 --locale en-US --device-set APP_IPAD_PRO_3GEN_129 \
  ./shots/ipad/01.png ./shots/ipad/02.png
```

The `--device-set` value is an Apple `ScreenshotDisplayType` (for example, `APP_IPHONE_69`, `APP_IPHONE_67`, `APP_IPAD_PRO_3GEN_129`). See the [device class table](../reference/state-yaml.md#specscreenshots) for the full list and pixel dimensions.

### Idempotent by MD5

Each device slot uploads 1 to 10 screenshots. Flightline computes an MD5 of each local file and skips any slot whose `sourceFileChecksum` already matches the live one, so re-running an upload that has not changed is a no-op. This is the same checksum the diff engine uses, so `flightline plan` and the upload verb agree on what would change.

### Resume an interrupted upload

If an upload is interrupted (Ctrl-C, network drop), re-run with `--resume` to pick up from the on-disk checkpoint instead of restarting:

```bash
flightline screenshots upload app.tideterm.ios \
  --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 --resume \
  ./shots/01.png
```

## IAP review screenshots

The review screenshot shows App Review how to reach an in-app purchase. A missing review screenshot is a common rejection cause, and the L3 rule `iap.reviewScreenshot.exists` checks for it.

```bash
flightline iap review-screenshot upload <bundleId> \
  --product <productId> --file <path> [--resume]
```

```bash
flightline iap review-screenshot upload app.tideterm.ios \
  --product app.tideterm.ios.lifetime \
  --file ./review/lifetime.png
```

Required flags: `--product` (the parent IAP's productId) and `--file`. Pass `--resume` to continue an interrupted upload.

## Custom Product Page screenshots

CPP screenshots are described under `spec.customProductPages` in the state file and upload through `flightline apply`. CPPs support a subset of device classes (iPhone and iPad only; no TV, Watch, or Vision Pro). See the [CPP section of the state-yaml reference](../reference/state-yaml.md#speccustomproductpages) for the supported device list.

## See also

- [State as code: a 5-minute walkthrough](./state-as-code.md), the fetch, edit, plan, apply loop.
- [State-yaml reference](../reference/state-yaml.md), the `spec.screenshots`, `spec.iap`, and `spec.customProductPages` fields.
- `flightline apply --help`, `flightline screenshots upload --help`, `flightline iap review-screenshot upload --help`.

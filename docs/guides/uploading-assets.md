# Uploading assets

App Store binary assets (version screenshots, IAP review screenshots, Custom Product Page screenshots) upload through dedicated L1 verbs, not through `flightline apply`. Apple's multipart upload API (reserve, PUT, commit, often via a separate signed-URL host) is structurally different from a JSON PATCH on a config field, so `apply` deliberately does not drive it. Config fields converge through `apply`; asset bytes flow through the upload verbs.

This guide covers the upload workflow. For the state-file fields that describe these assets, see the [state-yaml reference](../reference/state-yaml.md).

## How it fits the apply workflow

When `flightline plan` shows a screenshot or review-screenshot change, `flightline apply` returns a typed error for that path and points you at the upload verb. The intended flow is two commands: upload the asset, then run `apply` for everything else.

```bash
flightline screenshots upload com.under5.passdmv \
  --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 \
  ./screenshots/iphone67-1.png

flightline apply state.yaml --confirm
```

The full orchestrator integration (so `apply` drives uploads directly, with checksum-skip and chunked resume) is tracked in [QA-010](../../.project/issues/open/QA-010-orchestrator-upload-integration.md).

## Version screenshots

```bash
flightline screenshots upload <bundleId> \
  --version <v> --locale <locale> --device-set <displayType> \
  <file> [<file> ...]
```

Files are positional arguments; you can pass a glob or several paths. Required flags: `--version`, `--locale`, `--device-set`.

```bash
# A whole device set with a glob
flightline screenshots upload com.under5.passdmv \
  --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 \
  ./shots/iphone-67/*.png

# Explicit files for an iPad set
flightline screenshots upload com.under5.passdmv \
  --version 1.0.1 --locale en-US --device-set APP_IPAD_PRO_3GEN_129 \
  ./shots/ipad/01.png ./shots/ipad/02.png
```

The `--device-set` value is an Apple `ScreenshotDisplayType` (for example, `APP_IPHONE_69`, `APP_IPHONE_67`, `APP_IPAD_PRO_3GEN_129`). See the [device class table](../reference/state-yaml.md#specscreenshots) for the full list and pixel dimensions.

### Idempotent by MD5

Each device slot uploads 1 to 10 screenshots. Flightline computes an MD5 of each local file and skips any slot whose `sourceFileChecksum` already matches the live one, so re-running an upload that has not changed is a no-op. This is the same checksum the diff engine uses, so `flightline plan` and the upload verb agree on what would change.

### Resume an interrupted upload

If an upload is interrupted (Ctrl-C, network drop), re-run with `--resume` to pick up from the on-disk checkpoint instead of restarting:

```bash
flightline screenshots upload com.under5.passdmv \
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
flightline iap review-screenshot upload com.under5.passdmv \
  --product com.under5.passdmv.lifetime \
  --file ./review/lifetime.png
```

Required flags: `--product` (the parent IAP's productId) and `--file`. Pass `--resume` to continue an interrupted upload.

## Custom Product Page screenshots

CPP screenshots are described under `spec.customProductPages` in the state file and upload through the custom-product-pages screenshot verb. CPPs support a subset of device classes (iPhone and iPad only; no TV, Watch, or Vision Pro). See the [CPP section of the state-yaml reference](../reference/state-yaml.md#speccustomproductpages) for the supported device list.

## See also

- [State as code: a 5-minute walkthrough](./state-as-code.md), the fetch, edit, plan, apply loop.
- [State-yaml reference](../reference/state-yaml.md), the `spec.screenshots`, `spec.iap`, and `spec.customProductPages` fields.
- `flightline screenshots upload --help`, `flightline iap review-screenshot upload --help`.

# Uploading assets

`flightline apply` reconciles App Store binary assets declared in state YAML, including version and Custom Product Page (CPP) screenshots and preview videos, plus IAP review screenshots. Uploads use Apple's multipart reserve, PUT, and commit protocol. Flightline compares local MD5 checksums with live `sourceFileChecksum` values before uploading.

The dedicated screenshot, preview, IAP review-screenshot, and review-attachment commands handle explicit one-off actions. The commands here describe current source and locally tested workflows; check `flightline ... --help` in your installed version. All identifiers are synthetic.

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
flightline screenshots upload com.example.myapp \
  --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 \
  ./shots/iphone-67/*.png

# Explicit files for an iPad set
flightline screenshots upload com.example.myapp \
  --version 1.0.1 --locale en-US --device-set APP_IPAD_PRO_3GEN_129 \
  ./shots/ipad/01.png ./shots/ipad/02.png
```

The `--device-set` value is an Apple `ScreenshotDisplayType` (for example, `APP_IPHONE_69`, `APP_IPHONE_67`, `APP_IPAD_PRO_3GEN_129`). See the [device class table](../reference/state-yaml.md#specscreenshots) for the full list and pixel dimensions.

### Idempotent by MD5

For the state-managed main screenshot sets, the schema accepts 1 to 10 files per device set. Flightline computes an MD5 of each local file and skips a file whose `sourceFileChecksum` already matches an asset in the selected set, so re-running an unchanged upload is a no-op. The plan uses the same checksum identity.

### Resume an interrupted upload

If an upload is interrupted (Ctrl-C, network drop), re-run with `--resume` to pick up from the on-disk checkpoint instead of restarting:

```bash
flightline screenshots upload com.example.myapp \
  --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 --resume \
  ./shots/01.png
```

## IAP review screenshots

The review screenshot gives App Review supporting material for an in-app purchase. The preflight rule `iap.reviewScreenshot.exists` checks whether one is present.

```bash
flightline iap review-screenshot upload <bundleId> \
  --product <productId> --file <path> [--resume]
```

```bash
flightline iap review-screenshot upload com.example.myapp \
  --product com.example.myapp.lifetime \
  --file ./review/lifetime.png
```

Required flags: `--product` (the parent IAP's productId) and `--file`. Pass `--resume` to continue an interrupted upload.

## Custom Product Page screenshots

CPP screenshots are described under `spec.customProductPages.<page>.localizations.<locale>.screenshots` and upload through `flightline apply`. CPPs support the iPhone and iPad classes listed in the [state YAML reference](../reference/state-yaml.md#speccustomproductpages). Their `screenshotOrder: true` flag opts into ordering each declared set after collection reconciliation.

For an existing set, `screenshots reorder` changes relationship order without uploading image bytes. Supply every existing screenshot ID in the desired order. The main listing uses `--version`, `--locale`, and `--device-set`; a CPP uses both `--custom-product-page` and the exact `--custom-product-page-version` instead of `--version`.

```bash
flightline screenshots reorder com.example.myapp \
  --version 1.2.0 --locale en-US --device-set APP_IPHONE_67 \
  --screenshot SCREENSHOT_ID_1 --screenshot SCREENSHOT_ID_2
```

The command rereads set membership and rejects missing, duplicate, or foreign IDs.

## Preview videos

State YAML manages complete preview arrays at `spec.previews.locales.<locale>.<previewType>` and under CPP localizations. Omission leaves a type unmanaged; an explicit empty array clears it. An item contains `path` and optionally `previewFrameTimeCode`. A changed local file is a different asset even if its filename is unchanged. A pending or failed remote processing state blocks a successful state snapshot; inspect the preview before replanning.

For a one-off main-listing upload, select the version, locale, and Apple `PreviewType`:

```bash
flightline previews upload com.example.myapp ./media/preview.mov \
  --version 1.2.0 --locale en-US --type IPHONE_67 --confirm
flightline previews list com.example.myapp --version 1.2.0 --locale en-US
```

For a CPP, use `--page <page-name>` and `--locale` instead of a main-listing version. A CPP mutation requires a consistent editable draft. Upload and delete require `--confirm`; list and wait do not. `--frame-time-code` sets an optional frame after processing. `--max-polls` and `--poll-interval` bound processing waits.

```bash
flightline previews wait com.example.myapp \
  --version 1.2.0 --locale en-US --preview PREVIEW_ID
flightline previews delete com.example.myapp \
  --version 1.2.0 --locale en-US --preview PREVIEW_ID --confirm
```

## App Review attachments

Review attachments belong to the review detail of one exact App Store version. They are explicit CLI assets, not desired-state fields. The detail must already exist before upload.

```bash
flightline review-attachments upload com.example.myapp ./review/steps.png \
  --version 1.2.0 --confirm
flightline review-attachments list com.example.myapp --version 1.2.0
flightline review-attachments wait com.example.myapp \
  --version 1.2.0 --attachment ATTACHMENT_ID
flightline review-attachments delete com.example.myapp \
  --version 1.2.0 --attachment ATTACHMENT_ID --confirm
```

Use `--platform` when the version is not iOS. Upload and delete require `--confirm`. Upload compares checksums, skips an existing matching attachment, and waits for processing. If a transfer stops while an asset is `AWAITING_UPLOAD`, rerun upload with `--resume` and the same file and parent; the checkpoint must match. For a committed asset still processing, use `wait` with its ID. A same-name asset with a different or unknown checksum requires inspection before another upload.

Upload checkpoints use schema version 2 and bind the absolute source path to the asset kind, resource type, and exact parent. A checkpoint for another parent is never reused. Ambiguous same-target checkpoints and legacy version-1 checkpoints fail locally; inspect and remove the obsolete checkpoint before starting a fresh upload. Successful same-parent multipart resume remains supported.

Preview and attachment uploads reserve, transfer, and commit bytes, then poll processing with bounded attempts. Processing timeout or cancellation retains the committed asset ID in the error; use the matching `wait` command before attempting another upload. Reruns inspect the parent collection to avoid reserving another asset with an unresolved matching identity. A same-name asset with an unknown or different checksum requires inspection or explicit deletion.

## IAP promotional images

`iap promotion images` lists, uploads, waits for, and deletes non-subscription promotional images using the existing non-versioned IAP image resource. Upload/delete are explicit confirmed actions. Pending uploads are inspected before a new reservation; resume requires a matching checkpoint and exact image identity. The reported image state distinguishes processing from App Review approval.

`iap promotion promoted` manages non-subscription promotion visibility/enabled settings and explicit deletion. Its order action requires the complete app promotion set, including existing subscription promotions, and rereads the resulting order. These actions are not state reconciliation and do not author subscriptions or versioned IAP images.

## See also

- [State as code](./state-as-code.md), the fetch, edit, plan, apply loop.
- [State YAML reference](../reference/state-yaml.md), the asset fields and ordering flags.
- [App events](./events.md), [product page experiments](./experiments.md), and [IAP commerce](./iap-commerce.md) for their respective assets and actions.
- `flightline apply --help`, `flightline screenshots upload --help`, `flightline previews --help`, `flightline review-attachments --help`, `flightline iap review-screenshot upload --help`.

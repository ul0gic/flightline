# Product page experiments: treatments, assets, review proposal

`flightline experiments` manages selected App Store product page experiment workflows directly. These v2 experiments, their treatments, and treatment media have no L2 state YAML contract. The examples use the synthetic bundle ID `com.example.app` and placeholder IDs returned by your own account. Verify the commands in your installed build with `flightline experiments --help` and each subcommand's `--help`. This source workflow has local test coverage; live Apple qualification is pending.

## Select the version and inspect

```bash
flightline experiments list com.example.app \
  --version 1.0 --platform IOS --output json

flightline experiments get com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID --output json
```

All selected experiment, treatment, locale, and asset commands resolve the bundle ID and the unique App Store version identified by `--version` and `--platform` (`IOS` defaults). They verify experiment membership in that version's v2 collection and app/platform identity. Use IDs returned by reads; names are for humans and can collide.

Creation is different: Apple's create endpoint binds the experiment to the **app**, not to the caller-selected version. Flightline resolves the requested version as a guard, but its create response explicitly says version association is not established by the create API. Inspect `experiments list` for the selected version after creation. Until it appears there, selected treatment, lifecycle, and proposal commands cannot treat it as a member of that version. Do not infer membership from a successful create response.

## Create and author a treatment

```bash
flightline experiments create com.example.app \
  --version 1.0 --platform IOS \
  --name 'Example icon test' --traffic 50 --confirm --output json

flightline experiments list com.example.app \
  --version 1.0 --platform IOS --output json
```

`--traffic` is an integer from 1 to 100. A create call with an existing app experiment of the same name and platform returns that experiment only if traffic matches; conflicting traffic or duplicate matches require inspection. If creation has an uncertain outcome, inspect the app/version before retrying. Authoring proceeds only after the version-scoped list confirms the experiment ID.

```bash
flightline experiments treatments list com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID --output json

flightline experiments treatments create com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID \
  --name 'Alternate artwork' --confirm --output json

flightline experiments treatments localizations create com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID \
  --treatment TREATMENT_ID --locale en-US --confirm --output json

flightline experiments treatments localizations list com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID \
  --treatment TREATMENT_ID --output json
```

There can be at most three treatments. Treatment creation also accepts `--app-icon-name` for an icon in the current app binary. Reusing the same treatment name with a different icon is a conflict. Creating a locale that already exists returns the existing localization. Keep treatment and locale IDs from the read responses for diagnosis, even though asset commands select the locale by `--locale`.

Experiment name/traffic updates, treatment edits, localization changes, media uploads/deletes, and experiment deletion require an editable draft: state `PREPARE_FOR_SUBMISSION`, no start date, and no end date. Mutations require `--confirm`. `experiments update` accepts `--name` and/or `--traffic`; `treatments update` accepts `--name` and/or `--app-icon-name`. Changes to started, stopped, completed, or other noneditable states fail the local guard.

## Upload and inspect treatment media

Asset commands select the experiment, treatment, locale, kind, and display type. `--kind screenshot` uses a screenshot display type, such as `APP_IPHONE_67`; `--kind preview` uses a preview type, such as `IPHONE_67`. The type vocabularies differ. Use the values supported by the installed build for the relevant kind before choosing a device slot.

```bash
flightline experiments treatments localizations assets list com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID \
  --treatment TREATMENT_ID --locale en-US --output json

flightline experiments treatments localizations assets upload \
  com.example.app ./alternate-iphone.png \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID \
  --treatment TREATMENT_ID --locale en-US \
  --kind screenshot --display-type APP_IPHONE_67 \
  --confirm --output json
```

Upload finds or creates the selected media set, uploads the file, then waits for processing. Screenshot and preview media use the same command with different `--kind` and `--display-type`. Local files must exist, be regular, and be nonempty. `--max-polls` defaults to 20 and `--poll-interval` to 3 seconds; increase the bound when processing takes longer. A returned asset ID identifies the reservation to inspect after a timeout.

```bash
flightline experiments treatments localizations assets wait com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID \
  --treatment TREATMENT_ID --locale en-US \
  --kind screenshot --display-type APP_IPHONE_67 --asset ASSET_ID \
  --output json
```

`wait` first confirms that the asset belongs to the selected set, then polls processing without uploading again. Unlike event media, treatment screenshots and previews expose source checksums. Upload computes a local MD5 and compares filename/checksum against assets in the selected set. A matching processed asset can be reused; a conflicting or ambiguous filename/checksum requires inspection. An `AWAITING_UPLOAD` asset requires `--resume` with the exact matching local checkpoint and file. Do not create a second reservation merely because processing is still pending.

To upload a preview, use `--kind preview --display-type IPHONE_67` with a suitable video file. The same `assets list` and `assets wait` paths apply. Deleting an asset is explicit: `assets delete` takes the same selector flags, `--asset ASSET_ID`, and `--confirm`; it is allowed only while the experiment remains an editable draft.

```bash
flightline experiments treatments localizations assets upload \
  com.example.app ./alternate-preview.mp4 \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID \
  --treatment TREATMENT_ID --locale en-US \
  --kind preview --display-type IPHONE_67 \
  --confirm --output json
```

List assets again to inspect the selected set, filenames, checksums, and processing states. A committed upload is not complete until its processing state is ready; use `assets wait` with the returned asset ID when the upload's bounded wait expires.

## Lifecycle and review readiness

```bash
flightline experiments get com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID --output json

flightline experiments proposal com.example.app \
  --version 1.0 --platform IOS --experiment EXPERIMENT_ID --output json
```

The read-only proposal checks unique membership in the selected version and requires `READY_FOR_REVIEW` with no start or end date. It returns an `appStoreVersionExperimentV2` item proposal. **A proposal does not attach an item or submit a review.** Recheck it immediately before a separate submission assembly action. Flightline does not use this command to force the experiment into `READY_FOR_REVIEW`; that state must be observed from Apple.

`experiments start` requires an `APPROVED` experiment with `reviewRequired` false and no prior start/end date. `experiments stop` requires a started experiment. Both require the selected version, `--experiment`, and `--confirm`. A `STOPPED` or `COMPLETED` experiment, or one with an end date, cannot restart. Treat stop as a terminal decision; inspect the fresh state before issuing it.

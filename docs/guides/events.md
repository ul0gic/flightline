# In-App Events: draft, localize, upload, inspect

`flightline events` manages selected In-App Event workflows directly in App Store Connect. It is outside `flightline fetch`, `plan`, and `apply`; events have no L2 state YAML contract. The commands below use the synthetic bundle ID `com.example.app` and placeholder IDs returned by your own account. Check the commands and flags in your installed build with `flightline events --help` and the relevant subcommand `--help` before using this guide. This source workflow has local test coverage; live Apple qualification is pending.

## Inspect the app and event

```bash
flightline events list com.example.app --output json
flightline events get com.example.app --event EVENT_ID --output json
flightline events localizations list com.example.app --event EVENT_ID --output json
```

The bundle ID is resolved to one app. Each event ID is checked against that app; localization and media IDs are checked against the selected parent before use. Read the event's `eventState`, `primaryLocale`, and `territorySchedules` before editing. Use the actual IDs returned by list/get, never a display name as an ID.

Creation requires `--confirm` and must return a `DRAFT` event. Updating, localizing, and changing media require an existing `DRAFT` event and `--confirm`. Event deletion permits `DRAFT`, `ARCHIVED`, or `APPROVED`; inspect state and identity before that destructive operation. Flightline does not offer a command here to force an event to another lifecycle state.

## Create the draft and localization

```bash
flightline events create com.example.app \
  --reference-name 'Example launch event' \
  --badge SPECIAL_EVENT \
  --deep-link 'https://example.com/event' \
  --purchase-requirement 'No purchase required' \
  --primary-locale en-US \
  --priority NORMAL \
  --purpose APPROPRIATE_FOR_ALL_USERS \
  --territory-schedules '[{"territories":["USA"],"publishStart":"2026-12-01T12:00:00Z","eventStart":"2026-12-02T12:00:00Z","eventEnd":"2026-12-03T12:00:00Z"}]' \
  --confirm --output json
```

Replace these example dates with future RFC 3339 times. The schedule must have `publishStart <= eventStart < eventEnd`; event duration is 15 minutes to 31 days, and publication can precede the event by at most 14 days. If you customize starts across territories, their spread is limited to 48 hours. Territory codes in one event cannot repeat. A draft can be created with less metadata, but the proposal check later requires all fields shown above and a schedule.

The create response contains the event ID. If creation reports an uncertain outcome, list events and inspect the reference name before retrying: a duplicate name is treated as a conflict, not an automatic reuse.

```bash
flightline events localizations create com.example.app \
  --event EVENT_ID --locale en-US \
  --name 'Example launch' \
  --short-description 'Explore the new experience' \
  --long-description 'Open the app during the event to explore the new experience.' \
  --confirm --output json

flightline events localizations get com.example.app \
  --event EVENT_ID --localization LOCALIZATION_ID --output json
```

`--name`, `--short-description`, and `--long-description` have 30, 50, and 120 character limits. The primary locale must exist among the event localizations before proposal. Each localization needs all three text fields for review. To change draft text, use `flightline events localizations update` with `--event`, `--localization`, changed fields, and `--confirm`. To change draft event metadata, use `flightline events update` with `--event`, changed fields, and `--confirm`; `eventState` cannot be patched.

```bash
flightline events update com.example.app \
  --event EVENT_ID --priority HIGH --confirm --output json

flightline events localizations update com.example.app \
  --event EVENT_ID --localization LOCALIZATION_ID \
  --short-description 'See what is new' --confirm --output json
```

Updates merge changed fields with the current record; omitted fields are retained. Re-read the event after a schedule change because the proposed times must still satisfy the future-date bounds at review time.

## Upload card and detail media

Each localization needs one processed `EVENT_CARD` media item and one processed `EVENT_DETAILS_PAGE` item for proposal. Each slot can hold an image or video. Flightline's selected workflow rejects an ambiguous slot with multiple resources, including mixed image and video. Inspect existing media first:

```bash
flightline events media list com.example.app \
  --event EVENT_ID --localization LOCALIZATION_ID --output json

flightline events media upload com.example.app ./event-card.png \
  --event EVENT_ID --localization LOCALIZATION_ID \
  --asset-type EVENT_CARD --confirm --output json

flightline events media upload com.example.app ./event-details.png \
  --event EVENT_ID --localization LOCALIZATION_ID \
  --asset-type EVENT_DETAILS_PAGE --confirm --output json
```

Images accept `.jpg`, `.jpeg`, or `.png`. For a `.mov`, `.m4v`, or `.mp4`, add `--video`; video uploads may also set `--preview-frame-time-code`. Upload reserves, transfers, commits, then waits for processing. The default polling bound is 20 reads, 3 seconds apart; `--max-polls` and `--poll-interval` adjust it. A committed upload can still be processing, so retain its returned media ID.

```bash
flightline events media get com.example.app \
  --event EVENT_ID --localization LOCALIZATION_ID --media MEDIA_ID --output json

flightline events media wait com.example.app \
  --event EVENT_ID --localization LOCALIZATION_ID --media MEDIA_ID --output json
```

`wait` checks the selected media without uploading bytes again. A timeout or processing error calls for inspection of that ID. Apple's event media response does not provide a server source checksum; Flightline cannot prove that an occupied slot matches a local file. An occupied completed slot blocks another upload, even when the filename matches. Inspect it and, if replacement is intended, explicitly delete the selected draft media before uploading again:

```bash
flightline events media delete com.example.app \
  --event EVENT_ID --localization LOCALIZATION_ID --media MEDIA_ID --confirm
```

`--resume` is only for a matching `AWAITING_UPLOAD` reservation and its exact local checkpoint, file, parent localization, and media ID. It is not a replacement mechanism. Keep the original file and checkpoint when resuming an interrupted upload; otherwise inspect before retrying.

## Check review readiness

```bash
flightline events propose-submission-item com.example.app \
  --event EVENT_ID --output json
```

This read-only command checks that the event is still `DRAFT`, the schedule remains valid and future-dated, required metadata and primary localization exist, every localization has complete text, and both media slots are unambiguous and `COMPLETE`. It returns an `appEvent` item proposal with the event ID. **A proposal does not attach an item, create a review submission, or submit anything to Apple.** Recheck readiness immediately before any separate submission assembly action; the event or media may have changed meanwhile.

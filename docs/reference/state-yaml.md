# Flightline AppState: YAML Reference

## TL;DR

A Flightline state file is a YAML document that declares the desired configuration for one app across every App Store Connect surface Flightline manages. You run `flightline fetch <bundleId>` once to capture live state, edit the YAML, then use `flightline plan` and `flightline apply` to preview and write the diff. The schema is embedded in the binary; your editor autocompletes via the `yaml-language-server` directive at the top of every fetched file.

Every top-level child of `spec` is optional. Omitting a section tells Flightline "leave this surface alone." You don't have to manage everything, start with `spec.metadata` and `spec.version`, add sections when you need them.

The schema URL is `https://flightline.dev/schemas/v1alpha1/state.schema.json`. Fields not listed in this document are not part of the v1alpha1 contract; unrecognized keys cause a `LoadState` error.

**This reference covers v1alpha1.** The `apiVersion` constant locks that. If a future release bumps to `v1beta1`, the diff will be explicit and documented.

Apply checkpoints now use format 3 because parent/child and pricing/declaration operations changed. Older checkpoints are rejected by `--resume`; inspect a fresh plan before applying without `--resume`. Live state is fetched again so already completed operations are not blindly replayed.

See [the state-as-code guide](../guides/state-as-code.md) for a 5-minute walkthrough.

---

## File anatomy

A minimal state file:

```yaml
# yaml-language-server: $schema=https://flightline.dev/schemas/v1alpha1/state.schema.json

apiVersion: flightline.dev/v1alpha1
kind: AppState

metadata:
  bundleId: com.example.myapp
  version: "1.2.0"

spec:
  version:
    releaseType: AFTER_APPROVAL
```

The four top-level keys are required: `apiVersion`, `kind`, `metadata`, `spec`.

| Key | Type | Required | Notes |
|-----|------|----------|-------|
| `apiVersion` | string const | yes | Must be exactly `flightline.dev/v1alpha1` |
| `kind` | string const | yes | Must be exactly `AppState` |
| `metadata` | object | yes | Bundle identity, see [metadata](#metadata) |
| `spec` | object | yes | Desired state, all children optional |

**Editor setup.** The `# yaml-language-server: $schema=...` directive at the top of every `flightline fetch` output activates autocomplete and inline validation in VS Code, Neovim (with yaml-language-server), and any editor that supports the LSP YAML extension. The schema is hosted at `https://flightline.dev/schemas/v1alpha1/state.schema.json` and also embedded in the `flightline` binary.

---

## metadata

Identifies the app and the version being described. These three fields are also the keys Flightline uses when resolving the live ASC resource to diff against.

```yaml
metadata:
  bundleId: app.tideterm.ios
  version: "1.0.1"
  platform: IOS
```

| Field | Type | Required | Default | Constraint | Gotcha |
|-------|------|----------|---------|------------|--------|
| `bundleId` | string | yes |, | `^[a-zA-Z0-9._-]+$`, min 3 chars | Must already exist in ASC. Flightline does not create apps. |
| `version` | string | yes |, | `^[0-9]+(\.[0-9]+)*$` | Marketing version (CFBundleShortVersionString). Leading zeros rejected, `01.0` fails schema. Quote it: `"1.0"`. |
| `platform` | enum | no | `IOS` | `IOS`, `MAC_OS`, `TV_OS`, `VISION_OS` | Defaults to `IOS` when absent. Must match the platform your build targets. |

**Authoring loop.** `metadata.bundleId` + `metadata.version` are the identity keys Flightline uses at every step: fetch → lint → plan → apply → preflight.

---

## spec.version

Controls how and when the version releases once approved by Apple Review.

```yaml
spec:
  version:
    releaseType: AFTER_APPROVAL
    copyright: "© 2026 Tideterm Labs"
```

| Field | Type | Required | Default | Constraint | Gotcha |
|-------|------|----------|---------|------------|--------|
| `releaseType` | enum | no |, | `MANUAL`, `AFTER_APPROVAL`, `SCHEDULED` | If absent, Flightline leaves the current ASC setting unchanged. `SCHEDULED` requires `earliestReleaseDate`. |
| `earliestReleaseDate` | string | no |, | ISO 8601 datetime | Required when `releaseType=SCHEDULED`. Apple rejects times in the past. |
| `copyright` | string | no |, | maxLength 100 | Displayed on the App Store listing. Apple shows this under the app name. |
| `downloadable` | boolean | no |, |, | Rarely needed; controls whether the build is downloadable from TestFlight or the App Store. |

**Release timing.** Version release type is set before submission. `MANUAL` gives you a hold button after approval; `AFTER_APPROVAL` releases automatically; `SCHEDULED` releases at the specified time.

---

## Phased release

`spec.version.phasedRelease.enabled: true` enables an absent phased release in `INACTIVE` state for an eligible update. Omit `state` on creation. Omission preserves existing rollout configuration; `enabled: false` and rollout deletion are unsupported.

For an existing rollout, `state` may change between `ACTIVE` and `PAUSED` only when the exact version is Ready for Distribution. `INACTIVE` and `COMPLETE` round-trip as observed values and cannot be requested as transitions. `startDate`, `totalPauseDuration`, and `currentDayNumber` are observed fields: omit them from authored intent, or preserve fetched values unchanged. Apple remains authoritative on update eligibility when local history does not prove it.

Phased intent permits observing a noneditable version for planning, but every resulting non-phased change is rejected before apply. It does not enable metadata edits on a distributed app. Phased transitions reread current rollout and version status before writing.

Manual release is exclusively the confirmed `version-release` command for a `MANUAL` version in `PENDING_DEVELOPER_RELEASE`. It is never triggered by state apply. Completing a rollout immediately for all users is not selected state behavior.

### Phased release fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `enabled` | boolean | No | See the behavior described above. |
| `state` | enum | No |  Values: `INACTIVE`, `ACTIVE`, `PAUSED`, `COMPLETE`. |
| `startDate` | string | No |  Format: `date-time`. |
| `totalPauseDuration` | integer | No |  Minimum: 0. |
| `currentDayNumber` | integer | No |  Minimum: 0. |


## spec.build

Attaches a build (already uploaded via Xcode / altool) to this version. Flightline looks up the build by `version + number` and attaches it by its ASC resource ID.

```yaml
spec:
  build:
    number: "42"
```

| Field | Type | Required | Default | Constraint | Gotcha |
|-------|------|----------|---------|------------|--------|
| `number` | string | no |, |, | CFBundleVersion. Quote it: `"42"` not `42`. Bare integers are YAML numbers, not strings, and Flightline's strict loader rejects the type mismatch. |

Builds must reach `VALID` state in ASC before Flightline can attach them. Upload is done by Xcode, `altool`, or `xcrun notarytool`, not by Flightline. If the build isn't processed yet, `flightline apply` will surface a typed error.

---

## spec.metadata

Per-locale store listing content. Keys under `spec.metadata.locales` are Apple locale codes (`en-US`, `es-MX`, `fr-FR`, `ja`, etc.). You can manage any subset of locales, locales not listed here are left alone.

```yaml
spec:
  metadata:
    locales:
      en-US:
        name: ".PassDMV: California"
        subtitle: "Pass the test, first try"
        description: |
          California DMV practice tests with the latest official questions.
        keywords: "DMV,driver,test,license,California,permit,practice"
        whatsNew: "v1.0.1, bug fixes, faster question loading."
        promotionalText: "Free updates as the DMV question bank evolves."
        marketingUrl: "https://tideterm.app"
        supportUrl: "https://tideterm.app/support"
        privacyPolicyUrl: "https://tideterm.app/privacy"
      es-MX:
        name: ".PassDMV: California"
        subtitle: "Aprueba a la primera"
```

### Locale key format

Locale codes follow the pattern `^[a-z]{2}(-[A-Z]{2})?$`, two-letter language code, optionally followed by a dash and two-letter region code. Examples: `en-US`, `fr-FR`, `ja`, `zh-Hans`, `pt-BR`.

### Per-locale fields

| Field | Type | Required | maxLength | Gotcha |
|-------|------|----------|-----------|--------|
| `name` | string | baseline | 30 | App name on the store listing. Apple enforces 30 chars; plan/apply will reject strings over this limit before hitting the wire. Applies to `appInfoLocalization`. |
| `subtitle` | string | no | 30 | One-line subtitle beneath the app name. Applies to `appInfoLocalization`. |
| `description` | string | baseline | 4000 | Long body text on the listing page. Applies to `appStoreVersionLocalization`. |
| `keywords` | string | no | 100 | Comma-separated, no spaces around commas. Goes to `appStoreVersionLocalization`. Not shown to users but affects search indexing. |
| `whatsNew` | string | no | 4000 | "What's New in this Version" text. Version-scoped: this is for the current `metadata.version`. |
| `promotionalText` | string | no | 170 | Promotional text block. **Can be updated without resubmission**, the only metadata field you can change on a live, approved version. Use it for time-sensitive copy. |
| `marketingUrl` | URI | no |, | Full URL. |
| `supportUrl` | URI | baseline |, | Full URL. |
| `privacyPolicyUrl` | URI | no |, | Full URL. |

**Cross-resource routing.** `name` and `subtitle` live on `appInfoLocalization`; everything else lives on `appStoreVersionLocalization`. Flightline handles the routing, you author a single locale map and Flightline dispatches to the correct ASC resource per field.

**Per-locale completeness.** Every locale you declare must be complete enough for Apple to accept. The `localizations.completeness` rule checks Flightline's submission baseline (`name`, `description`, and `supportUrl`) even when metadata is the only localized surface, and reports locale coverage gaps across other managed surfaces. Other fields remain lifecycle-dependent; check the [Apple documentation](https://developer.apple.com/help/app-store-connect/manage-app-information/add-app-localizations/) for Apple's current requirements.

---

## spec.screenshots

Per-locale, per-device-class screenshot sets. Flightline computes an MD5 of each local file and skips slots whose `sourceFileChecksum` already matches, making repeated applies no-ops when screenshots haven't changed.

```yaml
spec:
  screenshots:
    locales:
      en-US:
        APP_IPHONE_69:
          - path: ./screenshots/iphone69-1.png
          - path: ./screenshots/iphone69-2.png
        APP_IPHONE_67:
          - path: ./screenshots/iphone67-1.png
```

### Device class keys

| Key | Device | Pixel dimensions |
|-----|--------|-----------------|
| `APP_IPHONE_69` | iPhone 16 Pro Max (6.9") | 1320×2868 or 2868×1320 |
| `APP_IPHONE_67` | iPhone 14 Plus / 15 Plus (6.7") | 1290×2796 or 2796×1290 |
| `APP_IPHONE_65` | iPhone 11 Pro Max / XS Max (6.5") | 1242×2688 or 2688×1242 |
| `APP_IPHONE_61` | iPhone 11 / XR (6.1") | 828×1792 or 1792×828 |
| `APP_IPHONE_55` | iPhone 8 Plus (5.5") | 1242×2208 or 2208×1242 |
| `APP_IPAD_PRO_3GEN_129` | iPad Pro 12.9" (3rd gen+) | 2048×2732 or 2732×2048 |
| `APP_IPAD_PRO_3GEN_11` | iPad Pro 11" (1st gen+) | 1668×2388 or 2388×1668 |
| `APP_APPLE_TV` | Apple TV | 1920×1080 or 3840×2160 |
| `APP_APPLE_WATCH` | Apple Watch | varies by generation |
| `APP_APPLE_VISION_PRO` | Apple Vision Pro | 2732×2048 |

### screenshotFile fields

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `path` | string | yes | Relative paths resolve against the directory containing the state file. PNG or JPG. |
| `alt` | string | no | Reserved. Apple does not currently surface alt text on the store listing. |

Each device slot accepts 1 to 10 screenshots (`minItems: 1`, `maxItems: 10`).

**Required devices for new submissions.** Apple requires at least one supported large-iPhone screenshot tier for new iOS app submissions. The L3 preflight rule `screenshots.requiredDevices` catches missing coverage offline.

`flightline apply` resolves these paths relative to the state file, compares local MD5 checksums with Apple's live checksums, uploads changed files, and deletes live files omitted from the managed device set. Use `--resume` to continue an interrupted multipart upload. The dedicated `flightline screenshots upload` command remains available for one-off uploads outside state management.

---

## Preview videos and screenshot order

`spec.previews.locales.<locale>.<previewType>` contains a complete managed array of preview files. Each item has `path`, optional `previewFrameTimeCode`, and an observed `sourceFileChecksum`. Omission leaves a preview type unmanaged; an explicit empty array clears that type. CPP localizations use the same arrays under `previews`. Local files hydrate their checksum on load; fetched checksum values survive serialization when local bytes are unavailable. A changed local file is a different asset even if its filename is unchanged. Frame omission preserves the existing frame selection for a matching asset.

Preview reconciliation waits for processing after upload. A pending, failed, or unknown remote processing state prevents a successful state snapshot; inspect the asset and use the explicit preview wait command before planning again. A successful upload commit alone is not processing completion. CPP preview changes require a consistent editable target; Flightline rejects a plan observed from an approved version when it cannot verify that same preview set in an editable draft. Create and inspect the draft before reconciling those assets. Review attachments are explicit CLI operations and are not desired-state fields.

Set `spec.screenshots.order: true` to use each declared screenshot array as its complete display order. CPP localizations opt in separately with `screenshotOrder: true`. Omitting the flag or setting it false retains unordered reconciliation. Relationship ordering follows collection reconciliation and is skipped if that dependency fails. Reordering existing matching assets changes linkage only; it does not upload their bytes again.

### Preview file fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `sourceFileChecksum` | string | No | Observed source checksum; recomputed from a readable local file. |
| `path` | string | Yes |  Minimum length: 1. |
| `previewFrameTimeCode` | string | No |  Minimum length: 1. |


## spec.iap

In-app purchases keyed by `productId`. This section covers consumable, non-consumable, and non-renewing IAPs. Auto-renewable subscriptions use ASC subscription groups and are not represented in this state section; `spec.testflight` manages beta testing.

```yaml
spec:
  iap:
    products:
      app.tideterm.ios.lifetime:
        type: NON_CONSUMABLE
        name: "Lifetime Access"
        familySharable: false
        reviewNote: |
          Tap "Unlock Full Access" on the home screen.
        reviewScreenshot:
          path: ./review-screenshots/lifetime.png
        localizations:
          en-US:
            name: "Lifetime Access"
            description: "Unlock all DMV practice tests forever."
```

### iapProduct fields

| Field | Type | Required | Constraint | Gotcha |
|-------|------|----------|------------|--------|
| `type` | enum | yes | `CONSUMABLE`, `NON_CONSUMABLE`, `NON_RENEWING_SUBSCRIPTION` | Immutable after creation. `AUTO_RENEWABLE_SUBSCRIPTION` belongs to ASC's subscription group surface. |
| `name` | string | on create |, | Apple-facing reference name in ASC. Not shown to customers. |
| `familySharable` | boolean | no |, | Whether Family Sharing is enabled for this IAP. |
| `contentHosting` | enum | no | `HOSTED`, `NON_HOSTED` | Hosted: Apple hosts the downloadable content. This is observed, read-only state: unchanged fetched values are accepted; setting it on a new IAP or changing it fails local validation. |
| `reviewNote` | string | no | maxLength 4000 | Instructions for the App Review team to exercise this IAP. Important for IAPs with non-obvious unlock paths. |
| `reviewScreenshot` | object | no |, | Path to a screenshot showing the IAP unlock screen. See note below. |
| `localizations` | map | no |, | Locale-keyed `{name, description}` pairs. See below. |

### reviewScreenshot

```yaml
reviewScreenshot:
  path: ./review-screenshots/lifetime.png
```

| Field | Type | Required | Gotcha |
|-------|------|----------|--------|
| `path` | string | yes | Relative to state file directory. The L3 preflight rule `iap.reviewScreenshot.exists` checks this is populated. Missing review screenshots are a common rejection cause. |

**Current limitation.** Like app screenshots, the `reviewScreenshot` binary upload is not driven by `flightline apply`. Use `flightline iap review-screenshot upload <bundleId> --product <productId> --file <path>` directly.

### iapLocalization fields

| Field | Type | Required | maxLength | Gotcha |
|-------|------|----------|-----------|--------|
| `name` | string | on create | 30 | Customer-visible IAP name; sent with a new localization in one complete request. |
| `description` | string | no | 45 | New or changed descriptions have a 45-character write limit. Longer observed values survive fetch/reload and remain valid when unchanged. |

---

## IAP commerce

`spec.iap.products.<productId>.commerce.pricing` manages the current base price as a complete `baseTerritory` / `pricePointId` pair. Both are required together. The selected point must belong to that IAP and territory. Apply rereads the schedule, rejects a stale planned price, and preserves verified historical, future, and other-territory manual windows. Detailed windows remain available through `iap commerce pricing`; state projects the currently active base pair. Schedule replay is locally tested and awaits live qualification.

`commerce.availability` accepts `availableInNewTerritories` and `availableTerritories`. Omitted components preserve observed settings. The territory list is a complete managed set when supplied; `[]` explicitly clears it. Initial configuration requires both components. Apply rejects concurrent changes to the observed availability set. Omitting `commerce` leaves commercial settings unmanaged.

Offer-code definitions and issuance are explicit L1 actions under `iap offer-codes`, never state reconciliation. Custom codes use a private input file; one-time values download only to a new private CSV file. Promotional assets are not yet supported by state.

### Commerce fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `pricing` | object | No | See `IAPPriceSpec`. |
| `availability` | object | No | See `IAPAvailabilitySpec`. |

### Purchase price fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `baseTerritory` | string | Yes |  Minimum length: 1. |
| `pricePointId` | string | Yes |  Minimum length: 1. |

### Purchase availability fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `availableInNewTerritories` | boolean | No | See the behavior described above. |
| `availableTerritories` | array | No | See the behavior described above. |


## spec.ageRating

Apple's age-rating questionnaire. Each field answers one content-category question. All fields are optional individually, answering only the categories relevant to your app is fine. However, Apple requires the questionnaire to be complete before you can submit; the L3 preflight rule `version.ageRating.answered` verifies this.

```yaml
spec:
  ageRating:
    cartoonOrFantasyViolence: NONE
    realisticViolence: NONE
    profanityOrCrudeHumor: NONE
    matureSuggestiveThemes: NONE
    horrorOrFearThemes: NONE
    medicalOrTreatmentInformation: NONE
    alcoholTobaccoOrDrugUseOrReferences: NONE
    contestsAndGambling: NONE
    sexualContentOrNudity: NONE
    sexualContentGraphicAndNudity: NONE
    gambling: false
    unrestrictedWebAccess: false
```

### Frequency fields (enum)

These fields accept `NONE`, `INFREQUENT_OR_MILD`, `FREQUENT_OR_INTENSE`, `INFREQUENT`, or `FREQUENT`:

| Field | Flightline name | Apple API field |
|-------|-------------|-----------------|
| `cartoonOrFantasyViolence` | cartoon or fantasy violence | `violenceCartoonOrFantasy` |
| `realisticViolence` | realistic violence | `violenceRealistic` |
| `profanityOrCrudeHumor` | profanity or crude humor | `profanityOrCrudeHumor` |
| `matureSuggestiveThemes` | mature or suggestive themes | `matureOrSuggestiveThemes` |
| `horrorOrFearThemes` | horror or fear themes | `horrorOrFearThemes` |
| `medicalOrTreatmentInformation` | medical or treatment information | `medicalOrTreatmentInformation` |
| `alcoholTobaccoOrDrugUseOrReferences` | alcohol, tobacco, or drug use | `alcoholTobaccoOrDrugUseOrReferences` |
| `contestsAndGambling` | contests and gambling | `contests` (Apple wire name) |
| `sexualContentOrNudity` | sexual content or nudity | `sexualContentOrNudity` |
| `sexualContentGraphicAndNudity` | graphic sexual content and nudity | `sexualContentGraphicAndNudity` |

### Boolean fields

| Field | Type | Gotcha |
|-------|------|--------|
| `prolongedGraphicSadisticRealisticViolence` | frequency enum | Preserves Apple's exact frequency value. Replace old boolean values with an explicit questionnaire answer; Flightline does not infer which frequency `true` meant. |
| `gambling` | boolean | Gambling features (not just references, actual gambling mechanics). |
| `unrestrictedWebAccess` | boolean | App provides unrestricted internet access (e.g., a web browser). |
| `kidsAgeBand` | enum or null | `FIVE_AND_UNDER`, `SIX_TO_EIGHT`, `NINE_TO_ELEVEN`, or `null`. Set only for Kids category apps. |
| `seventeenPlus` | boolean | **Read-only.** Apple derives the 17+ rating from your answers, you cannot set this field directly. `flightline apply` returns a typed error if you include this in a change set. You may include it in the file for documentation, but it has no write effect. |

---

## spec.exportCompliance

The build-level `usesNonExemptEncryption` answer and an optional App Encryption Declaration are separate managed operations. Supply the answers appropriate to your app; Flightline does not infer a classification.

```yaml
spec:
  build:
    number: "42"
  exportCompliance:
    usesNonExemptEncryption: true
    declaration:
      appDescription: "Description of the app and its encryption use."
      containsProprietaryCryptography: false
      containsThirdPartyCryptography: true
      availableOnFrenchStore: false
```

`declaration` requires an explicit `spec.build.number` and all four attributes shown. Flightline compares the selected build's associated declaration, reuses a matching eligible declaration or creates one complete resource, then associates it with that build. A failed build attachment blocks dependent export operations. A failed declaration association remains an error; a subsequent apply reuses the created declaration instead of creating another.

| Field | Type | Contract |
|-------|------|----------|
| `usesNonExemptEncryption` | boolean | Independently manages the selected build's encryption answer. |
| `declaration.appDescription` | string | Required for declaration creation. |
| `declaration.containsProprietaryCryptography` | boolean | Required declaration answer. |
| `declaration.containsThirdPartyCryptography` | boolean | Required declaration answer. |
| `declaration.availableOnFrenchStore` | boolean | Required declaration answer. |

Legacy declaration fields `usesEncryption`, `exempt`, `eccn`, `documentName`, and `documentUrl` fail local write-intent validation. Document upload, Apple review/approval, and classification assignment are separate lifecycle steps and are not performed by this state operation. The build association workflow is covered by local API fixtures; live qualification remains pending.

---

## spec.reviewerDemo

Login credentials for App Review. Apple requires demo credentials for any app that has a sign-in wall. Flightline never logs or echoes passwords.

```yaml
spec:
  reviewerDemo:
    username: "demo@tideterm.app"
    passwordRef: env:PASSDMV_DEMO_PASSWORD
    notes: |
      Tap any practice test to start. The IAP unlock screen appears
      after the second free question.
    contactName: "Joel Nuts"
    contactEmail: "devteam@corelift.io"
    contactPhone: "+1-555-0100"
```

| Field | Type | Required | Constraint | Gotcha |
|-------|------|----------|------------|--------|
| `username` | string | no |, | Demo account username. |
| `passwordRef` | string | no | `^env:[A-Z_][A-Z0-9_]*$` | Env var reference, e.g. `env:DEMO_PASSWORD`. Flightline resolves the variable at apply time. Never put the password directly in the YAML file, it will end up in git. |
| `passwordFile` | string | no |, | Path to a file containing the password. Trailing newline is trimmed. Alternative to `passwordRef`. |
| `notes` | string | no | maxLength 4000 | Instructions for the reviewer. Include where to find any non-obvious flows (IAP, restricted features, login-wall bypass). |
| `contactName` | string | no |, | Your name or the developer's name. |
| `contactEmail` | string | no | email format | Contact for Review team questions. |
| `contactPhone` | string | no |, | International format recommended. |

**Password field constraint.** Exactly one of `passwordRef` or `passwordFile` may be present, or neither. Both together is a schema validation error (`oneOf`). If neither is provided, Flightline applies the other reviewer-demo fields without a password update.

---

## spec.categories

App Store category assignment. Category IDs correspond to Apple's category taxonomy, use `flightline categories list` to enumerate valid IDs.

```yaml
spec:
  categories:
    primary: EDUCATION
    secondary: REFERENCE
    primarySubcategories:
      - EDUCATION_TUTORIALS
```

| Field | Type | Required | Constraint | Gotcha |
|-------|------|----------|------------|--------|
| `primary` | string | no |, | Primary category ID (e.g. `BUSINESS`, `GAMES`, `EDUCATION`). See `/v1/appCategories` in the ASC API. |
| `secondary` | string | no |, | Secondary category ID. |
| `primarySubcategories` | array of string | no | maxItems 2 | Subcategory IDs under `primary`. Only valid for categories that have subcategories (notably `GAMES`). |
| `secondarySubcategories` | array of string | no | maxItems 2 | Subcategory IDs under `secondary`. |

---

Omitting either subcategory list leaves it unmanaged. An explicit empty list (`[]`) clears that list.

## spec.pricing

The active base-territory price point. A change is one atomic territory/price-point pair; an omitted field is preserved from verified live state. An unpriced app requires both. Changing the base territory also requires an explicit matching price-point ID. Unrelated manual prices and future windows are preserved; ambiguous schedules fail before writing. Per-territory authoring remains a future L2 extension.

```yaml
spec:
  pricing:
    baseTerritory: USA
    appPricePointId: "<APP_PRICE_POINT_ID>"
```

| Field | Type | Required | Gotcha |
|-------|------|----------|--------|
| `baseTerritory` | string | no | ISO 3166-1 alpha-3 territory code (e.g. `USA`, `GBR`, `JPN`, `AUS`). Not alpha-2. |
| `appPricePointId` | string | no | Apple's app-specific `appPricePoint` resource ID for the selected territory. Use a real ID from ASC or a fetched state file; `FREE` is not a universal resource ID. |

---

## App availability

`spec.appAvailability.territories` maps territory codes to optional `available` and `releaseDate` intent. Changes are limited to freshly observed active preorder territories. Omitted territories and fields remain unchanged; territory creation and ordinary released-app availability changes are not qualified.

`availableInNewTerritories`, `preOrderEnabled`, `preOrderPublishDate`, and `contentStatuses` are observed, read-only fields. Unchanged fetched values round-trip. A release date must be a nonempty `YYYY-MM-DD`; omission preserves it and null clearing is not represented. Ending preorders is an explicit `app-availability preorders end --confirm` action that immediately releases the app, never an automatic apply step.

### Availability fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `availableInNewTerritories` | boolean | No | See the behavior described above. |
| `territories` | object | No | See the behavior described above. |

### Territory fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `available` | boolean | No | See the behavior described above. |
| `releaseDate` | string | No |  Format: `date`. Minimum length: 10. |
| `preOrderEnabled` | boolean | No | See the behavior described above. |
| `preOrderPublishDate` | string | No | See the behavior described above. |
| `contentStatuses` | array | No | See the behavior described above. |


## spec.testflight

TestFlight beta distribution configuration. Only groups listed here are managed by Flightline. Groups absent from this section are left alone.

```yaml
spec:
  testflight:
    groups:
      friends-and-family:
        isInternal: false
        publicLink: false
        testers:
          - email: tester1@example.com
            firstName: Test
            lastName: One
      internal-team:
        isInternal: true
```

### Group key format

Group keys match `^[A-Za-z0-9 _-]+$`. They are the human-readable names you assign, Flightline resolves the ASC resource ID by matching the name against the live group list.

### testflightGroup fields

| Field | Type | Required | Constraint | Gotcha |
|-------|------|----------|------------|--------|
| `isInternal` | boolean | on create | immutable | Internal groups are your App Store Connect team members. External groups are outside testers. |
| `publicLink` | boolean | no |, | Enables Apple's public invite link for this group. Valid only for external groups. |
| `publicLinkLimit` | integer | no | 1 to 10000 | Maximum testers via the public link. Requires `publicLink: true`. |
| `testers` | array | no |, | Explicit tester list. Each entry requires at least `email`. |

An omitted `testers` list leaves membership unmanaged. Explicit `testers: []` removes all managed group memberships. A new group and its declared testers are separate dependent changes; failure to create the group prevents tester operations.

### testflightTester fields

| Field | Type | Required |
|-------|------|----------|
| `email` | string (email format) | yes |
| `firstName` | string | no |
| `lastName` | string | no |

---

## TestFlight metadata and build membership

`spec.testflight.metadata.appLocalizations` maps locales to optional `description`, `feedbackEmail`, `marketingUrl`, `privacyPolicyUrl`, and `tvOsPrivacyPolicy`. `reviewDetails` manages beta review contact information, demo-account name/required status, and notes. It is separate from App Store reviewer information. Apple must already have created the beta review detail. Passwords are excluded from state; explicit L1 beta metadata commands support secret references.

`metadata.builds` contains entries with an exact `build` selector (`number`, `version`, `platform`) and locale-keyed `localizations` containing `whatsNew`. Duplicate selectors are rejected. Fetch observes app-level beta metadata and the attached build; plan/apply/preflight also observe explicitly requested historical builds, without scanning every build's localizations.

Each `spec.testflight.groups.<name>.builds` is an optional complete set of exact build selectors. Omission preserves membership; `[]` explicitly removes all builds. Fetch includes these managed sets only with `--include-beta-builds`; reconciliation reads memberships for groups that declare them. Existing tester-roster behavior is unchanged.

Apply checks every proposed addition's fresh auto-notify policy before changing membership and requires it to be explicitly disabled. Use `testflight recruitment policy` for deliberate policy changes and `testflight recruitment notify --confirm` for notifications. Metadata writes reject conflicting concurrent changes. Invitation resend and recruitment-criteria writes remain unsupported; neither is triggered by fetching or applying state.

### Beta metadata fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `appLocalizations` | object | No | See the behavior described above. |
| `reviewDetails` | object | No | See `BetaReviewDetailsSpec`. |
| `builds` | array | No | See the behavior described above. |

### Beta app localization fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `description` | string | No | See the behavior described above. |
| `feedbackEmail` | string | No | See the behavior described above. |
| `marketingUrl` | string | No | See the behavior described above. |
| `privacyPolicyUrl` | string | No | See the behavior described above. |
| `tvOsPrivacyPolicy` | string | No | See the behavior described above. |

### Beta review fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `contactFirstName` | string | No | See the behavior described above. |
| `contactLastName` | string | No | See the behavior described above. |
| `contactEmail` | string | No | See the behavior described above. |
| `contactPhone` | string | No | See the behavior described above. |
| `demoAccountName` | string | No | See the behavior described above. |
| `notes` | string | No | See the behavior described above. |
| `demoAccountRequired` | boolean | No | See the behavior described above. |

### Beta build metadata fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `build` | object | Yes | See `BetaBuildSelector`. |
| `localizations` | object | No | See the behavior described above. |

### Build selector fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `number` | string | Yes |  Minimum length: 1. |
| `version` | string | Yes |  Minimum length: 1. |
| `platform` | string | Yes |  Values: `IOS`, `MAC_OS`, `TV_OS`, `VISION_OS`. Minimum length: 1. |

### Beta build localization fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `whatsNew` | string | No | See the behavior described above. |


## spec.customProductPages

Custom Product Pages (CPPs), alternate store listings with different screenshots and promotional text, used for ad-driven traffic. Keys are page identifiers (slugs).

```yaml
spec:
  customProductPages:
    summer-discovery-2026:
      visible: true
      localizations:
        en-US:
          promotionalText: "Pass your DMV test before the road trip."
          screenshots:
            APP_IPHONE_69:
              - path: ./screenshots/cpp-summer-iphone69-1.png
```

### customProductPage fields

| Field | Type | Required | Gotcha |
|-------|------|----------|--------|
| `visible` | boolean | no | Controls whether the CPP is visible/active. |
| `localizations` | map | no | Locale-keyed content. |

### CPP localization fields

| Field | Type | Required | maxLength | Gotcha |
|-------|------|----------|-----------|--------|
| `promotionalText` | string | no | 170 | CPP-specific promotional text, overriding the main listing. |
| `screenshots` | map | no |, | Device-class screenshot sets, same format as `spec.screenshots`. |

**Device classes for CPPs.** CPPs support a subset of device classes: `APP_IPHONE_67`, `APP_IPHONE_69`, `APP_IPHONE_65`, `APP_IPHONE_61`, `APP_IPHONE_55`, `APP_IPAD_PRO_3GEN_129`, `APP_IPAD_PRO_3GEN_11`. TV, Watch, and Vision Pro device classes are not supported on CPPs.

**Asset uploads.** `flightline apply` drives main and CPP screenshot uploads from managed local files. Main and CPP previews also have supported asset intent. See [uploading assets](../guides/uploading-assets.md) for checksum comparison, processing, and recovery.

---

## Content rights and custom EULA

`spec.contentRights` accepts an explicit `DOES_NOT_USE_THIRD_PARTY_CONTENT` or `USES_THIRD_PARTY_CONTENT` declaration. Flightline does not infer legal rights from app metadata. Omission preserves the existing declaration.

`spec.appEula` accepts `agreementText` and a complete `territories` array. Creating a custom agreement requires both nonempty text and territories. For an existing agreement, omitted components preserve the observed value. Fetched empty values may round-trip unchanged, but changing an agreement to empty text or an empty territory set is rejected. Removal is an explicit confirmed CLI action, not omission or empty state. Apply rereads the agreement, rejects a stale plan, and confirms the resulting value after a write.

### Custom EULA fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `agreementText` | string | No | See the behavior described above. |
| `territories` | array | No | See the behavior described above. |


## Accessibility declarations

`spec.accessibilityDeclarations.families` maps device families (`IPHONE`, `IPAD`, `APPLE_TV`, `APPLE_WATCH`, `MAC`, `VISION`) to explicit support answers. Supported fields are `supportsAudioDescriptions`, `supportsCaptions`, `supportsDarkInterface`, `supportsDifferentiateWithoutColorAlone`, `supportsLargerText`, `supportsReducedMotion`, `supportsSufficientContrast`, `supportsVoiceControl`, and `supportsVoiceover`. Each is an optional boolean: omitted answers remain unmanaged; explicit `false` is preserved.

Fetch selects the current draft for a family, or the published declaration if no draft exists. `state` is observed and read-only. Replaced history is available through `accessibility-declarations list`, outside desired state. An empty families map deletes nothing. Creating an absent family requires at least one explicit support answer; Flightline never infers accessibility support.

Apply can create a draft or update explicit answers in an existing draft. A change to published answers fails locally: create a draft deliberately through the CLI first. Publishing and deleting a declaration require explicit CLI commands with `--confirm` and a fresh draft-state check; they are never automatic apply operations. These workflows are locally tested; Apple lifecycle acceptance requires live qualification.

### Accessibility answer fields

| Field | Type | Required | Schema constraint |
|---|---|---|---|
| `state` | string | No |  Values: `DRAFT`, `PUBLISHED`. |
| `supportsAudioDescriptions` | boolean | No | See the behavior described above. |
| `supportsCaptions` | boolean | No | See the behavior described above. |
| `supportsDarkInterface` | boolean | No | See the behavior described above. |
| `supportsDifferentiateWithoutColorAlone` | boolean | No | See the behavior described above. |
| `supportsLargerText` | boolean | No | See the behavior described above. |
| `supportsReducedMotion` | boolean | No | See the behavior described above. |
| `supportsSufficientContrast` | boolean | No | See the behavior described above. |
| `supportsVoiceControl` | boolean | No | See the behavior described above. |
| `supportsVoiceover` | boolean | No | See the behavior described above. |


## What Flightline does NOT manage

### Privacy nutrition labels

`spec.privacyLabels` does not exist in the v1alpha1 schema and is not planned for v1. Apple's App Store Connect API v4.5 does not expose the `appPrivacyDetails` resource, there are no read or write endpoints for privacy nutrition labels in the public API.

Flightline ships a `flightline privacy-labels get <bundleId>` stub that returns a typed diagnostic explaining the gap. The JSON contract has `supported: false` and a pointer to the portal.

Manage privacy labels in the App Store Connect web UI.

### Asset uploads

`flightline apply` reconciles declared main and custom product page screenshots, preview videos, and IAP review screenshots. It resolves paths relative to the state file and uses checksums to compare assets. Upload recovery uses saved checkpoints with `--resume`.

An upload commit does not prove that Apple finished processing the asset. Preview reconciliation waits for processing and rejects pending, failed or unknown outcomes. Use the [asset guide](../guides/uploading-assets.md) for upload and recovery commands. App Review attachments remain direct CLI operations, outside the state schema.

---

## Common gotchas

### YAML coercion

Flightline uses `yaml.v3` with `KnownFields(true)` and strict type matching. These are the most common coercion traps:

| Problem | Bad | Good |
|---------|-----|------|
| Boolean string | `yes`, `no`, `on`, `off`, `true`, `false` unquoted in a string field | Quote strings: `"false"` |
| Bare boolean in an enum field | `releaseType: true` | `releaseType: AFTER_APPROVAL` |
| Integer version | `version: 1.0` (YAML float) | `version: "1.0"` (quoted string) |
| Integer build number | `number: 42` (YAML int) | `number: "42"` (quoted string) |
| Leading zeros | `number: "042"` fails the pattern `^[0-9]+(.[0-9]+)*$` in version | Use `"42"` not `"042"` |
| Scientific notation | `1e3` parsed as float in some YAML parsers | Always quote version and build number strings |

The strict loader surfaces these as `file:line:col` diagnostics before schema validation runs, so you see the specific location.

### Per-locale completeness

Every locale you declare in `spec.metadata.locales` is managed. Fill the baseline `name`, `description`, and `supportUrl` fields for each locale or omit that locale entirely. Flightline's `localizations.completeness` rule checks those fields and reports locale coverage gaps across the managed localization surfaces.

### Cross-resource field routing

`name` and `subtitle` live on `appInfoLocalization` in the ASC API; `description`, `keywords`, `whatsNew`, `promotionalText`, `marketingUrl`, `supportUrl`, and `privacyPolicyUrl` live on `appStoreVersionLocalization`. Flightline handles the dispatch, you do not need to know which resource owns which field. But if you see a diff that looks like a no-op update, check whether you have the same locale in two separate ASC resource states.

### seventeenPlus is read-only

The `seventeenPlus` boolean in `spec.ageRating` reflects Apple's computed rating from your questionnaire answers. You cannot set it directly, Flightline returns a typed error if this field appears in a change set. You may include it in the YAML as documentation of the current state (as written by `flightline fetch`), but changes to it are ignored with an error, not silently applied.

### Rating overrides and GRAC classification

`ageRatingOverrideV2` accepts NONE, NINE_PLUS, THIRTEEN_PLUS, SIXTEEN_PLUS, EIGHTEEN_PLUS, or UNRATED. `koreaAgeRatingOverride` accepts NONE, ALL, TWELVE_PLUS, FIFTEEN_PLUS, or NINETEEN_PLUS. `gracRatingClassificationNumber` preserves a user-supplied string. These optional fields are not required by the general questionnaire completeness rule. Flightline does not infer a regional classification or approval.

The deprecated legacy `ageRatingOverride` remains available through the existing L1 setter for compatibility, with its own SEVENTEEN_PLUS enum; it is not part of new desired state. Use V2 for new state declarations.

### contestsAndGambling maps to Apple's "contests" field

The schema uses `contestsAndGambling` for the frequency question about contests and gambling features. On Apple's wire API, this field is called `contests`. Flightline translates in both directions; you always use `contestsAndGambling` in your YAML.

### Omitted spec sections are not managed

If you omit `spec.screenshots` entirely, Flightline will not touch your screenshots, not delete them, not diff them, nothing. This is intentional: partial state files let you manage only the surfaces you care about. If you want Flightline to own a surface, fetch the full state first (`flightline fetch`), then edit.

### Version must be in editable state

Except for the narrowly scoped phased-release operations below, `flightline plan` and `flightline apply` require the version identified by `metadata.version` to be in an editable state in ASC (e.g., `PREPARE_FOR_SUBMISSION`, `DEVELOPER_REJECTED`). If the version is `READY_FOR_SALE` or under review, writes will fail with a 422 from Apple's API.

---

## See also

- [State-as-code guide](../guides/state-as-code.md), 5-minute fetch → edit → plan → apply walkthrough
- [Schema source](../../schemas/flightline.schema.json), the JSON Schema 2020-12 contract
- [Preflight in CI](../guides/preflight-in-ci.md), run live release checks as a deployment gate
- `flightline --help`, `flightline fetch --help`, `flightline plan --help`, `flightline apply --help`

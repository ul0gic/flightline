# Flightline AppState: YAML Reference

## TL;DR

A Flightline state file is a YAML document that declares the desired configuration for one app across every App Store Connect surface Flightline manages. You run `flightline fetch <bundleId>` once to capture live state, edit the YAML, then use `flightline plan` and `flightline apply` to preview and write the diff. The schema is embedded in the binary; your editor autocompletes via the `yaml-language-server` directive at the top of every fetched file.

Every top-level child of `spec` is optional. Omitting a section tells Flightline "leave this surface alone." You don't have to manage everything, start with `spec.metadata` and `spec.version`, add sections when you need them.

The schema URL is `https://flightline.dev/schemas/v1alpha1/state.schema.json`. Fields not listed in this document are not part of the v1alpha1 contract; unrecognized keys cause a `LoadState` error.

**This reference covers v1alpha1.** The `apiVersion` constant locks that. If a future release bumps to `v1beta1`, the diff will be explicit and documented.

See [docs/state-yaml-quickstart.md](./state-yaml-quickstart.md) for a 5-minute walkthrough.

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
  bundleId: com.under5.passdmv
  version: "1.0.1"
  platform: IOS
```

| Field | Type | Required | Default | Constraint | Gotcha |
|-------|------|----------|---------|------------|--------|
| `bundleId` | string | yes |, | `^[a-zA-Z0-9._-]+$`, min 3 chars | Must already exist in ASC. Flightline does not create apps. |
| `version` | string | yes |, | `^[0-9]+(\.[0-9]+)*$` | Marketing version (CFBundleShortVersionString). Leading zeros rejected, `01.0` fails schema. Quote it: `"1.0"`. |
| `platform` | enum | no | `IOS` | `IOS`, `MAC_OS`, `TV_OS`, `VISION_OS` | Defaults to `IOS` when absent. Must match the platform your build targets. |

**PRD lifecycle step:** `metadata.bundleId` + `metadata.version` are the identity keys Flightline uses at every step of the [authoring loop](../.project/prd.md#lifecycle) (fetch → lint → plan → apply → preflight → submit).

---

## spec.version

Controls how and when the version releases once approved by Apple Review.

```yaml
spec:
  version:
    releaseType: AFTER_APPROVAL
    copyright: "© 2026 CoreLift LLC"
```

| Field | Type | Required | Default | Constraint | Gotcha |
|-------|------|----------|---------|------------|--------|
| `releaseType` | enum | no |, | `MANUAL`, `AFTER_APPROVAL`, `SCHEDULED` | If absent, Flightline leaves the current ASC setting unchanged. `SCHEDULED` requires `earliestReleaseDate`. |
| `earliestReleaseDate` | string | no |, | ISO 8601 datetime | Required when `releaseType=SCHEDULED`. Apple rejects times in the past. |
| `copyright` | string | no |, | maxLength 100 | Displayed on the App Store listing. Apple shows this under the app name. |
| `downloadable` | boolean | no |, |, | Rarely needed; controls whether the build is downloadable from TestFlight or the App Store. |

**PRD lifecycle step:** Version release type is set before you submit. `MANUAL` gives you a hold button after approval; `AFTER_APPROVAL` releases automatically; `SCHEDULED` releases at the specified time.

---

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
        marketingUrl: "https://under5.com/passdmv"
        supportUrl: "https://under5.com/passdmv/support"
        privacyPolicyUrl: "https://under5.com/passdmv/privacy"
      es-MX:
        name: ".PassDMV: California"
        subtitle: "Aprueba a la primera"
```

### Locale key format

Locale codes follow the pattern `^[a-z]{2}(-[A-Z]{2})?$`, two-letter language code, optionally followed by a dash and two-letter region code. Examples: `en-US`, `fr-FR`, `ja`, `zh-Hans`, `pt-BR`.

### Per-locale fields

| Field | Type | Required | maxLength | Gotcha |
|-------|------|----------|-----------|--------|
| `name` | string | no | 30 | App name on the store listing. Apple enforces 30 chars; plan/apply will reject strings over this limit before hitting the wire. Applies to `appInfoLocalization`. |
| `subtitle` | string | no | 30 | One-line subtitle beneath the app name. Applies to `appInfoLocalization`. |
| `description` | string | no | 4000 | Long body text on the listing page. Applies to `appStoreVersionLocalization`. |
| `keywords` | string | no | 100 | Comma-separated, no spaces around commas. Goes to `appStoreVersionLocalization`. Not shown to users but affects search indexing. |
| `whatsNew` | string | no | 4000 | "What's New in this Version" text. Version-scoped: this is for the current `metadata.version`. |
| `promotionalText` | string | no | 170 | Promotional text block. **Can be updated without resubmission**, the only metadata field you can change on a live, approved version. Use it for time-sensitive copy. |
| `marketingUrl` | URI | no |, | Full URL. |
| `supportUrl` | URI | no |, | Full URL. |
| `privacyPolicyUrl` | URI | no |, | Full URL. |

**Cross-resource routing.** `name` and `subtitle` live on `appInfoLocalization`; everything else lives on `appStoreVersionLocalization`. Flightline handles the routing, you author a single locale map and Flightline dispatches to the correct ASC resource per field.

**Per-locale completeness.** Every locale you declare must be complete enough for Apple to accept. Apple will reject a submission if a declared locale has a `description` but no `supportUrl`. Flightline's L3 preflight rule `localizations.completeness` (Phase 5) will catch this offline; until then, check the [Apple documentation](https://developer.apple.com/help/app-store-connect/manage-app-information/add-app-localizations/) for required fields per locale.

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

Each device slot accepts 1–10 screenshots (`minItems: 1`, `maxItems: 10`).

**Required devices for new submissions.** Apple requires at least 6.9" and 6.7" screenshots for new iOS app submissions. The L3 preflight rule `screenshots.requiredDevices` (Phase 5) catches this offline.

**Current limitation.** `flightline apply` cannot yet drive the multipart binary upload for screenshots. The diff engine and fetch projection are correct, apply will surface a typed error pointing you to the L1 verb (`flightline screenshots upload`). This is tracked in [QA-010](../.project/issues/open/QA-010-orchestrator-upload-integration.md). Use `flightline screenshots upload` directly for now; the state file's screenshot section is still useful for tracking what should be there.

---

## spec.iap

In-app purchases keyed by `productId`. This section covers consumable, non-consumable, and non-renewing IAPs. Auto-renewable subscriptions live under `spec.testflight` (the subscription group surface), they are not represented here.

```yaml
spec:
  iap:
    products:
      com.under5.passdmv.lifetime:
        type: NON_CONSUMABLE
        name: "Lifetime Access"
        familySharable: false
        contentHosting: NON_HOSTED
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
| `type` | enum | yes | `CONSUMABLE`, `NON_CONSUMABLE`, `NON_RENEWING_SUBSCRIPTION` | `AUTO_RENEWABLE_SUBSCRIPTION` is not valid here, auto-renewing subs live in ASC's subscription group surface. |
| `name` | string | no |, | Apple-facing reference name in ASC. Not shown to customers. |
| `familySharable` | boolean | no |, | Whether Family Sharing is enabled for this IAP. |
| `contentHosting` | enum | no | `HOSTED`, `NON_HOSTED` | Hosted: Apple hosts the downloadable content. Most IAPs are `NON_HOSTED`. |
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
| `path` | string | yes | Relative to state file directory. The L3 preflight rule `iap.reviewScreenshot.exists` (Phase 5) checks this is populated. Missing review screenshots are a common rejection cause. |

**Current limitation.** Like app screenshots, the `reviewScreenshot` binary upload is not yet driven by `flightline apply`. Use `flightline iap update --review-screenshot upload` until [QA-010](../.project/issues/open/QA-010-orchestrator-upload-integration.md) lands.

### iapLocalization fields

| Field | Type | Required | maxLength | Gotcha |
|-------|------|----------|-----------|--------|
| `name` | string | no | 30 | Customer-visible IAP name. |
| `description` | string | no | 45 | Customer-visible IAP description. Note the 45-char ceiling, much shorter than app metadata. |

---

## spec.ageRating

Apple's age-rating questionnaire. Each field answers one content-category question. All fields are optional individually, answering only the categories relevant to your app is fine. However, Apple requires the questionnaire to be complete before you can submit; the L3 preflight rule `version.ageRating.answered` (Phase 5) verifies this.

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

These fields accept `NONE`, `INFREQUENT_OR_MILD`, or `FREQUENT_OR_INTENSE`:

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
| `prolongedGraphicSadisticRealisticViolence` | boolean | Apple's API uses a frequency enum for this field; Flightline maps any non-`NONE` value fetched from Apple to `true`. Set `true` for apps with prolonged, graphic, sadistic violence. |
| `gambling` | boolean | Gambling features (not just references, actual gambling mechanics). |
| `unrestrictedWebAccess` | boolean | App provides unrestricted internet access (e.g., a web browser). |
| `kidsAgeBand` | enum or null | `FIVE_AND_UNDER`, `SIX_TO_EIGHT`, `NINE_TO_ELEVEN`, or `null`. Set only for Kids category apps. |
| `seventeenPlus` | boolean | **Read-only.** Apple derives the 17+ rating from your answers, you cannot set this field directly. `flightline apply` returns a typed error if you include this in a change set. You may include it in the file for documentation, but it has no write effect. |

---

## spec.exportCompliance

Per-build encryption declaration. Every build submitted to Apple must have export compliance answered. Most apps that only use system TLS set `usesNonExemptEncryption: false` and are done.

```yaml
spec:
  exportCompliance:
    usesNonExemptEncryption: false
```

| Field | Type | Required | Gotcha |
|-------|------|----------|--------|
| `usesNonExemptEncryption` | boolean | no | `false` for apps that use only standard system TLS/HTTPS. `true` triggers the ECCN classification block below. |
| `declaration` | object | no | Required only when `usesNonExemptEncryption: true` and the app needs full ECCN classification (rare). |

### declaration (ECCN classification)

Only populate this if your app implements proprietary cryptographic algorithms beyond TLS.

| Field | Type | Gotcha |
|-------|------|--------|
| `containsProprietaryCryptography` | boolean | Your own crypto implementation. |
| `containsThirdPartyCryptography` | boolean | Third-party crypto beyond the OS. |
| `availableOnFrenchStore` | boolean | French regulatory requirement. |
| `usesEncryption` | boolean | Any encryption usage. |
| `exempt` | boolean | Meets an EAR exemption. |
| `eccn` | string | ECCN classification string (e.g. `5D002`). |
| `documentName` | string | Name of the encryption documentation. |
| `documentUrl` | URI | URL to the documentation. |

---

## spec.reviewerDemo

Login credentials for App Review. Apple requires demo credentials for any app that has a sign-in wall. Flightline never logs or echoes passwords.

```yaml
spec:
  reviewerDemo:
    username: "demo@under5.com"
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

## spec.pricing

Single base-territory price point. Per-territory overrides are a future L2 extension.

```yaml
spec:
  pricing:
    baseTerritory: USA
    appPricePointId: "FREE"
```

| Field | Type | Required | Gotcha |
|-------|------|----------|--------|
| `baseTerritory` | string | no | ISO 3166-1 alpha-3 territory code (e.g. `USA`, `GBR`, `JPN`, `AUS`). Not alpha-2. |
| `appPricePointId` | string | no | Apple's `appPricePoint` resource ID. Use `flightline price-points list` to enumerate. `"FREE"` is the free tier. Quote the value, bare `FREE` is not a YAML string. |

---

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
| `isInternal` | boolean | no |, | Internal groups are your App Store Connect team members. External groups are outside testers. |
| `publicLink` | boolean | no |, | Enables Apple's public invite link for this group. Valid only for external groups. |
| `publicLinkLimit` | integer | no | 1–10000 | Maximum testers via the public link. Requires `publicLink: true`. |
| `testers` | array | no |, | Explicit tester list. Each entry requires at least `email`. |

### testflightTester fields

| Field | Type | Required |
|-------|------|----------|
| `email` | string (email format) | yes |
| `firstName` | string | no |
| `lastName` | string | no |

---

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

**Current limitation.** CPP screenshot binary uploads are not yet driven by `flightline apply`, same as main screenshots. See [QA-010](../.project/issues/open/QA-010-orchestrator-upload-integration.md).

---

## What Flightline does NOT manage

### Privacy nutrition labels

`spec.privacyLabels` does not exist in the v1alpha1 schema and is not planned for v1. Apple's App Store Connect API v4.3 does not expose the `appPrivacyDetails` resource, there are no read or write endpoints for privacy nutrition labels in the public API.

Flightline ships a `flightline privacy-labels get <bundleId>` stub that returns a typed diagnostic explaining the gap. The JSON contract has `supported: false` and a pointer to the portal.

Manage privacy labels in the App Store Connect web UI. See [ISSUE-002](../.project/issues/closed/ISSUE-002-privacy-labels-not-in-asc-api.md) for full context.

### Asset upload paths (screenshots, IAP review screenshots, CPP screenshots)

The `flightline apply` orchestrator does not yet drive multipart binary uploads. The sections for `spec.screenshots`, `spec.iap.products[*].reviewScreenshot`, and `spec.customProductPages.<name>.localizations.<locale>.screenshots` are fully supported in `flightline fetch` and `flightline plan` (the diff engine compares checksums), but `flightline apply` returns a typed error for these change paths and directs you to the L1 upload verbs:

```
flightline screenshots upload <bundleId> --version <v> --locale <l> --device-set <d> <path>
flightline iap update <productId> --review-screenshot <path>
```

This is tracked in [QA-010](../.project/issues/open/QA-010-orchestrator-upload-integration.md). The full apply orchestration (including checksum-skip and resume) lands when that issue closes.

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

Every locale you declare in `spec.metadata.locales` is managed. If you declare `es-MX` but only provide `name`, Apple may reject the submission because `supportUrl` is missing. Either fill all required fields for a locale or omit the locale entirely. Flightline's L3 rule `localizations.completeness` (Phase 5) will enforce this offline.

### Cross-resource field routing

`name` and `subtitle` live on `appInfoLocalization` in the ASC API; `description`, `keywords`, `whatsNew`, `promotionalText`, `marketingUrl`, `supportUrl`, and `privacyPolicyUrl` live on `appStoreVersionLocalization`. Flightline handles the dispatch, you do not need to know which resource owns which field. But if you see a diff that looks like a no-op update, check whether you have the same locale in two separate ASC resource states.

### seventeenPlus is read-only

The `seventeenPlus` boolean in `spec.ageRating` reflects Apple's computed rating from your questionnaire answers. You cannot set it directly, Flightline returns a typed error if this field appears in a change set. You may include it in the YAML as documentation of the current state (as written by `flightline fetch`), but changes to it are ignored with an error, not silently applied.

### contestsAndGambling maps to Apple's "contests" field

The schema uses `contestsAndGambling` for the frequency question about contests and gambling features. On Apple's wire API, this field is called `contests`. Flightline translates in both directions; you always use `contestsAndGambling` in your YAML.

### Omitted spec sections are not managed

If you omit `spec.screenshots` entirely, Flightline will not touch your screenshots, not delete them, not diff them, nothing. This is intentional: partial state files let you manage only the surfaces you care about. If you want Flightline to own a surface, fetch the full state first (`flightline fetch`), then edit.

### Version must be in editable state

`flightline plan` and `flightline apply` require the version identified by `metadata.version` to be in an editable state in ASC (e.g., `PREPARE_FOR_SUBMISSION`, `DEVELOPER_REJECTED`). If the version is `READY_FOR_SALE` or under review, writes will fail with a 422 from Apple's API.

---

## See also

- [Quickstart](./state-yaml-quickstart.md), 5-minute fetch → edit → plan → apply walkthrough
- [Schema source](../schemas/flightline.schema.json), the JSON Schema 2020-12 contract
- [PRD Lifecycle](../.project/prd.md#lifecycle), the full authoring loop (lint → plan → apply → preflight → submit)
- `flightline --help`, `flightline fetch --help`, `flightline plan --help`, `flightline apply --help`

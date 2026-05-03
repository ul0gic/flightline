# Flightline Preflight Rules

Flightline's L3 layer catches the clerical mistakes that cause Apple to reject a release. Every rule in this document encodes a real rejection pattern — either from Apple's documented guidelines or from production experience.

Two commands run the rules:

- **`fline lint <state.yaml>`** — offline. Validates the state file against the JSON Schema and runs every Offline and Both-mode rule. No network required. Run this in CI on every commit that touches `state.yaml`.
- **`fline preflight <bundleId> --version <v>`** — live. Reads the actual ASC API state for the specified bundle and version, then runs every Live and Both-mode rule. Requires a valid `.p8` credential. Run this just before `fline submit`.

Both commands emit a stable JSON contract via `--output json`:

```json
{
  "diagnostics": [
    {
      "ruleId": "iap.attached-to-review-submission",
      "severity": "error",
      "message": "IAP \"com.example.lifetime\" is READY_TO_SUBMIT but not in review submission abc123",
      "path": "/spec/iap/products/com.example.lifetime",
      "fixHint": "add the IAP to the submission: `fline review-submissions items ...`",
      "reference": "PRD §L3 — IAP attached-to-review-submission"
    }
  ]
}
```

The `ruleId` field is a stable contract. Pipe to `jq`, grep in CI, or feed to LLM prompts. Renaming a rule ID is a breaking change.

### Severity and exit codes

| Severity | Meaning | Exit code |
|----------|---------|-----------|
| `error` | Known rejection cause or structural problem that blocks `apply`. Preflight exits non-zero. | `1` |
| `warning` | Something to address; Apple is unlikely to hard-reject for it, but it's worth fixing. | `2` (when no errors present) |
| `info` | A reminder or hint. Never gates submission. | `0` |

When both errors and warnings are present, exit code is `1`.

---

## Quick index

| Rule ID | Mode | Severity |
|---------|------|----------|
| [`build.attached-and-valid`](#buildattached-and-valid) | Live | Error |
| [`iap.attached-to-review-submission`](#iapattached-to-review-submission) | Live | Error |
| [`iap.promotional-image-distinct`](#iappromotional-image-distinct) | Live | Error |
| [`iap.review-screenshot-exists`](#iapreview-screenshot-exists) | Live | Error |
| [`localizations.completeness`](#localizationscompleteness) | Offline | Warning |
| [`screenshots.required-devices`](#screenshotsrequired-devices) | Both | Error |
| [`strict.format-email`](#strictformat-email) | Offline | Warning |
| [`strict.required-nonzero`](#strictrequired-nonzero) | Offline | Error |
| [`strict.yaml-coercion`](#strictyaml-coercion) | Offline | Error |
| [`version.account-deletion-attested`](#versionaccount-deletion-attested) | Both | Info |
| [`version.age-rating-answered`](#versionage-rating-answered) | Both | Error |
| [`version.export-compliance-answered`](#versionexport-compliance-answered) | Both | Error |

Rules are sorted alphabetically by ID. The runner also executes them in this order for stable JSON output.

---

## Rule reference

---

### `build.attached-and-valid`

- **Mode:** Live
- **Severity:** Error
- **What it checks:** Whether the App Store version has a build attached and whether that build's `processingState` is `VALID`.
- **Why it matters:** Apple's submission flow blocks "Submit for Review" until both conditions are true. Submitting without a valid build is impossible — but the error only surfaces at submit time, after you've confirmed the dialog. This rule surfaces the gap at preflight, before you commit to review.
- **When it fires:**
  - The version has no build attached (the `/v1/appStoreVersions/{id}/build` relationship resolves to empty or 404).
  - A build is attached but its `processingState` is not `VALID` (e.g., still `PROCESSING`, or `FAILED`/`INVALID`).
- **How to fix:**
  - Upload the build via Xcode or `altool`, wait for Apple to finish processing (the `processingState` transitions `PROCESSING` → `VALID`), then attach it: `fline builds attach <bundleId> --version <v> --build <buildNumber>`.
  - If the build is `FAILED` or `INVALID`, re-archive and re-upload. `fline builds list <bundleId>` shows current state for every uploaded build.
- **Reference:** PRD §L3 — build.attached-and-valid

**Example diagnostic JSON:**

```json
{
  "ruleId": "build.attached-and-valid",
  "severity": "error",
  "message": "version \"1.2.0\" has no build attached",
  "path": "/spec/build",
  "fixHint": "upload via Xcode/altool, wait for VALID, then `fline builds attach com.example.myapp --version 1.2.0 --build <n>`.",
  "reference": "PRD §L3 — build.attached-and-valid"
}
```

Or, when a build is attached but still processing:

```json
{
  "ruleId": "build.attached-and-valid",
  "severity": "error",
  "message": "build \"1.2.0\" is attached but processingState=\"PROCESSING\" (must be VALID)",
  "path": "/spec/build",
  "fixHint": "wait for Apple to finish processing (PROCESSING -> VALID), or re-upload if the build is FAILED/INVALID. `fline builds list <bundleId>` shows current state.",
  "reference": "PRD §L3 — build.attached-and-valid"
}
```

---

### `iap.attached-to-review-submission`

- **Mode:** Live
- **Severity:** Error
- **What it checks:** Whether every IAP product in `READY_TO_SUBMIT` state is also present in the latest review submission's items list.
- **Why it matters:** This is the #1 IAP rejection cause. Developers mark an IAP ready, assume "ready" means "submitted," and then the app goes through review without the IAP attached. Apple either rejects the app ("IAP referenced but not in review") or approves the app with the IAP silently marked `SCHEDULED-but-not-live`. The IAP never goes live. The rule catches this before you submit.
- **When it fires:** An IAP product has `state=READY_TO_SUBMIT` but its resource ID does not appear in the open review submission's items (`/v1/reviewSubmissions/{id}/items`). Also fires when there is no open review submission at all.
- **How to fix:**
  - Inspect the current submission items: `fline review-submissions items <bundleId> --submission <submissionId>`.
  - Attach the IAP through App Store Connect (Submission > Add Items) or via the review-submissions write surface.
  - If there is no open submission, create one first.
- **Reference:** PRD §L3 — IAP attached-to-review-submission

**Example diagnostic JSON:**

```json
{
  "ruleId": "iap.attached-to-review-submission",
  "severity": "error",
  "message": "IAP \"com.example.lifetime\" is READY_TO_SUBMIT but not in review submission abc123def456",
  "path": "/spec/iap/products/com.example.lifetime",
  "fixHint": "add the IAP to the submission: `fline review-submissions items com.example.myapp --submission abc123def456` to inspect, then attach via App Store Connect or the submissions write surface.",
  "reference": "PRD §L3 — IAP attached-to-review-submission"
}
```

When there is no open submission:

```json
{
  "ruleId": "iap.attached-to-review-submission",
  "severity": "error",
  "message": "IAP \"com.example.lifetime\" is READY_TO_SUBMIT but no open review submission found for this app",
  "path": "/spec/iap/products/com.example.lifetime",
  "fixHint": "create or open a review submission for the app and attach the IAP product. `fline review-submissions list <bundleId>` shows current submissions.",
  "reference": "PRD §L3 — IAP attached-to-review-submission"
}
```

---

### `iap.promotional-image-distinct`

- **Mode:** Live
- **Severity:** Error
- **What it checks:** Whether any IAP's review screenshot shares a `sourceFileChecksum` with any of the app's store screenshots. Apple Guideline 2.3.2 explicitly prohibits reusing a store screenshot as IAP promotional art.
- **Why it matters:** Guideline 2.3.2 is a hard rejection. Apple's reviewer checks whether the IAP "promotional" image is distinct from the app's store screenshots. Developers who drag-and-drop a screenshot to the IAP review screenshot field to save time will hit this.
- **API note:** Apple's public API does not expose the IAP "promotional artwork" (the StoreKit promoted-purchase image) directly. The closest accessible asset is the review screenshot at `/v2/inAppPurchases/{id}/appStoreReviewScreenshot`. Flightline checks for checksum equality between that asset and the app's store screenshots — which catches the exact misuse pattern (reusing a screenshot file across surfaces) regardless of which field it landed on. When Apple exposes promotional-artwork hashes via the API, a sibling check will be added.
- **When it fires:** An IAP's review screenshot `sourceFileChecksum` matches any screenshot in the app's store screenshot sets (any locale, any device class, any version).
- **How to fix:** Replace the IAP review screenshot with a unique image that is not reused from the app's store listing. The image should specifically represent the IAP purchase, not the app in general.
- **Reference:** Apple Guideline 2.3.2

**Example diagnostic JSON:**

```json
{
  "ruleId": "iap.promotional-image-distinct",
  "severity": "error",
  "message": "IAP \"com.example.lifetime\" review screenshot reuses an app screenshot (sourceFileChecksum a1b2c3d4e5f6)",
  "path": "/spec/iap/products/com.example.lifetime/reviewScreenshot",
  "fixHint": "use a unique image for the IAP; reusing a store screenshot violates Guideline 2.3.2.",
  "reference": "Apple Guideline 2.3.2"
}
```

---

### `iap.review-screenshot-exists`

- **Mode:** Live
- **Severity:** Error
- **What it checks:** Whether every IAP product that is approaching review has an App Store review screenshot attached.
- **Why it matters:** Apple requires a review screenshot for any IAP submitted for review. The screenshot is attached via a 3-levels-deep tab in App Store Connect that is easy to miss. Missing it is a hard rejection cause with no ambiguity — the submission is returned immediately.
- **When it fires:** An IAP is in one of the states where Apple will imminently review it (`READY_TO_SUBMIT`, `WAITING_FOR_REVIEW`, `IN_REVIEW`, `DEVELOPER_ACTION_NEEDED`, `PENDING_BINARY_APPROVAL`) and the IAP's `/v2/inAppPurchases/{id}/appStoreReviewScreenshot` relationship resolves to null or 404.
- **How to fix:** Upload the review screenshot: `fline iap review-screenshot upload <bundleId> --product <productId> <file>`. The screenshot should show the IAP purchase within the app context.
- **Reference:** PRD §L3 — IAP review-screenshot-exists

**Example diagnostic JSON:**

```json
{
  "ruleId": "iap.review-screenshot-exists",
  "severity": "error",
  "message": "IAP \"com.example.lifetime\" has no App Store review screenshot attached",
  "path": "/spec/iap/products/com.example.lifetime/reviewScreenshot",
  "fixHint": "upload one: `fline iap review-screenshot upload com.example.myapp --product com.example.lifetime <file>`",
  "reference": "PRD §L3 — IAP review-screenshot-exists"
}
```

---

### `localizations.completeness`

- **Mode:** Offline
- **Severity:** Warning
- **What it checks:** Whether every locale that appears in one localizable surface (metadata, screenshots, IAP localizations) also appears in every other surface managed in the state file.
- **Why it matters:** Apple may reject a submission where a locale has metadata but no screenshots (or vice versa) because reviewers can't preview the listing in that language. Inconsistency across surfaces is also a signal of a partially-applied edit — a locale was added to screenshots but the metadata block wasn't updated.
- **When it fires:** A locale exists in the union of all declared surfaces (metadata, screenshots, iap.localizations) but is absent from at least one of those surfaces. Each missing (surface, locale) pair produces one warning.
- **How to fix:** Either add the locale to every localizable surface, or remove it from all of them. A locale that is legitimately only in one surface (e.g., a screenshots-only locale) can be left as-is — the warning is advisory. Use `--output json` and pipe to `jq` to filter out specific rule IDs in CI if you have intentional single-surface locales.
- **Reference:** PRD §L3 — localizations.completeness

**Example diagnostic JSON:**

```json
{
  "ruleId": "localizations.completeness",
  "severity": "warning",
  "message": "locale \"fr-FR\" is declared in another surface but missing from screenshots",
  "path": "/spec/screenshots/locales/fr-FR",
  "fixHint": "either add the locale to every localizable surface (metadata, screenshots, iap.localizations) or remove it everywhere.",
  "reference": "PRD §L3 — localizations.completeness"
}
```

---

### `screenshots.required-devices`

- **Mode:** Both
- **Severity:** Error
- **What it checks:** Whether every locale has screenshots for the two device classes Apple currently requires for new iPhone submissions: 6.9" (`APP_IPHONE_69`) and 6.7" (`APP_IPHONE_67`). Apple's submission flow hard-blocks "Submit for Review" until both are present per locale.
- **Why it matters:** Apple rotates its required-device list with major hardware releases. As of the v1 rule set, both 6.9" and 6.7" are required. Missing either causes a submission block that is not surfaced as a reviewer rejection — the UI simply won't allow you to proceed.
- **When it fires:**
  - **Offline:** A locale in `spec.screenshots.locales` has no entry (or an empty array) for `APP_IPHONE_69` or `APP_IPHONE_67`.
  - **Live:** A locale on the live App Store version has no screenshot set for `APP_IPHONE_69` or `APP_IPHONE_67`.
- **How to fix:**
  - Upload the missing screenshots: `fline screenshots upload <bundleId> --version <v> --locale <locale> --device-set APP_IPHONE_69 <files...>`.
  - Screenshot dimensions: 6.9" requires 1320×2868 or 2868×1320 (portrait/landscape); 6.7" requires 1290×2796 or 2796×1290.
- **Reference:** PRD §L3 — screenshots.required-devices

**Example diagnostic JSON:**

```json
{
  "ruleId": "screenshots.required-devices",
  "severity": "error",
  "message": "locale \"en-US\" is missing required device APP_IPHONE_69",
  "path": "/spec/screenshots/locales/en-US/APP_IPHONE_69",
  "fixHint": "add at least one screenshot for the APP_IPHONE_69 device class to spec.screenshots.locales.en-US.",
  "reference": "PRD §L3 — screenshots.required-devices"
}
```

Live variant:

```json
{
  "ruleId": "screenshots.required-devices",
  "severity": "error",
  "message": "locale \"en-US\" has no live screenshots for required device APP_IPHONE_67",
  "path": "/spec/screenshots/locales/en-US/APP_IPHONE_67",
  "fixHint": "upload screenshots for APP_IPHONE_67 in locale en-US — `fline screenshots upload <bundleId> --version <v> --locale en-US --device-set APP_IPHONE_67 ...`",
  "reference": "PRD §L3 — screenshots.required-devices"
}
```

---

### `strict.format-email`

- **Mode:** Offline
- **Severity:** Warning
- **What it checks:** Whether the contact email fields in `spec.reviewerDemo.contactEmail` and `spec.testflight.groups.*.testers[].email` match a permissive RFC 5322 simple-form pattern (`local@domain.tld`).
- **Why it matters:** The JSON Schema declares `format: email` on these fields, but `santhosh-tekuri/jsonschema/v6` does not enforce format keywords by default. A value like `"joe at example dot com"` slips past schema validation silently. Apple's systems catch the bad address on submit or invite — but by then you've already committed to review or sent a broken invite. This rule catches malformed addresses offline, before any wire call.
- **Background:** This rule was created to close QA-011 at the lint layer. The loader stays permissive so existing state files don't break; the lint layer surfaces the gaps with actionable diagnostics. See `.project/issues/closed/QA-011-loader-quirks-format-and-yes-no-coercion.md`.
- **When it fires:** An email field has a non-empty value that does not match `^[^\s@]+@[^\s@]+\.[^\s@]+$`. Empty values are handled by `strict.required-nonzero`, not here, to avoid double-reporting.
- **How to fix:** Replace the value with a valid email address in the form `local@domain.tld`. For reviewer contact info, Apple uses this address to reach you about review questions; a bad address silently breaks that channel.
- **Reference:** QA-011 (resolved via this rule); JSON Schema `format: email`

**Example diagnostic JSON:**

```json
{
  "ruleId": "strict.format-email",
  "severity": "warning",
  "message": "spec.reviewerDemo.contactEmail \"joe at example dot com\" does not look like an email",
  "path": "/spec/reviewerDemo/contactEmail",
  "fixHint": "use a real email address: local@domain.tld. Apple uses this to contact you about review issues.",
  "reference": "QA-011 (resolved via this rule); schema format: email"
}
```

Tester variant:

```json
{
  "ruleId": "strict.format-email",
  "severity": "warning",
  "message": "testflight group \"internal\" tester[0] email \"not-an-email\" does not look like an email",
  "path": "/spec/testflight/groups/internal/testers/0/email",
  "fixHint": "use a real email address; Apple's invite system rejects non-RFC-5322 addresses on submit.",
  "reference": "QA-011 (resolved via this rule); schema format: email"
}
```

---

### `strict.required-nonzero`

- **Mode:** Offline
- **Severity:** Error
- **What it checks:** Whether required string fields that the JSON Schema marks as `required` are also non-empty in the authored YAML. The specific focus in v1 is `spec.testflight.groups.*.testers[].email`.
- **Why it matters:** Go's `json.Marshal` emits `"email": ""` for a zero-value `string` field even when the YAML omits the key entirely. The schema's `required: ["email"]` constraint is satisfied by the *presence* of the key in the JSON object — not by its non-emptiness — so a tester without an email is silently accepted by the validator. The tester is then quietly dropped by the apply-time resolver because it has no address to invite.
- **Background:** This rule was created to close QA-011 at the lint layer. The JSON Schema approach (`minLength: 1`) would also catch this, but the lint rule catches it at the YAML source level with a file-and-line reference. See `.project/issues/closed/QA-011-loader-quirks-format-and-yes-no-coercion.md`.
- **When it fires:** A tester mapping under `spec.testflight.groups.<group>.testers[]` is missing the `email` key entirely, or has `email: ""` (explicitly empty).
- **How to fix:** Every tester row must have a non-empty `email` field:

  ```yaml
  spec:
    testflight:
      groups:
        internal:
          testers:
            - email: alice@example.com
              firstName: Alice
              lastName: Smith
  ```

- **Reference:** QA-011 (resolved via this rule)

**Example diagnostic JSON:**

```json
{
  "ruleId": "strict.required-nonzero",
  "severity": "error",
  "message": "line 14:5 — testflight group \"internal\" tester[0] is missing a non-empty email",
  "path": "/spec/testflight/groups/internal/testers/0/email",
  "fixHint": "every tester row must have a non-empty `email` field. Empty strings satisfy the schema's `required` (because the JSON key is present) but cannot be invited.",
  "reference": "QA-011 (resolved via this rule)"
}
```

---

### `strict.yaml-coercion`

- **Mode:** Offline
- **Severity:** Error
- **What it checks:** Whether any boolean field in the state file carries a YAML 1.1 boolean token (`yes`, `no`, `on`, `off`, `y`, `n` in any case) instead of a proper Go-compatible boolean (`true` or `false`).
- **Why it matters:** `go.yaml.in/yaml/v3` implements the YAML 1.1 core schema and silently coerces `yes`/`no`/`on`/`off` to `bool` when decoding into a `*bool`. **Quoting does not suppress this coercion** — `gambling: "yes"` and `gambling: 'no'` both decode to `true` and `false` respectively. A developer who types `gambling: "yes"` expecting a string answer gets `true` applied to ASC, silently.
- **Background:** This rule was created to close QA-011 at the lint layer. The loader stays permissive (existing state files keep working); the lint layer surfaces the gap with a file-and-line reference. See `.project/issues/closed/QA-011-loader-quirks-format-and-yes-no-coercion.md`.
- **When it fires:** A YAML scalar node at a known boolean field path has a value from the YAML 1.1 boolean token set (`yes`, `no`, `on`, `off`, `y`, `n`). Both unquoted and quoted forms fire — the coercion is the same in both cases.

  Known boolean fields (v1): `usesNonExemptEncryption`, `availableOnFrenchStore`, `containsProprietaryCryptography`, `containsThirdPartyCryptography`, `usesEncryption`, `exempt`, `prolongedGraphicSadisticRealisticViolence`, `gambling`, `unrestrictedWebAccess`, `seventeenPlus`, `familySharable`, `contentHosting`, `downloadable`, `isInternal`, `publicLink`, `visible`.

- **How to fix:** Replace `yes`/`no`/`on`/`off` with `true` or `false` everywhere in the state file. There are no legitimate uses of YAML 1.1 boolean tokens on these fields.

  ```yaml
  # Before (fires the rule)
  spec:
    exportCompliance:
      usesNonExemptEncryption: "no"

  # After (clean)
  spec:
    exportCompliance:
      usesNonExemptEncryption: false
  ```

- **Reference:** QA-011 (resolved via this rule); `go.yaml.in/yaml/v3` YAML 1.1 core schema

**Example diagnostic JSON:**

```json
{
  "ruleId": "strict.yaml-coercion",
  "severity": "error",
  "message": "line 8:5 — bool field \"usesNonExemptEncryption\" has quoted YAML 1.1 token \"no\" (yaml.v3 coerces yes/no/on/off to bool when decoding into *bool, even when quoted)",
  "path": "/usesNonExemptEncryption",
  "fixHint": "use a real boolean: write `true` or `false`. Quoting yes/no does not suppress the coercion in yaml.v3.",
  "reference": "QA-011 (resolved via this rule); yaml.v3 YAML 1.1 core schema"
}
```

---

### `version.account-deletion-attested`

- **Mode:** Both
- **Severity:** Info
- **What it checks:** Reminds you to confirm the in-app account-deletion attestation if the app has user accounts.
- **Why it matters:** Apps with user accounts that haven't toggled the account-deletion attestation are a frequent rejection cause (Guideline 5.1.1(v)). The attestation is a one-time panel toggle in App Store Connect that many developers miss.
- **API limitation:** Apple's public App Store Connect API does not expose the account-deletion attestation field. The toggle lives in App Store Connect's web UI (App > Distribution > App Privacy > Account Deletion) and is not accessible via any `/v1` endpoint. As a result, this rule cannot perform a definitive yes/no check — it always emits an Info-severity reminder so you remember to verify the panel before submission. When Apple ships an API endpoint for the attestation, this rule will be upgraded to a hard Error check.
- **When it fires:** Always — on every `lint` and `preflight` run. This is intentional: the reminder fires every time so it doesn't get buried.
- **How to fix:** In App Store Connect: App > Distribution > App Privacy > Account Deletion > toggle on. If the app has no user accounts (no login, no profile, no persistent identity), document that it's exempt and ignore the reminder.
- **Reference:** Apple Guideline 5.1.1(v)

**Example diagnostic JSON:**

```json
{
  "ruleId": "version.account-deletion-attested",
  "severity": "info",
  "message": "if the app has user accounts, confirm the in-app account-deletion attestation is enabled in App Store Connect (App > Distribution > App Privacy > Account Deletion). Apple does not expose this field via the public API; preflight cannot verify it.",
  "path": "/spec/appInfo",
  "fixHint": "in App Store Connect: App Privacy > Account Deletion > toggle on, or document why your app is exempt (no user accounts).",
  "reference": "Apple Guideline 5.1.1(v)"
}
```

---

### `version.age-rating-answered`

- **Mode:** Both
- **Severity:** Error
- **What it checks:** Whether the age-rating questionnaire is fully answered. Apple's submission flow blocks "Submit for Review" until every prompt has a value.
- **Why it matters:** The age-rating questionnaire has 12 prompts spread across frequency-enum fields (violence, sexual content, profanity, etc.) and boolean prompts (gambling, unrestricted web access). A partially-answered questionnaire surfaces as a soft block on the "Submit for Review" button — Apple won't tell you which field is missing until you open the specific panel. This rule reports every missing field by name.
- **When it fires:**
  - **Offline:** `spec.ageRating` is absent from the state file entirely, or any of the 12 required fields is `nil` (pointer not set). A pointer-to-empty-string is also treated as unanswered.
  - **Live:** The `ageRatingDeclaration` fetched via `/v1/appInfos/{id}/ageRatingDeclaration` has an empty string for any frequency-enum field, or a nil pointer for any boolean prompt.
- **How to fix:**
  - Add or complete the `spec.ageRating` block in your state file. Every frequency-enum field accepts `NONE`, `INFREQUENT_OR_MILD`, or `FREQUENT_OR_INTENSE`. Every boolean prompt accepts `true` or `false`. `NONE` and `false` are valid — they mean "this content type is absent from the app."
  - See [docs/state-yaml.md#spec-agerating](./state-yaml.md) for the full field list.
  - For live failures: answer the prompts in App Store Connect or via `fline age-rating set <bundleId> --version <v> --from age.yaml`.
- **Reference:** PRD §L3 — version.age-rating-answered

**Example diagnostic JSON** (missing block):

```json
{
  "ruleId": "version.age-rating-answered",
  "severity": "error",
  "message": "spec.ageRating is missing — Apple requires every prompt to be answered",
  "path": "/spec/ageRating",
  "fixHint": "add the ageRating block; every frequency-enum and boolean prompt must have a value. See docs/state-yaml.md#spec-agerating.",
  "reference": "PRD §L3 — version.age-rating-answered"
}
```

Specific field missing:

```json
{
  "ruleId": "version.age-rating-answered",
  "severity": "error",
  "message": "age-rating field \"gambling\" is not answered",
  "path": "/spec/ageRating/gambling",
  "fixHint": "set every age-rating prompt to a value (NONE is valid for frequency fields; false is valid for boolean prompts).",
  "reference": "PRD §L3 — version.age-rating-answered"
}
```

---

### `version.export-compliance-answered`

- **Mode:** Both
- **Severity:** Error
- **What it checks:** Whether the export-compliance question (`usesNonExemptEncryption`) has been answered. Apple requires this declaration before submission.
- **Why it matters:** Export compliance is a legal and technical requirement. Apple blocks submission until the answer is on record — either in the build's `Info.plist` (`ITSAppUsesNonExemptEncryption`) or set per-version in App Store Connect. Forgetting it is one of the most common submission blocks for developers shipping new builds.
- **When it fires:**
  - **Offline:** `spec.exportCompliance` is present in the state file but `usesNonExemptEncryption` is `nil`. Note: omitting `spec.exportCompliance` entirely does NOT fire the rule — the user may be relying on the build's `Info.plist` answer. The rule only fires when the block is present but the answer is unset.
  - **Live:** The build attached to the version has no value for `usesNonExemptEncryption` in its build attributes.
- **How to fix:**
  - Most apps: set `spec.exportCompliance.usesNonExemptEncryption: false`. This is correct for apps that don't use non-exempt encryption (which is the vast majority of apps — standard HTTPS/TLS is exempt).
  - Apps using non-exempt encryption (custom crypto, certain VPN implementations): set `usesNonExemptEncryption: true` and complete the supplemental declaration fields.
  - Alternatively, set `ITSAppUsesNonExemptEncryption` in the app's `Info.plist` to answer it at the build level. The ASC per-version field and the `Info.plist` key are equivalent; whichever you set, only one needs to be set.
  - For live failures: `fline export-compliance set <bundleId> --version <v> --uses-non-exempt-encryption=false`.
- **Reference:** PRD §L3 — version.export-compliance-answered

**Example diagnostic JSON** (offline, block present but unanswered):

```json
{
  "ruleId": "version.export-compliance-answered",
  "severity": "error",
  "message": "spec.exportCompliance is set but usesNonExemptEncryption is nil",
  "path": "/spec/exportCompliance/usesNonExemptEncryption",
  "fixHint": "set the answer: `spec.exportCompliance.usesNonExemptEncryption: false` (most apps) or `true` plus a declaration block. See docs/state-yaml.md#spec-exportcompliance.",
  "reference": "PRD §L3 — version.export-compliance-answered"
}
```

Live variant:

```json
{
  "ruleId": "version.export-compliance-answered",
  "severity": "error",
  "message": "the build attached to this version has not declared usesNonExemptEncryption",
  "path": "/spec/exportCompliance/usesNonExemptEncryption",
  "fixHint": "answer the export-compliance question in App Store Connect, or `fline export-compliance set <bundleId> --version <v> --uses-non-exempt-encryption=false`.",
  "reference": "PRD §L3 — version.export-compliance-answered"
}
```

---

## Authoring new rules

Every rejection eaten in production should become a rule. The pattern is small: each rule is a single Go file in `internal/lint/rules_<domain>_<name>.go`, roughly 50–100 lines, with one rule type implementing the `Rule` interface and one `init()` call to `lint.Register(...)`.

```go
package lint

// myNewRule fires when <describe the rejection scenario>.
//
// Mode=<Offline|Live|Both>. <Why this mode.>
type myNewRule struct{}

func init() { Register(myNewRule{}) }

func (myNewRule) ID() string         { return "domain.rule-name" }    // stable, kebab-case
func (myNewRule) Severity() Severity { return SeverityError }
func (myNewRule) Mode() Mode         { return ModeLive }

func (r myNewRule) Check(ctx CheckContext) []Diagnostic {
    if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
        return nil
    }
    // ... check logic ...
    return []Diagnostic{{
        RuleID:    r.ID(),
        Severity:  SeverityError,
        Message:   "...",
        Path:      "/spec/...",
        FixHint:   "...",
        Reference: "Apple Guideline X.Y.Z or PRD §L3",
    }}
}
```

Rules self-register via `init()` — no wiring needed in `runner.go` or `rules.go`. The runner picks up all registered rules automatically through `lint.All()` and `lint.Filter(mode)`.

Rule IDs are a stable JSON contract. Once published, an ID cannot be renamed without a major-version bump. Choose carefully: `domain.what-it-checks`, all lowercase, kebab-case within the name segment, dot separator between domain and name.

After writing the rule, add it to the quick-index table in `docs/preflight-rules.md` and write a rule section following the pattern above.

---

## What rules cannot check

Two categories of rejection are outside Flightline's reach as of ASC API v4.3:

### Resolution-center reviewer messages

Apple does not expose the reviewer-written rejection text via the public API. The text lives in App Store Connect's Resolution Center (an inbox-style web surface) and has no API endpoint in the v4.3 spec.

`fline rejection <bundleId> --version <v>` reports the API-visible rejection state: the version state, review submission state, and per-item states. For the reviewer's actual written reason, open App Store Connect > your app > App Review > Resolution Center.

This is a known gap. See `.project/issues/closed/ISSUE-001-oapi-codegen-collisions.md` for related background on the API surface coverage decisions.

### Privacy nutrition labels (`appPrivacyDetails`)

Privacy nutrition labels are entirely absent from the ASC API v4.3 spec. The `appPrivacyDetails` resource does not exist in `openapi.oas.json`. As a result, Flightline cannot read or write privacy labels, and no lint rule can check their completeness.

Manage privacy labels in the App Store Connect web UI (App > App Privacy). `fline privacy-labels get` returns a typed diagnostic explaining the gap rather than silently returning empty data.

Track Apple's spec versions. If a future ASC API version adds `appPrivacyDetails` endpoints, Flightline will wire them and the gap rules will be upgraded accordingly.

### App Store Review Guidelines: subjective judgments

Rules reducible to API-checkable state (missing assets, unanswered questions, attachment relationships) are Flightline's domain. Rules that depend on subjective design or content judgments (Guideline 4.0 Design, metadata keyword stuffing, content quality) are not checkable via API. No rule in this set tries to second-guess a human reviewer.

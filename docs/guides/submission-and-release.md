# Submission and release

Production submission, beta review, manual release, and phased rollout are separate actions. State apply prepares supported configuration; production submission uses an explicit plan, assembly, and final-submit workflow.

These commands describe the current source tree. Check your installed binary's help before using them. Local tests do not establish controlled live qualification: IAP-version attachment and phased-release writes still need that qualification.

## 1. Inspect the version and readiness

Use an existing version and replace the example coordinates throughout:

```bash
flightline versions get com.example.app --version 1.0 --platform IOS
flightline preflight com.example.app --version 1.0 --platform IOS
flightline review-submissions list com.example.app
```

Resolve blocking diagnostics before continuing. Preflight checks supported rules and observed state; it cannot promise Apple acceptance or review approval. If you manage YAML, first [apply and verify that intent](./state-as-code.md).

## 2. Plan exact submission membership

For an app-version-only submission:

```bash
flightline submission-assembly plan com.example.app --version 1.0 --platform IOS
```

Plan resolves the selected version and validates proposals without writing. It is not the full live preflight run. Optional repeatable flags select additional members:

| Flag | Required identity |
|---|---|
| `--iap-version` | An IAP **version resource ID**, not a product ID or parent IAP ID |
| `--event` | An in-app event resource ID |
| `--experiment` | An App Store version experiment resource ID |

The command does not automatically attach every eligible purchase, event, or experiment. Obtain the exact resource IDs before selecting them. For IAP versions, use Apple's version relationship for the parent IAP; Flightline's parent-product ID is not a substitute. See [IAP commerce](./iap-commerce.md), [events](./events.md), and [experiments](./experiments.md) for their preparation boundaries.

## 3. Assemble a draft

Repeat the same version and optional item flags, adding confirmation:

```bash
flightline submission-assembly assemble com.example.app --version 1.0 --platform IOS --confirm
```

Assembly creates or reuses an eligible `READY_FOR_REVIEW` draft, attaches the app version before optional items, verifies exact membership, and runs fresh preflight. Ambiguous drafts, foreign resources, extra members, and unsupported states stop the workflow instead of silently changing the selection.

Record the returned `submissionId`. Assembly is not final submission. It is also not transactional: a preflight or later attachment failure can leave an assembled or partially assembled draft. Inspect it before retrying:

```bash
flightline review-submissions items com.example.app --submission SUBMISSION_ID
```

Do not infer that a failed command rolled back earlier writes. Resolve the reported mismatch or readiness issue, then repeat the intended assembly. Do not select a different draft merely to bypass a failure.

## 4. Submit explicitly

After reviewing the draft, repeat the exact planned membership and identify the assembled submission:

```bash
flightline submission-assembly submit com.example.app --version 1.0 --platform IOS --submission SUBMISSION_ID --confirm
```

If assembly used optional item flags, include those same flags here. Final submit revalidates ownership, eligibility, exact membership, and fresh preflight, then checks draft membership again before submitting. Unknown or drifting observations block the write.

Successful submission confirms review progression to `WAITING_FOR_REVIEW` or `IN_REVIEW`; it does not mean approval. After a timeout or uncertain outcome, inspect the submission and version before retrying. For rejection details available through the API:

```bash
flightline rejection com.example.app --version 1.0
```

Resolution-center correspondence remains a portal workflow.

## 5. Release after approval

For a version configured for `MANUAL` release and freshly observed in `PENDING_DEVELOPER_RELEASE`:

```bash
flightline version-release com.example.app --version 1.0 --platform IOS --confirm
flightline versions get com.example.app --version 1.0 --platform IOS
```

An accepted release request is not proof that storefront distribution has completed. Inspect the version after an uncertain result before retrying. This action is separate from apply and App Review submission.

## Phased rollout

Inspect phased state first:

```bash
flightline phased-release get com.example.app --version 1.0 --platform IOS
```

`phased-release enable --confirm` creates an inactive phased release for an eligible prerelease update. Apple determines eligibility, including release history. After distribution begins, `pause --confirm` requires an active phased release and `resume --confirm` requires a paused one; both require the version to be freshly observed in `READY_FOR_DISTRIBUTION`.

```bash
flightline phased-release pause com.example.app --version 1.0 --platform IOS --confirm
flightline phased-release get com.example.app --version 1.0 --platform IOS
```

Use only the transition appropriate to the observed state. Flightline does not expose phased-release deletion or an immediate complete-rollout action. Selected phased intent is also modeled in state YAML, but that does not relax version editability for other changes. Live phased-write qualification remains pending.

For beta distribution instead, use the separate [TestFlight workflow](./testflight.md).

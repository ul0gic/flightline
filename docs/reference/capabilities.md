# Capabilities and workflow boundaries

Flightline provides direct App Store Connect commands, selected desired-state workflows, and preflight checks. Support in one layer does not imply support in every layer or coverage of every Apple endpoint.

This reference describes current source. Verify command availability with your installed binary's `--help`; a published release may lag these docs. The [CLI index](./cli.md) lists command groups, and nested help provides their current flags and arguments.

| Workflow | Direct commands | Desired state | Guide |
|---|---|---|---|
| Version metadata, build attachment, reviewer details | Supported selected operations | Supported selected fields | [State as code](../guides/state-as-code.md) |
| Main and custom-page screenshots and previews | Upload and inspection operations | Supported asset intent | [Assets](../guides/uploading-assets.md) |
| Review attachments | Explicit upload and management | No general attachment authoring | [Assets](../guides/uploading-assets.md) |
| IAP products and commerce | Selected product, pricing, availability, offer, image, and purchase actions | Selected product, price, availability, and review-screenshot intent | [IAP commerce](../guides/iap-commerce.md) |
| TestFlight | Metadata, groups, builds, testers, review, and selected distribution actions | Selected metadata and opt-in group build membership | [TestFlight](../guides/testflight.md) |
| In-app events | Events, localizations, media, and submission proposals | No | [Events](../guides/events.md) |
| Product-page experiments | Experiments, treatments, assets, and submission proposals | No | [Experiments](../guides/experiments.md) |
| Rights, EULA, accessibility, tags | Selected declarations and explicit publication/visibility actions | Rights, EULA, accessibility draft intent | [Declarations](../guides/declarations.md) |
| Customer review responses and webhooks | Explicit app-scoped management and delivery actions | No | [Reviews and webhooks](../guides/review-responses-and-webhooks.md) |
| Production submission and manual release | Explicit assembly, final submission, and release | No final submission or manual release | [Submission and release](../guides/submission-and-release.md) |
| Phased rollout | Selected enable, pause, and resume operations | Selected transitions | [Submission and release](../guides/submission-and-release.md) |
| Reports and operational inspection | Sales, finance, reviews, analytics, crashes, and metrics | No | [Observability](../guides/observability.md) |

## Qualification and deliberate limits

Local fixture tests validate request contracts and safety checks; they are not evidence that each write has been exercised on a live account. Controlled IAP-version submission attachment and phased-release writes still require live qualification.

App availability support is partial. Price discovery and availability reads are available; selected changes to observed active preorder territories are supported. Ordinary released-territory authoring and preorder creation, enabling, or publication are not generally qualified workflows. Preorder release is a separate explicit action.

Subscription inspection does not imply subscription authoring. Game Center authoring and building, signing, or uploading application binaries are outside the supported authoring workflows. Privacy nutrition labels and resolution-center reviewer correspondence remain portal workflows under the pinned API reference.

Preflight has [15 registered rules](./preflight-rules.md). It detects supported schema, consistency, and readiness problems; it cannot infer subjective review policy, inspect every in-app path, or guarantee approval. Missing or ambiguous observations must not be treated as successful verification.

See [API compatibility](./api-compatibility.md) for the pinned Apple reference and update policy, and [the three-layer model](../concepts/three-layer-model.md) for how commands, state, and preflight interact.

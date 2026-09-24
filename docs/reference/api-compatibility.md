# App Store Connect API compatibility

Flightline uses a hand-written client against the committed `openapi.oas.json`. Endpoint presence in that reference does not imply Flightline implements the endpoint or has qualified it against a live account. The README capability matrix distinguishes direct commands, desired-state support and preflight checks.

## Pinned reference

- Apple API version: 4.5, imported 2026-09-23.
- Source: [Apple OpenAPI download](https://developer.apple.com/sample-code/app-store-connect/app-store-connect-openapi-specification.zip).
- JSON SHA-256: `1e8ef250d6a41bab0f5670b669abf3f06298ef129c8455ba258149fda86927b2`.
- Inventory: 973 path keys, 1,407 schemas; the previous 4.3 reference had 923 paths and 1,337 schemas.
- The refresh added 50 path keys and 62 operations. Path counts alone do not establish operation or schema compatibility.

Current IAP V2 and localization endpoints remain supported by the pinned reference. Version-owned IAP resources and their review-submission relationship are additive; Flightline does not migrate existing resources automatically. Subscription authoring remains deferred. Age-rating state uses current frequency enums and modeled regional/V2 fields; the legacy override remains available only as deprecated direct-command compatibility.

Privacy nutrition labels and resolution-center reviewer text have no corresponding surface in the pinned reference. They remain portal workflows. Game Center authoring, subscription writes and build/sign/upload are outside the selected correction scope.

## Reviewing an update

1. Save Apple's download separately and record download date, version and JSON checksum before changing the pinned file.
2. Compare method plus path pairs, operation parameters, required fields, enums, relationship names and types, response shapes, and deprecation markers. Review changed existing operations as well as additions.
3. Map differences to implemented command, state, upload, checkpoint and preflight contracts. Decide explicitly whether each change needs a correction, a new supported workflow, or a documented deferral.
4. Update typed adapters and focused fake-server regressions together. Verify complete pagination, omission semantics, resource ownership, processing completion and unknown write outcomes where affected.
5. Regenerate CLI/rule references, compare both state-schema copies, and run the project gate using its pinned Go toolchain. Keep local fixture evidence separate from controlled live qualification.

Never enable a write merely because its endpoint exists. Publication, release, review submission, notifications and similar external actions require explicit commands and their documented confirmation boundary.

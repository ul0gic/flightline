# TestFlight metadata and distribution

Flightline can inspect TestFlight groups, testers, beta review, metadata, build assignment, and notification policy. These commands are in the current source tree; check your **local binary** with `flightline testflight --help` before using this guide. An installed `@latest` binary may not include them yet. The source includes local fixture tests for these workflows; live App Store Connect behavior is not verified here.

Examples use the synthetic bundle ID `com.example.app`. Supply an ASC API key with access to the app. Build numbers are `CFBundleVersion` values; when a number may occur in multiple release trains, pass `--version` and `--platform` to select the intended build.

## Inspect groups, testers, and beta review

```sh
flightline testflight groups list com.example.app --output json
flightline testflight testers list com.example.app
flightline testflight testers list com.example.app --group GROUP_ID
flightline testflight beta-review get com.example.app --build 42 \
  --version 1.2 --platform IOS
```

`groups list` includes internal and external groups. The group-scoped tester read accepts a group ID; confirm that ID in the app's group list first. A beta-review read is scoped to a build. No submission is a valid state, shown as such; a submitted build may move through Apple's review states later. `beta-review submit` creates a one-shot submission for a specific build and returns the existing submission with `changed=false` on a repeat:

```sh
flightline testflight beta-review submit com.example.app --build 42 \
  --version 1.2 --platform IOS
flightline testflight beta-review get com.example.app --build 42 \
  --version 1.2 --platform IOS
```

Submission is a direct command, not a state YAML field. Resolve ambiguous build numbers with `--version`; `--platform` defaults to `IOS` for this command.

## Maintain groups and membership

```sh
flightline testflight groups create com.example.app --name "External Beta" \
  --public-link --public-link-limit 1000
flightline testflight groups list com.example.app
flightline testflight groups update GROUP_ID --feedback=true
```

Create defaults to an external group; add `--internal` when creating an internal group. Public-link settings apply to external groups; Apple may ignore them for internal groups. Create matches an existing group by exact name and returns `changed=false` without updating its settings. Update reads the group first and sends only explicitly supplied fields that differ; use `--public-link=false`, `--feedback=false`, or a new `--public-link-limit` to change those values. Group kind cannot be changed after creation. `groups delete <GROUP_ID>` deletes a group and reports `changed=false` if it is already absent; inspect its membership and build assignments first.

The direct tester commands use **tester IDs**, not email addresses:

```sh
flightline testflight testers list com.example.app --output json
flightline testflight testers add GROUP_ID --tester TESTER_ID
flightline testflight testers list com.example.app --group GROUP_ID
```

`testers add` filters existing members before writing. `testers remove <GROUP_ID> --tester <TESTER_ID>` removes only current members and reports a no-op for already absent IDs. State YAML can reconcile testers by email (below). Neither direct command is an invitation-resend operation.

## Edit beta metadata

Read the current app copy, build notes, and reviewer details:

```sh
flightline testflight metadata app-localizations com.example.app
flightline testflight metadata build-localizations com.example.app \
  --build 42 --version 1.2 --platform IOS
flightline testflight metadata review-details com.example.app
```

Set only the fields you intend to change; omitted flags leave existing values in place. For app copy, available fields include `--description`, `--feedback-email`, `--marketing-url`, `--privacy-policy-url`, and `--tvos-privacy-policy`:

```sh
flightline testflight metadata set-app-localization com.example.app \
  --locale en-US --description "Try the next release"
flightline testflight metadata set-build-localization com.example.app \
  --build 42 --version 1.2 --platform IOS --locale en-US \
  --whats-new "Exercise the new checkout flow"
```

Reviewer detail updates require an existing beta review detail in ASC; create it there first if the read shows none. Contact fields, demo account name, notes, and `--demo-account-required=true|false` are supported. Pass a demo password through `--password-ref env:NAME` or `--password-file PATH`, never as a command argument or YAML value. The password is not emitted in command output. Read reviewer details again after writing; password values are intentionally not shown.

## Assign builds with notification policy in view

```sh
flightline testflight distribution list com.example.app --group GROUP_ID
flightline testflight recruitment policy get com.example.app \
  --build 42 --version 1.2 --platform IOS
```

Build assignment checks that the group belongs to the app and reads the build's auto-notify setting. If it is false, add the build directly. If true, disable it first or explicitly accept potential notifications:

```sh
flightline testflight recruitment policy set com.example.app \
  --build 42 --version 1.2 --platform IOS --enabled=false
flightline testflight distribution add com.example.app \
  --group GROUP_ID --build 42 --version 1.2 --platform IOS
flightline testflight distribution list com.example.app --group GROUP_ID
```

Enabling auto-notify with `policy set ... --enabled=true` requires `--confirm`. When auto-notify is already enabled, `distribution add` requires both `--allow-notifications` and `--confirm`; an unknown auto-notify state blocks assignment until it is explicitly set in ASC. Re-adding an assigned build reports `changed=false`. `distribution remove` takes the same group/build selectors and removes an existing assignment without a confirmation flag; inspect the group again afterward.

`flightline testflight recruitment notify com.example.app --build 42 --version 1.2 --platform IOS --confirm` sends a one-shot tester notification. Inspect the target build and group before invoking it; do not use it as a retry-safe reconciliation step. `recruitment criteria <bundleId> --group <GROUP_ID>` and `recruitment criteria-options` are read-only. Recruitment criteria writes and individual invitation resend are not implemented.

## Manage repeatable state in YAML

Fetch live state, then edit a narrow TestFlight section in the fetched file. The excerpt below illustrates managed fields; keep the rest of the fetched state intact:

```yaml
spec:
  testflight:
    metadata:
      appLocalizations:
        en-US:
          description: Try the next release
      builds:
        - build: {number: "42", version: "1.2", platform: IOS}
          localizations:
            en-US: {whatsNew: Exercise the new checkout flow}
    groups:
      external-beta:
        isInternal: false
        publicLink: true
        publicLinkLimit: 1000
        builds:
          - {number: "42", version: "1.2", platform: IOS}
        testers:
          - email: tester@example.com
```

```sh
flightline fetch com.example.app -o state.yaml
# Edit the intended TestFlight fields in state.yaml.
flightline plan state.yaml
flightline apply state.yaml --confirm
flightline plan state.yaml
```

Omitted TestFlight sections are unmanaged. An explicit `groups.<name>.builds` list reconciles the complete assigned build set for that group; an empty list removes all assignments. An explicit tester list likewise controls group membership by email, including removals. Each build selector needs number, version, and platform. Review the plan carefully for removals. State apply requires auto-notify to be disabled for every build it adds to a group; an enabled or unknown setting blocks that assignment. The state path covers app/build localizations, existing beta-review detail fields, group settings, testers, and group build assignments; it does not submit beta review, issue tester notifications, set build auto-notify policy, or write recruitment criteria. Keep reviewer passwords in the direct metadata command's secret reference path rather than state YAML.

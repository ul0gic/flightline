# Declarations and visibility

Flightline records explicit answers for content rights, a custom EULA, and accessibility support. It does not determine legal rights or accessibility facts from metadata or binaries. These workflows reflect current source and locally tested behavior; check the installed version's `--help` before use. Identifiers below are synthetic.

## Content rights

```bash
flightline content-rights get com.example.myapp
flightline content-rights set com.example.myapp USES_THIRD_PARTY_CONTENT --confirm
```

Choose either `USES_THIRD_PARTY_CONTENT` or `DOES_NOT_USE_THIRD_PARTY_CONTENT` based on your own verified rights assessment. The command rereads the declaration and confirms the write; an unchanged answer is a no-op. If the outcome is unconfirmed, use `get` before retrying. In state YAML, `spec.contentRights` carries the same explicit declaration. Omission leaves it unmanaged.

## Custom EULA

```bash
flightline app-eula get com.example.myapp
flightline app-eula set com.example.myapp \
  --text-file ./legal/eula.txt --territory USA --territory CAN --confirm
flightline app-eula delete com.example.myapp --confirm
```

The agreement file must contain nonempty text. The repeated `--territory` values form the **complete territory set** for this command; use every intended territory. Set rereads and verifies the resulting agreement. Delete explicitly removes the custom EULA and verifies absence. Inspect with `get` if a write reports an unconfirmed outcome.

In state YAML, `spec.appEula.agreementText` and `spec.appEula.territories` can reconcile an agreement. Creation requires both nonempty text and territories; on an existing agreement, omitted fields preserve their observed values. Omitting `spec.appEula` does not remove the agreement. Removal is the confirmed CLI delete action.

## Accessibility declarations

```bash
flightline accessibility-declarations list com.example.myapp
flightline accessibility-declarations create com.example.myapp \
  --device-family IPHONE --supports-voiceover=true --supports-captions=false --confirm
flightline accessibility-declarations get DECLARATION_ID
flightline accessibility-declarations update DECLARATION_ID \
  --supports-reduced-motion=true --confirm
flightline accessibility-declarations publish DECLARATION_ID --confirm
```

Supported families are `IPHONE`, `IPAD`, `APPLE_TV`, `APPLE_WATCH`, `MAC`, and `VISION`. At least one support flag must be explicitly set for create or update; an omitted flag is not an answer, while `false` is an explicit answer. Use `flightline accessibility-declarations create --help` for the nine supported answer flags. Create makes a draft. Update and delete require a draft, and publish is a separate confirmed action with an immediate publication effect. A published declaration cannot be edited in place; create a new draft deliberately if answers must change. To discard a draft, use `flightline accessibility-declarations delete DECLARATION_ID --confirm`.

State YAML uses `spec.accessibilityDeclarations.families.<family>` for draft creation and draft answer updates. Fetch selects the draft if present, otherwise the published declaration. `state` is observed, not writable; applying state never publishes or deletes. A planned change to published answers fails until a draft has been explicitly created. The [state YAML reference](../reference/state-yaml.md#accessibility-declarations) has the field list and lifecycle rules.

## App Store tag visibility

```bash
flightline app-tags list com.example.myapp
flightline app-tags set-visibility com.example.myapp TAG_ID \
  --visible-in-app-store=true --confirm
```

The tag must already be assigned to the selected app. Visibility requires an explicit `true` or `false`; the command checks current membership and skips an unchanged value. This is an explicit CLI action, separate from state reconciliation.

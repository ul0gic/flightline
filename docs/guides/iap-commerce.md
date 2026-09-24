# Non-subscription IAP commerce

Flightline can inspect and change pricing, availability, offer codes, and App Store promotions for non-subscription in-app purchases (IAPs). These commands are in the current source tree; before following this guide, check that your **local binary** exposes them with `flightline iap --help`. An installed `@latest` binary may not include this work yet. The source includes local fixture tests for these workflows; live App Store Connect behavior is not verified here.

Use an App Store Connect API key with access to the app. Examples use the synthetic bundle ID `com.example.app` and product ID `com.example.app.lifetime`; replace them with your own values. See [API key setup](../getting-started/apple-api-key.md) for credentials and [State as Code](state-as-code.md) for the fetch/plan/apply workflow. All direct commerce and offer-code commands select the IAP by `--product`, rather than an ASC resource ID.

## Inspect pricing before changing it

```sh
flightline iap get com.example.app --product com.example.app.lifetime
flightline iap commerce price-points com.example.app --product com.example.app.lifetime --territory USA
flightline iap commerce pricing com.example.app --product com.example.app.lifetime --output json
```

The price-point read gives ASC price-point IDs, customer prices, and proceeds. Use an ID from the intended **base territory**; do not pass a displayed currency amount as `--price-point`. `pricing` reads the complete manual price schedule, including dated windows. Preserve a copy of that view before making a change.

To set the current base price directly:

```sh
flightline iap commerce set-price com.example.app \
  --product com.example.app.lifetime --base-territory USA \
  --price-point PRICE_POINT_ID --confirm
flightline iap commerce pricing com.example.app --product com.example.app.lifetime
```

The command reads the current schedule and creates a replacement schedule that preserves manual windows. It checks the previously observed active base price before writing; if ASC changed concurrently, inspect and retry from fresh state. It is a current-base-price operation, not an editor for scheduled manual windows. `--confirm` is required for the direct write.

## Set territory availability

```sh
flightline iap commerce availability com.example.app --product com.example.app.lifetime --output json
flightline iap commerce set-availability com.example.app \
  --product com.example.app.lifetime --new-territories=false \
  --territory USA --territory CAN --confirm
flightline iap commerce availability com.example.app --product com.example.app.lifetime
```

Repeated `--territory` values express the **complete available territory set**, not additions. Use `--clear-territories` instead of `--territory` to set an empty list. The two options cannot be combined. `--new-territories=true|false` controls whether newly added ASC territories become available. After initial availability exists, you may set only that boolean or only the complete territory set; Flightline retains the other value from its verified read. Initial availability requires both values explicitly. Duplicate or empty territory IDs are rejected.

## Manage the same fields in state YAML

Fetch first, then edit the relevant product in the fetched file. This is an excerpt, not a replacement for the rest of your app state:

```yaml
spec:
  iap:
    products:
      com.example.app.lifetime:
        commerce:
          pricing:
            baseTerritory: USA
            pricePointId: PRICE_POINT_ID
          availability:
            availableInNewTerritories: false
            availableTerritories: [USA, CAN]
```

```sh
flightline fetch com.example.app -o state.yaml
# Edit only the intended product and commerce fields in state.yaml.
flightline plan state.yaml
flightline apply state.yaml --confirm
flightline plan state.yaml
```

An omitted `commerce`, `pricing`, or `availability` block leaves that area unmanaged. An omitted availability field can inherit verified live state; creating initial availability still requires both fields. An explicit `availableTerritories: []` clears the set. The state planner covers the current base price and territory availability. It does not manage manual price windows, offer-code issuance, promotional-image uploads, or promoted-purchase order. Those use direct commands.

## Issue and retrieve offer codes

Inspect offer definitions and their prices and batches before issuing codes:

```sh
flightline iap offer-codes list com.example.app --product com.example.app.lifetime
flightline iap offer-codes get com.example.app --product com.example.app.lifetime \
  --offer OFFER_ID --output json
```

Create a definition using territory-specific price-point IDs and one or more customer eligibility values (`NON_SPENDER`, `ACTIVE_SPENDER`, or `CHURNED_SPENDER`):

```sh
flightline iap offer-codes create-definition com.example.app \
  --product com.example.app.lifetime --name "Launch offer" \
  --eligibility NON_SPENDER --price USA=PRICE_POINT_ID --confirm
```

Definitions and batches are deliberate issuance operations; do not treat them as state reconciliation. Inspect the returned ID, then create the needed batch:

```sh
flightline iap offer-codes create-one-time com.example.app \
  --product com.example.app.lifetime --offer OFFER_ID \
  --count 100 --expires 2099-12-31 --environment SANDBOX --confirm
flightline iap offer-codes get com.example.app --product com.example.app.lifetime \
  --offer OFFER_ID --output json
flightline iap offer-codes download com.example.app \
  --product com.example.app.lifetime --offer OFFER_ID \
  --batch ONE_TIME_BATCH_ID --file private-codes.csv --confirm
```

`download` writes a **new private CSV** and refuses to overwrite an existing file. Keep it outside source control. For a custom-code batch, put exactly one nonempty code in a private regular file (mode `0600`), then use `create-custom` with `--offer`, `--code-file`, `--count`, optional `--expires YYYY-MM-DD`, and `--confirm`. The custom code is read from the file, not supplied on the command line. Offer definitions, custom batches, one-time batches, and downloads each require `--confirm`; review the offer and batch before retrying an ambiguous request.

## Promotional images and promoted purchases

```sh
flightline iap promotion images list com.example.app com.example.app.lifetime
flightline iap promotion images upload com.example.app com.example.app.lifetime \
  ./promotion.png --confirm
flightline iap promotion images list com.example.app com.example.app.lifetime
```

Upload reserves an ASC asset, sends the file, and polls processing. If processing remains pending, use `images wait <bundleId> <productId> <imageId>`; after an interrupted upload, inspect the image list and retry with `images upload ... --resume --confirm` only when the matching checkpoint exists. Flightline checks filename, checksum, state, and checkpoint before reusing an asset. A different file with the same name requires an explicit `images delete <bundleId> <productId> <imageId> --confirm` after inspection.

```sh
flightline iap promotion promoted list com.example.app --output json
flightline iap promotion promoted set com.example.app com.example.app.lifetime \
  --visible-for-all-users=true --enabled=true --confirm
flightline iap promotion promoted list com.example.app
```

`set` requires an explicit `--visible-for-all-users` value and `--confirm`; `--enabled` is optional and changes only when supplied. `promoted delete <bundleId> <productId> --confirm` removes a promotion. `promoted order <bundleId> --id <PROMOTION_ID> ... --confirm` replaces the **complete app-wide order**: list every current promoted-purchase ID in the intended sequence, including unrelated products. Inspect the list again after any write. If a write reports an unconfirmed outcome, read ASC before retrying; a request may have succeeded despite a response failure.

These commands manage non-subscription IAPs. Auto-renewable subscriptions use the separate `subscriptions` command family.

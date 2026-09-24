# Review responses and webhooks

These are explicit CLI workflows. They are not managed by an AppState file. The commands below reflect the current source and locally tested behavior; check `flightline ... --help` in your installed version before using them. Identifiers and endpoints here are synthetic.

## Respond to a customer review

Find the review ID with `flightline reviews list com.example.myapp`, then inspect the review and any existing response:

```bash
flightline reviews get REVIEW_ID
flightline reviews responses get com.example.myapp --review REVIEW_ID
```

Submit the exact authored text from a file, or use `--body` instead of `--body-file`:

```bash
flightline reviews responses create com.example.myapp \
  --review REVIEW_ID --body-file ./response.txt --confirm
```

The file must be regular and the response must contain non-whitespace text. Flightline verifies that the review belongs to the selected app. An identical existing response is skipped; a different response blocks creation. To replace it, inspect its ID, explicitly delete that response, then create the new one:

```bash
flightline reviews responses delete com.example.myapp \
  --review REVIEW_ID --response RESPONSE_ID --confirm
```

Deletion checks the current response ID against `--response`; an absent response is a no-op. A create or delete error can leave the remote outcome uncertain. Read the response again before retrying. Creation may return a pending publication state; do not treat submission as proof that Apple has published it.

## Configure a webhook

```bash
flightline webhooks list com.example.myapp
flightline webhooks create com.example.myapp \
  --name release-updates --url https://hooks.example.invalid/asc \
  --event APP_STORE_VERSION_APP_VERSION_STATE_UPDATED \
  --enabled=true --secret-ref env:ASC_WEBHOOK_SECRET --confirm
flightline webhooks get com.example.myapp WEBHOOK_ID
```

Creation requires a name, an HTTPS URL, at least one supported `--event`, an explicit `--enabled=true` or `--enabled=false`, exactly one secret source, and `--confirm`. Repeat `--event` for multiple distinct event types. The URL cannot contain credentials, a query, or a fragment. `--secret-ref env:NAME` reads the environment variable; `--secret-file` reads a private regular file as an alternative. Keep the signing secret outside command arguments and state files. Flightline does not return the secret; read output shows only the endpoint's scheme and host.

Updates specify only fields to change. Pass a secret source to rotate the secret; its value cannot be read back for comparison. A configuration-only no-op is reported without a write.

```bash
flightline webhooks update com.example.myapp WEBHOOK_ID --enabled=false --confirm
flightline webhooks update com.example.myapp WEBHOOK_ID \
  --secret-ref env:ASC_WEBHOOK_NEW_SECRET --confirm
flightline webhooks delete com.example.myapp WEBHOOK_ID --confirm
```

Flightline checks that each selected webhook belongs to the app. After a failed or unconfirmed mutation, inspect `webhooks list` before retrying; repeating create or secret rotation blindly can cause another change.

## Inspect and retry deliveries

```bash
flightline webhooks ping com.example.myapp WEBHOOK_ID --confirm
flightline webhooks deliveries list com.example.myapp WEBHOOK_ID
flightline webhooks deliveries redeliver com.example.myapp WEBHOOK_ID DELIVERY_ID --confirm
```

Ping and redelivery are outbound actions. Redelivery requires an existing delivery owned by the selected webhook and creates a new delivery attempt. A ping ID or new delivery ID records the request; inspect delivery state separately to determine the outcome.

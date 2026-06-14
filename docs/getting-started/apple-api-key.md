# App Store Connect API key

Flightline talks to App Store Connect using an API key you generate in the developer portal, not your Apple ID. The key is a `.p8` private key that Flightline signs an ES256 JWT with on every request. This is a one-time setup.

## 1. Why a key

Flightline never sees your Apple ID password. It authenticates with an App Store Connect API key: a 10-character Key ID, an Issuer ID (a UUID for your team), and a `.p8` private key file. Flightline mints a short-lived ES256 JWT per request and signs it with the `.p8`. Nothing is cached and nothing is sent to any server but Apple's.

## 2. Generate the key

In a browser, go to App Store Connect, then **Users and Access > Integrations > App Store Connect API** (direct link: https://appstoreconnect.apple.com/access/integrations/api), and:

1. Click **+** to create a new key.
2. Name it (for example, `flightline`).
3. Grant the role **App Manager**. Use **Admin** instead only if you also need finance reports.
4. Click **Generate**.

The App Manager role covers all of Flightline's authoring and most observation surfaces. Finance reports require Admin.

## 3. Download the .p8 (once)

Click **Download API Key**. You can only download the `.p8` file once. If you lose it, you have to revoke the key and generate a new one.

While you are on this screen, note two values:

- The **Key ID**, 10 characters (for example, `ABCD1234EF`).
- The **Issuer ID**, a UUID shown at the top of the Integrations page (for example, `12345678-90ab-cdef-1234-567890abcdef`).

## 4. Place the file and lock it down

Move the downloaded key into `~/.appstoreconnect/` and set its permissions to `600`:

```bash
mkdir -p ~/.appstoreconnect
mv ~/Downloads/AuthKey_ABCD1234EF.p8 ~/.appstoreconnect/
chmod 600 ~/.appstoreconnect/AuthKey_ABCD1234EF.p8
```

Replace `ABCD1234EF` with your actual Key ID. Flightline expects the file at `~/.appstoreconnect/AuthKey_<KEY_ID>.p8` and refuses to load a `.p8` whose permissions are wider than `600`, printing the exact `chmod` command to fix it. Never copy the `.p8` into a project directory or anywhere it could be committed to git.

## 5. Export the credentials

Add these to your shell profile (`~/.zshrc` or `~/.bashrc`):

```bash
export APP_STORE_CONNECT_KEY_ID="ABCD1234EF"
export APP_STORE_CONNECT_ISSUER_ID="12345678-90ab-cdef-1234-567890abcdef"
export APP_STORE_CONNECT_VENDOR_NUMBER="12345678"
```

The vendor number is required for sales and finance reports only; the other two are required for everything. Reload your shell:

```bash
source ~/.zshrc
```

These three environment variables are the blessed source for credential metadata. You can also pass `--key-id` and `--issuer-id` as flags on any command (flags take precedence over environment variables).

## 6. Verify

```bash
flightline whoami
```

Expected output:

```
FIELD          VALUE
KEY_ID         ABCD1234EF
ISSUER_ID      12345678-90ab-cdef-1234-567890abcdef
VENDOR_NUMBER  12345678
AUTHORIZED     true
API_BASE_URL   https://api.appstoreconnect.apple.com
```

If `AUTHORIZED` is `true`, you are done.

## 7. Troubleshooting

If `whoami` errors, the message names the problem: a missing environment variable, a `.p8` not found, wrong permissions, or an invalid key. The redacted error includes a hint pointing at the exact fix.

**`asc: HTTP 401 Unauthorized`**

Your credentials are wrong or expired. Confirm the environment variables are set and the key file exists:

```bash
echo $APP_STORE_CONNECT_KEY_ID
echo $APP_STORE_CONNECT_ISSUER_ID
ls ~/.appstoreconnect/AuthKey_${APP_STORE_CONNECT_KEY_ID}.p8
```

JWTs are minted per request and expire after 20 minutes; there is no token cache to invalidate.

**`asc: HTTP 403 Forbidden`**

Your key exists but lacks permission for this operation. Check the role on your API key in App Store Connect (Users and Access > Integrations). App Manager covers most operations; finance reports need Admin.

**`config: chmod 600 ~/.appstoreconnect/AuthKey_<id>.p8`**

Your `.p8` file mode is too wide (group- or world-readable). Fix it:

```bash
chmod 600 ~/.appstoreconnect/AuthKey_${APP_STORE_CONNECT_KEY_ID}.p8
```

Flightline refuses to load a key file with permissions wider than `600`.

## Next steps

[Run your first commands](./first-run.md) to confirm the credential works end to end.

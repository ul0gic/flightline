# Observability: reports, reviews, and metrics from the terminal

The observation half of Flightline reads everything App Store Connect knows about your app: sales, finance, subscriptions, customer reviews, analytics, TestFlight feedback, crash diagnostics, and performance metrics. Every command here is read-only, supports `--output json`, and is designed to be piped. The examples use Tideterm (`app.tideterm.ios`); substitute your own bundle ID.

## Prerequisites

- Credentials configured per [the API key guide](../getting-started/apple-api-key.md).
- `APP_STORE_CONNECT_VENDOR_NUMBER` exported for sales, finance, and subscription reports (find it in App Store Connect under Payments and Financial Reports). The commands refuse to run without it rather than failing on the wire.
- Finance reports require a key with the **Admin** role. Everything else works with **App Manager**.

## Sales reports

Pull Sales and Trends data from `/v1/salesReports`:

```bash
flightline sales app.tideterm.ios --days 30
flightline sales app.tideterm.ios --week 2026-06-14
flightline sales app.tideterm.ios --month 2026-05
flightline sales app.tideterm.ios --year 2025
```

`--days`, `--week`, `--month`, and `--year` are mutually exclusive; with no date flag you get the last 7 days. The table view is a per-day summary:

```
DATE        UNITS  PROCEEDS  CURRENCY
2026-06-29  14     27.86     USD
2026-06-30  9      17.91     USD
2026-07-01  22     43.78     USD
```

Two things to know about how Apple serves this data:

- **Reports are vendor-wide.** Apple does not filter by app on the wire; the bundle ID argument scopes the typed output client-side so a multi-app account stays focused.
- **Daily windows cost one API call per day.** A `--days 30` pull is 30 calls against Apple's 500 requests/hour cap. Data lags about one day, so windows end yesterday.

`--report-type` selects the report (default `SALES`; also `SUBSCRIPTION`, `SUBSCRIPTION_EVENT`, `SUBSCRIBER`, `INSTALLS`), and `--report-sub-type` the granularity (default `SUMMARY`). For raw data, `--output tsv` streams Apple's gunzipped wire format unfiltered:

```bash
flightline sales app.tideterm.ios --days 1 --output tsv > today.tsv
```

## Finance reports

Settlement reports from `/v1/financeReports`. Finance data is monthly or yearly only (daily granularity belongs to `sales`), and each call is scoped to a region code:

```bash
flightline finance app.tideterm.ios --month 2026-05
flightline finance app.tideterm.ios --month 2026-05 --region US
flightline finance app.tideterm.ios --year 2025
```

Exactly one of `--month YYYY-MM` or `--year YYYY` is required. `--region` defaults to `Z1` (worldwide). The table summarizes by country and settlement currency:

```
COUNTRY  CURRENCY  QTY  PARTNER_SHARE  EXT_PARTNER_SHARE
DE       EUR       31   1.39           43.09
US       USD       204  1.39           283.56
```

`--report-type` accepts `FINANCIAL` (default) or `FINANCE_DETAIL`, and `--output tsv` passes through the raw report.

## Subscription reports

Time-series subscription data, distinct from `subscriptions list`/`get` (which read product configuration):

```bash
flightline subscriptions reports app.tideterm.ios --type summary --range P30D
flightline subscriptions reports app.tideterm.ios --type events --range P7D
flightline subscriptions reports app.tideterm.ios --type retention --month 2026-05
```

`--type` maps to Apple's report types: `summary` (active counts and proceeds), `events` (cancels, upgrades, downgrades), `retention` (subscriber-level rows). `--range` is an ISO-8601 duration ending yesterday (`P1D`, `P7D`, `P14D`, `P30D`, `P1M`, `P90D`, `P1Y`); `--month YYYY-MM` pulls a single monthly report instead. Like `sales`, daily ranges make one call per day and `--output tsv` is available.

## Customer reviews

```bash
flightline reviews list app.tideterm.ios
flightline reviews list app.tideterm.ios --rating 1..2 --territory USA
flightline reviews list app.tideterm.ios --since 30d --limit 50
flightline reviews get 6e2b9b14-1234-4567-8910-abcdef012345
flightline reviews summary app.tideterm.ios
```

Filters on `list`:

| Flag | Accepts |
|------|---------|
| `--rating` | A single rating (`1`) or a range (`1..3`) |
| `--territory` | ISO 3166-1 alpha-3 code (`USA`, `GBR`) |
| `--since` | A duration (`30d`, `12h`) or ISO date (`2026-06-01`) |
| `--limit` | Max reviews to emit (`0` = no cap) |

```
RATING  DATE        TERRITORY  TITLE                            ID
★★☆☆☆   2026-06-28  USA        Tide times wrong after update    a3f8c210-...
★★★★★   2026-06-27  GBR        Best tide app I have used        99d1e4b7-...
```

`get` includes your developer response when one exists. `summary` reads Apple's per-locale AI summarization of recent reviews; apps Apple has not generated a summary for get a typed `note` field instead of an error.

Triage one-star reviews with an LLM in a single pipe:

```bash
flightline reviews list app.tideterm.ios --rating 1 --since 30d --output json \
  | jq '[.reviews[].attributes | {title, body, territory}]' \
  | llm "Cluster these App Store reviews by root cause and rank by frequency"
```

## Analytics reports (async)

Analytics is Apple's only asynchronous report surface: you request a report, Apple generates it over minutes to hours, then you download the result. Flightline persists the request state to `$XDG_STATE_HOME/flightline/<bundleId>/analytics.json`, so every step is resumable; a Ctrl-C or dropped connection never loses the request.

### One-shot with --wait

```bash
flightline analytics request app.tideterm.ios --access-type ONE_TIME_SNAPSHOT --wait
```

`--wait` blocks until Apple's reports are available, then prints them. `--access-type` is required: `ONE_TIME_SNAPSHOT` for a point-in-time pull, or `ONGOING` for a standing request. Because `ONGOING` requests never auto-terminate, `--wait` with `ONGOING` requires `--max-duration` (for example `--max-duration 10m`) as an upper bound.

### Step by step

The same lifecycle as separate commands, useful in scripts and when the wait is long:

```bash
# 1. Submit; the request ID persists to the state file immediately
flightline analytics request app.tideterm.ios --access-type ONE_TIME_SNAPSHOT

# 2. Check on it any time later, from any shell
flightline analytics status app.tideterm.ios

# 3. Enumerate the report instances Apple produced
flightline analytics list-instances app.tideterm.ios --category APP_USAGE

# 4. Download every segment of an instance as CSV
flightline analytics download app.tideterm.ios --instance INST-42 --out ./reports/
```

`status` shows where the request stands:

```
FIELD                VALUE
BUNDLE_ID            app.tideterm.ios
STATE_FILE           /Users/dev/.local/state/flightline/app.tideterm.ios/analytics.json
REQUEST_ID           d5f0a9c2-...
STATUS               completed
SUBMITTED_AT         2026-07-02T14:03:11Z
LAST_POLL_AT         2026-07-02T14:19:47Z
REPORTS              12
DOWNLOADED_SEGMENTS  0
```

`list-instances` narrows with `--report-id`, `--category` (for example `APP_USAGE`, `COMMERCE`), or `--name-contains`. `download` requires `--instance` and writes one CSV per segment, named `<bundleId>-<instanceId>-segment<N>.csv`; `--out` takes a directory (created if missing) and refuses to clobber an existing file.

If a `--wait` is interrupted, nothing is lost: the request ID was persisted at submit time, so `status` and `list-instances` pick up where it left off.

## TestFlight beta feedback

Crash and screenshot feedback submitted by your testers:

```bash
flightline beta-feedback crash app.tideterm.ios
flightline beta-feedback screenshot app.tideterm.ios
flightline beta-feedback download CRASH-1234 --out crash.txt
flightline beta-feedback download SHOT-5678 --type screenshot
```

Both list commands take `--since` and `--limit`, plus `--build` to filter by build number (CFBundleVersion, the same value `diagnostics` and `performance` take). `download` fetches the crash log text or the first screenshot image for a submission; `--type` is `crash` (default) or `screenshot`, and `--out` names the destination file.

## Crash and hang diagnostics

Apple deduplicates crash and hang reports into signatures: same call stack, one signature, regardless of user count. The API scopes them to a build, so `--build` (the build number, for example `87`) is required:

```bash
flightline diagnostics list app.tideterm.ios --build 87
flightline diagnostics list app.tideterm.ios --build 87 --type HANGS
flightline diagnostics get DIAG-SIG-1234 --output json
```

`--type` filters by `DISK_WRITES`, `HANGS`, or `LAUNCHES`. `get` fetches the full log payload for one signature; the complete stack trace is only in the JSON output, the table summarizes.

## Performance metrics

The same battery, memory, hang, launch, and disk-write metrics as the Xcode Organizer Metrics tab:

```bash
flightline performance app app.tideterm.ios
flightline performance app app.tideterm.ios --category MEMORY
flightline performance build app.tideterm.ios --build 87
```

`app` is the cross-build aggregate; `build` is build-specific and requires `--build` (the build number). Filter with `--category` (`HANG`, `LAUNCH`, `MEMORY`, `DISK`, `BATTERY`, `TERMINATION`, `ANIMATION`) and `--device` (Apple model ID, for example `iPhone15,3`). Apple needs enough user telemetry before metrics appear, typically 7 to 30 days after release; until then the command returns a typed `note` rather than an error.

Watch for regressions Apple has flagged:

```bash
flightline performance app app.tideterm.ios --output json | jq '.insights.regressions'
```

## Composing it all

Every command emits stable JSON under `--output json` (the shape is a versioned contract; see [JSON output](../reference/json-output.md)), which makes the observation surface scriptable in three directions.

**Pipe to jq:**

```bash
flightline sales app.tideterm.ios --days 30 --output json \
  | jq '[.summary[].units] | add'

flightline reviews list app.tideterm.ios --rating 1..2 --output json \
  | jq -r '.reviews[].attributes.body'
```

**Feed to an LLM.** JSON output was designed as an LLM-ingestible surface: field names are self-describing and the envelope carries the request parameters, so a model gets full context from the payload alone.

```bash
flightline beta-feedback crash app.tideterm.ios --since 7d --output json \
  | llm "Summarize the crash patterns and suggest which build introduced them"
```

**Cron snapshots.** Because everything is a single binary call with env-var auth, scheduled snapshots are one crontab line each:

```cron
0 9 * * * flightline sales app.tideterm.ios --days 1 --output json >> ~/tideterm/sales.jsonl
0 9 * * 1 flightline reviews summary app.tideterm.ios --output json > ~/tideterm/review-summary.json
```

## See also

- [JSON output contract](../reference/json-output.md), the envelope shapes and stability guarantees
- [CLI reference](../reference/cli.md), every command group; `flightline <group> --help` for flags
- [The three-layer model](../concepts/three-layer-model.md), where observation fits in L1

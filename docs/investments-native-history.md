# Native historical market data

`marketdata` supports `--provider tinvest` (default), `moex`, and `twelvedata`.
Native workers process only reviewed `daily_candle` jobs. They do not search or
infer mappings, fetch broker accounts, place orders, or write operations/payouts.

Apply backend migrations first (schema 62 or newer). Explicitly enable the entitled
source (or use `--register-source` with a source title) after archive restore;
credentials and source entitlements are not granted by query references. Start workers with a tenant
UUID and entitlement scope, for example:

```sh
marketdata --provider moex --tenant "$TENANT_ID" --access-scope personal
marketdata --provider twelvedata --tenant "$TENANT_ID" --access-scope personal
```

MOEX uses public ISS. Twelve Data reads `TWELVEDATA_KEY_FILE`; on macOS, an empty
path uses Keychain service `budget.twelvedata.readonly` for the current OS user.
Never put the raw credential in argv, environment variables, or public files.
The request header carries the key; redirects are refused. Cooperating workers
share an irreversible credential fingerprint, an 8 second request gate and a
750 request UTC-day ceiling. Other clients can still exhaust the provider quota.
No subscription is purchased or activated. The conservative ceiling leaves headroom
below the [Basic plan quotas](https://support.twelvedata.com/en/articles/5615854-credits).

Canonical `HistoricalPeriods.NativeInstrument` stores the query identity:
MOEX security/ISIN/board or Twelve Data symbol/exchange/MIC. The worker verifies
that this identity reproduces its provider UUID. These references can rebuild
licensed catalog/cache after canonical archive restore. Native data never reuse
a broker UID or overwrite its current quote schedule.

MOEX covers shares/funds and bonds. Bond candles retain that session's nominal
and nominal currency. Twelve Data currently covers USD common shares/REITs on
NYSE/XNYS and NASDAQ/XNGS, plus NYSE/ARCX ETFs, explicitly requesting `adjust=none`. Daily labels use
the approved retrospective rule: a completed raw date becomes eligible on the
next UTC day. This is not verified calendar or publication-time evidence.

Open mapped positions refresh the recent 14 days every 6 hours. Open/closed history
hydrates through bounded 365 day jobs. Each native provider has at most 20 active
background jobs per tenant, across entitlement scopes; the existing 100 job tenant
limit remains. This leaves capacity for broker quotes. Terminal transient errors
can produce a new backfill job after 24 hours, retaining the old failure. Invalid
responses and permanent provider rejections require correction. A successful
empty response stays `empty_unexplained` and emits metric outcome `empty`, not
`ready`; it is not evidence of complete valuation.

`portfoliohistory` imports a private reviewed native price batch atomically:

```sh
portfoliohistory --tenant "$TENANT_ID" --actor "$ACTOR_ID" \
  --input reviewed.json --sha256 "$INPUT_SHA256" --prepare-output prepared.json
# Review the returned InputSHA256. Preparation rehearses and rolls back.
portfoliohistory --tenant "$TENANT_ID" --actor "$ACTOR_ID" \
  --input prepared.json --sha256 "$PREPARED_SHA256"
portfoliohistory --tenant "$TENANT_ID" --actor "$ACTOR_ID" \
  --input prepared.json --sha256 "$PREPARED_SHA256" --apply
```

Preparation freezes actual mapping versions in a new file with mode 0600. Replacing an
existing historical schedule requires explicit `--replace-history` during
preparation. Source/mapping changes commit together, preserving current quote
periods. A completed retry of the same prepared input is a no-op and cannot
rewind newer corrections. No journal, household, FX, or asset rows are imported.

Worker metrics use `moexhistory`/`equityhistory`; they do not advertise live quote
coverage. `scripts/investments/provision_refresh.py --native` adds only the missing
Twelve Data key through a hidden prompt and can verify all five workers. It
preserves existing keys and does not deploy. Services are released through GitOps;
readiness, nonempty provider updates and financial/product acceptance are separate.

The inactive `deploy/investments` Kustomize template includes native MOEX and
Twelve Data deployments with zero replicas. They use the reviewed image digest,
tenant/access-scope configuration and existing database Secret when enabled by
an operator overlay. The Twelve Data template mounts a `budget-twelvedata-readonly`
Secret's `token` as a read-only file; no inline credential is included. Deployment
names and Secret references can be adapted in GitOps. The provisioning helper
accepts reviewed clean tracker scopes at schema 61 (before upgrade) or 62 (after
upgrade), so adding the missing key can precede deployment and five-worker
verification can follow it. Other schemas and dirty migrations are rejected.

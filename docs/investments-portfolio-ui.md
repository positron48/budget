# Portfolio overview and growth

The overview shows value, remaining acquisition cost, profit and completed-day
change in money and percent. USD/RUB and EUR/RUB quotes compare the current
eligible observation with the preceding source observation; their dates and
source are available on hover. Daily profit uses completed retrospective
valuations, not an intraday P&L estimate.

Categories show allocation, value/cost, profit and weight. Selecting a category
opens its securities and their allocation. Growth has a shared date range,
category, security and currency selection with separate value, profit, period
profit and security-profit charts. IMOEX and SPY comparison lines use the same
selected monetary flows; SPY is a price ETF proxy for S&P 500.

Charts use horizontal grids, date ticks, a pointer/keyboard value cursor and
trade markers. Price lines have no per-observation circles. Long lines retain
real endpoints and bucket minima/maxima, normally up to 400 vertices per line.
Source/coverage changes and missing days split the line before sampling, so
many separate segments can exceed that target. Sampling does not change exact
financial metrics, period totals or export values.

## History loading and reuse

`GetTrackerHistory` and `GetTrackerInstrumentPrices` accept `page_days` up to 93;
zero preserves the existing 31-day contract. Standalone unit history processes
the quarter and its baseline in one journal pass, without calculating FIFO
cost. The web client consumes pages in date order with a bounded window of speculative read requests. Servers returning
legacy pages still work. Scope, daily continuity, policy and basis checks remain
mandatory, and abandoning a load cancels outstanding requests.

Chart history requests use `compact_issues`: one instance of each quality code
plus the original `issue_count`. Monetary fractions, complete/incomplete state,
position counts and the coverage hash remain exact. A date's full per-position
breakdown is fetched separately using `detail_date`.

The backend keeps disposable exact daily projections in a process-local LRU
cache, bounded to 32 MiB and 24 hours. Entries are fenced by tenant, actor,
selected journal/market/FX/classification basis, currency and calculation mode.
Every hit first rechecks current membership, ledger identities and source basis
inside the read transaction. Periods sharing a basis can reuse overlapping days.
At most two history calculations per repository run concurrently; queued reads
have not loaded journals or allocated daily position snapshots.
Source corrections invalidate reuse; a process restart or eviction makes the
cache cold. This cache avoids repeating valuation arithmetic, not the database
checks that establish current eligibility. Large entries may bypass caching.

Historical candles remain in PostgreSQL. No new financial journal or projection
schema is introduced by this UI change. Seven years are about 28 quarter-sized
pages; first-load production latency must be measured separately from the local
single-page and warm-cache benchmarks.

## Local full-range benchmark

`TestInvestmentTrackerHistory3000Operations100Instruments` and
`TestTrackerHistorySevenYearScale100000Operations500Instruments` traverse all 2557
dates in 28 sequential requests, then repeat with a warm projection cache.
The synthetic fixture has daily candles for the first 31 dates and deliberately
missing later prices. It tests partial coverage as well as exact valued dates;
it is not a full quote-coverage or production latency benchmark.

One local PostgreSQL 15 run measured 3.48 s cold / 1.02 s warm for 3000 operations
and 100 instruments, and 35.99 s / 21.15 s for 100000 operations and 500
instruments. These are server traversal times without network/browser rendering;
the upper-bound fixture still needs further optimization for interactive use.
Both fixtures also pass on PostgreSQL 16. Browser scenarios separately verify
seven years, ordered page loading, bounded requests and sampled line geometry.

CI runs the maximum-size full traversal in a separate step without race
instrumentation. The 100000-operation quarterly correctness checks, cache
concurrency checks and smaller seven-year traversal remain race-enabled.

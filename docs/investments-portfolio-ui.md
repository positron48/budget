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

`GetTrackerChart` returns one server-computed daily bundle, including exact
monthly/yearly totals and monetary comparisons. Schema 63 stores these disposable
protobuf projections in `investment_tracker_charts`. Each tenant/actor retains at
most 24 views of up to 16 MiB. A process restart preserves the projections.
Snapshot tokens are used only during calculation and are never persisted.

Every read rechecks current membership, ledger identities and the scoped
journal/market/FX/classification basis. Built-in benchmark prices and source
eligibility have an additional fence, even when the index is not a holding.
Corrections cause a recalculation; expired projections are not returned. A cold
view calculates quarterly internally, with one cold bundle at a time and
cancellable waiting. A warm view does not wait behind a cold calculation.
The 32 MiB process-local daily LRU remains an arithmetic optimization for those
internal pages and for existing paginated clients.

`budgetd` checks recent requested projections every 15 minutes and recalculates
those older than six hours or from a previous Moscow date. Recent views ending
at the then-latest completed day also get an extended projection for the new
completed day. Explicit historical periods remain fixed. Failed refreshes keep
the old derived record, but a read still refuses a changed source basis. Fixed
failure messages contain no portfolio facts or credentials.

Historical candles and projections remain in PostgreSQL; no separate time-series
service is required for daily portfolios. The first unseen or corrected scope
still needs a cold calculation. Measure cold calculation, persisted reads and
browser rendering separately. Clients fall back to paginated loading only when
an older server explicitly returns Unimplemented.

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

Full-history valuation reuses the IANA reporting location and an indexed FX
snapshot. The FX index validates each fixing once, selects by availability, and
preserves conflict, expiry, withdrawal and direct/inverse/cross-path rules.
Exact monetary amounts are unchanged. `TestFXSeriesMatchesResolverAcrossSevenYears`
compares every cutoff with the original resolver using populated daily FX evidence;
`BenchmarkDayCutoffSevenYears` covers repeated reporting-date conversion.

## Dated estimates and incomplete source history

Reviewed `Mapping.PriceEstimates` provide an explicit clean price per security,
including zero, when an owner chooses a reference valuation for an untraded
holding. `At` retains the price/valuation date; `From` is the first instant it may
value. These estimates override exchange selection only for the configured
instrument and effective dates, are versioned with mapping evidence, and never
backfill earlier history. The API labels them `estimated` / `manual_estimate`;
category rows and instrument headers show their dates. Mapping-only updates
preserve the estimate schedule; an explicit empty schedule removes it.

Pending source trades affect selected categories and securities only when their
identity is established by posted source rows of the same import. Unknown or
ambiguous identities remain incomplete in every selection. Missing quantities
and acquisition costs are never manufactured to make the overall profit complete.

## Preliminary profit and independent index growth

Monetary profit excludes unknown flows and securities without usable end-date
valuations from a fixed preliminary subset. Temporary earlier valuation gaps
remain gaps: they do not discard every trade and payout for that security across
the entire period. Boundary profit can recover when both valuations and every
intervening flow are known. Unknown costs/flows are never replaced by zero.

Value, profit, monthly/yearly changes, security-profit bars and monetary benchmark
comparisons share the monetary subset, labelled preliminary with its excluded
instruments. Value drilldown uses that same instrument scope and basis. The
percentage-growth comparison separately requires a continuous daily history;
its stricter selection is disclosed. Both calculations retain authorization and
correction checks. An empty subset never means an unfiltered selection.

During an index price gap, virtual cash flows wait without earning index returns.
They enter the model at the next eligible close. Missing valuations stay gaps,
but do not permanently terminate every subsequent comparison point. Unknown
flows, history resets and insufficient virtual balances still stop the model.
This is a retrospective fractional comparison, not a claim of executable trades.

Category allocation includes only nonzero security positions. Payout-only and
closed positions retain their recorded income but do not create zero-asset
allocation rows. Portfolio value lines are solid; intentional reference/invested
lines remain dashed. Missing observations and coverage changes still split lines.

Independent growth compares benchmark price returns without simulating personal
trades. Where a continuous portfolio TWR exists, it is shown alongside those
returns, with flow effects removed. This is not the historical value of today's
fixed holdings. All visible growth lines share the first common observed baseline;
missing observations remain gaps. SPY is a price proxy, without reinvested fund
dividends. For all benchmark modes, missing initial price/FX delays the baseline
to the first eligible date rather than hiding every later observation; that date
is disclosed. No future fixing is carried backward to fill the initial gap.

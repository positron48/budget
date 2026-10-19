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
cost. Tracker accounts with custody transfers or distributions use the atomic
`investment_security_legs` projection for daily quantities, including split and
distribution deltas. Historical acquisition lots and transfer cost books are
not replayed per day. Legacy cash books retain their existing replay contract.
The web client consumes pages in date order with a bounded window of speculative read requests. Servers returning
legacy pages still work. Scope, daily continuity, policy and basis checks remain
mandatory, and abandoning a load cancels outstanding requests.

Chart history requests use `compact_issues`: one instance of each quality code
plus the original `issue_count`. Monetary fractions, complete/incomplete state,
position counts and the coverage hash remain exact. A date's full per-position
breakdown is fetched separately using `detail_date`.

`GetTrackerChart` returns one server-computed daily bundle, including derived
monthly/yearly totals and monetary comparisons. Running reporting sums are
bounded to 24 decimals when inverse-FX denominators grow past 128 bits; over
ten years rounding error stays below 2e-21 currency units. Individual flows,
source journal and FIFO remain exact. Schema 63 stores these disposable
protobuf projections in `investment_tracker_charts`. Each tenant/actor retains at
most 24 views of up to 16 MiB. A process restart preserves the projections.
Snapshot tokens are used only during calculation and are never persisted.

Every read rechecks current membership, ledger identities and the scoped
journal/market/FX/classification basis. Internal basis-only checks read distinct
instrument identities and source versions without calculating positions or
quotes; they produce the same basis as the full history read. Built-in benchmark prices and source
eligibility have an additional fence, even when the index is not a holding.
Corrections cause a recalculation; expired projections are not returned. A cold
view calculates quarterly internally, with one cold bundle at a time and
cancellable waiting. A warm view does not wait behind a cold calculation.
The web probes the ready chart for at most 1.5 seconds. A cold projection falls
back to validated, cancellable quarterly pages and shows progressive history;
source-change, authorization and validation errors remain errors. This avoids
waiting on a multi-year bundle before showing any dates. A separate recent
quarter can appear first while older pages load; independently read windows
are never merged into the final history. Invested capital and returns stay in
the loading state until the full signed sequence has been checked.
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

Daily history retains the preceding candle when a window starts after a long
exchange closure. Expired valid prices remain dated `last_known_price` estimates;
future candles, incomplete candles and contradictory evidence do not fill gaps.
Dense pages and sparse contribution boundaries use the same rule. Unknown
intraday timing does not invalidate known end-of-day security units. A scoped
selection tests its own opening balances when deciding whether earlier recorded
holdings start from zero; unrelated unknown openings retain their warnings only
in scopes containing them. These changes invalidate derived history caches.
Nonnegative charts keep a nonnegative axis; area fills end at zero, including
profit charts that legitimately cross below zero.

Pending source trades affect selected categories and securities only when their
identity is established by posted source rows of the same import. Unknown or
ambiguous identities remain incomplete in every selection. Missing quantities
and acquisition costs are never manufactured to make the overall profit complete.

## Preliminary profit and independent index growth

Monetary profit excludes unknown flows and securities without usable boundary
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

## Portfolio navigation and charts

All investment pages share a top navigation bar. Account selections and chart
filters survive navigation; market FX is the user-facing convention, including
old URLs containing `fx=cbr`. The underlying CBR data/API remains available to
ordinary Budget features.

Trades and receipts render bounded pages, link securities to their cards and
show signed trade quantities. Categories use focusable SVG sectors with value
and allocation-share tooltips. Security groups are edited on one board by
dragging account-specific positions or using their keyboard-accessible selectors;
one Save action retains the version-fenced atomic group update.

Line and period charts use round 1/2/5 × 10^n scale steps. Long period charts
scroll horizontally while their left value scale remains outside the scrolling
plot. Each chart shows an indeterminate progress bar while loading. Bar tooltips
show the period and value on pointer hover or keyboard focus.

Missing period boundary prices keep the full `profit` absent. The saved chart
may additionally contain `known_profit` and `excluded_instrument_keys`, computed
from contributions of securities with known opening, closing and movement data
on every selected account. These bars are labelled partial; changing per-period
subsets need not sum to the full-range profit. Unknown flows and history resets
do not receive this fallback. CSV identifies partial rows as `month_known` or
`year_known` rather than silently exporting them as full period profits.

## Automatic payout calendar

The forecast reads the latest available coupon/dividend schedules for approved
market mappings and enabled, tenant-scoped sources in the same read-only database
transaction as the portfolio. Announced amounts remain separate from unknown
amounts. Previous-year dividends other than known special distributions can
repeat one year later as explicitly labelled projections; recorded splits adjust
the per-unit amount, and unsupported transfers/distributions suppress that
projection. Current holdings and current market FX are a scenario, not confirmed
personal entitlement or a prediction of future exchange rates.

Derived events have deterministic IDs and `origin=provider` or
`origin=historical_projection`; they are never saved as personal events, linked
automatically to receipts, or turned into journal operations. Saved user events,
including cancellations, take priority for the same security/kind/payment date.
The calendar shows known monthly totals alongside incomplete-amount indicators.

## Consistent chart loading and return scopes

The persisted chart and paginated fallback use `monetary_excluded_keys` for
monetary profit and a separate `analytics_excluded_keys` scope for continuous
returns. A temporary price gap must not silently change the profit universe
when the chart probe times out. Both paths validate the scoped calculation and
recheck the original basis after calculating subsets.

Continuous return subsets exclude connected corporate distributions together
when one side is unavailable. Otherwise a selected fractional-review unit can
create an unknown external flow after its parent was excluded. This does not
change any operation or its acquisition cost.

Index history starts independently of the first complete portfolio valuation.
Available TWR segments remain visible even if the whole-period TWR is
incomplete; a reset is not joined to the old index as an investment loss.

The global historical chart includes archived tracker accounts so acquisitions
and realized results remain part of the lifetime strategy, and custody moves
from a closed broker do not become new external capital. Navigation and current
holdings still use active accounts. Explicit single/multiple account selections
keep their requested scope, including the existing transfer-boundary rules.

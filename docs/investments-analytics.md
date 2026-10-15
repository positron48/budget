# Simple portfolio monetary analytics

`/investments/analytics` shares the portfolio's account, currency, FX and period
filters, plus an optional security selection. Security and group filters intersect.
It shows daily securities value, opening value plus net investment,
cumulative profit, normalized returns, monthly/yearly monetary profit and CSV.
The overview retains current portfolio totals separately from selected-period
analytics. Both selected dates are included; profit starts at the previous day's
closing valuation. Presets include 7 days, month, quarter, half-year, YTD, year,
five years and recorded history (at most ten years).

This is a cashless securities strategy. Purchases and fees add investment;
sales and received dividends/coupons return money. Broker deposits, withdrawals,
uninvested cash and tax calculations are outside this model. Profit equals
closing securities value minus opening securities value minus strategy flows.
A payout is therefore counted once. Daily monetary values and cash flows use
the selected dated FX policy; no current-rate fallback is introduced.

Daily profits require both adjacent valuations. Cumulative and month/year profit
can recover after an intermediate valuation gap when both relevant boundaries
and all intervening flows are known. Missing flows or an opening/history reset
invalidate the cumulative result. Each later month/year has its own boundary;
unknown results remain visible as unknown, including partially selected periods.

Monetary IMOEX and SPY comparisons use fractional units, the same starting
portfolio value, and the same daily strategy flows. This is an explicitly
retrospective valuation model: it rebalances at the eligible daily value,
including carried closes. It does not establish executable prices, publication
times, an exchange calendar, commissions, or benchmark dividend reinvestment.
An unknown flow, history reset, insufficient virtual balance, or a nonzero flow
without a price stops the simulation; trades are never backdated on recovery.
Modeled monetary values round to 24 decimal places at each eligible valuation
to bound denominator growth over long histories; actual portfolio money remains
exact. The initial virtual value equals the portfolio baseline exactly.
Price-only index/ETF identities and their native currencies remain labeled.

Chart pages are authenticated, signed and fenced against journal/market changes.
Analytics derive from these same facts. The client checks daily values against
the chart and rejects incoherent dates, policies, fractions and period coverage.
CSV retains exact rational money and returns, TWR segments and quality reasons,
annualized XIRR, benchmark indices and virtual monetary values.

Selected groups show their own daily value, cumulative profit and monthly/yearly
profit, as well as the contribution table. A security selection applies across
selected accounts; combined with groups, it includes only memberships in both.
Purchases, sales, assigned fees and payouts belong to the selected securities.
Unassigned expenses/payouts are excluded from every group/security selection
and remain visible separately in the contribution table.

Transfers wholly inside the selection cancel without requiring a transfer price;
a transfer crossing its boundary requires dated market value. A corporate
allocation crossing that boundary remains unknown without market-value evidence.
Group TWR/XIRR remain unavailable; the group chart offers monetary modes only.
Security selections without groups use their own signed strategy flows for
returns. Whole-account flows and returns are never inherited by a selection.

Signed pages bind the selected groups, security digest, account identities and
monetary-only mode. The client verifies echoed filters before showing previews
and rechecks the performance result; an older backend cannot silently broaden
the chart. CSV includes selected keys/groups and classification version.
See `investments-contributions.md` for contribution rules.

A valuation point opens a lazy breakdown, also available from a date selector
and keyboard. It shows each account/security quantity, split-rebased clean unit
price, native amount, effective FX and reporting-currency value. Selected FX
legs retain original pair/rate, inverse direction, revision, fixing timestamp,
rule eligibility and exclusive validity end. Dates describe retrospective
valuation eligibility, not historical publication-time evidence. Cash and
accrued interest remain excluded from this clean securities chart.

`GetTrackerHistory.detail_date` returns one breakdown within the original
from/to range. It does not sign a new performance page or write financial data.
The original range and filters retain the chart's journal/market basis. The
client checks that basis, policy, scope, quality, exact native and converted
identities, FX path and component total before display. Missing price/FX stays
unknown; empty and closed accounts remain zero without invented components.
If the basis changes, the breakdown asks for a chart refresh. Ordinary paged
history stays compact and does not carry every day's position breakdown.

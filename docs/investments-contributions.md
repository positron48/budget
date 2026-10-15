# Securities contribution to period profit

The portfolio and `/investments/analytics` show a contribution table below the
history chart. It uses the same accounts, inclusive period, display currency,
FX policy and current group/security selection. Search filters the table; the headline
remains the result of the selected account/group/security scope. Rows can be combined by
security across accounts or by current group. Closed positions with a result
remain visible; zero results can also be included. CSV retains account-level
provenance, exact monetary fractions, quantities, classification and quality.

Each account/security contribution is closing value minus the previous day's
opening boundary value minus its own recorded strategy flows. Purchases and fees
are positive flows; sales and actual payouts are negative. Received dividends
and coupons therefore count once. This is a monetary contribution, not FIFO
realized profit or a TWR/XIRR percentage. Those remain separate metrics.

Transfers are valued on their recorded date. A transfer crossing groups creates
opposite boundary flows, even when both accounts are included in the portfolio;
combining the groups cancels those flows. Fees and payouts without an instrument
are shown separately. They belong to the complete account result but are excluded
from any selected group or security result. No whole-account funding is inherited by a group.

The backend reads the journal, classification, both valuation boundaries and
required transfer prices in one read-only repeatable-read snapshot. It queries
small indexed candle windows around those dates rather than every candle between
them. The same quote normalization, split handling, dated nominal and historical
FX rules are used as for the daily chart. Up to 20 accounts, 500 securities,
100,000 operations and ten years are accepted within existing resource limits.

Missing boundary value, pending source trades, unknown flow conversion or an
opening reset leaves the affected contribution unknown. A corporate cost
allocation is not transfer-day market-value evidence: cross-instrument
allocation contributions remain unknown when that evidence is absent. Known
subtotals stay labeled as partial. Unknown values are never replaced by zero.

Profit shares use the complete selected result and can be negative or exceed
100%. They are withheld when the total is unknown or zero. Percentage fractions
are displayed to twelve decimals and withheld beyond a numerical range of 1e12;
actual monetary contributions remain exact. This bounds repeated long FX
fractions without rounding actual money. Shares are not instrument returns.

The client checks row identities, scope, group version, exact monetary totals
and row profit identities. It cancels requests on tenant changes and refreshes
when ledger versions or classifications change. This endpoint neither contacts
providers nor writes positions, trades, budget movements or tax calculations.

Group period profit reconciles with the selected daily/cumulative/month/year
profit charts whenever the required evidence is complete. The optional security
filter intersects group membership. Group TWR/XIRR are intentionally unavailable.
An individual corporate allocation contribution can remain unknown even when
its effect cancels inside a larger group selection.

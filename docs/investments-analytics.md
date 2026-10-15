# Simple portfolio monetary analytics

`/investments/analytics` shares the portfolio's account, currency, FX and period
filters. It shows daily securities value, opening value plus net investment,
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

Current group filters show classified securities value. Monetary profit and
TWR/XIRR currently use account scopes; no whole-account flows are inherited by
a selected group. Instrument contribution analytics remains separate work.

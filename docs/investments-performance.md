# Securities portfolio performance

The simple portfolio measures the recorded securities strategy without requiring
broker deposits, withdrawals, or a cash balance. These calculations do not
reconstruct unimported broker history and do not calculate taxes.

For each reporting day in Europe/Moscow, purchases and recorded commissions are
positive strategy contributions. Sales and received dividends, coupons, and
redemption proceeds are negative contributions. Currency conversion uses the
selected historical FX policy for that day; missing history remains unknown.
Actual trade settlement is used when supplied, without counting included expenses
twice. A separately dated commission follows its own date and currency.
Transfers between selected accounts cancel; a transfer across the selected scope
requires a historical value. An opening position resets performance history.

Period profit is ending securities value minus the previous day's securities
value minus net strategy contributions. Cash and received payouts do not remain
in securities value; payouts contribute to profit through the recorded flows.

TWR uses the documented daily end-of-day-flow approximation. The first funded
day establishes the baseline. Missing valuations, unknown flows, and a complete
withdrawal break the chain; independent segments cannot be joined as a complete
period return. First-day expenses affect profit and XIRR, but are not measured by
the TWR baseline. Bond valuation is clean-price valuation, not a yield-to-maturity
calculation.

XIRR is an annual rate using ACT/365, including for periods shorter than a year.
The investor starts with negative baseline value, receives the opposite of each
strategy contribution, and finishes with terminal value. Multiple or unreliable
roots are reported as unavailable. Long nonconventional histories use an exact
cumulative-balance uniqueness certificate only when its conditions hold; other
ambiguous histories retain their unavailable state.

Standalone trade-only trackers derive historical positions from recorded trades,
even when imported trades predate tracker creation. Their recorded strategy is
empty before the first trade. Opening balances and legacy connected cash books
retain their unknown-before-start contract. Pending source trades remain explicit
quality gaps.

Historical prices use the retrospective completed UTC candle rule and are eligible
from the following UTC day. This is not a claim that the close was demonstrably
published or tradable at that historical time.

The history and summary APIs use the same bounded, short-lived signed page facts.
The summary rechecks tenant membership, selected accounts, journal revisions,
price mappings, and FX revisions before accepting them. Corrections or an expired
snapshot require refreshing the chart. Tokens grant no access, are not persisted,
and contain no provider credential. Money is calculated as exact rational values;
reported return fractions are rounded to twelve decimal places.

## Return chart and price comparisons

The Returns view plots the portfolio's first measured TWR segment normalized to
100. It does not connect a later restarted segment to the original one. XIRR
remains a separate annual scalar. Unknown daily values split the plotted line.

Built-in comparisons are explicitly price-only:

- [IMOEX — MOEX Russia Index](https://www.moex.com/ru/index/IMOEX), a RUB price
  index, sourced from Moscow Exchange ISS, SNDX. Its native unit is index points;
  absent index volume stays absent.
- [SPY — SPDR S&P 500 ETF Trust](https://www.ssga.com/us/en/institutional/etfs/state-street-spdr-sp-500-etf-trust-spy),
  a USD ETF price proxy on NYSE Arca, sourced through Twelve Data's NYSE/ARCX
  identity. It is not the S&P 500 index or a total-return series. Requests disable
  split/dividend adjustments, and response MIC, currency and ETF type must match.

Both comparisons use the same retrospective daily-close rule and selected daily
FX policy as the portfolio. Their base is the portfolio's first measured day,
or the requested previous-day boundary when no TWR segment can be measured.
Missing base-date FX or price makes normalization unavailable; the base is not
silently moved. Later gaps remain gaps. There is no present-day FX substitution.

Comparison prices are read from one authorized database snapshot. The portfolio
summary then rechecks its original journal/FX/price basis. Native workers refresh
the reviewed built-in identities only under explicitly enabled `personal` sources;
code defaults do not enable a source or restore credentials. Recent refreshes
and bounded historical jobs use the ordinary durable queue and provider quota.
These price lines do not simulate actual purchases or executable trading sessions.

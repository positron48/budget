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

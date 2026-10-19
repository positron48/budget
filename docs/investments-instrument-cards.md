# Investment instrument cards

`/investments/instruments/[id]` opens from a holding or from the security finder
at `/investments/instruments`. Looking up a security does not create a position,
trade, or personal instrument mapping. Provider requests only populate market
metadata and price caches. The finder shows cached listings and can enqueue a
T-Invest catalog search; provider search results are not an exhaustive catalog.

The card shows the current price, instrument type, a price chart with trade
markers and an average acquisition-price reference, plus quantities, average
price, remaining cost and value per account. Trades, issuer dividends, bond
terms and source controls are expandable. Account and current group
filters limit the personal journal. Corporate journal operations remain separate
from the provider's bond schedules. Missing historical cost or FX remains unknown.

The price chart uses unadjusted completed UTC daily candles retrospectively from
the following UTC day. A provider series is explicitly distinct from the reviewed
personal listing timeline. Gaps and currency changes are not interpolated.
Prices, sources, dates and exact rational values can be exported to CSV.

Bond prices default to percent of nominal. Per-security clean money uses nominal
valid on the candle date; dirty money requires dated accrued interest. Today's
nominal or accrued interest never fills missing historical evidence. Latest bond
terms and dated history are displayed separately. A currency-less provider accrued
quotation does not inherit the nominal currency. It needs an explicit amount
currency, or an exact same-UTC-day match with the explicitly denominated current
ACI. Otherwise accrued and dirty prices remain unavailable while clean prices
remain usable; no FX conversion is inferred. Legacy snapshots retain their hashes. Coupon and event records are
provider observations, not confirmation of personal entitlement, currency
conversion terms or complete future cash flows. Coupon yield uses the next
known coupon and provider frequency divided by the current clean price. The
indicative effective annual maturity yield discounts an assumed continuation of
that fixed coupon plus bullet redemption against clean price plus current ACI.
It is explicitly an estimate, excludes tax/calls and is unavailable for variable,
amortizing, perpetual or unknown bond models. Currency evidence and a provider
facts timestamp within 24 hours of the quote are required.
The terms panel shows provider records within one year around the current date,
with at most 200 entries of each kind; its excerpt does not limit chart evidence.

History reads require current tenant membership and an enabled provider source.
Pages carry a basis hash; changed inputs cause a refresh rather than mixed pages.
Queued requests and pagination are cancelled when the tenant or selected price
scope changes. Automatic loading waits for a coherent chart response, polls jobs
for at most two minutes, and does not claim that a completed job proves full
price coverage. The user can retry source loading manually.

Cards also link to the [reviewed source picker](investments-price-sources.md).
Current and native historical schedules remain independent; opening a listing
never creates this mapping implicitly.

The compact overview and growth charts are described in [Portfolio UI](investments-portfolio-ui.md).

When a live price is unavailable, the portfolio uses a dated last-known price
and labels it as an estimate rather than a fresh exchange quote. Explicit manual
valuations (including a confirmed zero value) remain authoritative; fallback
estimates yield to a usable market quote. Missing quotes alone never establish
that a security has ceased to exist. Historical fallbacks cannot fill dates
before the price became available. Bond point quotes require the confirmed
nominal and its currency for the corresponding date.

Transferred principal-basis lots retain their acquisition cost and date even
when the source account is archived or excluded from the selected portfolio.
The overview can value those lots without adding the source account's trades
to the selected account's income or profit.

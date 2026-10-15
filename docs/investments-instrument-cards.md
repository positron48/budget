# Investment instrument cards

`/investments/instruments/[id]` opens from a holding or from the security finder
at `/investments/instruments`. Looking up a security does not create a position,
trade, or personal instrument mapping. Provider requests only populate market
metadata and price caches. The finder shows cached listings and can enqueue a
T-Invest catalog search; provider search results are not an exhaustive catalog.

The card shows personal quantities, remaining FIFO cost, realized and unrealized
results, actual payouts, trades, and trade markers. Account and current group
filters limit the personal journal. Corporate journal operations remain separate
from the provider's bond schedules. Missing historical cost or FX remains unknown.

The price chart uses unadjusted completed UTC daily candles retrospectively from
the following UTC day. A provider series is explicitly distinct from the reviewed
personal listing timeline. Gaps and currency changes are not interpolated.
Prices, sources, dates and exact rational values can be exported to CSV.

Bond prices default to percent of nominal. Per-security clean money uses nominal
valid on the candle date; dirty money requires dated accrued interest. Today's
nominal or accrued interest never fills missing historical evidence. Latest bond
terms and dated history are displayed separately. Coupon and event records are
provider observations, not confirmation of personal entitlement, currency
conversion terms or complete future cash flows. Yield to maturity is not computed.
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

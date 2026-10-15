# Connecting portfolio price sources

A security card links to a source picker. Choose current prices or historical
prices, search the available catalog, inspect the exact listing (ISIN, venue,
currency and name), choose its UTC start date and confirm the identity before
saving. A matching ticker is not sufficient evidence. Connecting a source does
not create trades or alter quantities or payouts.

Current prices use T-Invest. Historical prices use cached MOEX or Twelve Data
listings, kept in independent provider namespaces. T-Invest can request a
provider catalog search; native providers currently search only cached listings.
This is not a claim of universal external instrument discovery. Missing native
catalog entries still require native catalog ingestion. Viewing a provider card
alone never creates a personal mapping.

Incomplete T-Invest search results require metadata loading before confirmation.
The existing worker job fetches the full instrument record; polling stops after
two minutes and a manual refresh remains available. Bond mappings require a
confirmed nominal in the quoted currency on the chosen start date. Other dates
of amortizing bonds still require dated provider evidence. Cross-currency bond
terms are not inferred by the picker. Security quantities remain individual
units; no lot size is fabricated.

A new source keeps earlier periods and replaces the selected purpose's timeline
from the reviewed date onward. Changing current prices preserves an existing
historical schedule; when history previously fell back to current periods,
those old periods are retained as an independent history. Changing history does
not change the current price schedule. The user reviews the date and effect
before submitting. Existing scheduled changes after that date are replaced.

`InvestmentMarketMapping.historical_periods` exposes the historical schedule.
`SaveInvestmentMarketMapping.replace_historical` explicitly selects its mutation;
false preserves history, true applies the provided full historical schedule.
Historical periods supplied without this flag are rejected. Legacy current-only
requests preserve history. The repository validates enabled sources and cached
listing identities, records native canonical identities, and publishes the
mapping revision and invalidation in one transaction. Version conflicts require
refresh/review; retries reuse the same request key. Roles remain owner/admin for
writes and member for reads. Read-only runtime hides the connection action.

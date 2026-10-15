# Security groups

`/investments/groups` manages a flat primary classification for securities.
Membership belongs to an account and instrument together. The same security may
therefore belong to different groups in different accounts. Names are user labels;
naming a group after an index does not subscribe to that index's constituents.

Changes are edited as one draft and saved atomically with an optimistic version
and an idempotency key. Owner/admin may write; members may read. Deleting a group
removes its labels, leaving its securities ungrouped. Closed positions remain
available for assignment. Trades, payouts, prices and acquisition lots are not
changed by classification edits.

The portfolio supports a union of group selections, including Ungrouped, with no
double counting. Holdings, received payouts, trades, and historical securities
values use the same account-specific classification. Historical views use today's
classification for every date, rather than reconstructing historical labels.
The classification version fences historical pagination so a mid-load edit
requires refreshing. Refresh also reloads the current classification.

Group history is explicitly called Selected securities value. It does not inherit
account-level TWR or XIRR, and does not request account performance snapshots.
Unknown prices, FX, quantities or pending source history retain their quality gaps.
Distribution targets cannot be presented as free profits merely because their
original instrument lies outside the selected group.

Allocation may be shown by security or by the primary groups. Percentages require
complete acquisition costs. An unavailable classification does not silently move
all securities into Ungrouped.

Account, currency, FX mode, groups, allocation view, operation tab, closed-position
visibility, historical date range, chart type and benchmark visibility persist in
the URL. Browser Back/Forward restores the selection. Filter changes do not need
server navigation. Existing tenant-switch handling clears investment selections
and caches before mounting another tenant's view.

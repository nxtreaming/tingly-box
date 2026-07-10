# Dashboard Usage Query Performance

> Status: shipped on `claude/dashboard-data-performance-21da4o`.

## Problem

The usage dashboard (`/api/v1/usage/stats`, `/usage/timeseries`, `/usage/records`)
aggregated directly over the raw `usage_records` table on every load. With a large
record count (weeks of heavy proxy traffic) each 30/90-day dashboard load ran
multiple full range scans with `GROUP BY strftime(...)`, all serialized behind a
single `sync.Mutex` shared with the proxy's usage writes, and shipped uncompressed
JSON. Symptoms: multi-second dashboard loads, UI feeling frozen, large network
payloads.

## Fix (four layers)

### 1. `usage_daily` pre-aggregation (`internal/data/db/usage_daily.go`)

The previously dormant `usage_daily` table is now populated and queried:

- **Schema v2**: one row per `(UTC day, provider_uuid, model, user_id)` with
  additive sums (`request_count`, token sums, `error_count`, `streamed_count`,
  `latency_sum_ms`). The day key equals SQLite's `date(timestamp)` — the same
  UTC-day bucketing the raw queries already used — stored as `YYYY-MM-DD` TEXT.
  Old-layout tables (no `user_id` column) are dropped and rebuilt on startup;
  the table holds only derived data, so this is safe.
- **Lazy backfill**: the first query needing a completed day triggers
  `aggregateDay` (DELETE + INSERT…SELECT in a transaction). Which days are
  already aggregated is read directly from `usage_daily` on each call
  (`missingAggregatedDays`, an indexed range query on `date`) rather than
  cached in memory — the table is the source of truth, so there's no shadow
  state to keep in sync or lose across a restart. A day is only aggregated
  once it is ≥1h past UTC midnight (`dailyAggGrace`) so requests recorded
  shortly after midnight are not missed.
- **Query routing**: `GetAggregatedStats` (group_by ∈ model/provider/user/daily,
  no scenario/rule/status filter) and `GetTimeSeries` (interval=day, filters ⊆
  provider/model/user) spanning ≥2 complete days split the range into:
  raw scan of the partial leading day → `usage_daily` for complete days → raw
  scan of the trailing partial day(s); results merge additively, and averages
  (latency) recompute from sums. Anything else falls back to the raw scan, as
  does any aggregation error (logged at Warn).
- **Deletion consistency**: `DeleteOlderThan` also purges `usage_daily` rows up
  to and including the cutoff day, so the boundary day re-aggregates from the
  remaining raw rows on the next query.

Measured on ~200k records / 90 days (SQLite, in-repo test): stats 300ms → 12ms,
timeseries 227ms → 6ms steady-state; one-time backfill ≈ one raw scan. Raw-path
cost grows linearly with record count; the daily path stays flat.

### 2. Concurrent reads (`UsageStore.mu` → `sync.RWMutex`)

Queries take the read lock (SQLite WAL supports concurrent readers), writes the
write lock. The dashboard's parallel requests no longer queue behind each other
or behind proxy usage writes.

### 3. gzip on usage endpoints (`internal/server/middleware/gzip.go`)

`middleware.Gzip()` is real gin middleware (calls `c.Next()`), registered
per-route via `swagger.WithMiddleware(middleware.Gzip())` on the three usage
GET routes (JSON-only; never use it on streaming/SSE routes) so it composes
through the normal middleware chain instead of wrapping the handler directly.
Compresses when the client sends `Accept-Encoding: gzip`; typical usage JSON
shrinks ~10x.

### 4. Frontend fetch discipline (`DashboardPage.tsx`)

- Providers + API tokens (filter metadata) load once on mount and on manual
  refresh — not on every filter change / auto-refresh tick.
- A request sequence number drops out-of-order stats/timeseries responses when
  filters change faster than requests complete.
- `GetRecords` skips the `COUNT(*)` scan when the first page already contains
  the full result set.

## Invariants / gotchas

- Timestamps are bound as server-local time strings by the SQLite driver and
  compared lexicographically; all new query bounds are converted via
  `.In(time.Local)` to match. Per-day aggregation scans pad the timestamp range
  by ±2h (`dstScanPad`) and guard with exact `date(timestamp) = ?`.
- `usage_daily` has no scenario/rule/status dimension — queries filtering on
  those always use the raw table. Extend the schema (and bump the rebuild
  condition in `ensureUsageDailySchema`) if those filters ever need the fast path.
- Equivalence between the merged path and the raw path is locked in by
  `internal/data/db/usage_daily_test.go`; if you change either side, keep those
  tests green.

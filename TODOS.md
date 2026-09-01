# Terraform Provider TODOs

> API-parity work (the 2026-09 server catch-up) lives on GitHub — issues #5–#26,
> tracking in #27. This file keeps only the cross-cutting engineering debt that
> isn't tied to a single parity issue.

## P0 — Must fix

### Unset semantics / nil vs empty
- `omitempty` + nil suppression means users can't reliably clear optional fields;
  reads normalize API null to `""` → plan drift
- The 2026-09 API made this contractual instead of incidental:
  - Status page items: explicit `null` CLEARS a label, omission preserves it — the
    spec warns Go clients specifically. And `Items []StatusPageItem ... omitempty`
    makes emptying a page impossible (#12)
  - ~~`dns_expected_records`~~: fixed in #9 — the update input field is `*[]string`,
    so an explicit `[]` is sent and `mapToState` keeps the pinned empty list rather
    than flipping it to null. The pattern to copy for the rest
  - Instance secrets: blank-means-keep + `_set` booleans (#7)
- Fix: pointer types + explicit-empty handling per field, map API null →
  `types.StringNull()`
- Newly load-bearing on uptime monitors: #9 made `protocol` updatable, so switching
  protocol leaves the previous protocol's attributes stale server-side. Optional-only
  attributes (`port`, `keyword`, `dns_record_type`, `custom_body`, `content_type`,
  `custom_headers`) are never cleared, so the API keeps echoing a value the plan says
  is null → "Provider produced inconsistent result after apply" on, say, dns ⇒ https.
  Fix alongside the audit above, ideally by sending explicit nulls for attributes the
  target protocol does not use

### JSON execution_graph canonicalization
- `execution_graph_json` is a raw string attribute. Harmless *today* only because
  Read never fetches the graph back, so state always holds the user's own string
- Becomes an active false-diff bug the moment #10 lands (Read will return
  server-normalized JSON vs the user's formatting)
- Fix: semantic-equality JSON type (`jsontypes.Normalized` from
  terraform-plugin-framework-jsontypes), shipped with or before #10

## P1 — Should fix

### Acceptance tests (TF_ACC)
- Zero end-to-end coverage; unit fixtures encode API shapes and drift silently
  with the server
- Proof it matters: the pagination meta rename (#5) passed unit tests for months —
  the fixtures still speak `count`/`total`/`offset`
- The `UptimeMonitorStatus` payload shape landed in #9 unverified against a live
  API (no spec in-repo, no key in CI) — decoding is defensive, but only an
  acceptance test proves the field names
- Needs: dedicated staging org + `FIVENINES_API_KEY` in GitHub Secrets,
  terraform-plugin-testing full CRUD lifecycle tests

### Instance delete 202 handling
- `DELETE /instances` returns 202 (async); provider drops state immediately
  without polling
- Fix: poll `GET /instances/:id` until 404 or timeout (matters for
  delete-then-recreate of the same host)

### Uptime monitor update costs a round trip it cannot use
- `Update` re-GETs the monitor purely to harvest an ETag, discarding the body,
  while `Read` already receives that ETag from the same endpoint and discards it
- A plain config change is 3 round trips (refresh GET, ETag GET, PATCH); a
  pause-only change is 3 where a single POST /pause would do
- Because the GET and PATCH are microseconds apart, the If-Match window is
  effectively zero, so the precondition can almost never catch the concurrent
  modification it exists for
- Fix: persist the ETag from Read/Create in `resp.Private`, pass it straight to
  the PATCH, and keep the re-GET as the 412 recovery path only
- Found by: /ship performance specialist, 2026-09-01

### Create and Update disagree about protocol-scoped fields
- `UpdateUptimeMonitorInput` dropped `omitempty` on the seven protocol-scoped
  fields (#9) so they can be cleared; `CreateUptimeMonitorInput` still has it
- If the server DERIVES any of them on create (`content_type` from a POST body,
  `port` from a `https://host:8443/` URL) the value lands in state while the plan
  holds a known null — the same inconsistent-result error, on the first apply
- Unverified either way; needs one live create to confirm before changing
- Found by: /ship adversarial review, 2026-09-01

### Index filters are trusted without verification
- The data source forwards `status`/`protocol`/`q`/`updated_since`/`order`/
  `direction` and consumes the result verbatim. A server that ignores an
  unrecognised param returns every monitor with no error, and the result feeds
  `for_each`
- `status` and `protocol` are present on every returned object, so the provider
  could cheaply assert the server honoured them
- `updated_since` has no RFC3339 validator, so a malformed timestamp the server
  ignores yields a silent superset

### monitors is an ordered List with a server-defined default order
- `order`/`direction` are optional, so default ordering is whatever the API
  returns. A server-side reordering shifts every index, causing spurious diffs in
  anything indexing positionally and unstable keys in an index-keyed `for_each`
- Fix: a Set, or make `order` mandatory

### Unbounded rate-limit backoff
- `doRequest` derives the 429 wait from `X-RateLimit-Reset` via `time.Until` with
  a floor but no ceiling, so a skewed reset header parks a plan indefinitely
- `maxListPages` (1000) x `per_page` (100) also means up to 100k structs and 1000
  sequential 30s requests before the ceiling reports anything
- Fix: cap the backoff, and bound total records rather than pages

## P2 — Nice to have

### Unescaped resource ids in request paths
- #9 applied `url.PathEscape` across the uptime monitor endpoints; instances,
  tasks, workflows, network devices and status pages still concatenate the raw id
- Ids normally come from state, but `terraform import <addr> '<arbitrary>'` feeds
  the CLI argument straight into the path — `mon-1?foo=bar` becomes a query, and
  `a/../../b` traverses
- Fix: a shared `resourcePath(base, id, suffix...)` helper so the escaping cannot
  drift per-resource again

### fivenines_uptime_monitors has no limit
- The data source exposes `order`/`direction` but no `limit`/`per_page`, and the
  client always walks to the last page. "The 5 most recently updated monitors"
  still pulls every monitor in the org on every plan and refresh
- Fix: optional `limit`, threaded into ListUptimeMonitorsOptions, stopping the
  walk once `len(all) >= limit` and sizing per_page as `min(100, limit)`

### dns monitors may also require hostname
- `protocolRequirements` maps dns to `{dns_record_type}` only, but a DNS monitor
  has nothing to query without a hostname, and the repo's own example sets one
- Not added in #9 because the issue only specified `dns_record_type` and it is
  unclear whether the API accepts `url` for dns; a wrong guess false-rejects
  valid configs at plan time
- Fix: confirm against the server-side validation, then add `hostname`

### ping_key security model
- Task `ping_key` is Computed + Sensitive but persisted in state; the API still
  returns it on every read
- Decide: acceptable for the threat model, or move to write-only/ephemeral

## Recently closed

- **Pagination truncated every list at 100** (most of #5) — fixed in #9.
  `PaginationMeta` now decodes the real envelope (`current_page` / `total_pages` /
  `total_count` / `per_page`); all 8 list loops route through one `morePages`
  helper that stops on an empty page FIRST and only trusts the counters when
  `total_pages > 0`, so the next envelope rename over-fetches by one page instead
  of silently dropping rows. Fixtures were rewritten to the real shape — the old
  ones encoded the old envelope, which is how this stayed green for months.
  **Still open in #5: `ListIntegrations` is a single un-paginated GET, so
  `fivenines_integrations` still returns at most 25 channels.**
  Found by: Codex structured review + /ship red team, 2026-09-01

- **Uptime monitor pause/resume discards the API response** — fixed in #9.
  `PauseUptimeMonitor`/`ResumeUptimeMonitor` now return `(*UptimeMonitor, error)`
  and the resource assigns the response instead of hand-patching `Status` to a
  value (`"active"`) that isn't in the API's enum
- **Cross-field validation** — tasks in #8 (`ValidateConfig` enforces cron ⇒
  `schedule`, interval ⇒ `interval_seconds`), uptime monitors in #9
  (`ValidateConfig` + the `protocolRequirements` table: https ⇒ `url`, tcp ⇒
  `hostname`+`port`, icmp ⇒ `hostname`, dns ⇒ `dns_record_type`)

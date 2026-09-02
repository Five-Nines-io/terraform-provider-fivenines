# Terraform Provider TODOs

> API-parity work (the 2026-09 server catch-up) lives on GitHub — issues #5–#26,
> tracking in #27. Cross-cutting engineering debt is tracked in #29. This file
> keeps what neither of those owns.

## P0 — Must fix

### Unset semantics / nil vs empty
- `omitempty` + nil suppression means users can't reliably clear optional fields;
  reads normalize API null to `""` → plan drift
- The 2026-09 API made this contractual instead of incidental:
  - ~~Status page items~~: fixed in #29 and #12 — `UpdateStatusPageInput.Items` is
    `*[]StatusPageItemInput`, so a nil pointer omits the key while a pointer to an
    empty slice sends the explicit `[]` the API needs to empty a page; `Sections`
    follows the same shape. #12 added the per-item
    `display_label`/`description`/`section` as Optional+Computed attributes whose
    `,omitempty` tags leave a label curated in the dashboard alone
  - ~~`dns_expected_records`~~: fixed in #9 — the update input field is `*[]string`,
    so an explicit `[]` is sent and `mapToState` keeps the pinned empty list rather
    than flipping it to null. The pattern to copy for the rest
  - Instance secrets: blank-means-keep + `_set` booleans (#7) — still open, and the
    only remaining half of this item
- ~~Reads normalize API null to `""`~~: fixed in #29 — the nullable fields on
  `Instance`, `Task`, `Workflow`, `NetworkDevice` and `StatusPage` are pointers and
  map to `types.StringNull()`/`types.Int64Null()`. Attributes carrying a schema
  default keep the planned value on a null instead of wiping it, and `host_id` /
  `polling_host_id` / `snmp_username` follow the same no-`omitempty` convention #9
  established, so dropping them from a config clears them server-side
- ~~Protocol switches leave the old protocol's attributes stale~~: fixed in #9 — the
  seven protocol-scoped fields on `UpdateUptimeMonitorInput` lost `omitempty`, so a nil
  pointer marshals as JSON null and the server clears them. Residual: those attributes
  are Optional-only and cannot absorb a server-side null, so `protocolForbidden` rejects
  a leftover attribute at plan time rather than letting the apply fail with "Provider
  produced inconsistent result after apply". The create-side half of the asymmetry is
  still open under "Create and Update disagree about protocol-scoped fields" (P1)

## P1 — Should fix

### Acceptance tests (TF_ACC)
- Zero end-to-end coverage; unit fixtures encode API shapes and drift silently
  with the server
- Proof it matters: the pagination meta rename (#5) passed unit tests for months
  because the fixtures still spoke `count`/`total`/`offset`. #9 rewrote them, but
  nothing stops the next rename doing exactly the same thing
- The `UptimeMonitorStatus` payload shape landed in #9 unverified against a live
  API (no spec in-repo, no key in CI) — decoding is defensive, but only an
  acceptance test proves the field names
- The suite landed in #29: `internal/provider/*_test.go` drives full CRUD plus
  import for all six resources, and `.github/workflows/acceptance.yml` runs it
  nightly and on pushes to `main` (never on PRs — a fork must not see the key)
- **Residual, and the only thing left here: a dedicated staging org and its key as
  the `FIVENINES_API_KEY` GitHub secret.** Until it exists the workflow reports that
  it skipped rather than failing, so nothing is red — and nothing is covered

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

### Delete timeout is not operator-tunable
- `AsyncDeletionTimeout` (client.go) is a compile-time 5 minutes, passed straight
  through from instance and network device Delete. A backend slower than that
  fails `terraform destroy` where the old fire-and-forget delete succeeded, and
  there is no way to raise it
- Fix: a per-resource `timeouts { delete = "..." }` block via
  terraform-plugin-framework-timeouts, defaulting to the current 5 minutes
- Found by: /ship performance specialist, 2026-09-02

### The struct-tag policy table only guards structs someone remembered to list
- `TestUpdateInputTagsMatchTheirPolicy` is the guard that makes a `json` tag's
  clear-vs-preserve choice fail loudly, but its `specs` slice is a hand-written
  list of input types. A brand new `Create*Input`/`Update*Input` is not unclassified
  — it is invisible: the test never reflects over it, so it passes while the new
  struct has no policy at all
- Both maintenance window inputs escaped it that way in #14 until they were added
  by hand; `CreateIntegrationInput` escaped it the same way in #15 — the suite
  stayed green with the struct entirely unclassified, and it was added by hand
  again. That is twice in two resources, so the next one will escape too
- Go cannot enumerate a package's types at runtime, so the fix is either a
  registry the inputs register into, or a small `go vet`-style check that greps
  `internal/client/models.go` for `Input struct` and fails on any name absent from
  the table
- Found by: /ship, 2026-09-02

### SNMP credentials can never be cleared
- `snmp_community` / `snmp_auth_password` / `snmp_priv_password` keep `omitempty`
  because the API treats blank as "keep", so no config the provider can produce
  clears a stored credential: removing the attribute, switching `snmp_version`,
  and importing all omit the key
- A rotated-out password stays live server-side and `terraform plan` shows nothing
- Fix: confirm whether an explicit null clears one. If it does, drive the
  omit/null choice off plan-vs-state rather than a fixed struct tag; if it cannot,
  document that replacing the device is the only revocation path
- Found by: /ship security specialist, 2026-09-02

### Acceptance workflow goes green while running zero tests
- `.github/workflows/acceptance.yml` skips the run step when `FIVENINES_API_KEY`
  is unset and the job still succeeds, so a permanently unconfigured nightly is
  indistinguishable from a passing one
- Fix: fail (or `::warning::`) on the `schedule` trigger, and record the number of
  tests actually executed in `$GITHUB_STEP_SUMMARY`
- Found by: /ship testing specialist, 2026-09-02

### CI actions are pinned to mutable tags
- `acceptance.yml` is the first workflow handed a live API key with create/destroy
  rights, and pins `actions/checkout@v4`, `actions/setup-go@v5`,
  `hashicorp/setup-terraform@v3` — `setup-terraform` installs the very binary that
  later runs with the key in its environment
- Fix: pin all `uses:` refs to full commit SHAs (repo-wide, not just this file) and
  add Dependabot's `github-actions` ecosystem
- Found by: /ship security specialist, 2026-09-02

### Network device import leaves defaulted attributes null if the API omits them
- `mapNetworkDeviceToState` uses `stringOrKeep` for the defaulted SNMP attributes,
  but import starts from an empty model (`ImportStatePassthroughID` sets only
  `id`), so there is nothing to keep and they fall back to whatever the API sends
- Harmless while the API returns them, and
  `TestMapNetworkDeviceToState_ImportStartsFromEmptyState` pins that assumption
- `snmp_version` is now Optional+Computed rather than Required (#11), so it is the
  one attribute here with no schema default to fall back to. `stringOrKeep`
  collapses an unknown plan value to null rather than echoing the unknown back,
  which is what an unconfigured create would otherwise fail on
- Only an acceptance run against a real v2c device settles it; if it fails, either
  add them to `ImportStateVerifyIgnore` or fall back to the schema default
- Found by: /ship testing specialist + red team, 2026-09-02


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

## Recently closed

- **JSON `execution_graph` canonicalization** — fixed in #29. `execution_graph_json`
  is a `jsontypes.Normalized` and the "did the graph change?" check in Update goes
  through semantic comparison instead of a byte compare, so reformatting the graph
  no longer publishes a new workflow version. Note what this does NOT do yet: the
  attribute is Optional-only and the framework does not apply semantic equality
  during PlanResourceChange, so a reformat still shows an in-place update in the
  plan. That goes away when #10 makes Read populate the graph and the attribute
  becomes Optional+Computed — which is why this had to land first

- **Async 202 deletes dropped state early** — fixed in #29. `DeleteInstance` and
  `DeleteNetworkDevice` report whether the API took the async path, and the resources
  poll `GET` until it 404s (backing off, five minute cap, cancellation propagating)
  before releasing state. Destroying a host and recreating it under the same name in
  one apply used to race the backend

- **ping_key security model** — decided in #29: it stays in state, documented rather
  than engineered away. It is a Computed value, and Terraform's write-only arguments
  cover practitioner-supplied values, not server-generated ones; an ephemeral value
  could not back a usable `ping_url` either. The blast radius is one task's heartbeat
  — no read access, no configuration change. `ping_url` embeds the key verbatim and is
  now `Sensitive` too, and README gained a "Secrets in state" section

- **Pagination truncated every list at 100** (most of #5) — fixed in #9.
  `PaginationMeta` now decodes the real envelope (`current_page` / `total_pages` /
  `total_count` / `per_page`); all 8 list loops route through one `morePages`
  helper that trusts the counters only when `total_pages > 0` and otherwise walks
  until an empty page, so an unreadable envelope over-fetches by one page instead
  of silently dropping rows. Fixtures were rewritten to the real shape — the old
  ones encoded the old envelope, which is how this stayed green for months.
  ~~Still open in #5: `ListIntegrations` is a single un-paginated GET~~ — fixed in
  #15, which routes it through the same `morePages` helper. The index went
  25-per-page on 2026-09-01, so `fivenines_integrations` had been silently
  returning at most 25 channels; it arrived as a separate bug because this was
  the one loop with no meta to misread.
  Found by: Codex structured review + /ship red team, 2026-09-01

- **Uptime monitor pause/resume discards the API response** — fixed in #9.
  `PauseUptimeMonitor`/`ResumeUptimeMonitor` now return `(*UptimeMonitor, error)`
  and the resource assigns the response instead of hand-patching `Status` to a
  value (`"active"`) that isn't in the API's enum
- **Cross-field validation** — tasks in #8 (`ValidateConfig` enforces cron ⇒
  `schedule`, interval ⇒ `interval_seconds`), uptime monitors in #9
  (`ValidateConfig` + the `protocolRequirements` table: https ⇒ `url`, tcp ⇒
  `hostname`+`port`, icmp ⇒ `hostname`, dns ⇒ `dns_record_type`), status pages in
  #12 (`ValidateConfig` enforces items[].section ⇒ declared in `sections`), and
  integrations in #15 (`ValidateConfig` + the `integrationRules` table: webhook ⇒
  `url`, pagerduty ⇒ `name`+`routing_key`, pushover ⇒ `name`+`user_key`+
  `app_token`, plus a rejection for the five types the API cannot create at all).
  It matters more here than elsewhere: an apply that reached the API for a
  `pagerduty` channel would already have fired a live test alert before failing

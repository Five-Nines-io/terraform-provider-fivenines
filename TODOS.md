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
    only remaining half of this item. The wire half is already solved by the #31 tag
    convention (a write-only secret is `,omitempty`, so omission preserves — see the
    three SNMP credentials); what #7 adds is the `_set` booleans that make an
    unreadable secret's presence visible in state. `fivenines_mqtt_broker` (#18) is
    the worked example of that pairing
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
- `fivenines_host_groups` (#25) forwards `q`/`updated_since`/`order`/`direction`
  the same way. `order`/`direction` are enumerated so they are validated at plan
  time, but `q` and `updated_since` are not, and `name` is present on every
  returned group, so the same cheap server-honoured-it assertion is available

### monitors is an ordered List with a server-defined default order
- `order`/`direction` are optional, so default ordering is whatever the API
  returns. A server-side reordering shifts every index, causing spurious diffs in
  anything indexing positionally and unstable keys in an index-keyed `for_each`
- `fivenines_host_groups.host_groups` (#25) has the same shape, and its default
  order is `position` — the one column the API renumbers when any sibling moves,
  without touching `updated_at`. Indexing it positionally is the least stable of
  the two
- Fix: a Set, or make `order` mandatory

### Offset pagination skips a row when an earlier one is deleted mid-walk
- Every index walk (12 of them, including the two that back a resource Read:
  `walkAPITokens` and `walkEnrollmentTokens`) pages by `page`/`per_page`. Deleting
  a row sorted BEFORE the one being sought, between two page requests, re-slices
  the remaining rows and shifts the boundary row onto a page already read — so it
  is never served
- Confirmed empirically, 2026-09-02: page 1 = [1,2] of 4, row 1 deleted, page 2
  serves [4,5]. Row 3 exists and `GetEnrollmentToken(3)` reports 404
- Consequence for the two token resources specifically: a false 404 removes the
  resource from state, and the next apply mints a replacement while the ORIGINAL
  token stays live — a working credential Terraform no longer tracks
- `direction=asc` on the enrollment token walk (#17) closes the INSERTION half of
  this (a row created mid-walk is appended, not shifted) but not the deletion half
- Needs >100 rows of that type before any of it is reachable, since `per_page` is
  already at the API maximum
- No clean fix available client-side: the API has no cursor parameter and does not
  expose `id` as a sortable column, so keyset pagination cannot be expressed. A
  server-side cursor is the real fix. A cheaper partial one is to confirm a
  synthetic 404 with a second walk before removing a resource from state
- Found by: /ship adversarial review (Codex), 2026-09-02

### morePages trusts that the server honours `page`
- The unreadable-envelope fallback walks until an empty page. If an index ever
  returns no recognised meta AND ignores `page` — the shape `ListIntegrations`
  had at this same API until 2026-09-01 — the loop runs to `maxListPages` (1000)
  and appends the same rows 1000 times before erroring
- #16 makes this worth more than it was: `GetAPIToken` is the provider's first
  walk that runs per managed resource per refresh rather than once per data
  source read, so the cost multiplies by the number of tokens in the
  configuration. Its early exit caps the common case at one page, but only when
  the token is found
- Fix: a repeated-page guard in `morePages` (remember the first row of the
  previous page, stop when it repeats). Cheap, and it belongs in the shared
  helper rather than in one caller
- Found by: /ship performance specialist, 2026-09-02

### ImportState duplicates the same int64 parse eight times
- `host_group`, `status_page`, `network_device`, `workflow`, `task`,
  `maintenance_window`, `api_token` and now `enrollment_token` each carry the same
  seven-line `strconv.ParseInt` + `SetAttribute` block
- `enrollment_token` appends a warning diagnostic after the parse (an imported
  token has no value and never will), so the helper needs to return control rather
  than own the whole function body
- Fix: `importInt64ID(ctx, req, resp)` in `internal/resources/mapping.go`, where
  the other shared mapping helpers live. ~40 lines repo-wide
- Found by: /ship simplification specialist, 2026-09-02

### Unbounded rate-limit backoff
- `doRequest` derives the 429 wait from `X-RateLimit-Reset` via `time.Until` with
  a floor but no ceiling, so a skewed reset header parks a plan indefinitely
- `maxListPages` (1000) x `per_page` (100) also means up to 100k structs and 1000
  sequential 30s requests before the ceiling reports anything
- Fix: cap the backoff, and bound total records rather than pages

### Unit tests cannot see Terraform's plan validation
- Driving `Create`/`Update` directly, the way every `internal/resources/*_test.go`
  does, skips the step where Terraform compares the planned object to the
  configuration. A provider can be structurally unable to produce a valid plan
  while that entire suite stays green
- Proof: #13 shipped a plan modifier that set `position` to unknown whenever it
  changed. Every unit test passed. Real Terraform rejected it for every
  practitioner who configured a position, before the API was called at all —
  `planned value cty.UnknownVal(cty.Number) does not match config value
  cty.NumberIntVal(5)`. A provider may not plan unknown over a known config value,
  even for Optional+Computed
- #13 added `internal/provider/host_group_plan_test.go`: real Terraform against an
  httptest server, so it needs no organisation and no key and runs in `make test`
  wherever the terraform binary is
- #18 added the MQTT pair, and #16 added `api_token_plan_test.go` (17 cases) and
  extracted the harness the three had copied byte-for-byte into
  `internal/provider/plan_test_harness_test.go`. The remaining resources have no
  equivalent — their plan-time behaviour is only covered by the TF_ACC suite,
  which does not run in CI and needs a staging org that does not exist yet
- Fix: extend the hermetic pattern to the resources whose plan behaviour is
  non-trivial — uptime monitor (`protocolForbidden`), status page (items/sections
  semantics), task (`schedule_type` switching)
- Found by: /ship Codex structured review, 2026-09-02

### Required name attributes are overwritten from the API response
- `mapXToState` writes the API's `name` into state for instances, tasks, network
  devices, status pages, host groups and enrollment tokens, but `name` is
  `Required` (not Computed) on all of them, so Terraform pins the applied value
  to the exact config string
- If the API ever normalises a name — trims it, folds case, collapses whitespace —
  the apply aborts with "Provider produced inconsistent result after apply", and
  on a create the resource exists server-side against a failed apply
- Unverified either way: no evidence the API normalises today. Host groups make it
  more likely to matter, since their names are documented unique per organisation
  *case-insensitively*, which is the kind of constraint normalisation usually
  accompanies
- Enrollment tokens (#17) raise the stakes rather than the likelihood: `name` is
  `RequiresReplace` there, so a normalisation would not merely fail one apply — it
  would make every subsequent refresh propose replacing every token, revoking live
  fleet credentials and minting values nothing has deployed
- Fix, if confirmed: keep the planned name rather than the response value, the way
  #13 keeps the planned position
- Found by: /ship adversarial review, 2026-09-02

### The ETag retry loop retries instantly, three times, with no backoff
- Every resource does GET(ETag) then PATCH(If-Match), retrying a 412 immediately
  up to three times. All three attempts land inside the same contention window
  that caused the first failure
- Host groups make this sharper than the rest: any group's move renumbers every
  other group, so at parallelism 10 one group's PATCH invalidates the ETag a
  sibling just fetched. #13 added a 412-specific diagnostic telling the user to
  re-run, but the retries themselves are still unjittered
- Fix: jittered backoff and a higher attempt count, applied across all resources
  rather than one — this is the same call site copy-pasted seven times
- Found by: /ship adversarial review, 2026-09-02

### sanitizeETag only understands nginx, and a stripped ETag silently disables If-Match
- `sanitizeETag` rewrites nginx's `-gzip` suffix and nothing else. A CDN that
  rewrites a strong ETag to `W/"..."` produces an If-Match that can never match
  under strong comparison: permanent 412, three wasted round trips, dead apply
- Worse, a proxy that drops `ETag` entirely leaves `etag == ""`, and every
  `Update*` skips the `If-Match` header when the ETag is empty — the write goes
  through unconditionally, with no warning, and the optimistic concurrency the
  whole retry loop exists to provide is silently gone
- Fix: treat a missing ETag as a condition worth surfacing rather than a reason to
  drop the precondition, and decide explicitly what a weak ETag should do
- Found by: /ship adversarial review, 2026-09-02

### API tokens are visible only to the user who minted them
- `GET /api/v1/api_tokens` is scoped to the calling user, and there is no show
  endpoint, so `fivenines_api_token` cannot distinguish "not mine" from "deleted"
- Rotating `FIVENINES_API_KEY` to a DIFFERENT user's token makes the next plan
  propose recreating every token in the configuration, while the originals stay
  live and unmanaged. Documented on the resource; there is nothing the provider
  can do about it from here
- Fix would have to be server-side: an org-scoped read, or a show endpoint that
  404s honestly
- Found by: /ship API contract specialist, 2026-09-02

### Dashboard sections write last-writer-wins, unlike every other resource

- Every other resource does GET(ETag) then PATCH(If-Match). Sections cannot: the
  API publishes no `GET /dashboards/{id}/sections/{id}`, so a section's ETag only
  ever comes back in the response to a PATCH that already happened. There is
  nothing to pass to `If-Match` on the first write of a plan
- `UpdateDashboardSection` therefore takes no etag argument at all — deliberately,
  because a parameter that is always `""` reads like an oversight. The cost is
  real: two concurrent renames, or a rename racing the resequencing that any
  sibling's `position` write triggers, and the later one wins silently. `Read`
  has the same hole, since it reconstructs the section from the dashboard
  definition rather than from a section endpoint
- This is the ONE resource in the provider whose concurrency semantics differ, so
  it is worth naming rather than burying
- Fix: carry the ETag from the previous PATCH response in private state, so the
  second and later writes of a session are protected even though the first cannot
  be — or ask the server for a section `GET`, which fixes it properly and also
  removes the whole-dashboard fetch `Read` currently pays for
- Found by: /ship, 2026-09-02

### Dashboard clone and share are actions with no Terraform surface

- `POST /dashboards/{id}/clone` and `POST|DELETE /dashboards/{id}/share` are
  actions, not resources, so #19 exposes neither
- `shared` and `share_url` are Computed on `fivenines_dashboard`, so a fleet can
  be AUDITED from Terraform but not published or revoked from it
- Decide whether share belongs in the provider at all before adding it: the slug
  IS the credential, and putting it under `terraform apply` puts it in state and
  in plan output. `share_url` is already marked Sensitive for that reason
- Found by: /ship, 2026-09-02

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
  again. #13 then made it three for three: `CreateHostGroupInput` and
  `UpdateHostGroupInput` were both invisible to the guard, and the whole suite was
  green with neither classified. Predicting the next one will escape is no longer
  a prediction
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

### The list data sources have no limit
- `fivenines_uptime_monitors` exposes `order`/`direction` but no `limit`/`per_page`,
  and the client always walks to the last page. "The 5 most recently updated
  monitors" still pulls every monitor in the org on every plan and refresh
- `fivenines_host_groups` (#25) is the same, though it costs less: a host group
  list is small and its `q` filter narrows server-side before the walk
- Fix: optional `limit`, threaded into the ListXOptions struct, stopping the walk
  once `len(all) >= limit` and sizing per_page as `min(100, limit)`

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

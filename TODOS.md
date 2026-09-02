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
  - `dns_expected_records`: `[]` normalises to null server-side, but `omitempty`
    can never send it (#9)
  - Instance secrets: blank-means-keep + `_set` booleans (#7)
- Fix: pointer types + explicit-empty handling per field, map API null →
  `types.StringNull()`
- The dashboard resources (#19) already do this and are the reference: pointer
  fields WITHOUT `omitempty` so a nil marshals to `null`, `*[]string` for target
  lists so "not mentioned" and "explicitly empty" stay distinguishable, and
  Optional-without-Computed wherever the API never materializes a default

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
- Needs: dedicated staging org + `FIVENINES_API_KEY` in GitHub Secrets,
  terraform-plugin-testing full CRUD lifecycle tests

### Uptime monitor pause/resume discards the API response
- `uptimeMonitorAction` closes the body and returns only `error`, then
  `uptime_monitor_resource.go` hand-patches `monitor.Status`
- The server renders the updated monitor back from both `pause!` and `resume!`, so
  state keeps stale computed fields until the next refresh
- Fix: mirror `taskAction` (#8) — return `(*UptimeMonitor, error)` and assign the
  response instead of patching Status
- Found by: /ship pre-landing review, 2026-09-01

### Dashboard sections write last-writer-wins
- Sections have no `GET` of their own — the dashboard definition lists them — so
  a section ETag only ever comes back from a `PATCH` response
- `UpdateDashboardSection` therefore sends no `If-Match`, unlike every other
  resource, and a concurrent rename or reorder is silently overwritten
- Fix: carry the ETag from the previous `PATCH` response in private state, or
  ask the server for a section `GET`

### Dashboard clone and share are not modelled
- `POST /dashboards/{id}/clone` and `POST|DELETE /dashboards/{id}/share` are
  actions, not resources, so v1 exposes neither (#19)
- `shared` and `share_url` are Computed on `fivenines_dashboard`, so a fleet can
  be AUDITED from Terraform but not published or revoked from it
- Decide whether share belongs in the provider at all: the slug is a credential,
  and putting it under `terraform apply` puts it in state and in plan output

### Instance delete 202 handling
- `DELETE /instances` returns 202 (async); provider drops state immediately
  without polling
- Fix: poll `GET /instances/:id` until 404 or timeout (matters for
  delete-then-recreate of the same host)

## P2 — Nice to have

### Cross-field validation
- Tasks are done (#8): `ValidateConfig` in `task_resource.go` enforces cron ⇒ `schedule`
  and interval ⇒ `interval_seconds`, plus `interval_seconds >= 1` to match the model
- Uptime monitors still have none: https ⇒ `url`, tcp ⇒ `hostname`+`port`,
  dns ⇒ `dns_record_type`
- The API 422s cleanly, so this is UX only: fail at plan time, not apply time

### ping_key security model
- Task `ping_key` is Computed + Sensitive but persisted in state; the API still
  returns it on every read
- Decide: acceptable for the threat model, or move to write-only/ephemeral

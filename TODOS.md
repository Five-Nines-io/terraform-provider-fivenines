# Terraform Provider TODOs

## P0 — Must fix (Codex + Eng Review findings)

### Immutable fields need RequiresReplace
- `protocol` on uptime monitors is configurable in schema but not in the Update input model
- Changing it in .tf files currently no-ops or causes infinite drift
- Fix: confirm whether the API permits `protocol` on PATCH. If it does, wire it into
  `UpdateUptimeMonitorInput` (as was done for task `schedule_type`); if not, add
  `stringplanmodifier.RequiresReplace()`
- Found by: Codex outside voice review, 2026-03-25
- Task `schedule_type` half resolved 2026-09-01: the API shares its permit list between
  create and update, so it is now updatable in place (#8)

### JSON execution_graph canonicalization
- Raw JSON string attribute will produce false diffs on every plan due to key ordering/whitespace
- Fix: use `jsondiff` normalization or implement `PlanModifier` that canonicalizes JSON before comparison
- Blocked by: workflow version management implementation

### Context propagation in HTTP client
- HTTP requests use `http.NewRequest` without context — ignores Terraform cancellation
- `time.Sleep` in backoff blocks ctrl+C
- Fix: pass `context.Context` through all client methods, use `http.NewRequestWithContext`, use `select` with timer for backoff

### Unset semantics / nil vs empty
- `omitempty` + nil suppression means users can't reliably clear optional fields
- Reads normalize absence to `""` instead of null → plan drift
- Fix: audit all optional fields, use pointer types consistently, map API null → types.StringNull()

## P1 — Should fix

### Acceptance tests (TF_ACC)
- Zero end-to-end coverage. Unit tests catch serialization bugs but not API contract drift.
- Needs: dedicated staging org + FIVENINES_API_KEY in GitHub Secrets
- Use terraform-plugin-testing framework for full CRUD lifecycle tests
- Priority: HIGH — Codex flagged deferring these as the wrong call

### Uptime monitor pause/resume discards the API response
- `uptimeMonitorAction` (client.go) closes the body and returns only `error`, then
  `uptime_monitor_resource.go` hand-patches `monitor.Status`
- The server renders the updated monitor back from both `pause!` and `resume!`, so state
  keeps stale computed fields until the next refresh
- Fix: mirror what `taskAction` now does — return `(*UptimeMonitor, error)` and assign the
  response instead of patching Status
- Found by: /ship pre-landing review, 2026-09-01 (task-side equivalent fixed in #8)

### Instance delete 202 handling
- `DELETE /instances` returns 202 Accepted (async deletion)
- Provider drops state immediately without polling for completion
- Fix: poll `GET /instances/:id` until 404 or timeout

## P2 — Nice to have

### Cross-field validation
- No protocol-specific required field validation on uptime monitors (dns needs dns_record_type, etc.)
- Fix: implement `ValidateConfig` on the resource, as `task_resource.go` now does for
  schedule_type (cron needs schedule, interval needs interval_seconds)

### ping_key security model
- Task ping_key is marked `Sensitive: true` but persisted in state file
- Evaluate if this is acceptable for threat model or if it should be write-only

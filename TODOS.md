# Terraform Provider TODOs

> API-parity work (the 2026-09 server catch-up) lives on GitHub — issues #5–#26,
> tracking in #27. Cross-cutting engineering debt is tracked in #29. This file
> keeps what neither of those owns.

## P1 — Should fix

### Uptime monitor pause/resume discards the API response
- `uptimeMonitorAction` closes the body and returns only `error`, then
  `uptime_monitor_resource.go` hand-patches `monitor.Status`
- The server renders the updated monitor back from both `pause!` and `resume!`, so
  state keeps stale computed fields until the next refresh
- Fix: mirror `taskAction` (#8) — return `(*UptimeMonitor, error)` and assign the
  response instead of patching Status
- Found by: /ship pre-landing review, 2026-09-01

## Blocked on infrastructure

### Enable the acceptance test workflow

The suite (`internal/provider/*_test.go`) and its CI job
(`.github/workflows/acceptance.yml`) are in place, but they need somewhere to
run:

- [ ] Create a dedicated staging organisation — the tests create and destroy
      real instances, monitors, tasks, devices, status pages and workflows
- [ ] Add its key as the `FIVENINES_API_KEY` GitHub secret; optionally set the
      `FIVENINES_BASE_URL` repository variable to target a non-production API

Until the secret exists the workflow reports that it skipped instead of
failing, so nothing is red in the meantime — but nothing is covered either, and
that is exactly how the pagination meta drift (#5) survived for months.

## Closed

The six cross-cutting items this file used to carry (#29) are done and live in
git history rather than here: unset/null semantics, `execution_graph` JSON
canonicalization, the acceptance suite, async 202 delete handling, cross-field
config validators — the task half via #8's `ValidateConfig`, uptime monitors via
`ConfigValidators` — and the `ping_key` security model (decided: it stays in
state, documented under "Secrets in state" in the README).

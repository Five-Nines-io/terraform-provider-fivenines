# terraform-provider-fivenines

Terraform provider for the FiveNines monitoring API.

Cross-cutting engineering debt and deferred work lives in `TODOS.md`; API-parity
work is tracked in GitHub issues #5-#26.

## Build and test

- `make build` / `make test` / `make fmt` — standard targets.
- `make testacc` runs the acceptance suite (`internal/provider/*_test.go`) against
  a live organisation, creating and destroying real resources. It needs `TF_ACC=1`,
  a staging `FIVENINES_API_KEY` and the `terraform` CLI on `PATH`; without `TF_ACC`
  those tests skip, so `make test` stays offline.
- `make docs` runs `tfplugindocs generate`. It derives the provider name from the
  directory name, so from a checkout not named `terraform-provider-fivenines`
  (a git worktree, for example) you must pass both flags or it deletes `docs/`
  and fails:
  `tfplugindocs generate --provider-name fivenines --rendered-provider-name terraform-provider-fivenines`
- CI regenerates docs and fails on any diff, so commit `docs/` alongside schema
  or `examples/` changes.

## Releases and versioning

- There is no `VERSION` file and no `CHANGELOG.md`, by design. `.goreleaser.yml`
  reads `{{ .Version }}` from the git tag and generates release notes from the
  commit subjects since the last tag (excluding `^docs:`, `^test:`, `^chore:`).
  Do not create either file — commit subjects are the changelog, so write the
  subject for the person reading the release page.
- Cut a release by tagging: `git tag vX.Y.Z && git push origin vX.Y.Z`. The
  Terraform Registry picks the release up automatically.

## Skill routing

When the user's request matches an available skill, invoke it via the Skill tool. When in doubt, invoke the skill.

Key routing rules:
- Product ideas/brainstorming → invoke /office-hours
- Strategy/scope → invoke /plan-ceo-review
- Architecture → invoke /plan-eng-review
- Design system/plan review → invoke /design-consultation or /plan-design-review
- Full review pipeline → invoke /autoplan
- Bugs/errors → invoke /investigate
- QA/testing site behavior → invoke /qa or /qa-only
- Code review/diff check → invoke /review
- Visual polish → invoke /design-review
- Ship/deploy/PR → invoke /ship or /land-and-deploy
- Save progress → invoke /context-save
- Resume context → invoke /context-restore
- Author a backlog-ready spec/issue → invoke /spec

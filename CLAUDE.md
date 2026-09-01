# terraform-provider-fivenines

Terraform provider for the FiveNines monitoring API.

## Build and test

- `make build` / `make test` / `make fmt` — standard targets.
- `make docs` runs `tfplugindocs generate`. It derives the provider name from the
  directory name, so from a checkout not named `terraform-provider-fivenines`
  (a git worktree, for example) you must pass both flags or it deletes `docs/`
  and fails:
  `tfplugindocs generate --provider-name fivenines --rendered-provider-name terraform-provider-fivenines`
- CI regenerates docs and fails on any diff, so commit `docs/` alongside schema
  or `examples/` changes.

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

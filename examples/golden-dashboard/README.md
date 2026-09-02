# Golden dashboard

One dashboard layout, instantiated per environment.

`modules/golden_dashboard` declares a dashboard, two sections and a handful of
panels as ordinary resources. `main.tf` calls it twice — staging and production
— passing each environment's instance and uptime-monitor ids.

```bash
export FIVENINES_API_KEY=fn_live_...

terraform init
terraform apply \
  -var='production_host_ids=["<instance-uuid>"]' \
  -var='production_monitor_ids=["<monitor-uuid>"]'
```

## Why panels are resources, not a template

`fivenines_dashboard` also takes a `template_slug`, which builds one of the
gallery's dashboards in a single call. That is the faster way to get something
on screen, but the panels it creates are not managed by Terraform: the API drops
panels the organization cannot feed, and later edits to them are invisible to a
plan. Declaring panels — as this module does — is what makes "add a panel to
every environment" a one-line diff.

## Two things worth knowing

**Sections renumber each other.** `position` is the index a section should end
up at, and the API resequences its siblings to match. Creating several sections
at once can therefore take a second `apply` to settle into the declared order.

**Panels reference their section by name.** That is what makes a dashboard
definition portable between organizations, and referencing
`fivenines_dashboard_section.compute.name` is also what tells Terraform to
create the section before the panels that live in it.

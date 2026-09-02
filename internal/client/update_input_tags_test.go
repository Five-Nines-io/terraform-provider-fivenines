package client

import (
	"reflect"
	"strings"
	"testing"
)

// The clear-vs-preserve decision lives in the json tag, and every call site
// that builds these structs looks identical — `stringPtr(plan.X)` whether the
// field clears or preserves. Nothing in the compiler ties a tag to the schema
// classification it encodes, so adding a field and reflexively copying the
// neighbouring `,omitempty` silently turns "user can clear this" into "user
// cannot", or worse, turns a write-only credential into one that gets wiped by
// an unrelated update.
//
// This table is that missing guard: every field of every update input is
// classified once, here, and a new field fails the build until it is added.
func TestUpdateInputTagsMatchTheirPolicy(t *testing.T) {
	const (
		clears    = "clears on nil (Optional-only: the provider owns it)"
		preserves = "preserves on nil (Optional+Computed, defaulted, or write-only)"
	)

	specs := []struct {
		input  interface{}
		policy map[string]string
	}{
		{
			input: UpdateInstanceInput{},
			policy: map[string]string{
				"display_name": preserves, "description": preserves,
				"enabled": preserves, "maintenance_mode": preserves,
			},
		},
		{
			input: UpdateTaskInput{},
			policy: map[string]string{
				"name": preserves, "schedule_type": preserves,
				// The API keeps the counterpart it stored across a schedule_type
				// switch (#8), so neither may be cleared.
				"schedule": preserves, "interval_seconds": preserves,
				"grace_period_minutes": preserves, "time_zone": preserves,
				"host_id": clears,
			},
		},
		{
			input: UpdateNetworkDeviceInput{},
			policy: map[string]string{
				"name": preserves, "ip_address": preserves, "device_type": preserves,
				"polling_interval": preserves, "snmp_version": preserves,
				"snmp_security_level": preserves, "snmp_auth_protocol": preserves,
				"snmp_priv_protocol": preserves,
				// Write-only, blank-means-keep server-side: an explicit null
				// would wipe a working credential on an unrelated update.
				"snmp_community": preserves, "snmp_auth_password": preserves,
				"snmp_priv_password": preserves,
				"polling_host_id":    clears, "snmp_username": clears,
			},
		},
		{
			input: UpdateWorkflowInput{},
			policy: map[string]string{
				"name": preserves, "description": preserves, "interval_seconds": preserves,
			},
		},
		{
			input: UpdateStatusPageInput{},
			policy: map[string]string{
				"name": preserves, "description": preserves, "public": preserves,
				"uptime": preserves, "custom_domain": preserves,
				"custom_domain_enabled": preserves, "custom_footer": preserves,
				"custom_footer_enabled": preserves, "incidents_history_enabled": preserves,
				"theme_variant": preserves,
				// A pointer-to-slice: nil omits, &[] sends the explicit [] that
				// is the only way to empty a page.
				"items": preserves,
			},
		},
		{
			input: UpdateUptimeMonitorInput{},
			policy: map[string]string{
				"name": preserves, "protocol": preserves, "url": preserves,
				"hostname": preserves, "http_method": preserves, "ip_version": preserves,
				"interval_seconds": preserves, "timeout_seconds": preserves,
				"confirmation_count": preserves, "keyword_absent": preserves,
				"follow_redirects": preserves, "expected_status_codes": preserves,
				"recovery_count": preserves, "probe_region_ids": preserves,
				// Protocol-scoped: switching protocol has to actively clear the
				// previous protocol's settings (#9).
				"port": clears, "keyword": clears, "dns_record_type": clears,
				"dns_expected_records": clears, "custom_headers": clears,
				"custom_body": clears, "content_type": clears,
			},
		},
	}

	for _, spec := range specs {
		typ := reflect.TypeOf(spec.input)
		t.Run(typ.Name(), func(t *testing.T) {
			seen := make(map[string]bool, typ.NumField())

			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				tag := field.Tag.Get("json")
				if tag == "" || tag == "-" {
					continue
				}
				name, opts, _ := strings.Cut(tag, ",")
				seen[name] = true

				want, classified := spec.policy[name]
				if !classified {
					t.Errorf("%s.%s (json:%q) is not classified — add it to the policy table "+
						"as %q or %q, whichever its Terraform attribute calls for",
						typ.Name(), field.Name, name, clears, preserves)
					continue
				}

				got := preserves
				if !strings.Contains(opts, "omitempty") {
					got = clears
				}
				if got != want {
					t.Errorf("%s.%s (json:%q) %s, but the policy says it %s",
						typ.Name(), field.Name, tag, got, want)
				}
			}

			for name := range spec.policy {
				if !seen[name] {
					t.Errorf("%s: the policy table names %q, but the struct has no such field",
						typ.Name(), name)
				}
			}
		})
	}
}

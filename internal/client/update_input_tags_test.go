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
// This table is that missing guard: every field of every create and update
// input is classified once, here, and a new field fails this test until it is
// added — which is the closest thing to a compile error the tags can get.
func TestUpdateInputTagsMatchTheirPolicy(t *testing.T) {
	const (
		clears    = "clears on nil (Optional-only: the provider owns it)"
		preserves = "preserves on nil (Optional+Computed, defaulted, or write-only)"
		always    = "always sent (Required: no nil case and no omitempty)"
		dropsZero = "dropped when zero-valued (only safe when \"\" or 0 is not a legal config value)"
	)

	specs := []struct {
		input  interface{}
		policy map[string]string
	}{
		{
			input: UpdateInstanceInput{},
			policy: map[string]string{
				"display_name": preserves, "enabled": preserves,
				"maintenance_mode": preserves,
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
				"theme_variant": preserves, "contact_url": preserves,
				"subscriptions_enabled": preserves, "search_indexing_enabled": preserves,
				"uptime_green_tolerance_seconds": preserves, "uptime_window_days": preserves,
				// Pointers-to-slice: nil omits, &[] sends the explicit [] that is
				// the only way to empty a page or drop every section.
				"items": preserves, "sections": preserves,
				// The API never echoes the logo back, so its Terraform attribute
				// is Optional-only and the configuration owns it end to end:
				// dropping `logo` has to delete the image, which only an explicit
				// null can do.
				"logo": clears,
			},
		},
		{
			input: UpdateStatusPageMaintenanceWindowInput{},
			policy: map[string]string{
				// Required attributes: the plan always holds a value, so nil never
				// reaches the tag and omitempty only documents that.
				"title": preserves, "starts_at": preserves, "ends_at": preserves,
				// Optional-only, so the configuration owns it: dropping `body` has
				// to clear it, which only an explicit null can do.
				"body": clears,
				// Pointer-to-slice, same shape as the status page's items. The
				// resource never sends nil here — an unconfigured list becomes the
				// explicit [] that clears — but nil still has to mean "omit"
				// rather than "null", because the API documents [] as the way to
				// clear and says nothing about accepting null.
				"affected_items": preserves,
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

	// Create inputs follow the same rule, and the same copy-a-neighbour trap:
	// CreateStatusPageInput dropped an explicit `description = ""` for exactly
	// that reason until it was pointerised.
	specs = append(specs, struct {
		input  interface{}
		policy map[string]string
	}{
		input: CreateStatusPageInput{},
		policy: map[string]string{
			"name": always, "description": preserves, "public": preserves,
			"uptime": preserves, "custom_domain": preserves,
			"custom_domain_enabled": preserves, "custom_footer": preserves,
			"custom_footer_enabled": preserves, "incidents_history_enabled": preserves,
			"theme_variant": dropsZero, "items": preserves, "sections": preserves,
			"contact_url": preserves, "subscriptions_enabled": preserves,
			"uptime_green_tolerance_seconds": preserves, "uptime_window_days": preserves,
			"search_indexing_enabled": preserves,
			// Nothing to clear on a page that does not exist yet, so the create
			// side omits an absent logo rather than sending an explicit null.
			"logo": preserves,
		},
	}, struct {
		input  interface{}
		policy map[string]string
	}{
		input: CreateStatusPageMaintenanceWindowInput{},
		policy: map[string]string{
			"title": always, "starts_at": always, "ends_at": always,
			// Nothing to clear on a window that does not exist yet, so create
			// omits an absent body and an absent item list rather than sending
			// an explicit null.
			"body": preserves, "affected_items": preserves,
		},
	}, struct {
		input  interface{}
		policy map[string]string
	}{
		input: CreateInstanceInput{},
		policy: map[string]string{
			"display_name": always, "enabled": preserves, "maintenance_mode": preserves,
		},
	}, struct {
		input  interface{}
		policy map[string]string
	}{
		input: CreateTaskInput{},
		policy: map[string]string{
			"name": always, "schedule_type": always, "schedule": dropsZero,
			"interval_seconds": preserves, "grace_period_minutes": preserves,
			"time_zone": dropsZero, "host_id": dropsZero,
		},
	}, struct {
		input  interface{}
		policy map[string]string
	}{
		input: CreateWorkflowInput{},
		policy: map[string]string{
			"name": always, "description": dropsZero, "interval_seconds": preserves,
		},
	}, struct {
		input  interface{}
		policy map[string]string
	}{
		input: CreateNetworkDeviceInput{},
		policy: map[string]string{
			"name": always, "ip_address": always, "polling_host_id": preserves,
			"device_type": dropsZero, "polling_interval": preserves,
			"snmp_version": dropsZero, "snmp_community": dropsZero,
			"snmp_username": dropsZero, "snmp_security_level": dropsZero,
			"snmp_auth_protocol": dropsZero, "snmp_auth_password": dropsZero,
			"snmp_priv_protocol": dropsZero, "snmp_priv_password": dropsZero,
		},
	}, struct {
		input  interface{}
		policy map[string]string
	}{
		input: CreateUptimeMonitorInput{},
		policy: map[string]string{
			"name": always, "protocol": always, "url": dropsZero,
			"hostname": dropsZero, "port": preserves, "http_method": dropsZero,
			"ip_version": dropsZero, "interval_seconds": preserves,
			"timeout_seconds": preserves, "confirmation_count": preserves,
			"keyword": dropsZero, "keyword_absent": preserves,
			"follow_redirects": preserves, "expected_status_codes": preserves,
			"probe_region_ids": preserves, "dns_record_type": dropsZero,
			"dns_expected_records": preserves, "custom_headers": preserves,
			"custom_body": dropsZero, "content_type": dropsZero,
			"recovery_count": preserves,
		},
	})

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

				// Pointers, slices and maps have a nil case, so their tag says
				// whether nil clears or preserves. A non-nilable field has no nil
				// case — but with omitempty its ZERO value still vanishes from the
				// body, which is how `description = ""` got dropped on create.
				got := always
				switch field.Type.Kind() {
				case reflect.Ptr, reflect.Slice, reflect.Map:
					got = preserves
					if !strings.Contains(opts, "omitempty") {
						got = clears
					}
				default:
					if strings.Contains(opts, "omitempty") {
						got = dropsZero
					}
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

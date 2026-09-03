package resources

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- API response → Terraform state ---
//
// The rule is that an API null becomes a Terraform null. Flattening null to ""
// or 0 makes an unset optional attribute drift on every plan, and Terraform
// rejects the apply outright ("was null, but now cty.StringVal(\"\")") for
// Optional-only attributes.

// optionalString maps a nullable API string onto a Terraform value.
func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// optionalNonEmptyString is optionalString for the fields where the API uses ""
// and null interchangeably to mean "unset".
func optionalNonEmptyString(s *string) types.String {
	if s == nil || *s == "" {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// stringOrKeep is optionalString for attributes that carry a schema default or
// are Required: a null from the API keeps whatever the plan already holds
// rather than wiping a known value to null, which Terraform rejects as an
// inconsistent result.
//
// An unknown plan value is the one thing it must not keep. A schema default
// resolves unknown at plan time, so for a defaulted or Required attribute
// `current` is always known and the guard below never fires. An Optional+
// Computed attribute with NO default (snmp_version) is the exception: on create
// the plan holds unknown, and echoing that back leaves an unknown in state,
// which fails the apply outright with "Provider produced inconsistent result
// after apply". There is nothing to keep in that case, so it reports null.
func stringOrKeep(s *string, current types.String) types.String {
	if s == nil {
		if current.IsUnknown() {
			return types.StringNull()
		}
		return current
	}
	return types.StringValue(*s)
}

// optionalInt64 maps a nullable API integer onto a Terraform value.
func optionalInt64(v *int64) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*v)
}

// optionalBool maps a nullable API boolean onto a Terraform value. A stored
// false and an unset field are different answers, so this must not collapse the
// two the way a plain bool would.
func optionalBool(v *bool) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*v)
}

// optionalFloat64 maps a nullable API number onto a Terraform value.
func optionalFloat64(v *float64) types.Float64 {
	if v == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*v)
}

// stringListValue renders a list the API always sends, where an empty array is
// a real answer rather than an absent one.
func stringListValue(values []string) types.List {
	elements := make([]attr.Value, len(values))
	for i, v := range values {
		elements[i] = types.StringValue(v)
	}
	list, _ := types.ListValue(types.StringType, elements)
	return list
}

// optionalStringListValue is stringListValue for a list the API sends as null
// when it stores nothing for it — which is a different answer from [].
func optionalStringListValue(values []string) types.List {
	if values == nil {
		return types.ListNull(types.StringType)
	}
	return stringListValue(values)
}

// --- Import IDs ---

// parseCompositeInt64ID splits the "<parent-id>:<child-id>" form that every
// resource nested under a numeric parent imports with. The labels only shape the
// error message, which is the whole value of the function: a practitioner who
// pastes a bare id gets told the shape the resource actually wants.
func parseCompositeInt64ID(importID, parentLabel, childLabel string) (int64, int64, error) {
	parts := strings.Split(importID, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, fmt.Errorf("expected %q to be in the form <%s>:<%s>", importID, parentLabel, childLabel)
	}
	parentID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse %s %q as an integer: %s", parentLabel, parts[0], err)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse %s %q as an integer: %s", childLabel, parts[1], err)
	}
	return parentID, id, nil
}

// --- Terraform plan → API update input ---
//
// One shape for every attribute: a pointer when the plan holds a value, nil
// otherwise. Whether nil means "leave it alone" or "clear it" is a property of
// the field, not of the call, so it lives in the struct tag on the update input
// (see UpdateUptimeMonitorInput for the convention):
//
//   - `json:"field,omitempty"` — nil omits the key, so the server keeps what it
//     stores. For Optional+Computed attributes and write-only secrets.
//   - `json:"field"` — nil marshals as an explicit null, so the server clears
//     the value. For Optional-only attributes the provider owns end to end.
//   - `*[]T` / `*map[K]V` with omitempty — nil omits the key, but a pointer to
//     an empty value marshals as an explicit [] / {} and clears. For list and
//     map attributes where `x = []` is itself a legal config, which a plain
//     slice with omitempty cannot express.

func stringPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func intPtr(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

// splitCSVUnits mirrors the API's parse of a comma-separated unit list —
// split on commas, strip, drop blanks, dedupe keeping first occurrence — so
// two spellings compare equal exactly when the server would store the same
// list for both.
func splitCSVUnits(s string) []string {
	seen := map[string]bool{}
	var units []string
	for _, part := range strings.Split(s, ",") {
		unit := strings.TrimSpace(part)
		if unit == "" || seen[unit] {
			continue
		}
		seen[unit] = true
		units = append(units, unit)
	}
	return units
}

// csvOrKeep keeps the configured spelling of a comma-separated list when it
// names the same units the API re-rendered. The server parses what it is
// sent but serializes canonically ("a.service,b.service" comes back as
// "a.service, b.service"), so storing the server's rendering verbatim fails
// the apply — the planned config value and the applied one differ — for any
// spelling but the canonical one. A genuinely different list, or a value on
// a host the configuration does not manage, still reports the server.
func csvOrKeep(apiValue *string, current types.String) types.String {
	if current.IsUnknown() || current.IsNull() {
		return optionalString(apiValue)
	}
	// The serializer renders the empty list as "", but a null is compared as
	// the same empty list rather than short-circuiting: a configuration
	// spelling the empty list as "" or " , " must not read back as null.
	rendered := ""
	if apiValue != nil {
		rendered = *apiValue
	}
	if slicesEqual(splitCSVUnits(current.ValueString()), splitCSVUnits(rendered)) {
		return current
	}
	return optionalString(apiValue)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// storedSecret reconciles a write-only credential with the API's only
// readable signal about it, the paired `<name>_set` boolean. While a secret
// is stored, the configured value stays in state as written (the API cannot
// echo it). When the API reports none stored — cleared in the dashboard, or
// refused server-side (a blank, or a Linux-only credential on a Windows
// host) — keeping the configured value would leave `terraform plan` claiming
// "No changes" against a host that has no credential, so the attribute goes
// null: a managed secret then diffs and is re-sent, and a refused one fails
// the apply loudly instead of recording a value the server never kept.
func storedSecret(current types.String, set bool) types.String {
	if !set || current.IsUnknown() {
		return types.StringNull()
	}
	return current
}

// optionalNonEmptyStringOrKeep is optionalNonEmptyString for attributes whose
// prior value distinguishes "" from null. The API uses the two interchangeably,
// so a body configured as "" comes back as null and vice versa; reporting the
// other form makes the attribute drift on every plan forever. Whichever of the
// two empties the practitioner wrote is kept; a value replaced by a real string,
// or a real string emptied out of band, still reports the server.
func optionalNonEmptyStringOrKeep(s *string, current types.String) types.String {
	if s != nil && *s != "" {
		return types.StringValue(*s)
	}
	if !current.IsUnknown() && (current.IsNull() || current.ValueString() == "") {
		return current
	}
	return types.StringNull()
}

// preserveTimestamp keeps the configured timestamp string whenever it denotes
// the same instant as the value the API returned. The API re-renders timestamps
// in its own form — a maintenance window configured as "2026-09-01T22:00:00Z"
// comes back as "2026-09-02T00:00:00+02:00" in the status page timezone, and an
// api_token expiry written as "2026-12-01" comes back as
// "2026-12-01T00:00:00Z". The same moment either way, but a permanent diff if
// stored verbatim.
//
// Pass "" for timeZone when the API renders in UTC, which is every caller but
// the maintenance window.
func preserveTimestamp(configured types.String, apiValue, timeZone string) types.String {
	if !isKnown(configured) {
		return types.StringValue(apiValue)
	}
	loc := time.UTC
	if timeZone != "" {
		if l, err := time.LoadLocation(timeZone); err == nil {
			loc = l
		}
	}
	want, _, wantOK := parseISOTime(configured.ValueString(), loc)
	got, _, gotOK := parseISOTime(apiValue, loc)
	if wantOK && gotOK && want.Equal(got) {
		return configured
	}
	return types.StringValue(apiValue)
}

// Layouts accepted by parseISOTime, after the date/time separator has been
// normalized to "T". The zoned set carries an explicit UTC offset; the local set
// is resolved in the caller-supplied location — the status page timezone for a
// maintenance window, UTC for everything else.
var (
	isoZonedLayouts = []string{
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999Z0700",
		"2006-01-02T15:04Z07:00",
		"2006-01-02T15:04Z0700",
	}
	isoLocalLayouts = []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04",
		// Date-only, which the api_tokens API accepts for expires_at (a
		// maintenance window's own format requires a time, so a bare date there
		// is refused by the API long before it reaches this comparison).
		"2006-01-02",
	}
)

// parseISOTime parses an extended ISO 8601 timestamp, resolving values with no
// UTC offset in loc. It reports whether the input carried its own offset, and
// whether it parsed at all.
func parseISOTime(s string, loc *time.Location) (parsed time.Time, zoned bool, ok bool) {
	// Normalise the two spellings maintenanceWindowTimestampRE allows but Go's
	// layouts do not: a space in place of the T, and whitespace before the offset. Replacing
	// the first space blindly turns "...00:00 +02:00" into "...00:00T+02:00",
	// which then fails to parse and makes the attribute drift on every read.
	s = strings.TrimSpace(s)
	if len(s) > 10 && s[10] == ' ' {
		s = s[:10] + "T" + s[11:]
	}
	s = strings.Join(strings.Fields(s), "")
	for _, layout := range isoZonedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true, true
		}
	}
	for _, layout := range isoLocalLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, false, true
		}
	}
	return time.Time{}, false, false
}

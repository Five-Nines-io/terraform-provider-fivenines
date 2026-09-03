package datasources

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }

// A tfsdk tag that does not match its schema attribute is not a compile error:
// it surfaces at plan time as "Value Conversion Error" against a live API. These
// pin every metrics model to the schema it is read into and written back from.
func TestMetricsDataSourceSchemasMatchModels(t *testing.T) {
	cases := []struct {
		typeName   string
		dataSource datasource.DataSource
		model      interface{}
	}{
		{"fivenines_uptime", NewUptimeDataSource(), uptimeModel{}},
		{"fivenines_ssl_status", NewSSLStatusDataSource(), sslStatusModel{}},
		{"fivenines_metric_query", NewMetricQueryDataSource(), metricQueryModel{}},
		{"fivenines_incident_stats", NewIncidentStatsDataSource(), incidentStatsModel{}},
		{"fivenines_cve_stats", NewCveStatsDataSource(), cveStatsModel{}},
	}

	for _, tc := range cases {
		t.Run(tc.typeName, func(t *testing.T) {
			metadata := &datasource.MetadataResponse{}
			tc.dataSource.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "fivenines"}, metadata)
			if metadata.TypeName != tc.typeName {
				t.Errorf("expected type name %q, got %q", tc.typeName, metadata.TypeName)
			}

			resp := &datasource.SchemaResponse{}
			tc.dataSource.Schema(context.Background(), datasource.SchemaRequest{}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
			}
			assertSameNames(t, "attributes", attrNames(resp.Schema.Attributes), tfsdkTags(tc.model))

			if resp.Schema.Description == "" {
				t.Error("expected a description, which is what tfplugindocs renders")
			}
		})
	}
}

func TestMetricsNestedSchemasMatchModels(t *testing.T) {
	items := metricItemsAttribute("items")
	assertSameNames(t, "items", attrNames(items.NestedObject.Attributes), tfsdkTags(metricItemModel{}))

	series := metricSeriesAttribute("series")
	assertSameNames(t, "series", attrNames(series.NestedObject.Attributes), tfsdkTags(metricSeriesModel{}))

	points := series.NestedObject.Attributes["points"].(schema.ListNestedAttribute)
	assertSameNames(t, "series.points", attrNames(points.NestedObject.Attributes), tfsdkTags(metricPointModel{}))

	resp := &datasource.SchemaResponse{}
	NewUptimeDataSource().Schema(context.Background(), datasource.SchemaRequest{}, resp)
	uptimeSeries := resp.Schema.Attributes["series"].(schema.ListNestedAttribute)
	assertSameNames(t, "uptime.series", attrNames(uptimeSeries.NestedObject.Attributes), tfsdkTags(uptimeSeriesModel{}))

	blocks := uptimeSeries.NestedObject.Attributes["blocks"].(schema.ListNestedAttribute)
	assertSameNames(t, "uptime.series.blocks", attrNames(blocks.NestedObject.Attributes), tfsdkTags(uptimeBlockModel{}))
}

// Which id field the API fills depends on the endpoint and the target kind, and
// a collapsed or organization-wide row carries none.
func TestMapMetricItems_EntityIDPerTargetKind(t *testing.T) {
	items := mapMetricItems([]client.MetricsItem{
		{Name: "web-01", HostID: strPtr("host-uuid"), Value: floatPtr(99.9), Formatted: "99.9%"},
		{Name: "API", MonitorID: strPtr("monitor-uuid"), Value: floatPtr(100)},
		{Name: "Backup", TaskID: strPtr("task-uuid"), Value: floatPtr(98)},
		{Name: "Uptime (min)", Value: floatPtr(98)},
		{Name: "No data"},
	})

	want := []types.String{
		types.StringValue("host-uuid"),
		types.StringValue("monitor-uuid"),
		types.StringValue("task-uuid"),
		types.StringNull(),
		types.StringNull(),
	}
	for i, w := range want {
		if !items[i].ID.Equal(w) {
			t.Errorf("item %d: expected id %v, got %v", i, w, items[i].ID)
		}
	}

	if !items[4].Value.IsNull() {
		t.Errorf("expected a missing value to stay null, got %v", items[4].Value)
	}
	if items[0].Formatted.ValueString() != "99.9%" {
		t.Errorf("expected formatted 99.9%%, got %q", items[0].Formatted)
	}
}

func TestMapMetricSeries(t *testing.T) {
	series, err := mapMetricSeries([]client.MetricsSeries{{
		Name:   "web-01",
		HostID: strPtr("host-uuid"),
		Data:   [][]float64{{1781000000, 12.5}, {1781000120, 13.5}},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(series[0].Points))
	}
	if series[0].Points[0].Timestamp.ValueInt64() != 1781000000 {
		t.Errorf("unexpected timestamp: %v", series[0].Points[0].Timestamp)
	}
	if series[0].Points[1].Value.ValueFloat64() != 13.5 {
		t.Errorf("unexpected value: %v", series[0].Points[1].Value)
	}
	if series[0].ID.ValueString() != "host-uuid" {
		t.Errorf("unexpected series id: %v", series[0].ID)
	}
}

// Dropping a malformed point would leave a shorter series that still reads as a
// complete answer. These feed reports and gates, so a truncated pair has to
// surface rather than pass silently.
func TestMapMetricSeries_RefusesMalformedPoint(t *testing.T) {
	_, err := mapMetricSeries([]client.MetricsSeries{{
		Name: "web-01",
		Data: [][]float64{{1781000000, 12.5}, {1781000060}},
	}})
	if err == nil {
		t.Fatal("expected a malformed [timestamp, value] pair to be refused")
	}
	if !strings.Contains(err.Error(), "web-01") || !strings.Contains(err.Error(), "point 1") {
		t.Errorf("error should name the series and the offending index, got: %v", err)
	}
}

func TestLowestValue(t *testing.T) {
	items := []client.MetricsItem{
		{Value: floatPtr(99.99)},
		{Value: nil},
		{Value: floatPtr(97.5)},
	}
	if got := lowestValue(items).ValueFloat64(); got != 97.5 {
		t.Errorf("expected 97.5, got %v", got)
	}

	// No data must not read as 0: an SLA gate would take that for a total outage.
	if got := lowestValue(nil); !got.IsNull() {
		t.Errorf("expected null for no items, got %v", got)
	}
	if got := lowestValue([]client.MetricsItem{{Value: nil}}); !got.IsNull() {
		t.Errorf("expected null when every value is missing, got %v", got)
	}
}

func TestSingleValue(t *testing.T) {
	one := []client.MetricsItem{{Value: floatPtr(8100)}}

	if got := singleValue(one, types.StringNull()).ValueFloat64(); got != 8100 {
		t.Errorf("expected 8100, got %v", got)
	}
	// A grouped read answers with a ranking, not a single figure.
	if got := singleValue(one, types.StringValue("severity")); !got.IsNull() {
		t.Errorf("expected null for a grouped read, got %v", got)
	}
	if got := singleValue(nil, types.StringNull()); !got.IsNull() {
		t.Errorf("expected null for no items, got %v", got)
	}
	if got := singleValue([]client.MetricsItem{{}, {}}, types.StringNull()); !got.IsNull() {
		t.Errorf("expected null for multiple items, got %v", got)
	}
}

func TestTimeRange(t *testing.T) {
	// Nil, not an empty object, so the endpoint's own default window applies.
	if got := timeRange(types.StringNull(), types.StringNull()); got != nil {
		t.Errorf("expected nil for an unset window, got %+v", got)
	}

	got := timeRange(types.StringValue("2026-08-01T00:00:00Z"), types.StringNull())
	if got == nil || got.From != "2026-08-01T00:00:00Z" || got.To != "" {
		t.Errorf("unexpected window: %+v", got)
	}
}

func TestNullIfEmpty(t *testing.T) {
	if got := nullIfEmpty(""); !got.IsNull() {
		t.Errorf("expected null for an empty string, got %v", got)
	}
	if got := nullIfEmpty("duration"); got.ValueString() != "duration" {
		t.Errorf("expected duration, got %v", got)
	}
}

func TestStringSliceAndLabelFilters(t *testing.T) {
	ctx := context.Background()

	got, diags := stringSlice(ctx, types.ListNull(types.StringType))
	if diags.HasError() || got != nil {
		t.Errorf("expected nil for a null list, got %v (%v)", got, diags)
	}

	list, _ := types.ListValueFrom(ctx, types.StringType, []string{"a", "b"})
	got, diags = stringSlice(ctx, list)
	if diags.HasError() || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("expected [a b], got %v (%v)", got, diags)
	}

	filters, diags := labelFilters(ctx, types.MapNull(types.ListType{ElemType: types.StringType}))
	if diags.HasError() || filters != nil {
		t.Errorf("expected nil for a null map, got %v (%v)", filters, diags)
	}

	m, _ := types.MapValueFrom(ctx, types.ListType{ElemType: types.StringType},
		map[string][]string{"device": {"sda", "sdb"}})
	filters, diags = labelFilters(ctx, m)
	if diags.HasError() || !reflect.DeepEqual(filters, map[string][]string{"device": {"sda", "sdb"}}) {
		t.Errorf("expected device filter, got %v (%v)", filters, diags)
	}
}

func TestFilterBoolAndFilterInt64(t *testing.T) {
	if got := filterBool(types.BoolNull()); got != nil {
		t.Errorf("expected nil so the field is omitted, got %v", *got)
	}
	// false must still be sent: it is a meaningful value, not an unset one.
	if got := filterBool(types.BoolValue(false)); got == nil || *got {
		t.Errorf("expected a pointer to false, got %v", got)
	}
	if got := filterInt64(types.Int64Null()); got != nil {
		t.Errorf("expected nil so the field is omitted, got %v", *got)
	}
	if got := filterInt64(types.Int64Value(10)); got == nil || *got != 10 {
		t.Errorf("expected a pointer to 10, got %v", got)
	}
}

func attrNames(attrs map[string]schema.Attribute) []string {
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func tfsdkTags(model interface{}) []string {
	t := reflect.TypeOf(model)
	tags := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if tag, ok := t.Field(i).Tag.Lookup("tfsdk"); ok {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags
}

func assertSameNames(t *testing.T, label string, schemaNames, modelTags []string) {
	t.Helper()
	if !reflect.DeepEqual(schemaNames, modelTags) {
		t.Errorf("%s: schema has %v, model has %v", label, schemaNames, modelTags)
	}
}

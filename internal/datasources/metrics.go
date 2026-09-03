package datasources

import (
	"context"
	"fmt"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Shared plumbing for the fivenines_metric_query, fivenines_uptime,
// fivenines_ssl_status, fivenines_incident_stats and fivenines_cve_stats data
// sources, which all read the POST /api/v1/metrics/* endpoints and all return
// the same items / series envelope.

// ephemeralNote is appended to every metrics data source description. Data
// sources are re-read on every plan, so these values move on their own.
const ephemeralNote = "\n\n" +
	"Metrics are read on every `terraform plan`, so the values below change between runs by design — " +
	"treat them as ephemeral and keep windows relative (`timeadd(timestamp(), \"-720h\")`) rather than pinning " +
	"absolute dates. Referencing them from a resource argument produces a permanent diff; the intended use is " +
	"`output`, `locals` and `precondition` / `postcondition` checks."

type metricItemModel struct {
	Name      types.String  `tfsdk:"name"`
	ID        types.String  `tfsdk:"id"`
	Value     types.Float64 `tfsdk:"value"`
	Formatted types.String  `tfsdk:"formatted"`
}

type metricSeriesModel struct {
	Name   types.String       `tfsdk:"name"`
	ID     types.String       `tfsdk:"id"`
	Points []metricPointModel `tfsdk:"points"`
}

type metricPointModel struct {
	Timestamp types.Int64   `tfsdk:"timestamp"`
	Value     types.Float64 `tfsdk:"value"`
}

func metricItemsAttribute(description string) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Description: description,
		Computed:    true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					Description: "Display name of what the value describes.",
					Computed:    true,
				},
				"id": schema.StringAttribute{
					Description: "UUID of the instance, uptime monitor or task the value belongs to. " +
						"Null for organization-wide values and for collapsed results, which describe no single entity.",
					Computed: true,
				},
				"value": schema.Float64Attribute{
					Description: "The numeric value.",
					Computed:    true,
				},
				"formatted": schema.StringAttribute{
					Description: "The value rendered for humans, with its unit (`99.98%`, `2h 15m`, `12 days`).",
					Computed:    true,
				},
			},
		},
	}
}

func metricSeriesAttribute(description string) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Description: description,
		Computed:    true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					Description: "Display name of the series.",
					Computed:    true,
				},
				"id": schema.StringAttribute{
					Description: "UUID of the instance, uptime monitor or task the series belongs to. " +
						"Null for organization-wide series.",
					Computed: true,
				},
				"points": schema.ListNestedAttribute{
					Description: "The points of the series, oldest first.",
					Computed:    true,
					NestedObject: schema.NestedAttributeObject{
						Attributes: map[string]schema.Attribute{
							"timestamp": schema.Int64Attribute{
								Description: "Unix timestamp of the point, in seconds.",
								Computed:    true,
							},
							"value": schema.Float64Attribute{
								Description: "Value at that timestamp.",
								Computed:    true,
							},
						},
					},
				},
			},
		},
	}
}

func mapMetricItems(items []client.MetricsItem) []metricItemModel {
	out := make([]metricItemModel, 0, len(items))
	for _, item := range items {
		out = append(out, metricItemModel{
			Name:      types.StringValue(item.Name),
			ID:        nullIfEmpty(item.EntityID()),
			Value:     optionalFloat64(item.Value),
			Formatted: types.StringValue(item.Formatted),
		})
	}
	return out
}

// mapMetricSeries refuses a malformed point rather than dropping it. Skipping it
// would leave a shorter series that still looks like a complete answer, and
// these feed reports and gates — the same reason the security endpoints refuse
// a half-null response instead of returning the readable half.
func mapMetricSeries(series []client.MetricsSeries) ([]metricSeriesModel, error) {
	out := make([]metricSeriesModel, 0, len(series))
	for _, s := range series {
		points := make([]metricPointModel, 0, len(s.Data))
		for i, pair := range s.Data {
			// Points are [timestamp, value] pairs; anything else is not one.
			if len(pair) < 2 {
				return nil, fmt.Errorf(
					"series %q point %d has %d value(s), expected a [timestamp, value] pair",
					s.Name, i, len(pair))
			}
			points = append(points, metricPointModel{
				Timestamp: types.Int64Value(int64(pair[0])),
				Value:     types.Float64Value(pair[1]),
			})
		}
		out = append(out, metricSeriesModel{
			Name:   types.StringValue(s.Name),
			ID:     nullIfEmpty(s.EntityID()),
			Points: points,
		})
	}
	return out, nil
}

// lowestValue returns the smallest value across items, which the uptime and SSL
// endpoints both sort to the front: the worst availability, the next certificate
// to expire. Null when nothing came back, so a gate on it fails loudly rather
// than reading a fabricated zero as "down".
func lowestValue(items []client.MetricsItem) types.Float64 {
	var lowest *float64
	for _, item := range items {
		if item.Value == nil {
			continue
		}
		if lowest == nil || *item.Value < *lowest {
			lowest = item.Value
		}
	}
	return optionalFloat64(lowest)
}

// nullIfEmpty maps the API's absent-as-empty strings to a Terraform null, so a
// missing id or unit reads as "not set" rather than as an empty string.
func nullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// singleValue returns the one figure of an ungrouped aggregated read. Null when
// the read was grouped (the answer is a ranking, not a number), when it was a
// time series, or when nothing in the window qualified.
func singleValue(items []client.MetricsItem, groupBy types.String) types.Float64 {
	if groupBy.ValueString() != "" || len(items) != 1 {
		return types.Float64Null()
	}
	return optionalFloat64(items[0].Value)
}

// timeRange builds the optional ISO 8601 window shared by the metrics
// endpoints. Nil when neither bound is set, so the endpoint's own default window
// applies instead of an empty object.
func timeRange(from, to types.String) *client.MetricsTimeRange {
	if from.ValueString() == "" && to.ValueString() == "" {
		return nil
	}
	return &client.MetricsTimeRange{From: from.ValueString(), To: to.ValueString()}
}

func stringSlice(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	var out []string
	return out, list.ElementsAs(ctx, &out, false)
}

func labelFilters(ctx context.Context, m types.Map) (map[string][]string, diag.Diagnostics) {
	if m.IsNull() || m.IsUnknown() {
		return nil, nil
	}
	var out map[string][]string
	return out, m.ElementsAs(ctx, &out, false)
}

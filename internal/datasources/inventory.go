package datasources

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// The twenty per-instance collector inventories share one shape: a row list, a
// pagination envelope and a `collector` block. Rather than twenty near-identical
// files, they are declared as a table in inventory_collectors.go and served by
// the generic data source below.

// fieldKind is how an API column maps onto a Terraform attribute type.
type fieldKind int

const (
	fieldString fieldKind = iota
	fieldInt
	fieldFloat
	fieldBool
	fieldStringList
	// fieldJSON is a free-form object or array of objects, exposed as its raw
	// JSON encoding for jsondecode(). Terraform has no type for a value whose
	// shape the API does not pin down.
	fieldJSON
)

// inventoryField is one column of a collector's rows.
type inventoryField struct {
	name string
	kind fieldKind
	desc string
}

// inventoryFilter is a query parameter the endpoint accepts. Only the
// semantic filters are exposed; page, per_page, order and direction are not,
// because the data source always reads every page and the API's default
// ordering is already deterministic.
type inventoryFilter struct {
	name string
	kind fieldKind // fieldString or fieldBool
	desc string
	// oneOf is the endpoint's documented filter vocabulary, where it has one.
	// It becomes a OneOf validator, which is the only thing that turns an
	// unknown value into a plan-time error instead of a 400 at apply.
	oneOf []string
}

// inventoryCollector declares one data source.
type inventoryCollector struct {
	// name is the endpoint segment, the response key, the data source name
	// suffix and the attribute the rows land in -- one word for the whole
	// endpoint, mirroring the API.
	name    string
	desc    string
	fields  []inventoryField
	filters []inventoryFilter
}

var _ datasource.DataSource = &inventoryDataSource{}

type inventoryDataSource struct {
	collector inventoryCollector
	client    *client.Client
}

// InventoryDataSources returns a constructor for every collector inventory in
// the table, for the provider to register.
func InventoryDataSources() []func() datasource.DataSource {
	out := make([]func() datasource.DataSource, 0, len(inventoryCollectors))
	for _, c := range inventoryCollectors {
		out = append(out, func() datasource.DataSource {
			return &inventoryDataSource{collector: c}
		})
	}
	return out
}

func (d *inventoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.collector.name
}

func (d *inventoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	rowAttrs := make(map[string]schema.Attribute, len(d.collector.fields))
	for _, f := range d.collector.fields {
		rowAttrs[f.name] = rowAttribute(f)
	}

	attrs := map[string]schema.Attribute{
		"instance_id": schema.StringAttribute{
			Description: "The instance (host) UUID the rows belong to.",
			Required:    true,
		},
		d.collector.name: schema.ListNestedAttribute{
			Description:  "The rows the collector reported.",
			Computed:     true,
			NestedObject: schema.NestedAttributeObject{Attributes: rowAttrs},
		},
		"collector": schema.SingleNestedAttribute{
			Description: "Why the row list looks the way it does. Read this before treating an empty list as a clean bill of health: `[]` alone cannot distinguish \"this host genuinely runs none\" from \"the collector is switched off\" (which deletes the rows) or \"this agent is too old to report them\".",
			Computed:    true,
			Attributes:  collectorAttributes(),
		},
	}
	for _, f := range d.collector.filters {
		attrs[f.name] = filterAttribute(f)
	}

	resp.Schema = schema.Schema{Description: d.collector.desc, Attributes: attrs}
}

func (d *inventoryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type",
			"Expected *client.Client, got unexpected type.")
		return
	}
	d.client = c
}

func (d *inventoryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var instanceID types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("instance_id"), &instanceID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Each filter is read once: its config value is echoed straight back into
	// state, and an unset one is left out of the query rather than sent as a
	// zero value. That distinction matters on qemu_vms, where omitting
	// `vanished` is what returns the tombstones.
	filters := make(map[string]string)
	configured := make(map[string]attr.Value, len(d.collector.filters))
	for _, f := range d.collector.filters {
		if f.kind == fieldBool {
			var v types.Bool
			resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(f.name), &v)...)
			configured[f.name] = v
			if !v.IsNull() && !v.IsUnknown() {
				filters[f.name] = strconv.FormatBool(v.ValueBool())
			}
			continue
		}
		var v types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(f.name), &v)...)
		configured[f.name] = v
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
			filters[f.name] = v.ValueString()
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	rows, status, err := d.client.ListInventory(ctx, instanceID.ValueString(), d.collector.name, filters)
	if err != nil {
		resp.Diagnostics.AddError("Error listing "+d.collector.name, err.Error())
		return
	}
	if status == nil {
		// Refusing to render rows without it is the point: without the block an
		// empty list is exactly the ambiguity this data source exists to remove.
		resp.Diagnostics.AddError(
			"Missing collector block",
			fmt.Sprintf("The API response for %s carried no `collector` block, so an empty list cannot be told apart from a switched-off collector. This is an API contract violation.", d.collector.name),
		)
		return
	}

	rowType := d.rowObjectType()
	elems := make([]attr.Value, 0, len(rows))
	for i, row := range rows {
		attrs := make(map[string]attr.Value, len(d.collector.fields))
		for _, f := range d.collector.fields {
			v, err := fieldValue(f, row[f.name])
			if err != nil {
				resp.Diagnostics.AddError(
					"Unexpected value in "+d.collector.name+" response",
					fmt.Sprintf("Row %d, field %q: %s", i, f.name, err),
				)
				return
			}
			attrs[f.name] = v
		}
		obj, diags := types.ObjectValue(rowType.AttrTypes, attrs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		elems = append(elems, obj)
	}

	rowList, diags := types.ListValue(rowType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	collectorObj, diags := collectorValue(status)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The state object is assembled by hand rather than reflected out of a Go
	// struct, because the attribute set differs per collector.
	stateType, ok := resp.State.Schema.Type().(basetypes.ObjectType)
	if !ok {
		resp.Diagnostics.AddError("Unexpected schema type",
			fmt.Sprintf("Expected an object type for %s, got %T.", d.collector.name, resp.State.Schema.Type()))
		return
	}
	stateAttrs := map[string]attr.Value{
		"instance_id":    instanceID,
		d.collector.name: rowList,
		"collector":      collectorObj,
	}
	for name, v := range configured {
		stateAttrs[name] = v
	}

	state, diags := types.ObjectValue(stateType.AttrTypes, stateAttrs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (d *inventoryDataSource) rowObjectType() types.ObjectType {
	attrTypes := make(map[string]attr.Type, len(d.collector.fields))
	for _, f := range d.collector.fields {
		attrTypes[f.name] = f.attrType()
	}
	return types.ObjectType{AttrTypes: attrTypes}
}

func (f inventoryField) attrType() attr.Type {
	switch f.kind {
	case fieldInt:
		return types.Int64Type
	case fieldFloat:
		return types.Float64Type
	case fieldBool:
		return types.BoolType
	case fieldStringList:
		return types.ListType{ElemType: types.StringType}
	default: // fieldString, fieldJSON
		return types.StringType
	}
}

func rowAttribute(f inventoryField) schema.Attribute {
	switch f.kind {
	case fieldInt:
		return schema.Int64Attribute{Description: f.desc, Computed: true}
	case fieldFloat:
		return schema.Float64Attribute{Description: f.desc, Computed: true}
	case fieldBool:
		return schema.BoolAttribute{Description: f.desc, Computed: true}
	case fieldStringList:
		return schema.ListAttribute{Description: f.desc, Computed: true, ElementType: types.StringType}
	default:
		return schema.StringAttribute{Description: f.desc, Computed: true}
	}
}

func filterAttribute(f inventoryFilter) schema.Attribute {
	if f.kind == fieldBool {
		return schema.BoolAttribute{Description: f.desc, Optional: true}
	}
	attr := schema.StringAttribute{Description: f.desc, Optional: true}
	if len(f.oneOf) > 0 {
		attr.Validators = []validator.String{stringvalidator.OneOf(f.oneOf...)}
	}
	return attr
}

// fieldValue converts one decoded JSON value into its Terraform attribute.
//
// A JSON null (and a column the response omitted) becomes a Terraform null,
// never a zero. That mapping is the whole honesty contract these endpoints are
// built on: a null oom_kill_count means "cgroup v1 has no such counter", a null
// vulnerability_count means "never scanned", a null scrub_errors means "nobody
// has ever checked" -- and 0 would read as an all-clear in every one of them.
func fieldValue(f inventoryField, raw interface{}) (attr.Value, error) {
	if raw == nil {
		return nullValue(f), nil
	}

	switch f.kind {
	case fieldInt:
		n, ok := raw.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected a number, got %T", raw)
		}
		i, err := n.Int64()
		if err != nil {
			// A whole number the API rendered as a float still fits.
			fl, ferr := n.Float64()
			if ferr != nil {
				return nil, fmt.Errorf("expected an integer, got %q", n.String())
			}
			i = int64(fl)
		}
		return types.Int64Value(i), nil

	case fieldFloat:
		n, ok := raw.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected a number, got %T", raw)
		}
		fl, err := n.Float64()
		if err != nil {
			return nil, fmt.Errorf("expected a number, got %q", n.String())
		}
		return types.Float64Value(fl), nil

	case fieldBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected a boolean, got %T", raw)
		}
		return types.BoolValue(b), nil

	case fieldStringList:
		items, ok := raw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("expected an array, got %T", raw)
		}
		elems := make([]attr.Value, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected an array of strings, got a %T element", item)
			}
			elems = append(elems, types.StringValue(s))
		}
		list, diags := types.ListValue(types.StringType, elems)
		if diags.HasError() {
			return nil, fmt.Errorf("building list: %v", diags.Errors())
		}
		return list, nil

	case fieldJSON:
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("re-encoding as JSON: %w", err)
		}
		return types.StringValue(string(encoded)), nil

	default:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string, got %T", raw)
		}
		return types.StringValue(s), nil
	}
}

func nullValue(f inventoryField) attr.Value {
	switch f.kind {
	case fieldInt:
		return types.Int64Null()
	case fieldFloat:
		return types.Float64Null()
	case fieldBool:
		return types.BoolNull()
	case fieldStringList:
		return types.ListNull(types.StringType)
	default:
		return types.StringNull()
	}
}

// --- The collector block, identical on all twenty data sources ---

func collectorAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Description: "The collector these rows come from.",
			Computed:    true,
		},
		"enabled": schema.BoolAttribute{
			Description: "Whether the operator has this collector switched on for the instance. Switching it off deletes the rows, so `false` fully explains an empty list.",
			Computed:    true,
		},
		"supported": schema.BoolAttribute{
			Description: "Whether this agent can collect these rows -- its own capability verdict where it reports one, its version otherwise. `false` alongside `enabled` means the instance was asked for something it cannot deliver. Read it as \"the agent has not said it cannot\": the config-driven collectors (haproxy, rabbitmq, php_fpm, wireguard) have no version floor, so an agent predating one still reports `true` with an empty list.",
			Computed:    true,
		},
		"pending": schema.BoolAttribute{
			Description: "Enabled, and the agent knows the feature, but the capability is not satisfied yet (missing binary, missing permission). The rows are legitimately absent and may appear on a later tick.",
			Computed:    true,
		},
		"unavailable_reason": schema.StringAttribute{
			Description: "Why `supported` is false: `host_lacks_feature` (the agent probed and reported it cannot) or `agent_outdated` (the agent predates the release that collects these rows). Null when supported.",
			Computed:    true,
		},
		"blocked_reason": schema.StringAttribute{
			Description: "The agent's own explanation, where it sent one. Null otherwise.",
			Computed:    true,
		},
		"last_reported_at": schema.StringAttribute{
			Description: "The instance's last agent check-in -- not this collector's. It separates \"this collector went quiet\" (rows stale, instance reporting) from \"the whole agent went quiet\" (both). Per-row freshness is each row's `last_synced_at`.",
			Computed:    true,
		},
	}
}

func collectorObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"name":               types.StringType,
		"enabled":            types.BoolType,
		"supported":          types.BoolType,
		"pending":            types.BoolType,
		"unavailable_reason": types.StringType,
		"blocked_reason":     types.StringType,
		"last_reported_at":   types.StringType,
	}}
}

func collectorValue(s *client.CollectorStatus) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(collectorObjectType().AttrTypes, map[string]attr.Value{
		"name":               types.StringValue(s.Name),
		"enabled":            types.BoolValue(s.Enabled),
		"supported":          types.BoolValue(s.Supported),
		"pending":            types.BoolValue(s.Pending),
		"unavailable_reason": optionalString(s.UnavailableReason),
		"blocked_reason":     optionalString(s.BlockedReason),
		"last_reported_at":   optionalString(s.LastReportedAt),
	})
}

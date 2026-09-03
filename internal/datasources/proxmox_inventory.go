package datasources

import (
	"context"
	"fmt"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// The Proxmox child inventories are served on three routes with identical rows:
// under an instance (the twenty per-instance collectors, in inventory.go), under
// a cluster, and -- for guests -- organization-wide. Only the route and the
// scoping argument differ, so the row shapes are read straight off the collector
// table rather than restated here; see proxmoxRowFields.
//
// These routes carry NO `collector` block, unlike the per-instance ones: the
// collector is a property of a host's agent, and a cluster is reported by
// several. That is why they cannot simply reuse inventoryDataSource.

var _ datasource.DataSource = &proxmoxInventoryDataSource{}

// proxmoxInventory declares one cluster-scoped or organization-wide data source.
type proxmoxInventory struct {
	// name is the data source suffix and the attribute the rows land in.
	name string
	// key is the response envelope key, which is NOT the name: the route says
	// `nodes` and the envelope says `proxmox_nodes`.
	key string
	// segment is the path segment under /proxmox_clusters/{id}. Empty for the
	// organization-wide route, which is what makes the data source unscoped.
	segment string
	desc    string
	fields  []inventoryField
	filters []inventoryFilter
}

// clusterScoped reports whether the rows are reached through one cluster, which
// is what adds the required cluster_id argument.
func (p proxmoxInventory) clusterScoped() bool { return p.segment != "" }

type proxmoxInventoryDataSource struct {
	inventory proxmoxInventory
	client    *client.Client
}

// ProxmoxInventoryDataSources returns a constructor for every Proxmox child
// inventory, for the provider to register.
func ProxmoxInventoryDataSources() []func() datasource.DataSource {
	out := make([]func() datasource.DataSource, 0, len(proxmoxInventories))
	for _, p := range proxmoxInventories {
		out = append(out, func() datasource.DataSource {
			return &proxmoxInventoryDataSource{inventory: p}
		})
	}
	return out
}

func (d *proxmoxInventoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.inventory.name
}

func (d *proxmoxInventoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	rowAttrs := make(map[string]schema.Attribute, len(d.inventory.fields))
	for _, f := range d.inventory.fields {
		rowAttrs[f.name] = rowAttribute(f)
	}

	attrs := map[string]schema.Attribute{
		d.inventory.name: schema.ListNestedAttribute{
			Description:  "The rows the cluster's authoritative reporter wrote.",
			Computed:     true,
			NestedObject: schema.NestedAttributeObject{Attributes: rowAttrs},
		},
	}
	if d.inventory.clusterScoped() {
		attrs["cluster_id"] = schema.StringAttribute{
			Description: "The Proxmox cluster's uuid -- `fivenines_proxmox_clusters`' `id`, NOT its " +
				"`cluster_key`.",
			Required: true,
		}
	}
	for _, f := range d.inventory.filters {
		attrs[f.name] = filterAttribute(f)
	}

	resp.Schema = schema.Schema{Description: d.inventory.desc, Attributes: attrs}
}

func (d *proxmoxInventoryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *proxmoxInventoryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var clusterID types.String
	if d.inventory.clusterScoped() {
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("cluster_id"), &clusterID)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	filters, configured := readInventoryFilters(ctx, req.Config, d.inventory.filters, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var rows []map[string]interface{}
	var err error
	if d.inventory.clusterScoped() {
		rows, err = d.client.ListProxmoxClusterRows(ctx, clusterID.ValueString(), d.inventory.segment, d.inventory.key, filters)
	} else {
		rows, err = d.client.ListOrganizationProxmoxGuests(ctx, filters)
	}
	if err != nil {
		resp.Diagnostics.AddError("Error listing "+d.inventory.name, err.Error())
		return
	}

	rowList := inventoryRowList(d.inventory.fields, rows, d.inventory.name, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// The state object is assembled by hand rather than reflected out of a Go
	// struct, because the attribute set differs per data source.
	stateType, ok := resp.State.Schema.Type().(basetypes.ObjectType)
	if !ok {
		resp.Diagnostics.AddError("Unexpected schema type",
			fmt.Sprintf("Expected an object type for %s, got %T.", d.inventory.name, resp.State.Schema.Type()))
		return
	}
	stateAttrs := map[string]attr.Value{d.inventory.name: rowList}
	if d.inventory.clusterScoped() {
		stateAttrs["cluster_id"] = clusterID
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

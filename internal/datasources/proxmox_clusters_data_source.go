package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource = &proxmoxClustersDataSource{}
	_ datasource.DataSource = &proxmoxClusterDataSource{}
)

// The three-valued-quorum contract is the same on both data sources, so it is
// written once.
const proxmoxQuorumContract = "`quorate` IS THREE-VALUED AND THE NULL IS LOAD-BEARING. `true` means a fresh " +
	"reporter saw quorum -- corosync cannot grant it to two partitions at once, so any fresh reporter " +
	"claiming it settles the question, which is what keeps this stable during a split brain where the " +
	"minority partition reports false. `false` means fresh reporters read cluster status and none has " +
	"quorum. NULL means UNKNOWN, never \"lost\": the cluster is `standalone`, or every fresh reporter " +
	"failed to read cluster status (API down, credentials expired), or the cluster is `stale`. A " +
	"consumer that treats null as false fires a lost-quorum alarm on a monitoring outage -- the exact " +
	"false page this derivation avoids. Branch on `standalone` and `stale` to tell the three apart.\n\n" +
	"THE VERDICT CARRIES ITS PROVENANCE: `stale`, `fresh_reporter_count` and " +
	"`unreachable_reporter_count` say whether anyone is watching and whether they can see the API."

type proxmoxClustersDataSource struct {
	client *client.Client
}

type proxmoxClusterDataSource struct {
	client *client.Client
}

type proxmoxClustersModel struct {
	Query           types.String          `tfsdk:"query"`
	UpdatedSince    types.String          `tfsdk:"updated_since"`
	Standalone      types.Bool            `tfsdk:"standalone"`
	Stale           types.Bool            `tfsdk:"stale"`
	Order           types.String          `tfsdk:"order"`
	Direction       types.String          `tfsdk:"direction"`
	ProxmoxClusters []proxmoxClusterModel `tfsdk:"proxmox_clusters"`
}

type proxmoxClusterModel struct {
	ID                       types.String `tfsdk:"id"`
	ClusterKey               types.String `tfsdk:"cluster_key"`
	Name                     types.String `tfsdk:"name"`
	Version                  types.String `tfsdk:"version"`
	Standalone               types.Bool   `tfsdk:"standalone"`
	Quorate                  types.Bool   `tfsdk:"quorate"`
	Stale                    types.Bool   `tfsdk:"stale"`
	LastSeenAt               types.String `tfsdk:"last_seen_at"`
	ReporterCount            types.Int64  `tfsdk:"reporter_count"`
	FreshReporterCount       types.Int64  `tfsdk:"fresh_reporter_count"`
	UnreachableReporterCount types.Int64  `tfsdk:"unreachable_reporter_count"`
	AuthoritativeHostID      types.String `tfsdk:"authoritative_host_id"`
	NodesTotal               types.Int64  `tfsdk:"nodes_total"`
	NodesOnline              types.Int64  `tfsdk:"nodes_online"`
	GuestsTotal              types.Int64  `tfsdk:"guests_total"`
	GuestsRunning            types.Int64  `tfsdk:"guests_running"`
	StorageTotal             types.Int64  `tfsdk:"storage_total"`
	StorageActive            types.Int64  `tfsdk:"storage_active"`
	CreatedAt                types.String `tfsdk:"created_at"`
	UpdatedAt                types.String `tfsdk:"updated_at"`
}

// proxmoxClusterDetailModel is proxmoxClusterModel plus the reporters the index
// omits.
//
// Two models rather than one for the reason cephClusterDetailModel gives: the
// two SCHEMAS differ by the nested `reporters` attribute, and a model is
// reflected against exactly one schema. The shared columns are embedded rather
// than restated -- the framework promotes an embedded struct's tfsdk tags.
type proxmoxClusterDetailModel struct {
	proxmoxClusterModel
	Reporters []proxmoxClusterReporterModel `tfsdk:"reporters"`
}

type proxmoxClusterReporterModel struct {
	HostID               types.String `tfsdk:"host_id"`
	HostName             types.String `tfsdk:"host_name"`
	Fresh                types.Bool   `tfsdk:"fresh"`
	Authoritative        types.Bool   `tfsdk:"authoritative"`
	Reachable            types.Bool   `tfsdk:"reachable"`
	ClusterOK            types.Bool   `tfsdk:"cluster_ok"`
	NodesOK              types.Bool   `tfsdk:"nodes_ok"`
	GuestsOK             types.Bool   `tfsdk:"guests_ok"`
	StorageOK            types.Bool   `tfsdk:"storage_ok"`
	CompletenessScore    types.Int64  `tfsdk:"completeness_score"`
	MaxCompletenessScore types.Int64  `tfsdk:"max_completeness_score"`
	QuorateSeen          types.Bool   `tfsdk:"quorate_seen"`
	NodesOnlineSeen      types.Int64  `tfsdk:"nodes_online_seen"`
	NodesTotalSeen       types.Int64  `tfsdk:"nodes_total_seen"`
	LastError            types.String `tfsdk:"last_error"`
	LastSyncedAt         types.String `tfsdk:"last_synced_at"`
	ReceivedAt           types.String `tfsdk:"received_at"`
}

func NewProxmoxClustersDataSource() datasource.DataSource {
	return &proxmoxClustersDataSource{}
}

func NewProxmoxClusterDataSource() datasource.DataSource {
	return &proxmoxClusterDataSource{}
}

func (d *proxmoxClustersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_proxmox_clusters"
}

func (d *proxmoxClusterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_proxmox_cluster"
}

// proxmoxClusterAttributes are the fields both the index rows and the single
// cluster publish.
func proxmoxClusterAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "The cluster's uuid -- what `fivenines_proxmox_cluster` and the " +
				"`fivenines_proxmox_cluster_*` data sources take, and the dashboard's `/proxmox/{id}`. NOT " +
				"what `fivenines_metric_query` takes; that is `cluster_key`.",
			Computed: true,
		},
		"cluster_key": schema.StringAttribute{
			Description: "The corosync cluster name, or `standalone:<host_id>` for a node that never " +
				"joined one. THIS is what the `proxmox_clusters` scope of `fivenines_metric_query` takes. " +
				"Sending the `id` there returns an empty series rather than an error, which is why both " +
				"are published.",
			Computed: true,
		},
		"name": schema.StringAttribute{
			Description: "The cluster name.",
			Computed:    true,
		},
		"version": schema.StringAttribute{
			Description: "The Proxmox VE version the reporters see. Null when not reported.",
			Computed:    true,
		},
		"standalone": schema.BoolAttribute{
			Description: "A single node with no corosync cluster. `quorate` is null for every one of " +
				"these by definition, not for want of data.",
			Computed: true,
		},
		"quorate": schema.BoolAttribute{
			Description: "Whether the cluster has quorum. THREE-VALUED: null means UNKNOWN, never " +
				"\"lost\" -- the cluster is `standalone`, or every fresh reporter failed to read cluster " +
				"status, or the cluster is `stale`. Treating null as false fires a lost-quorum alarm on a " +
				"monitoring outage.",
			Computed: true,
		},
		"stale": schema.BoolAttribute{
			Description: "No reporter has checked in inside the organization's offline threshold.",
			Computed:    true,
		},
		"last_seen_at": schema.StringAttribute{
			Description: "The most recent scrape from ANY reporter.",
			Computed:    true,
		},
		"reporter_count": schema.Int64Attribute{
			Description: "Hosts that have ever reported this cluster.",
			Computed:    true,
		},
		"fresh_reporter_count": schema.Int64Attribute{
			Description: "How many hosts the `quorate` verdict was derived FROM. Zero means nothing is " +
				"watching, and `quorate` is null for that reason rather than any other.",
			Computed: true,
		},
		"unreachable_reporter_count": schema.Int64Attribute{
			Description: "Fresh hosts that could not reach the Proxmox API this cycle. Distinct from " +
				"\"not reporting\", which is `fresh_reporter_count`.",
			Computed: true,
		},
		"authoritative_host_id": schema.StringAttribute{
			Description: "The elected reporter -- the most complete fresh one. It is the SINGLE writer of " +
				"the child inventory and of the cluster series, which is why one cluster has one set of " +
				"nodes, guests and storages however many hosts report it. Null whenever the cluster is stale.",
			Computed: true,
		},
		"nodes_total": schema.Int64Attribute{
			Description: "The cluster's whole node inventory. Deliberately NOT freshness filtered -- " +
				"`stale` is the field that qualifies these counts. Null on a response that did not compute " +
				"the rollups, never zero: a null is \"not counted\", where 0 would read as \"no nodes\".",
			Computed: true,
		},
		"nodes_online": schema.Int64Attribute{
			Description: "Nodes the CLUSTER can see. An offline node is the surviving nodes' verdict on " +
				"it, not ours -- and is distinct from both `stale` and `unreachable_reporter_count`.",
			Computed: true,
		},
		"guests_total":   schema.Int64Attribute{Description: "Guests (VMs and containers) on the cluster.", Computed: true},
		"guests_running": schema.Int64Attribute{Description: "Guests currently running.", Computed: true},
		"storage_total": schema.Int64Attribute{
			Description: "Storage rows, which are keyed PER NODE -- one datacenter-wide pool is counted " +
				"once per node it is mounted on.",
			Computed: true,
		},
		"storage_active": schema.Int64Attribute{
			Description: "Storage rows active on their node. Same per-node keying as `storage_total`.",
			Computed:    true,
		},
		"created_at": schema.StringAttribute{
			Description: "When the cluster row was first recorded.",
			Computed:    true,
		},
		"updated_at": schema.StringAttribute{
			Description: "When the cluster's own columns last changed. The quorum verdict, the freshness " +
				"and the rollups are derived on every read and move WITHOUT touching this, so " +
				"`updated_since` is not a health change feed.",
			Computed: true,
		},
	}
}

func (d *proxmoxClustersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The organization's Proxmox VE clusters -- the fleet overview, with the quorum " +
			"verdict and the node / guest / storage rollups.\n\n" +
			proxmoxQuorumContract + "\n\n" +
			"TWO IDENTIFIERS, NOT INTERCHANGEABLE: `id` is what `fivenines_proxmox_cluster` and the " +
			"`fivenines_proxmox_cluster_*` data sources take, `cluster_key` is what " +
			"`fivenines_metric_query`'s `proxmox_clusters` scope takes. Sending the wrong one returns an " +
			"empty series rather than an error, so both are published.\n\n" +
			"There is no `quorate` filter, for the reason the Ceph list has no `health` one: the verdict " +
			"is a fold over the reporter set, and a second implementation in SQL is how a filter starts " +
			"disagreeing with the field. `stale` IS filterable -- it has an exact SQL twin.",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Case-insensitive substring match on the name, the cluster key or the version " +
					"(the API's `q` filter).",
				Optional: true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only return clusters whose `updated_at` is at or after this ISO8601 " +
					"timestamp. Inclusive. NOT a health change feed -- see `updated_at`.",
				Optional: true,
			},
			"standalone": schema.BoolAttribute{
				Description: "A node that never joined a corosync cluster. Worth filtering on separately " +
					"from quorum, since `quorate` is null for every one of these by definition rather than " +
					"for want of data. Omit for both.",
				Optional: true,
			},
			"stale": schema.BoolAttribute{
				Description: "Freshness filter, built on the organization's offline threshold. `false` " +
					"returns the clusters at least one host is currently reporting; `true` returns the rest. " +
					"The two values partition the collection -- a cluster with no reporter row at all counts " +
					"as stale. Omit for both.",
				Optional: true,
			},
			"order": schema.StringAttribute{
				Description: "Column to sort by. Defaults to `name`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("name", "cluster_key", "created_at", "updated_at"),
				},
			},
			"direction": schema.StringAttribute{
				Description: `Sort direction: "asc" or "desc". Defaults to "asc".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"proxmox_clusters": schema.ListNestedAttribute{
				Description:  "Matching clusters, each with its quorum verdict, provenance and rollups.",
				Computed:     true,
				NestedObject: schema.NestedAttributeObject{Attributes: proxmoxClusterAttributes()},
			},
		},
	}
}

func (d *proxmoxClusterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := proxmoxClusterAttributes()
	// id is the argument here, not a computed column.
	attrs["id"] = schema.StringAttribute{
		Description: "The cluster's uuid. NOT its `cluster_key` -- see `fivenines_proxmox_clusters`. A " +
			"malformed id is a 404, never a 500.",
		Required: true,
	}
	attrs["reporters"] = schema.ListNestedAttribute{
		Description: "The per-host breakdown -- the \"Reporting Hosts\" table from the dashboard, which " +
			"the `fivenines_proxmox_clusters` index omits.",
		Computed:     true,
		NestedObject: schema.NestedAttributeObject{Attributes: proxmoxReporterAttributes()},
	}

	resp.Schema = schema.Schema{
		Description: "One Proxmox VE cluster, plus the per-host `reporters` breakdown the " +
			"`fivenines_proxmox_clusters` index omits.\n\n" +
			proxmoxQuorumContract + "\n\n" +
			"EACH REPORTER ROW IS PROVENANCE, NOT A SECOND VERDICT. `quorate_seen` is what THAT host's own " +
			"API call returned, and during a split brain the minority partition's reporter really does " +
			"report `false` while the cluster is quorate. Read `fresh` first; `authoritative` marks the " +
			"elected reporter, which writes the child inventory and emits the cluster series.\n\n" +
			"The metric series live on `fivenines_metric_query` with " +
			"`proxmox_clusters = [\"<cluster_key>\"]` -- the cluster KEY, not this id.",
		Attributes: attrs,
	}
}

func proxmoxReporterAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"host_id": schema.StringAttribute{
			Description: "UUID of the reporting host -- the join key back to a `fivenines_instance`.",
			Computed:    true,
		},
		"host_name": schema.StringAttribute{
			Description: "The host's display name or hostname. Null if the host row went missing.",
			Computed:    true,
		},
		"fresh": schema.BoolAttribute{
			Description: "Whether this row is recent enough to count toward the cluster verdict. The " +
				"window is the ORGANIZATION's offline threshold. READ THIS FIRST.",
			Computed: true,
		},
		"authoritative": schema.BoolAttribute{
			Description: "The elected writer of the child inventory and the cluster series. Exactly one " +
				"fresh reporter carries this; none does while stale.",
			Computed: true,
		},
		"reachable": schema.BoolAttribute{
			Description: "Whether this host reached the Proxmox API on its last tick. `false` is the red " +
				"\"can't reach\" state, distinct from \"not reporting\" (`fresh` false).",
			Computed: true,
		},
		"cluster_ok": schema.BoolAttribute{
			Description: "Whether the cluster-status call succeeded. This and the three flags below report " +
				"CALL success, not agreement between hosts.",
			Computed: true,
		},
		"nodes_ok":   schema.BoolAttribute{Description: "Whether the node collection succeeded.", Computed: true},
		"guests_ok":  schema.BoolAttribute{Description: "Whether the guest collection succeeded.", Computed: true},
		"storage_ok": schema.BoolAttribute{Description: "Whether the storage collection succeeded.", Computed: true},
		"completeness_score": schema.Int64Attribute{
			Description: "How much of the collection this host carries, and the precedence key for the " +
				"authoritative election. Read it against `max_completeness_score`.",
			Computed: true,
		},
		"max_completeness_score": schema.Int64Attribute{
			Description: "The denominator for `completeness_score`.",
			Computed:    true,
		},
		"quorate_seen": schema.BoolAttribute{
			Description: "THIS HOST's own reading, which is not the cluster's. Null for a standalone node " +
				"and whenever the host could not read cluster status.",
			Computed: true,
		},
		"nodes_online_seen": schema.Int64Attribute{
			Description: "This host's online-node count, null when it could not read one.",
			Computed:    true,
		},
		"nodes_total_seen": schema.Int64Attribute{
			Description: "This host's total-node count, null when it could not read one.",
			Computed:    true,
		},
		"last_error": schema.StringAttribute{
			Description: "The agent's own words when the collection failed. Null when it did not.",
			Computed:    true,
		},
		"last_synced_at": schema.StringAttribute{
			Description: "When this collection was stored. A server clock, never the agent's.",
			Computed:    true,
		},
		"received_at": schema.StringAttribute{
			Description: "When the tick carrying this collection arrived. Also a server clock.",
			Computed:    true,
		},
	}
}

func (d *proxmoxClustersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *proxmoxClusterDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *proxmoxClustersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state proxmoxClustersModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clusters, err := d.client.ListProxmoxClusters(ctx, client.ProxmoxClusterListOptions{
		Query:        filterString(state.Query),
		UpdatedSince: filterString(state.UpdatedSince),
		Standalone:   filterBool(state.Standalone),
		Stale:        filterBool(state.Stale),
		Order:        filterString(state.Order),
		Direction:    filterString(state.Direction),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing Proxmox clusters", err.Error())
		return
	}

	// Non-nil even when nothing matches: a null list breaks length()/for_each.
	state.ProxmoxClusters = make([]proxmoxClusterModel, 0, len(clusters))
	for _, c := range clusters {
		state.ProxmoxClusters = append(state.ProxmoxClusters, proxmoxClusterRow(c))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *proxmoxClusterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state proxmoxClusterDetailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cluster, err := d.client.GetProxmoxCluster(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading Proxmox cluster", err.Error())
		return
	}

	// The CONFIGURED id is preserved -- see the note in cephClusterDataSource.Read.
	id := state.ID
	state.proxmoxClusterModel = proxmoxClusterRow(*cluster)
	state.ID = id

	state.Reporters = make([]proxmoxClusterReporterModel, 0, len(cluster.Reporters))
	for _, r := range cluster.Reporters {
		state.Reporters = append(state.Reporters, proxmoxClusterReporterModel{
			HostID:               types.StringValue(r.HostID),
			HostName:             optionalString(r.HostName),
			Fresh:                types.BoolValue(r.Fresh),
			Authoritative:        types.BoolValue(r.Authoritative),
			Reachable:            types.BoolValue(r.Reachable),
			ClusterOK:            types.BoolValue(r.ClusterOK),
			NodesOK:              types.BoolValue(r.NodesOK),
			GuestsOK:             types.BoolValue(r.GuestsOK),
			StorageOK:            types.BoolValue(r.StorageOK),
			CompletenessScore:    types.Int64Value(r.CompletenessScore),
			MaxCompletenessScore: types.Int64Value(r.MaxCompletenessScore),
			QuorateSeen:          optionalBool(r.QuorateSeen),
			NodesOnlineSeen:      optionalInt64(r.NodesOnlineSeen),
			NodesTotalSeen:       optionalInt64(r.NodesTotalSeen),
			LastError:            optionalString(r.LastError),
			LastSyncedAt:         optionalString(r.LastSyncedAt),
			ReceivedAt:           optionalString(r.ReceivedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func proxmoxClusterRow(c client.ProxmoxCluster) proxmoxClusterModel {
	return proxmoxClusterModel{
		ID:                       types.StringValue(c.ID),
		ClusterKey:               types.StringValue(c.ClusterKey),
		Name:                     types.StringValue(c.Name),
		Version:                  optionalString(c.Version),
		Standalone:               types.BoolValue(c.Standalone),
		Quorate:                  optionalBool(c.Quorate),
		Stale:                    types.BoolValue(c.Stale),
		LastSeenAt:               optionalString(c.LastSeenAt),
		ReporterCount:            types.Int64Value(c.ReporterCount),
		FreshReporterCount:       types.Int64Value(c.FreshReporterCount),
		UnreachableReporterCount: types.Int64Value(c.UnreachableReporterCount),
		AuthoritativeHostID:      optionalString(c.AuthoritativeHostID),
		NodesTotal:               optionalInt64(c.NodesTotal),
		NodesOnline:              optionalInt64(c.NodesOnline),
		GuestsTotal:              optionalInt64(c.GuestsTotal),
		GuestsRunning:            optionalInt64(c.GuestsRunning),
		StorageTotal:             optionalInt64(c.StorageTotal),
		StorageActive:            optionalInt64(c.StorageActive),
		CreatedAt:                optionalString(c.CreatedAt),
		UpdatedAt:                optionalString(c.UpdatedAt),
	}
}

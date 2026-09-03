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
	_ datasource.DataSource = &cephClustersDataSource{}
	_ datasource.DataSource = &cephClusterDataSource{}
)

// The health provenance paragraph is the same contract on both data sources, so
// it is written once.
const cephHealthProvenance = "THE HEALTH CARRIES ITS PROVENANCE, and you need all four fields. A cluster is " +
	"reported by N hosts and the verdict is recomputed at READ from the FRESH ones -- never a stored " +
	"winner, because a complete-but-old \"healthy\" scrape would otherwise beat a fresh \"it is down\" " +
	"one forever. So `health` alone cannot tell you whether anyone is watching: read it with `stale` " +
	"(nobody has checked in inside the organization's offline threshold), `fresh_reporter_count` (how " +
	"many hosts the verdict came from) and `unreachable_reporter_count` (fresh hosts that cannot see " +
	"the cluster -- a per-host badge, not a cluster alarm).\n\n" +
	"`health` IS NULL FOR AN UNPROMOTED CLUSTER. A single-reporter cluster is usually a phantom -- a " +
	"stale ceph.conf on a cloned image -- so the product refuses to vouch for its health and so does " +
	"this. Branch on `promoted` rather than reading a null as healthy."

type cephClustersDataSource struct {
	client *client.Client
}

type cephClusterDataSource struct {
	client *client.Client
}

type cephClustersModel struct {
	Query        types.String       `tfsdk:"query"`
	UpdatedSince types.String       `tfsdk:"updated_since"`
	Promoted     types.Bool         `tfsdk:"promoted"`
	Stale        types.Bool         `tfsdk:"stale"`
	Order        types.String       `tfsdk:"order"`
	Direction    types.String       `tfsdk:"direction"`
	CephClusters []cephClusterModel `tfsdk:"ceph_clusters"`
}

type cephClusterModel struct {
	FSID                     types.String `tfsdk:"fsid"`
	Name                     types.String `tfsdk:"name"`
	ConfiguredName           types.String `tfsdk:"configured_name"`
	Promoted                 types.Bool   `tfsdk:"promoted"`
	PromotedAt               types.String `tfsdk:"promoted_at"`
	Health                   types.String `tfsdk:"health"`
	Stale                    types.Bool   `tfsdk:"stale"`
	LastSeenAt               types.String `tfsdk:"last_seen_at"`
	ReporterCount            types.Int64  `tfsdk:"reporter_count"`
	FreshReporterCount       types.Int64  `tfsdk:"fresh_reporter_count"`
	UnreachableReporterCount types.Int64  `tfsdk:"unreachable_reporter_count"`
	AuthoritativeHostID      types.String `tfsdk:"authoritative_host_id"`
	CreatedAt                types.String `tfsdk:"created_at"`
	UpdatedAt                types.String `tfsdk:"updated_at"`
}

// cephClusterDetailModel is cephClusterModel plus the reporters the index
// omits.
//
// Two models rather than one, because the two SCHEMAS genuinely differ: a
// nested `reporters` attribute exists on the single-cluster schema and not on
// the index, and a model is reflected against exactly one schema. The fields
// themselves are not restated -- terraform-plugin-framework promotes the tfsdk
// tags of an embedded struct, so one embedded value keeps the shared columns in
// a single place and a new API field is one edit rather than four.
type cephClusterDetailModel struct {
	cephClusterModel
	Reporters []cephClusterReporterModel `tfsdk:"reporters"`
}

type cephClusterReporterModel struct {
	HostID               types.String `tfsdk:"host_id"`
	HostName             types.String `tfsdk:"host_name"`
	Fresh                types.Bool   `tfsdk:"fresh"`
	Authoritative        types.Bool   `tfsdk:"authoritative"`
	Reachable            types.Bool   `tfsdk:"reachable"`
	StatusOK             types.Bool   `tfsdk:"status_ok"`
	DfOK                 types.Bool   `tfsdk:"df_ok"`
	TreeOK               types.Bool   `tfsdk:"tree_ok"`
	OsdDfOK              types.Bool   `tfsdk:"osd_df_ok"`
	PerfOK               types.Bool   `tfsdk:"perf_ok"`
	CompletenessScore    types.Int64  `tfsdk:"completeness_score"`
	MaxCompletenessScore types.Int64  `tfsdk:"max_completeness_score"`
	LastHealth           types.String `tfsdk:"last_health"`
	LastError            types.String `tfsdk:"last_error"`
	LastSyncedAt         types.String `tfsdk:"last_synced_at"`
	ReceivedAt           types.String `tfsdk:"received_at"`
}

func NewCephClustersDataSource() datasource.DataSource {
	return &cephClustersDataSource{}
}

func NewCephClusterDataSource() datasource.DataSource {
	return &cephClusterDataSource{}
}

func (d *cephClustersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ceph_clusters"
}

func (d *cephClusterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ceph_cluster"
}

// cephClusterAttributes are the fields both the index rows and the single
// cluster publish.
func cephClusterAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"fsid": schema.StringAttribute{
			Description: "The Ceph cluster fsid -- the identity, and the only id any FiveNines surface " +
				"accepts (this data source, the dashboard URL, and the `clusters` scope of " +
				"`fivenines_metric_query`). NOT globally unique: two organizations monitoring one shared " +
				"cluster carry the same fsid. There is deliberately no row `id`.",
			Computed: true,
		},
		"name": schema.StringAttribute{
			Description: "The operator-set name if there is one, else the short fsid -- the label the " +
				"dashboard renders.",
			Computed: true,
		},
		"configured_name": schema.StringAttribute{
			Description: "The raw name column. Null when nobody named the cluster.",
			Computed:    true,
		},
		"promoted": schema.BoolAttribute{
			Description: "Whether this cluster is past the anti-phantom gate. A cluster is promoted " +
				"automatically once two or more fresh hosts confirm the fsid; below that it is usually a " +
				"phantom (a stale ceph.conf on a cloned image) and publishes no health. Promoting is an " +
				"operator action and is deliberately not exposed as a resource.",
			Computed: true,
		},
		"promoted_at": schema.StringAttribute{
			Description: "When the cluster passed the gate. Null while unpromoted.",
			Computed:    true,
		},
		"health": schema.StringAttribute{
			Description: "The derived cluster health, and a WIDER vocabulary than Ceph's own. " +
				"`HEALTH_OK` / `HEALTH_WARN` / `HEALTH_ERR` come from the cluster and are the WORST reading " +
				"among the fresh reachable reporters, so a real error reported by any reachable monitor is " +
				"never masked by a healthier one. Two more come from us and are absences rather than " +
				"verdicts: `STALE` (no reporter has checked in inside the organization's offline threshold) " +
				"and `UNKNOWN` (fresh reporters exist but none could read a status). NULL for an unpromoted " +
				"cluster -- never read a null as healthy.",
			Computed: true,
		},
		"stale": schema.BoolAttribute{
			Description: "No reporter has checked in inside the organization's offline threshold. " +
				"Published even for an unpromoted cluster, whose `health` is null.",
			Computed: true,
		},
		"last_seen_at": schema.StringAttribute{
			Description: "The most recent scrape from ANY reporter, kept separate from the authoritative " +
				"sample's own arrival so the cluster does not read as stale while other hosts keep " +
				"checking in.",
			Computed: true,
		},
		"reporter_count": schema.Int64Attribute{
			Description: "Hosts that have ever reported this cluster.",
			Computed:    true,
		},
		"fresh_reporter_count": schema.Int64Attribute{
			Description: "How many hosts the `health` verdict was derived FROM. Zero means nothing is " +
				"watching this cluster right now, and `health` reads `STALE`.",
			Computed: true,
		},
		"unreachable_reporter_count": schema.Int64Attribute{
			Description: "Fresh hosts that could not reach the cluster this cycle. A per-host badge, not a " +
				"cluster alarm -- one monitor's local network blip is not an outage. It becomes the whole " +
				"story only when it equals `fresh_reporter_count`, which is when `health` reads `UNKNOWN`.",
			Computed: true,
		},
		"authoritative_host_id": schema.StringAttribute{
			Description: "The elected reporter: the most complete fresh one, ties broken by server arrival " +
				"then host id. It is the single writer of the per-entity series. Null whenever the cluster " +
				"is stale -- there is nothing to elect.",
			Computed: true,
		},
		"created_at": schema.StringAttribute{
			Description: "When the cluster row was first recorded.",
			Computed:    true,
		},
		"updated_at": schema.StringAttribute{
			Description: "When the cluster's own columns last changed (a rename, a promotion). The health, " +
				"the freshness and the reporter counts are derived on every read and move WITHOUT touching " +
				"this, which is why `updated_since` is useless as a health change feed.",
			Computed: true,
		},
	}
}

func (d *cephClustersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	rowAttrs := cephClusterAttributes()

	resp.Schema = schema.Schema{
		Description: "The organization's Ceph clusters, with the health derived from their reporter set.\n\n" +
			cephHealthProvenance + "\n\n" +
			"There is no `health` filter, deliberately: the verdict is a fold over the reporter set, and a " +
			"second implementation of it in SQL is how a \"show me what is broken\" query starts " +
			"disagreeing with the `health` each row publishes. A cluster list is a handful of rows -- read " +
			"it and branch on the field. `stale` IS filterable, because it has an exact SQL twin.\n\n" +
			"Use `fivenines_ceph_cluster` for one cluster's per-host reporter breakdown, which this index " +
			"omits.",
		Attributes: map[string]schema.Attribute{
			"query": schema.StringAttribute{
				Description: "Case-insensitive substring match on the operator name or the fsid (the API's " +
					"`q` filter).",
				Optional: true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only return clusters whose `updated_at` is at or after this ISO8601 " +
					"timestamp. Inclusive. NOT a health change feed -- see `updated_at`.",
				Optional: true,
			},
			"promoted": schema.BoolAttribute{
				Description: "Whether the cluster is past the anti-phantom gate. `false` returns the " +
					"clusters awaiting confirmation -- the ones whose `health` is null. Omit for both.",
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
				Description: "Column to sort by. Defaults to `configured_name` -- the order the dashboard " +
					"list uses.\n\nNOTE that `configured_name` is the RAW column, not the `name` each row " +
					"publishes: `name` falls back to the short fsid when nobody named the cluster, while " +
					"`configured_name` stays null. So an organization whose clusters were never named sorts " +
					"by null-then-id while every visible `name` reads as an fsid prefix. Sort by `fsid` for " +
					"an order that matches what you can see.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("configured_name", "fsid", "created_at", "updated_at", "promoted_at"),
				},
			},
			"direction": schema.StringAttribute{
				Description: `Sort direction: "asc" or "desc". Defaults to "asc".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"ceph_clusters": schema.ListNestedAttribute{
				Description:  "Matching clusters, each with its derived health and the provenance of it.",
				Computed:     true,
				NestedObject: schema.NestedAttributeObject{Attributes: rowAttrs},
			},
		},
	}
}

func (d *cephClusterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := cephClusterAttributes()
	// fsid is the argument here, not a computed column.
	attrs["fsid"] = schema.StringAttribute{
		Description: "The cluster fsid -- the identifier, not a row id. Unique within your organization.",
		Required:    true,
	}
	attrs["reporters"] = schema.ListNestedAttribute{
		Description: "The per-host breakdown -- the \"Reporting Hosts\" table from the dashboard, which " +
			"the `fivenines_ceph_clusters` index omits.",
		Computed:     true,
		NestedObject: schema.NestedAttributeObject{Attributes: cephReporterAttributes()},
	}

	resp.Schema = schema.Schema{
		Description: "One Ceph cluster, plus the per-host `reporters` breakdown the " +
			"`fivenines_ceph_clusters` index omits.\n\n" +
			cephHealthProvenance + "\n\n" +
			"EACH REPORTER ROW IS PROVENANCE, NOT A SECOND VERDICT. `last_health` is what THAT host's " +
			"`ceph status` last returned, and a host that has gone silent keeps its last reading forever. " +
			"Read `fresh` first: only fresh rows contribute to the cluster's `health`, and `authoritative` " +
			"marks the elected one.\n\n" +
			"The metric series live on `fivenines_metric_query` with `clusters = [\"<fsid>\"]`.",
		Attributes: attrs,
	}
}

func cephReporterAttributes() map[string]schema.Attribute {
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
			Description: "Whether this row is recent enough to count toward the cluster verdict. The window " +
				"is the ORGANIZATION's offline threshold, the same one instance liveness uses. READ THIS " +
				"FIRST: a stale row keeps its last reading forever.",
			Computed: true,
		},
		"authoritative": schema.BoolAttribute{
			Description: "The elected writer of the per-entity series. Exactly one reporter carries this, " +
				"and none does while the cluster is stale.",
			Computed: true,
		},
		"reachable": schema.BoolAttribute{
			Description: "Whether this host reached the cluster on its last scrape. A `false` here is a " +
				"per-host badge, never a cluster-level alarm.",
			Computed: true,
		},
		"status_ok": schema.BoolAttribute{
			Description: "Whether the `ceph status` call succeeded. This and the four flags below report " +
				"CALL success, not agreement between hosts.",
			Computed: true,
		},
		"df_ok":     schema.BoolAttribute{Description: "Whether the `ceph df` call succeeded.", Computed: true},
		"tree_ok":   schema.BoolAttribute{Description: "Whether the `ceph osd tree` call succeeded.", Computed: true},
		"osd_df_ok": schema.BoolAttribute{Description: "Whether the `ceph osd df` call succeeded.", Computed: true},
		"perf_ok":   schema.BoolAttribute{Description: "Whether the performance scrape succeeded.", Computed: true},
		"completeness_score": schema.Int64Attribute{
			Description: "How much of the scrape this host carries, and the precedence key for the " +
				"authoritative election. Read it against `max_completeness_score`.",
			Computed: true,
		},
		"max_completeness_score": schema.Int64Attribute{
			Description: "The denominator for `completeness_score`, published so the number is readable " +
				"without knowing the formula.",
			Computed: true,
		},
		"last_health": schema.StringAttribute{
			Description: "THIS HOST's last reported health string, which is NOT the cluster's -- the " +
				"cluster's `health` is the only value that answers \"is the cluster healthy\". A host that " +
				"has gone silent keeps this forever, so read `fresh` alongside it.",
			Computed: true,
		},
		"last_error": schema.StringAttribute{
			Description: "The agent's own words when the scrape failed. Null when it did not.",
			Computed:    true,
		},
		"last_synced_at": schema.StringAttribute{
			Description: "When this scrape was stored. A server clock, never the agent's.",
			Computed:    true,
		},
		"received_at": schema.StringAttribute{
			Description: "When the tick carrying this scrape arrived. Also a server clock.",
			Computed:    true,
		},
	}
}

func (d *cephClustersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *cephClusterDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *cephClustersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state cephClustersModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clusters, err := d.client.ListCephClusters(ctx, client.CephClusterListOptions{
		Query:        filterString(state.Query),
		UpdatedSince: filterString(state.UpdatedSince),
		Promoted:     filterBool(state.Promoted),
		Stale:        filterBool(state.Stale),
		Order:        filterString(state.Order),
		Direction:    filterString(state.Direction),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing Ceph clusters", err.Error())
		return
	}

	// Non-nil even when nothing matches: a null list breaks length()/for_each.
	state.CephClusters = make([]cephClusterModel, 0, len(clusters))
	for _, c := range clusters {
		state.CephClusters = append(state.CephClusters, cephClusterRow(c))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *cephClusterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state cephClusterDetailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cluster, err := d.client.GetCephCluster(ctx, state.FSID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading Ceph cluster", err.Error())
		return
	}

	// The CONFIGURED fsid is preserved: `fsid` is a Required argument, and
	// Terraform fails an apply with "Provider produced inconsistent result" if a
	// configured value comes back changed. They match today (the API looks the
	// cluster up by exact fsid), which is exactly why re-deriving it would be a
	// silent dependency on that staying true.
	fsid := state.FSID
	state.cephClusterModel = cephClusterRow(*cluster)
	state.FSID = fsid

	state.Reporters = make([]cephClusterReporterModel, 0, len(cluster.Reporters))
	for _, r := range cluster.Reporters {
		state.Reporters = append(state.Reporters, cephClusterReporterModel{
			HostID:               types.StringValue(r.HostID),
			HostName:             optionalString(r.HostName),
			Fresh:                types.BoolValue(r.Fresh),
			Authoritative:        types.BoolValue(r.Authoritative),
			Reachable:            types.BoolValue(r.Reachable),
			StatusOK:             types.BoolValue(r.StatusOK),
			DfOK:                 types.BoolValue(r.DfOK),
			TreeOK:               types.BoolValue(r.TreeOK),
			OsdDfOK:              types.BoolValue(r.OsdDfOK),
			PerfOK:               types.BoolValue(r.PerfOK),
			CompletenessScore:    types.Int64Value(r.CompletenessScore),
			MaxCompletenessScore: types.Int64Value(r.MaxCompletenessScore),
			LastHealth:           optionalString(r.LastHealth),
			LastError:            optionalString(r.LastError),
			LastSyncedAt:         optionalString(r.LastSyncedAt),
			ReceivedAt:           optionalString(r.ReceivedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func cephClusterRow(c client.CephCluster) cephClusterModel {
	return cephClusterModel{
		FSID:                     types.StringValue(c.FSID),
		Name:                     types.StringValue(c.Name),
		ConfiguredName:           optionalString(c.ConfiguredName),
		Promoted:                 types.BoolValue(c.Promoted),
		PromotedAt:               optionalString(c.PromotedAt),
		Health:                   optionalString(c.Health),
		Stale:                    types.BoolValue(c.Stale),
		LastSeenAt:               optionalString(c.LastSeenAt),
		ReporterCount:            types.Int64Value(c.ReporterCount),
		FreshReporterCount:       types.Int64Value(c.FreshReporterCount),
		UnreachableReporterCount: types.Int64Value(c.UnreachableReporterCount),
		AuthoritativeHostID:      optionalString(c.AuthoritativeHostID),
		CreatedAt:                optionalString(c.CreatedAt),
		UpdatedAt:                optionalString(c.UpdatedAt),
	}
}

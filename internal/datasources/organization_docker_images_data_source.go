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

var _ datasource.DataSource = &orgDockerImagesDataSource{}

type orgDockerImagesDataSource struct {
	client *client.Client
}

type orgDockerImagesModel struct {
	State             types.String `tfsdk:"state"`
	Ecosystem         types.String `tfsdk:"ecosystem"`
	PackagesTruncated types.Bool   `tfsdk:"packages_truncated"`
	Query             types.String `tfsdk:"q"`
	UpdatedSince      types.String `tfsdk:"updated_since"`
	Order             types.String `tfsdk:"order"`
	Direction         types.String `tfsdk:"direction"`

	Images  []dockerImageModel `tfsdk:"images"`
	Posture *imagePostureModel `tfsdk:"posture"`
}

type dockerImageModel struct {
	ID                         types.String   `tfsdk:"id"`
	OrganizationID             types.Int64    `tfsdk:"organization_id"`
	ImageID                    types.String   `tfsdk:"image_id"`
	ShortDigest                types.String   `tfsdk:"short_digest"`
	DisplayName                types.String   `tfsdk:"display_name"`
	Tags                       []types.String `tfsdk:"tags"`
	RepoDigests                []types.String `tfsdk:"repo_digests"`
	Distro                     types.String   `tfsdk:"distro"`
	Ecosystem                  types.String   `tfsdk:"ecosystem"`
	State                      types.String   `tfsdk:"state"`
	StateReason                types.String   `tfsdk:"state_reason"`
	StateErrorType             types.String   `tfsdk:"state_error_type"`
	Countable                  types.Bool     `tfsdk:"countable"`
	VulnerabilityCount         types.Int64    `tfsdk:"vulnerability_count"`
	CriticalVulnerabilityCount types.Int64    `tfsdk:"critical_vulnerability_count"`
	PackagesTruncated          types.Bool     `tfsdk:"packages_truncated"`
	FindingCountIsFloor        types.Bool     `tfsdk:"finding_count_is_floor"`
	LastSeenAt                 types.String   `tfsdk:"last_seen_at"`
	InventoryReceivedAt        types.String   `tfsdk:"inventory_received_at"`
	LastScannedAt              types.String   `tfsdk:"last_scanned_at"`
	CreatedAt                  types.String   `tfsdk:"created_at"`
	UpdatedAt                  types.String   `tfsdk:"updated_at"`
	RunningHostCount           types.Int64    `tfsdk:"running_host_count"`
}

type imagePostureModel struct {
	Pending     types.Int64 `tfsdk:"pending"`
	Scanned     types.Int64 `tfsdk:"scanned"`
	Unsupported types.Int64 `tfsdk:"unsupported"`
	Unscannable types.Int64 `tfsdk:"unscannable"`
}

func NewOrganizationDockerImagesDataSource() datasource.DataSource {
	return &orgDockerImagesDataSource{}
}

func (d *orgDockerImagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_docker_images"
}

func (d *orgDockerImagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the organization's container images with their scan verdicts. " +
			"Images are organization-scoped: one image running on 50 hosts is one row and one scan, " +
			"and `running_host_count` is its blast radius.\n\n" +
			"Images that could not be scanned are listed, never filtered out - hiding them would make " +
			"the list read as a complete picture of your image risk when it is not. They carry a null " +
			"`vulnerability_count`, never `0`: read `state` before the counts.\n\n" +
			"Requires a plan that includes `security_details` (Pro or above); a plan without it gets " +
			"an error, not an empty list.",
		Attributes: map[string]schema.Attribute{
			"state": schema.StringAttribute{
				Description: "Only return images in this scan state (pending, scanned, unsupported, unscannable).",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("pending", "scanned", "unsupported", "unscannable"),
				},
			},
			"ecosystem": schema.StringAttribute{
				Description: "Only return images whose packages were matched against this OSV ecosystem, e.g. `Debian:12`.",
				Optional:    true,
				Validators: []validator.String{
					// An empty string would be dropped by the filter helpers and
					// silently widen the query — on a security index, the answer
					// to a wider question still looks like an answer.
					stringvalidator.LengthAtLeast(1),
				},
			},
			"packages_truncated": schema.BoolAttribute{
				Description: "When true, only return the images whose package list the agent capped - the ones whose counts are a floor rather than a total.",
				Optional:    true,
			},
			"q": schema.StringAttribute{
				Description: "Case-insensitive substring match on the tags, digests, distro and ecosystem.",
				Optional:    true,
				Validators: []validator.String{
					// An empty string would be dropped by the filter helpers and
					// silently widen the query — on a security index, the answer
					// to a wider question still looks like an answer.
					stringvalidator.LengthAtLeast(1),
				},
			},
			"updated_since": schema.StringAttribute{
				Description: "Only images whose `updated_at` is at or after this ISO 8601 timestamp (inclusive). An image row is a stable upsert keyed by its digest, so unlike the findings indexes its id does not move — but there are still no tombstones: an image nobody has run for 30 days is reaped, and a sync built on this cursor alone will not see it go.",
				Optional:    true,
			},
			"order": schema.StringAttribute{
				Description: "Sort column. Defaults to `last_seen_at` — an image list is an inventory of what is RUNNING. The two count columns sort unscanned images last in both directions rather than filing them among the images that genuinely have none.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("last_seen_at", "last_scanned_at", "created_at", "updated_at",
						"vulnerability_count", "critical_vulnerability_count"),
				},
			},
			"direction": schema.StringAttribute{
				Description: "Sort direction, `asc` or `desc`. Defaults to `desc`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"images": schema.ListNestedAttribute{
				Description:  "The matching container images.",
				Computed:     true,
				NestedObject: schema.NestedAttributeObject{Attributes: dockerImageAttributes()},
			},
			"posture": schema.SingleNestedAttribute{
				Description: "Image counts per scan state across the whole organization. Deliberately not narrowed by the filters above: it is the answer to \"is this list the complete picture\", which a filtered count could not be.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"pending": schema.Int64Attribute{
						Description: "Discovered on a collect tick, package inventory not received yet. Terminal without the container-image-scanning entitlement.",
						Computed:    true,
					},
					"scanned": schema.Int64Attribute{
						Description: "Inventory received and matched against the advisory data - the only state whose counts mean anything.",
						Computed:    true,
					},
					"unsupported": schema.Int64Attribute{
						Description: "No mappable distro or package manager (scratch, distroless). Permanent for this digest.",
						Computed:    true,
					},
					"unscannable": schema.Int64Attribute{
						Description: "Extraction failed. `state_error_type` says whether that is worth retrying.",
						Computed:    true,
					},
				},
			},
		},
	}
}

// dockerImageAttributes is the image row, shared with the `docker_image` block
// the vulnerabilities data source returns for an image drill-down.
func dockerImageAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "The FiveNines image row ID (UUID) - what `fivenines_vulnerabilities.docker_image_id` takes.",
			Computed:    true,
		},
		"organization_id": schema.Int64Attribute{
			Description: "Owning organization.",
			Computed:    true,
		},
		"image_id": schema.StringAttribute{
			Description: "The `sha256:` config digest - the durable, content-addressed identity the scan is keyed on.",
			Computed:    true,
		},
		"short_digest": schema.StringAttribute{
			Description: "First 12 characters of the digest.",
			Computed:    true,
		},
		"display_name": schema.StringAttribute{
			Description: "A tag if the image carries one, else the short digest. A label, not an ID.",
			Computed:    true,
		},
		"tags": schema.ListAttribute{
			Description: "Every tag on the image. May be empty.",
			Computed:    true,
			ElementType: types.StringType,
		},
		"repo_digests": schema.ListAttribute{
			Description: "Registry digests - what an operator pastes into `docker pull`.",
			Computed:    true,
			ElementType: types.StringType,
		},
		"distro": schema.StringAttribute{
			Description: "The OS the agent detected inside the image.",
			Computed:    true,
		},
		"ecosystem": schema.StringAttribute{
			Description: "The advisory ecosystem the packages were matched against.",
			Computed:    true,
		},
		"state": schema.StringAttribute{
			Description: "The scan verdict, and the field to read BEFORE the counts: `pending` (discovered, inventory not received), `scanned` (matched against the advisory data), `unsupported` (no mappable distro or package manager - permanent for this digest), `unscannable` (extraction failed).",
			Computed:    true,
		},
		"state_reason": schema.StringAttribute{
			Description: "The human explanation behind a non-scanned state.",
			Computed:    true,
		},
		"state_error_type": schema.StringAttribute{
			Description: "The machine-readable code beside `state_reason`. Only `api_error` is transient - the rest are permanent for an immutable digest, so do not retry them.",
			Computed:    true,
		},
		"countable": schema.BoolAttribute{
			Description: "Whether the counts below mean anything at all. False for every non-scanned state.",
			Computed:    true,
		},
		"vulnerability_count": schema.Int64Attribute{
			Description: "Null unless `state` is `scanned`. A null is \"not scanned\", never \"zero vulnerabilities\" - only on a scanned image does `0` mean clean.",
			Computed:    true,
		},
		"critical_vulnerability_count": schema.Int64Attribute{
			Description: "Critical findings. Null unless `state` is `scanned`.",
			Computed:    true,
		},
		"packages_truncated": schema.BoolAttribute{
			Description: "The agent capped the package list it uploaded.",
			Computed:    true,
		},
		"finding_count_is_floor": schema.BoolAttribute{
			Description: "The counts are a FLOOR rather than a total (a scanned image whose package list was capped). Render \"12+\" rather than \"12\", and do not treat the count as complete in a gate.",
			Computed:    true,
		},
		"last_seen_at": schema.StringAttribute{
			Description: "Last time a container running this image was reported in this organization. Rows unseen for 30 days are reaped.",
			Computed:    true,
		},
		"inventory_received_at": schema.StringAttribute{
			Description: "When the agent last uploaded this image's package list.",
			Computed:    true,
		},
		"last_scanned_at": schema.StringAttribute{
			Description: "When the package list was last matched against the advisory data.",
			Computed:    true,
		},
		"created_at": schema.StringAttribute{
			Description: "Creation timestamp.",
			Computed:    true,
		},
		"updated_at": schema.StringAttribute{
			Description: "Last update timestamp.",
			Computed:    true,
		},
		"running_host_count": schema.Int64Attribute{
			Description: "Instances currently running a container off this image - the blast radius. Counts a host with a stopped container too: the image is still deployed there.",
			Computed:    true,
		},
	}
}

func (d *orgDockerImagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *orgDockerImagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state orgDockerImagesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	list, err := d.client.ListDockerImages(ctx, client.DockerImageListOptions{
		State:             filterString(state.State),
		Ecosystem:         filterString(state.Ecosystem),
		PackagesTruncated: filterBool(state.PackagesTruncated),
		Query:             filterString(state.Query),
		UpdatedSince:      filterString(state.UpdatedSince),
		Order:             filterString(state.Order),
		Direction:         filterString(state.Direction),
	})
	if err != nil {
		addSecurityError(&resp.Diagnostics, "Error listing container images", err)
		return
	}

	state.Images = make([]dockerImageModel, len(list.Images))
	for i, img := range list.Images {
		state.Images[i] = mapDockerImage(img)
	}
	state.Posture = &imagePostureModel{
		Pending:     types.Int64Value(list.Posture.Pending),
		Scanned:     types.Int64Value(list.Posture.Scanned),
		Unsupported: types.Int64Value(list.Posture.Unsupported),
		Unscannable: types.Int64Value(list.Posture.Unscannable),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// countableInt64 is the honesty contract enforced locally rather than trusted.
//
// The API already nulls these counts on a non-scanned image, so this is belt
// and braces — but it is the ONE invariant whose failure is silent and whose
// cost is an all-clear on an image nobody scanned. The underlying columns are
// NOT NULL with a default of 0, so any path that ever serves the raw column (a
// serializer regression, a cache, an older deployment behind a proxy) hands us
// a literal 0 that this provider would otherwise publish as a clean bill of
// health. `countable` travels in the same payload, so refusing costs nothing.
//
// The server double-gates this rule for the same reason, in the same shape.
func countableInt64(countable bool, count *int64) types.Int64 {
	if !countable {
		return types.Int64Null()
	}
	return optionalInt64(count)
}

// mapDockerImage carries the honesty contract into state: the counts stay null
// on anything the API did not report as scanned, so a `pending` image can never
// be read as "0 vulnerabilities".
func mapDockerImage(img client.DockerImage) dockerImageModel {
	return dockerImageModel{
		ID:                         types.StringValue(img.ID),
		OrganizationID:             types.Int64Value(img.OrganizationID),
		ImageID:                    types.StringValue(img.ImageID),
		ShortDigest:                types.StringValue(img.ShortDigest),
		DisplayName:                types.StringValue(img.DisplayName),
		Tags:                       stringList(img.Tags),
		RepoDigests:                stringList(img.RepoDigests),
		Distro:                     optionalString(img.Distro),
		Ecosystem:                  optionalString(img.Ecosystem),
		State:                      types.StringValue(img.State),
		StateReason:                optionalString(img.StateReason),
		StateErrorType:             optionalString(img.StateErrorType),
		Countable:                  types.BoolValue(img.Countable),
		VulnerabilityCount:         countableInt64(img.Countable, img.VulnerabilityCount),
		CriticalVulnerabilityCount: countableInt64(img.Countable, img.CriticalVulnerabilityCount),
		PackagesTruncated:          types.BoolValue(img.PackagesTruncated),
		FindingCountIsFloor:        types.BoolValue(img.FindingCountIsFloor),
		LastSeenAt:                 optionalString(img.LastSeenAt),
		InventoryReceivedAt:        optionalString(img.InventoryReceivedAt),
		LastScannedAt:              optionalString(img.LastScannedAt),
		CreatedAt:                  types.StringValue(img.CreatedAt),
		UpdatedAt:                  types.StringValue(img.UpdatedAt),
		RunningHostCount:           optionalInt64(img.RunningHostCount),
	}
}

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

var _ datasource.DataSource = &statusPageSubscribersDataSource{}

type statusPageSubscribersDataSource struct {
	client *client.Client
}

type statusPageSubscribersModel struct {
	StatusPageID types.Int64                 `tfsdk:"status_page_id"`
	Query        types.String                `tfsdk:"query"`
	UpdatedSince types.String                `tfsdk:"updated_since"`
	Status       types.String                `tfsdk:"status"`
	Order        types.String                `tfsdk:"order"`
	Direction    types.String                `tfsdk:"direction"`
	Subscribers  []statusPageSubscriberModel `tfsdk:"subscribers"`
}

type statusPageSubscriberModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Email       types.String `tfsdk:"email"`
	Status      types.String `tfsdk:"status"`
	ConfirmedAt types.String `tfsdk:"confirmed_at"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewStatusPageSubscribersDataSource() datasource.DataSource {
	return &statusPageSubscribersDataSource{}
}

func (d *statusPageSubscribersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page_subscribers"
}

func (d *statusPageSubscribersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The email subscribers of a status page, newest first.\n\n" +
			"REQUIRES THE `status_pages: update` PERMISSION even though it only reads. Subscriber " +
			"addresses are PII, so the permission that gates them is the one for CHANGING status pages, " +
			"not the one for reading them: a member on a read-only role gets a 403 here while reading " +
			"the page itself succeeds. This is the member's role, NOT the API token's read/write scope " +
			"-- widening the token will not help. The addresses also land in Terraform state in plain " +
			"text.\n\n" +
			"`updated_since` MOVES ON A SUBSCRIBE AND A CONFIRM BUT NEVER TOMBSTONES A REMOVAL: an " +
			"unsubscribe, an admin delete and the expired-confirmation cleanup all drop the row outright. " +
			"A reconciler still needs a periodic unfiltered read to notice departures.\n\n" +
			"The CSV export and the per-subscriber DELETE are deliberately out of scope: the export is " +
			"audited and belongs to subject-access requests rather than to a plan, and removing somebody " +
			"else's subscription is not a thing Terraform should converge on.",
		Attributes: map[string]schema.Attribute{
			"status_page_id": schema.Int64Attribute{
				Description: "ID of the status page whose subscribers to list.",
				Required:    true,
			},
			"query": schema.StringAttribute{
				Description: "Case-insensitive substring match on the email address (the API's `q` filter). " +
					"A `%` in the term matches a literal percent sign.",
				Optional: true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only return subscribers whose `updated_at` is at or after this ISO8601 " +
					"timestamp. Inclusive, so a row updated in the same instant as the cursor comes back " +
					"rather than falling through the gap. Surfaces subscribes and confirmations only -- see " +
					"the note on removals above.",
				Optional: true,
			},
			"status": schema.StringAttribute{
				Description: "Filter by confirmation state. `pending` is an address that has been sent a " +
					"confirmation email and has not clicked it; it receives no notifications. The two " +
					"values partition the list, so no subscriber is unreachable from either side.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("confirmed", "pending"),
				},
			},
			"order": schema.StringAttribute{
				Description: "Column to sort by. Defaults to `id`, which is newest-first and is the one " +
					"order this index serves straight from an index.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("id", "created_at", "updated_at", "email", "confirmed_at"),
				},
			},
			"direction": schema.StringAttribute{
				Description: `Sort direction: "asc" or "desc". Defaults to "desc".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"subscribers": schema.ListNestedAttribute{
				Description: "Matching subscribers.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Subscriber ID.",
							Computed:    true,
						},
						"email": schema.StringAttribute{
							Description: "The subscribed email address.\n\nMARKED SENSITIVE: these are third-party " +
								"addresses, so Terraform redacts them from plan and apply output and refuses to put " +
								"them in an output that is not itself `sensitive`. Use `nonsensitive()` where you " +
								"genuinely need the raw value. Sensitive does NOT encrypt state -- the addresses are " +
								"still stored in plain text there.",
							Computed:  true,
							Sensitive: true,
						},
						"status": schema.StringAttribute{
							Description: "`confirmed` or `pending`. Only confirmed subscribers receive notifications.",
							Computed:    true,
						},
						"confirmed_at": schema.StringAttribute{
							Description: "When the double opt-in link was followed. Null while pending.",
							Computed:    true,
						},
						"created_at": schema.StringAttribute{
							Description: "When the address subscribed.",
							Computed:    true,
						},
						"updated_at": schema.StringAttribute{
							Description: "The counterpart of the `updated_since` cursor: pass the newest one " +
								"you received back as the next cursor.",
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func (d *statusPageSubscribersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *statusPageSubscribersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state statusPageSubscribersModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := client.StatusPageSubscriberListOptions{
		Query:        filterString(state.Query),
		UpdatedSince: filterString(state.UpdatedSince),
		Status:       filterString(state.Status),
		Order:        filterString(state.Order),
		Direction:    filterString(state.Direction),
	}

	subscribers, err := d.client.ListStatusPageSubscribers(ctx, state.StatusPageID.ValueInt64(), opts)
	if err != nil {
		// The 403 gets its own diagnostic: it means the token lacks the
		// `status_pages: update` permission, and reading that as "this page has
		// no subscribers" is exactly the wrong conclusion.
		if client.IsForbidden(err) {
			resp.Diagnostics.AddError(
				"Reading status page subscribers requires the status_pages update permission",
				"FiveNines refused the request with 403 Forbidden. Subscriber emails are PII, so this "+
					"index requires the `status_pages: update` permission even to read it -- a member on "+
					"a read-only role is not enough. This is the member's ROLE, not the API token's "+
					"read/write scope; widening the token will not help.\n\nThis is deliberately an "+
					"error rather than an empty result: an empty list here would read as \"nobody is "+
					"subscribed\".\n\nAPI response: "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError("Error listing status page subscribers", err.Error())
		return
	}

	// Non-nil even when nothing matches: a nil slice serialises as a null list,
	// and length()/for_each/toset over a null fail.
	state.Subscribers = make([]statusPageSubscriberModel, 0, len(subscribers))
	for _, s := range subscribers {
		state.Subscribers = append(state.Subscribers, statusPageSubscriberModel{
			ID:          types.Int64Value(s.ID),
			Email:       types.StringValue(s.Email),
			Status:      types.StringValue(s.Status),
			ConfirmedAt: optionalString(s.ConfirmedAt),
			CreatedAt:   optionalString(s.CreatedAt),
			UpdatedAt:   optionalString(s.UpdatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

package resources

import (
	"context"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// --- mapOrganizationToState ---

func TestMapOrganizationToState(t *testing.T) {
	name := "Acme Corp"
	seats := int64(10)
	org := &client.Organization{
		ID: 42, Name: &name, Slug: "acme-corp", DisplayName: "Acme Corp",
		Plan: "pro", Trialing: false, SeatsUsed: 4, SeatsTotal: &seats,
		SeatsRemaining: 6, MembersCount: 3, PendingInvitationsCount: 1,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
	}

	state := &organizationModel{}
	mapOrganizationToState(org, state)

	if state.ID.ValueInt64() != 42 {
		t.Errorf("expected ID 42, got %d", state.ID.ValueInt64())
	}
	if state.Name.ValueString() != "Acme Corp" {
		t.Errorf("expected name Acme Corp, got %q", state.Name.ValueString())
	}
	if state.Slug.ValueString() != "acme-corp" {
		t.Errorf("expected slug acme-corp, got %q", state.Slug.ValueString())
	}
	if state.SeatsTotal.ValueInt64() != 10 {
		t.Errorf("expected seats_total 10, got %d", state.SeatsTotal.ValueInt64())
	}
	if state.SeatsRemaining.ValueInt64() != 6 {
		t.Errorf("expected seats_remaining 6, got %d", state.SeatsRemaining.ValueInt64())
	}
}

// An unnamed organization on an uncapped plan: both read back as null, not "".
func TestMapOrganizationToState_Nulls(t *testing.T) {
	state := &organizationModel{}
	mapOrganizationToState(&client.Organization{ID: 42, Slug: "acme-corp"}, state)

	if !state.Name.IsNull() {
		t.Errorf("expected a null name, got %q", state.Name.ValueString())
	}
	if !state.SeatsTotal.IsNull() {
		t.Errorf("expected a null seats_total, got %d", state.SeatsTotal.ValueInt64())
	}
}

// --- mapMemberToState ---

func TestMapMemberToState(t *testing.T) {
	member := &client.OrganizationMember{
		ID: 7, UserID: 91, Email: "engineer@acme.com", Role: "admin",
		TwoFactorEnabled: true,
		JoinedAt:         "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
	}

	state := &organizationMemberModel{}
	mapMemberToState(member, state)

	if state.ID.ValueInt64() != 7 {
		t.Errorf("expected membership id 7, got %d", state.ID.ValueInt64())
	}
	if state.UserID.ValueInt64() != 91 {
		t.Errorf("expected user id 91, got %d", state.UserID.ValueInt64())
	}
	if state.Email.ValueString() != "engineer@acme.com" {
		t.Errorf("expected engineer@acme.com, got %q", state.Email.ValueString())
	}
	if state.Role.ValueString() != "admin" {
		t.Errorf("expected role admin, got %q", state.Role.ValueString())
	}
	if !state.TwoFactorEnabled.ValueBool() {
		t.Error("expected two_factor_enabled true")
	}
}

// The API normalizes addresses; overwriting the configured casing would trip
// Terraform's "inconsistent result after apply" check on a Required attribute.
func TestMapMemberToState_KeepsConfiguredEmailCasing(t *testing.T) {
	state := &organizationMemberModel{Email: types.StringValue("Engineer@Acme.com")}
	mapMemberToState(&client.OrganizationMember{ID: 7, Email: "engineer@acme.com", Role: "member"}, state)

	if state.Email.ValueString() != "Engineer@Acme.com" {
		t.Errorf("expected the configured casing to survive, got %q", state.Email.ValueString())
	}
}

// A genuinely different address is drift, and has to be reported as such.
func TestMapMemberToState_ReportsChangedEmail(t *testing.T) {
	state := &organizationMemberModel{Email: types.StringValue("old@acme.com")}
	mapMemberToState(&client.OrganizationMember{ID: 7, Email: "new@acme.com", Role: "member"}, state)

	if state.Email.ValueString() != "new@acme.com" {
		t.Errorf("expected the API address, got %q", state.Email.ValueString())
	}
}

// --- mapInvitationToState ---

func TestMapInvitationToState(t *testing.T) {
	invitedBy := "admin@acme.com"
	invitation := &client.OrganizationInvitation{
		ID: 12, Email: "newhire@acme.com", Role: "member", Status: "pending",
		InvitedBy: &invitedBy, ExpiresAt: "2026-09-08T00:00:00Z",
		CreatedAt: "2026-09-01T00:00:00Z", UpdatedAt: "2026-09-01T00:00:00Z",
	}

	state := &organizationInvitationModel{}
	mapInvitationToState(invitation, state)

	if state.ID.ValueInt64() != 12 {
		t.Errorf("expected id 12, got %d", state.ID.ValueInt64())
	}
	if state.Status.ValueString() != "pending" {
		t.Errorf("expected status pending, got %q", state.Status.ValueString())
	}
	if state.InvitedBy.ValueString() != "admin@acme.com" {
		t.Errorf("expected invited_by admin@acme.com, got %q", state.InvitedBy.ValueString())
	}
	if !state.AcceptedAt.IsNull() {
		t.Errorf("expected a null accepted_at while pending, got %q", state.AcceptedAt.ValueString())
	}
}

func TestMapInvitationToState_KeepsConfiguredEmailCasing(t *testing.T) {
	state := &organizationInvitationModel{Email: types.StringValue("NewHire@Acme.com")}
	mapInvitationToState(&client.OrganizationInvitation{ID: 12, Email: "newhire@acme.com", Role: "member"}, state)

	if state.Email.ValueString() != "NewHire@Acme.com" {
		t.Errorf("expected the configured casing to survive, got %q", state.Email.ValueString())
	}
}

// --- requiresReplaceOnDifferentAddress ---

// Replacing one of these resources is destructive — it offboards a person, or
// revokes a live invitation — so a change that only re-cases the configured
// address must not trigger one.
func TestRequiresReplaceOnDifferentAddress(t *testing.T) {
	tests := []struct {
		name        string
		state, plan string
		want        bool
	}{
		{"different person", "old@acme.com", "new@acme.com", true},
		{"case only", "Lead@Acme.com", "lead@acme.com", false},
		{"identical", "lead@acme.com", "lead@acme.com", false},
	}

	ctx := context.Background()
	schema := fwresource.Schema{
		Attributes: map[string]fwresource.Attribute{
			"email": fwresource.StringAttribute{Required: true},
		},
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"email": tftypes.String}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The framework short-circuits the modifier on a null State or Plan,
			// so both carry a real object the way an update does.
			req := planmodifier.StringRequest{
				Path:        path.Root("email"),
				StateValue:  types.StringValue(tt.state),
				PlanValue:   types.StringValue(tt.plan),
				ConfigValue: types.StringValue(tt.plan),
				State: tfsdk.State{Schema: schema, Raw: tftypes.NewValue(objType,
					map[string]tftypes.Value{"email": tftypes.NewValue(tftypes.String, tt.state)})},
				Plan: tfsdk.Plan{Schema: schema, Raw: tftypes.NewValue(objType,
					map[string]tftypes.Value{"email": tftypes.NewValue(tftypes.String, tt.plan)})},
			}

			resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
			requiresReplaceOnDifferentAddress().PlanModifyString(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if resp.RequiresReplace != tt.want {
				t.Errorf("state %q -> plan %q: RequiresReplace = %v, want %v",
					tt.state, tt.plan, resp.RequiresReplace, tt.want)
			}
		})
	}
}

package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestEnrollmentTokenResourceSchema(t *testing.T) {
	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewEnrollmentTokenResource().Schema(ctx, fwresource.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema implementation errors: %v", diags)
	}
}

func TestMapEnrollmentTokenToState_Create(t *testing.T) {
	token := &client.EnrollmentToken{
		ID:                   7,
		Name:                 "web fleet",
		Active:               true,
		HostsRegisteredCount: 0,
		CreatedAt:            "2026-01-01T00:00:00Z",
		UpdatedAt:            "2026-01-01T00:00:00Z",
		Token:                "a1b2c3",
		InstallCommand:       "wget ... && sudo sh fivenines_setup.sh a1b2c3",
	}

	state := &enrollmentTokenModel{}
	mapEnrollmentTokenToState(token, state)

	if state.ID.ValueInt64() != 7 {
		t.Errorf("expected id 7, got %d", state.ID.ValueInt64())
	}
	if state.Name.ValueString() != "web fleet" {
		t.Errorf("expected name 'web fleet', got %q", state.Name.ValueString())
	}
	if state.Token.ValueString() != "a1b2c3" {
		t.Errorf("expected the token value, got %q", state.Token.ValueString())
	}
	if state.InstallCommand.ValueString() != token.InstallCommand {
		t.Errorf("expected the install command, got %q", state.InstallCommand.ValueString())
	}
	if state.CreatedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("expected created_at, got %q", state.CreatedAt.ValueString())
	}
	if state.UpdatedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("expected updated_at, got %q", state.UpdatedAt.ValueString())
	}
	if !state.LastUsedAt.IsNull() {
		t.Error("expected last_used_at to be null on a token nothing has used")
	}
}

// The value is returned once, on create. Index and revoke responses carry an
// empty Token, and mapping one must not blank the copy Terraform holds — that is
// the only copy in existence. This is the guard for the create-ordering bug
// caught during implementation: nothing else in the provider can restore the
// value once it is gone.
func TestMapEnrollmentTokenToState_PreservesValueAcrossValuelessResponse(t *testing.T) {
	state := &enrollmentTokenModel{
		Token:          types.StringValue("a1b2c3"),
		InstallCommand: types.StringValue("wget ... a1b2c3"),
	}

	mapEnrollmentTokenToState(&client.EnrollmentToken{
		ID: 7, Name: "web fleet", Active: true, UpdatedAt: "2026-01-02T00:00:00Z",
	}, state)

	if state.Token.ValueString() != "a1b2c3" {
		t.Errorf("expected the value to survive a valueless response, got %q", state.Token.ValueString())
	}
	if state.InstallCommand.ValueString() != "wget ... a1b2c3" {
		t.Errorf("expected install_command to survive, got %q", state.InstallCommand.ValueString())
	}
	// The metadata around it still refreshes.
	if state.UpdatedAt.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("expected updated_at to refresh, got %q", state.UpdatedAt.ValueString())
	}
}

// An imported token has no value in state and no way to acquire one; mapping
// must leave it null rather than inventing an empty string, which would render
// as "" in `terraform output` and read as a real (broken) credential.
func TestMapEnrollmentTokenToState_ImportedTokenStaysNull(t *testing.T) {
	state := &enrollmentTokenModel{}

	mapEnrollmentTokenToState(&client.EnrollmentToken{ID: 7, Name: "web fleet", Active: true}, state)

	if !state.Token.IsNull() {
		t.Errorf("expected token to stay null, got %q", state.Token.ValueString())
	}
	if !state.InstallCommand.IsNull() {
		t.Errorf("expected install_command to stay null, got %q", state.InstallCommand.ValueString())
	}
}

func TestMapEnrollmentTokenToState_UsedToken(t *testing.T) {
	lastUsed := "2026-02-01T00:00:00Z"
	state := &enrollmentTokenModel{}

	mapEnrollmentTokenToState(&client.EnrollmentToken{
		ID:                   7,
		Name:                 "legacy fleet",
		Active:               true,
		HostsRegisteredCount: 5,
		LastUsedAt:           &lastUsed,
	}, state)

	if state.HostsRegisteredCount.ValueInt64() != 5 {
		t.Errorf("expected hosts_registered_count 5, got %d", state.HostsRegisteredCount.ValueInt64())
	}
	if state.LastUsedAt.ValueString() != lastUsed {
		t.Errorf("expected last_used_at %q, got %q", lastUsed, state.LastUsedAt.ValueString())
	}
}

// Update is unreachable by construction: `name` is the only attribute a
// practitioner can set and it forces replacement, so Terraform never has an
// in-place change to make. That is what the error in Update asserts, and no
// configuration can reach it to prove it.
//
// So pin the structure instead of the message. The day someone adds a
// configurable attribute without a RequiresReplace modifier, Update becomes
// reachable and starts erroring on a change the practitioner legitimately asked
// for — this fails first, at the point the mistake is made.
func TestEnrollmentTokenSchema_EveryConfigurableAttributeReplaces(t *testing.T) {
	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewEnrollmentTokenResource().Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}

	const replaces = "destroy and recreate"

	for name, attr := range resp.Schema.Attributes {
		if !attr.IsOptional() && !attr.IsRequired() {
			continue // computed-only: not something a configuration can change
		}

		var descriptions []string
		switch a := attr.(type) {
		case schema.StringAttribute:
			for _, m := range a.PlanModifiers {
				descriptions = append(descriptions, m.Description(ctx))
			}
		case schema.Int64Attribute:
			for _, m := range a.PlanModifiers {
				descriptions = append(descriptions, m.Description(ctx))
			}
		case schema.BoolAttribute:
			for _, m := range a.PlanModifiers {
				descriptions = append(descriptions, m.Description(ctx))
			}
		case schema.ListAttribute:
			for _, m := range a.PlanModifiers {
				descriptions = append(descriptions, m.Description(ctx))
			}
		case schema.SetAttribute:
			for _, m := range a.PlanModifiers {
				descriptions = append(descriptions, m.Description(ctx))
			}
		default:
			// Not a silent skip: an unhandled attribute type would pass this test
			// without ever being checked, which is the failure mode the whole test
			// exists to prevent.
			t.Fatalf("attribute %q has type %T, which this test does not inspect — add it to the switch", name, attr)
		}

		found := false
		for _, d := range descriptions {
			if strings.Contains(d, replaces) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("attribute %q is configurable but does not force replacement (plan modifiers: %v).\n"+
				"The API cannot edit an enrollment token, so either give it a RequiresReplace plan modifier "+
				"or teach Update how to apply it.", name, descriptions)
		}
	}
}

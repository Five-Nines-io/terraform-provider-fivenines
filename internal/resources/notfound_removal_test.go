package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A resource deleted outside Terraform must come back as drift, not as an
// error: Read drops it from state so the next plan offers to recreate it,
// instead of failing every apply against an id that is gone.
//
// The six resources below route that decision through client.IsNotFound, which
// replaced twelve open-coded `err.(*client.APIError) && StatusCode == 404`
// checks. Five of them had no read-404 test at all, so the refactor landed
// unguarded — this is that guard. Each case drives the resource's real Read
// against a 404 and asserts BOTH halves: no diagnostic, and state actually
// emptied. Asserting only "no error" would pass with the removal deleted.
func TestRead_RemovesResourceDeletedOutOfBand(t *testing.T) {
	cases := []struct {
		name string
		// schema returns the resource's own schema, and read runs its Read
		// against a client pointed at srv.
		schema func(t *testing.T) rschema.Schema
		// seed writes a prior state carrying at least the id Read needs.
		seed func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State
		read func(c *client.Client) interface {
			Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
		}
	}{
		{
			name:   "instance",
			schema: instanceSchemaForTest,
			seed: func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State {
				m := instanceModel{ID: types.StringValue("host-uuid")}
				if d := s.Set(ctx, &m); d.HasError() {
					t.Fatalf("seeding state: %v", d.Errors())
				}
				return s
			},
			read: func(c *client.Client) interface {
				Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
			} {
				return &instanceResource{client: c}
			},
		},
		{
			name:   "task",
			schema: taskSchemaForTest,
			seed: func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State {
				m := taskModel{ID: types.StringValue("task-uuid")}
				if d := s.Set(ctx, &m); d.HasError() {
					t.Fatalf("seeding state: %v", d.Errors())
				}
				return s
			},
			read: func(c *client.Client) interface {
				Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
			} {
				return &taskResource{client: c}
			},
		},
		{
			name:   "uptime_monitor",
			schema: uptimeMonitorSchema,
			seed: func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State {
				// The collection attributes need typed nulls: a zero-value
				// types.List/Map carries no element type and fails conversion.
				m := uptimeMonitorModel{
					ID:                  types.StringValue("monitor-uuid"),
					ExpectedStatusCodes: types.ListNull(types.Int64Type),
					ProbeRegionIDs:      types.ListNull(types.Int64Type),
					DNSExpectedRecords:  types.ListNull(types.StringType),
					CustomHeaders:       types.MapNull(types.StringType),
				}
				if d := s.Set(ctx, &m); d.HasError() {
					t.Fatalf("seeding state: %v", d.Errors())
				}
				return s
			},
			read: func(c *client.Client) interface {
				Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
			} {
				return &uptimeMonitorResource{client: c}
			},
		},
		{
			name:   "network_device",
			schema: networkDeviceSchema,
			seed: func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State {
				m := networkDeviceModel{ID: types.StringValue("device-uuid")}
				if d := s.Set(ctx, &m); d.HasError() {
					t.Fatalf("seeding state: %v", d.Errors())
				}
				return s
			},
			read: func(c *client.Client) interface {
				Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
			} {
				return &networkDeviceResource{client: c}
			},
		},
		{
			name:   "workflow",
			schema: workflowSchema,
			seed: func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State {
				m := workflowModel{ID: types.Int64Value(11)}
				if d := s.Set(ctx, &m); d.HasError() {
					t.Fatalf("seeding state: %v", d.Errors())
				}
				return s
			},
			read: func(c *client.Client) interface {
				Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
			} {
				return &workflowResource{client: c}
			},
		},
		{
			name:   "status_page",
			schema: statusPageSchema,
			seed: func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State {
				m := statusPageModel{
					ID:       types.Int64Value(4),
					Sections: types.ListNull(types.StringType),
					Items:    types.ListNull(types.ObjectType{AttrTypes: statusPageItemAttrTypes}),
				}
				if d := s.Set(ctx, &m); d.HasError() {
					t.Fatalf("seeding state: %v", d.Errors())
				}
				return s
			},
			read: func(c *client.Client) interface {
				Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
			} {
				return &statusPageResource{client: c}
			},
		},
		{
			// The singleton addresses no id, but the contract is the same: a
			// 404 means the key no longer resolves an organization, and that is
			// drift rather than an error on every subsequent plan.
			name:   "organization",
			schema: organizationSchemaForTest,
			seed: func(t *testing.T, ctx context.Context, s tfsdk.State) tfsdk.State {
				m := organizationModel{ID: types.Int64Value(42)}
				if d := s.Set(ctx, &m); d.HasError() {
					t.Fatalf("seeding state: %v", d.Errors())
				}
				return s
			},
			read: func(c *client.Client) interface {
				Read(context.Context, resource.ReadRequest, *resource.ReadResponse)
			} {
				return &organizationResource{client: c}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The envelope a real 404 carries, request_id included — the client
			// parses it into the APIError that IsNotFound then classifies.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Id", "req-404")
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "Not found", "request_id": "req-404",
				})
			}))
			t.Cleanup(srv.Close)

			ctx := context.Background()
			s := tc.schema(t)
			objType := s.Type().TerraformType(ctx).(tftypes.Object)
			state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
			state = tc.seed(t, ctx, state)

			resp := &resource.ReadResponse{State: state}
			tc.read(client.NewClient(srv.URL, "test-key")).
				Read(ctx, resource.ReadRequest{State: state}, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("a 404 is drift, not an error: %v", resp.Diagnostics.Errors())
			}
			if !resp.State.Raw.IsNull() {
				t.Error("expected the resource to be removed from state after a 404")
			}
		})
	}
}

// The mirror of the case above: a non-404 failure must NOT silently drop the
// resource. A 500 that removed state would delete a live resource from
// Terraform's view and hand the next apply a create for something that exists.
func TestRead_KeepsResourceOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-500")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Internal server error", "request_id": "req-500",
		})
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	s := instanceSchemaForTest(t)
	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
	if d := state.Set(ctx, &instanceModel{ID: types.StringValue("host-uuid")}); d.HasError() {
		t.Fatalf("seeding state: %v", d.Errors())
	}

	resp := &resource.ReadResponse{State: state}
	(&instanceResource{client: client.NewClient(srv.URL, "test-key")}).
		Read(ctx, resource.ReadRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a 500 must surface as an error, not be swallowed as drift")
	}
	if resp.State.Raw.IsNull() {
		t.Error("a 500 must not remove the resource from state")
	}
	// The request id is what a support ticket quotes, and it only reaches the
	// user through the diagnostic's detail.
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "req-500") {
		t.Errorf("expected the request_id in the diagnostic, got %q", detail)
	}
}

func instanceSchemaForTest(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewInstanceResource().(*instanceResource).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func taskSchemaForTest(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewTaskResource().(*taskResource).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// Delete is the other half of the IsNotFound refactor, and mutation testing
// found it unguarded: making the 404 branch unreachable left the whole suite
// green. A resource already deleted out of band must let Terraform finish
// destroying it — surfacing the 404 as an error strands it in state, and the
// only way out is a manual `terraform state rm`.
func TestDelete_ToleratesResourceAlreadyGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-404")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Not found", "request_id": "req-404",
		})
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	s := instanceSchemaForTest(t)
	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
	if d := state.Set(ctx, &instanceModel{ID: types.StringValue("host-uuid")}); d.HasError() {
		t.Fatalf("seeding state: %v", d.Errors())
	}

	resp := &resource.DeleteResponse{State: state}
	(&instanceResource{client: client.NewClient(srv.URL, "test-key")}).
		Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("destroying an instance that is already gone must succeed: %v", resp.Diagnostics.Errors())
	}
}

// The mirror: a real failure on delete must NOT be swallowed, or Terraform
// drops a live resource from state and stops managing something that exists.
func TestDelete_SurfacesRealErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-500")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Internal server error", "request_id": "req-500",
		})
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	s := instanceSchemaForTest(t)
	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
	if d := state.Set(ctx, &instanceModel{ID: types.StringValue("host-uuid")}); d.HasError() {
		t.Fatalf("seeding state: %v", d.Errors())
	}

	resp := &resource.DeleteResponse{State: state}
	(&instanceResource{client: client.NewClient(srv.URL, "test-key")}).
		Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a 500 on delete must surface, not be swallowed as already-deleted")
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "req-500") {
		t.Errorf("expected the request_id in the diagnostic, got %q", detail)
	}
}

func organizationSchemaForTest(t *testing.T) rschema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationResource().(*organizationResource).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

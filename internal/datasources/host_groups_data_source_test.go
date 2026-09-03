package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Must be wired for Configure, otherwise d.client stays nil and Read panics on
// the first apply.
var _ datasource.DataSourceWithConfigure = &hostGroupsDataSource{}

func TestHostGroupsDataSource_MetadataAndConfigure(t *testing.T) {
	resp := &datasource.MetadataResponse{}
	NewHostGroupsDataSource().Metadata(context.Background(),
		datasource.MetadataRequest{ProviderTypeName: "fivenines"}, resp)
	if resp.TypeName != "fivenines_host_groups" {
		t.Errorf("expected type name %q, got %q", "fivenines_host_groups", resp.TypeName)
	}

	for _, tt := range []struct {
		name         string
		providerData interface{}
		wantError    bool
	}{
		// Terraform calls Configure with nil data before the provider is
		// configured; that must be a quiet no-op, not an error.
		{name: "nil provider data", providerData: nil},
		{name: "correct client", providerData: client.NewClient("https://example.com", "key")},
		{name: "wrong type", providerData: "not a client", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := &hostGroupsDataSource{}
			resp := &datasource.ConfigureResponse{}
			d.Configure(context.Background(),
				datasource.ConfigureRequest{ProviderData: tt.providerData}, resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Fatalf("expected error %v, got diagnostics %v", tt.wantError, resp.Diagnostics)
			}
			if !tt.wantError && tt.providerData != nil && d.client == nil {
				t.Error("expected the client to be stored")
			}
		})
	}
}

func TestHostGroupsDataSource_Read(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_groups": []interface{}{
				map[string]interface{}{
					"id": 7, "name": "Production", "position": 1,
					"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
				},
				map[string]interface{}{
					"id": 9, "name": "Production DB", "position": 0,
					"created_at": "2026-01-03T00:00:00Z", "updated_at": "2026-01-04T00:00:00Z",
				},
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &hostGroupsDataSource{client: c}, map[string]tftypes.Value{
		"query":         tftypes.NewValue(tftypes.String, "prod"),
		"updated_since": tftypes.NewValue(tftypes.String, "2026-01-01T00:00:00Z"),
		"order":         tftypes.NewValue(tftypes.String, "name"),
		"direction":     tftypes.NewValue(tftypes.String, "asc"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}

	// Every schema filter has to reach the API, including the query -> q rename.
	for key, want := range map[string]string{
		"q": "prod", "updated_since": "2026-01-01T00:00:00Z",
		"order": "name", "direction": "asc",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q in the request, got %q", key, want, got)
		}
	}

	var out hostGroupsModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.HostGroups) != 2 {
		t.Fatalf("expected 2 host groups, got %d", len(out.HostGroups))
	}
	// Filters are echoed back so the data source round-trips its own config.
	if out.Query.ValueString() != "prod" {
		t.Errorf("expected query to be preserved, got %q", out.Query.ValueString())
	}

	first := out.HostGroups[0]
	if first.ID.ValueInt64() != 7 || first.Name.ValueString() != "Production" {
		t.Errorf("unexpected first group: %+v", first)
	}
	if first.Position.ValueInt64() != 1 {
		t.Errorf("expected position 1, got %d", first.Position.ValueInt64())
	}
	if first.CreatedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("unexpected created_at: %q", first.CreatedAt.ValueString())
	}
	if first.UpdatedAt.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("unexpected updated_at: %q", first.UpdatedAt.ValueString())
	}

	// Position 0 is a real value — the default carried by a group that was never
	// explicitly ordered — not an absence to be nulled out.
	if second := out.HostGroups[1]; second.Position.IsNull() || second.Position.ValueInt64() != 0 {
		t.Errorf("expected position 0 to survive as a value, got %v", second.Position)
	}
}

func TestHostGroupsDataSource_Read_NoFiltersAndNoResults(t *testing.T) {
	var gotQuery url.Values
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_groups": []interface{}{},
			"meta":        map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	state, resp := readDataSource(t, &hostGroupsDataSource{client: c}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	// Null config attributes must not become empty-string filters — the endpoint
	// 400s on parameters it does not accept, and "q=" is not "no q".
	for _, key := range []string{"q", "updated_since", "order", "direction"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted when unset", key)
		}
	}

	var out hostGroupsModel
	if diags := state.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if len(out.HostGroups) != 0 {
		t.Errorf("expected no host groups, got %d", len(out.HostGroups))
	}
	// Zero matches must serialise as [] and not null: length()/for_each/toset
	// over a null list fail, and zero matches is normal for a filtered read.
	var groups types.List
	if diags := state.GetAttribute(context.Background(), path.Root("host_groups"), &groups); diags.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", diags.Errors())
	}
	if groups.IsNull() {
		t.Error("expected an empty list, got null")
	}
}

func TestHostGroupsDataSource_Read_Error(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "boom"})
	})

	_, resp := readDataSource(t, &hostGroupsDataSource{client: c}, nil)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic for a 500")
	}
}

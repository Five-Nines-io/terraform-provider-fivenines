package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

// The two catalogue data sources exist so a configuration can name a template
// slug or a node type without hard-coding one. Neither endpoint is covered by a
// published spec, so the `json` attribute carries the raw object: whatever the
// API returns stays reachable through jsondecode() even when a field this
// provider models turns out to be named something else.

// readCatalogue drives a no-argument catalogue data source and decodes the state
// it produced.
func readCatalogue(t *testing.T, d datasource.DataSource, target interface{}) {
	t.Helper()
	state, resp := readDataSource(t, d, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if diags := state.Get(context.Background(), target); diags.HasError() {
		t.Fatalf("state get: %v", diags)
	}
}

func TestWorkflowTemplatesDataSource_Read(t *testing.T) {
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"templates": []map[string]interface{}{
				{
					"slug": "disk-pressure", "name": "Disk Pressure",
					"description": "Alert on low disk", "category": "instances",
					"trigger_type": "metric_threshold",
					"unmodelled":   "kept for jsondecode",
				},
			},
		})
	})

	d := &workflowTemplatesDataSource{client: c}
	var state workflowTemplatesModel
	readCatalogue(t, d, &state)

	if gotPath != "/api/v1/workflows/templates" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if len(state.Templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(state.Templates))
	}
	got := state.Templates[0]
	if got.Slug.ValueString() != "disk-pressure" {
		t.Errorf("expected the slug, got %q", got.Slug.ValueString())
	}
	if got.Category.ValueString() != "instances" {
		t.Errorf("expected the category, got %q", got.Category.ValueString())
	}
	// The escape hatch: a field the provider does not model must still be
	// reachable, because these field names are not confirmed against a spec.
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(got.JSON.ValueString()), &raw); err != nil {
		t.Fatalf("json attribute must be decodable: %v", err)
	}
	if raw["unmodelled"] != "kept for jsondecode" {
		t.Errorf("expected unmodelled fields to survive in json, got %v", raw)
	}
}

func TestWorkflowNodeTypesDataSource_Read(t *testing.T) {
	var gotPath string
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_types": []map[string]interface{}{
				{
					"type": "metric_threshold", "name": "Metric Threshold",
					"category": "trigger", "description": "Fires on a metric",
					"config_schema": map[string]interface{}{"metric": "string"},
				},
			},
		})
	})

	d := &workflowNodeTypesDataSource{client: c}
	var state workflowNodeTypesModel
	readCatalogue(t, d, &state)

	if gotPath != "/api/v1/node_types" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if len(state.NodeTypes) != 1 {
		t.Fatalf("expected 1 node type, got %d", len(state.NodeTypes))
	}
	got := state.NodeTypes[0]
	if got.Type.ValueString() != "metric_threshold" {
		t.Errorf("expected the type, got %q", got.Type.ValueString())
	}
	// The node's configuration schema is the reason plan-time graph validation
	// is possible at all, and it only reaches a configuration through json.
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(got.JSON.ValueString()), &raw); err != nil {
		t.Fatalf("json attribute must be decodable: %v", err)
	}
	if raw["config_schema"] == nil {
		t.Errorf("expected the config schema to survive in json, got %v", raw)
	}
}

// An empty catalogue is a legal answer and must not read as an error.
func TestWorkflowTemplatesDataSource_EmptyCatalogue(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"templates": []interface{}{}})
	})

	var state workflowTemplatesModel
	readCatalogue(t, &workflowTemplatesDataSource{client: c}, &state)

	if len(state.Templates) != 0 {
		t.Errorf("expected no templates, got %d", len(state.Templates))
	}
}

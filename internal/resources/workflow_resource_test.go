package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// execution_graph_json has to stay a semantic JSON type. As a plain string it
// diffs on whitespace and key ordering, which cuts a new workflow version every
// time the server hands back a re-serialised graph.
func TestWorkflowSchema_ExecutionGraphIsSemanticJSON(t *testing.T) {
	var resp resource.SchemaResponse
	NewWorkflowResource().(*workflowResource).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}

	attribute, ok := resp.Schema.Attributes["execution_graph_json"]
	if !ok {
		t.Fatal("execution_graph_json is missing from the schema")
	}
	if _, ok := attribute.GetType().(jsontypes.NormalizedType); !ok {
		t.Errorf("expected jsontypes.NormalizedType, got %T", attribute.GetType())
	}
}

func TestExecutionGraph_SemanticEquality(t *testing.T) {
	compact := jsontypes.NewNormalizedValue(`{"nodes":[{"id":"a"}],"edges":[]}`)

	tests := []struct {
		name  string
		other string
		want  bool
	}{
		{"reformatted", "{\n  \"nodes\": [{ \"id\": \"a\" }],\n  \"edges\": []\n}", true},
		{"reordered keys", `{"edges":[],"nodes":[{"id":"a"}]}`, true},
		{"different graph", `{"nodes":[{"id":"b"}],"edges":[]}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equal, diags := compact.StringSemanticEquals(context.Background(), jsontypes.NewNormalizedValue(tt.other))
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if equal != tt.want {
				t.Errorf("semantic equality = %v, want %v", equal, tt.want)
			}
		})
	}
}

// The republish gate is the reason execution_graph_json is a semantic JSON type:
// a reformatted graph must not cut a new published version.
func TestShouldPublishGraph(t *testing.T) {
	compact := jsontypes.NewNormalizedValue(`{"nodes":[{"id":"a"}],"edges":[]}`)

	tests := []struct {
		name    string
		planned jsontypes.Normalized
		stored  jsontypes.Normalized
		want    bool
	}{
		{
			name:    "no graph in the config publishes nothing",
			planned: jsontypes.NewNormalizedNull(),
			stored:  compact,
		},
		{
			name:    "an unknown graph waits for apply",
			planned: jsontypes.NewNormalizedUnknown(),
			stored:  compact,
		},
		{
			name:    "a graph that was never stored always publishes",
			planned: compact,
			stored:  jsontypes.NewNormalizedNull(),
			want:    true,
		},
		{
			name:    "reformatting publishes nothing",
			planned: jsontypes.NewNormalizedValue("{\n  \"edges\": [],\n  \"nodes\": [{ \"id\": \"a\" }]\n}"),
			stored:  compact,
		},
		{
			name:    "a real change publishes",
			planned: jsontypes.NewNormalizedValue(`{"nodes":[{"id":"b"}],"edges":[]}`),
			stored:  compact,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := shouldPublishGraph(context.Background(), tt.planned, tt.stored)
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got != tt.want {
				t.Errorf("shouldPublishGraph = %v, want %v", got, tt.want)
			}
		})
	}
}

// Invalid JSON never reaches the API: the attribute type rejects it at plan
// time, and publishGraph refuses it again before the version call.
func TestPublishGraph_RejectsInvalidJSON(t *testing.T) {
	r := &workflowResource{}
	err := r.publishGraph(context.Background(), 1, "{not json")
	if err == nil {
		t.Fatal("expected invalid JSON to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid execution_graph_json") {
		t.Errorf("expected the error to name the attribute, got: %v", err)
	}
}

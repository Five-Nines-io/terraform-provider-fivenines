package resources

import (
	"context"
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

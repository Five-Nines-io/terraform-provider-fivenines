package datasources

import (
	"context"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func ptr[T any](v T) *T { return &v }

func vulnerabilitiesState(t *testing.T) tfsdk.State {
	t.Helper()
	resp := &datasource.SchemaResponse{}
	(&vulnerabilitiesDataSource{}).Schema(context.Background(), datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema errors: %v", resp.Diagnostics)
	}
	return tfsdk.State{Schema: resp.Schema}
}

func emptyVulnerabilitiesModel() vulnerabilitiesModel {
	return vulnerabilitiesModel{Severity: types.ListNull(types.StringType)}
}

// THE LOAD-BEARING BEHAVIOUR of this data source: the API refuses to answer for
// a never-scanned subject, and that refusal has to reach a practitioner as a
// NULL list. `length(null)` fails the plan, which on a security gate is the
// correct direction; an empty list would have passed it.
func TestVulnerabilities_NilFindingsRenderAsNull(t *testing.T) {
	ctx := context.Background()
	state := vulnerabilitiesState(t)

	model := emptyVulnerabilitiesModel()
	model.Vulnerabilities = nil
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("setting state: %v", diags)
	}

	var findings types.List
	if diags := state.GetAttribute(ctx, path.Root("vulnerabilities"), &findings); diags.HasError() {
		t.Fatalf("reading attribute: %v", diags)
	}
	if !findings.IsNull() {
		t.Error("expected a null list for a subject that was never scanned")
	}
}

// The other half of the same contract: a subject that WAS scanned and has
// nothing wrong is a real all-clear, and must read as an empty list.
func TestVulnerabilities_EmptyFindingsRenderAsEmptyList(t *testing.T) {
	ctx := context.Background()
	state := vulnerabilitiesState(t)

	model := emptyVulnerabilitiesModel()
	model.Vulnerabilities = []vulnerabilityModel{}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("setting state: %v", diags)
	}

	var findings types.List
	if diags := state.GetAttribute(ctx, path.Root("vulnerabilities"), &findings); diags.HasError() {
		t.Fatalf("reading attribute: %v", diags)
	}
	if findings.IsNull() {
		t.Fatal("expected an empty list, not null, for a scanned subject with no findings")
	}
	if len(findings.Elements()) != 0 {
		t.Errorf("expected 0 elements, got %d", len(findings.Elements()))
	}
}

// A full round trip through the schema, which is what proves the model structs
// and the schema agree - including the docker_image block the two data sources
// share.
func TestVulnerabilities_StateRoundTrip(t *testing.T) {
	ctx := context.Background()
	state := vulnerabilitiesState(t)

	model := emptyVulnerabilitiesModel()
	model.DockerImageID = types.StringValue("image-uuid")
	model.Vulnerabilities = []vulnerabilityModel{mapVulnerability(client.Vulnerability{
		ID:            1,
		DockerImageID: ptr("image-uuid"),
		PackageName:   "openssl",
		Severity:      "Critical",
		CVSSScore:     ptr(9.8),
		CVEIDs:        []string{"CVE-2024-2511"},
	})}
	image := mapDockerImage(client.DockerImage{ID: "image-uuid", State: "scanned"})
	model.DockerImage = &image
	model.Scan = &vulnerabilityScan{}

	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("setting state: %v", diags)
	}

	var severity types.String
	if diags := state.GetAttribute(ctx, path.Root("vulnerabilities").AtListIndex(0).AtName("severity"), &severity); diags.HasError() {
		t.Fatalf("reading attribute: %v", diags)
	}
	if severity.ValueString() != "Critical" {
		t.Errorf("expected Critical, got %q", severity.ValueString())
	}
}

func TestDockerImages_StateRoundTrip(t *testing.T) {
	ctx := context.Background()
	resp := &datasource.SchemaResponse{}
	(&dockerImagesDataSource{}).Schema(ctx, datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema errors: %v", resp.Diagnostics)
	}
	state := tfsdk.State{Schema: resp.Schema}

	model := dockerImagesModel{
		Images:  []dockerImageModel{mapDockerImage(client.DockerImage{ID: "image-uuid", State: "pending"})},
		Posture: &imagePostureModel{Pending: types.Int64Value(1)},
	}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("setting state: %v", diags)
	}

	var count types.Int64
	if diags := state.GetAttribute(ctx, path.Root("images").AtListIndex(0).AtName("vulnerability_count"), &count); diags.HasError() {
		t.Fatalf("reading attribute: %v", diags)
	}
	if !count.IsNull() {
		t.Errorf("expected a null count on a pending image, got %d", count.ValueInt64())
	}
}

// --- mapDockerImage ---

// The honesty contract: a non-scanned image must never be presented as
// "0 vulnerabilities". The API nulls the counts; the mapping keeps them null.
func TestMapDockerImage_NotScannedHasNoCounts(t *testing.T) {
	image := mapDockerImage(client.DockerImage{
		ID:             "image-uuid",
		State:          "unscannable",
		StateReason:    ptr("extraction failed"),
		StateErrorType: ptr("api_error"),
		Countable:      false,
	})

	if !image.VulnerabilityCount.IsNull() {
		t.Errorf("expected a null vulnerability_count, got %d", image.VulnerabilityCount.ValueInt64())
	}
	if !image.CriticalVulnerabilityCount.IsNull() {
		t.Errorf("expected a null critical_vulnerability_count, got %d", image.CriticalVulnerabilityCount.ValueInt64())
	}
	if image.Countable.ValueBool() {
		t.Error("expected countable false")
	}
	if image.StateErrorType.ValueString() != "api_error" {
		t.Errorf("expected state_error_type api_error, got %q", image.StateErrorType.ValueString())
	}
}

// A capped package list keeps the image `scanned` and makes its counts a floor.
func TestMapDockerImage_TruncatedCountIsAFloor(t *testing.T) {
	image := mapDockerImage(client.DockerImage{
		State:                      "scanned",
		Countable:                  true,
		VulnerabilityCount:         ptr(int64(12)),
		CriticalVulnerabilityCount: ptr(int64(3)),
		PackagesTruncated:          true,
		FindingCountIsFloor:        true,
		RunningHostCount:           ptr(int64(4)),
	})

	if image.VulnerabilityCount.ValueInt64() != 12 {
		t.Errorf("expected 12, got %d", image.VulnerabilityCount.ValueInt64())
	}
	if !image.PackagesTruncated.ValueBool() || !image.FindingCountIsFloor.ValueBool() {
		t.Error("expected the count to be marked a floor")
	}
	if image.RunningHostCount.ValueInt64() != 4 {
		t.Errorf("expected running_host_count 4, got %d", image.RunningHostCount.ValueInt64())
	}
	// tags/repo_digests are absent from the fixture, and an absent array is an
	// empty list rather than a null one.
	if image.Tags == nil || len(image.Tags) != 0 {
		t.Errorf("expected an empty tags list, got %v", image.Tags)
	}
}

// --- mapVulnerability ---

func TestMapVulnerability_MissingScoreIsNullNotZero(t *testing.T) {
	v := mapVulnerability(client.Vulnerability{
		ID:          7,
		PackageName: "zlib1g",
		Severity:    "Unknown",
		CVSSScore:   nil,
		FixVersion:  nil,
	})

	if !v.CVSSScore.IsNull() {
		t.Errorf("expected a null cvss_score, got %v", v.CVSSScore.ValueFloat64())
	}
	if v.Severity.ValueString() != "Unknown" {
		t.Errorf("expected Unknown, got %q", v.Severity.ValueString())
	}
	if !v.FixVersion.IsNull() {
		t.Error("expected a null fix_version when no fix is known")
	}
	// An advisory with no CVE alias is an empty list, never null.
	if v.CVEIDs == nil || len(v.CVEIDs) != 0 {
		t.Errorf("expected an empty cve_ids list, got %v", v.CVEIDs)
	}
}

func TestMapVulnerability_HostSubject(t *testing.T) {
	v := mapVulnerability(client.Vulnerability{
		HostID:    ptr("host-uuid"),
		HostName:  ptr("web-01"),
		Ecosystem: ptr("Ubuntu:22.04"),
		Severity:  "Critical",
		CVSSScore: ptr(9.8),
		Patchable: true,
	})

	if v.HostName.ValueString() != "web-01" {
		t.Errorf("expected web-01, got %q", v.HostName.ValueString())
	}
	if !v.DockerImageID.IsNull() {
		t.Error("expected the image subject to be null on a host finding")
	}
	if v.CVSSScore.ValueFloat64() != 9.8 {
		t.Errorf("expected 9.8, got %v", v.CVSSScore.ValueFloat64())
	}
	if !v.Patchable.ValueBool() {
		t.Error("expected patchable true")
	}
}

// --- addSecurityError ---

func TestAddSecurityError_ForbiddenNamesThePlanGate(t *testing.T) {
	var diags diag.Diagnostics
	addSecurityError(&diags, "Error listing vulnerabilities",
		&client.APIError{StatusCode: 403, Message: "require the Pro plan or above"})

	if !diags.HasError() {
		t.Fatal("expected an error diagnostic")
	}
	summary := diags.Errors()[0].Summary()
	if summary != "Security data requires a plan with security_details" {
		t.Errorf("expected the plan-gate summary, got %q", summary)
	}
}

func TestAddSecurityError_OtherErrorsPassThrough(t *testing.T) {
	var diags diag.Diagnostics
	addSecurityError(&diags, "Error listing vulnerabilities",
		&client.APIError{StatusCode: 404, Message: "not found"})

	if summary := diags.Errors()[0].Summary(); summary != "Error listing vulnerabilities" {
		t.Errorf("expected the generic summary, got %q", summary)
	}
}

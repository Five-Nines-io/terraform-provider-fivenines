package provider_test

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

// planTest wires REAL Terraform to a fake API, so a plan test needs no
// organisation and no key — unlike the TF_ACC-gated suites, these run wherever
// the terraform binary is (CI has it, because the docs check shells out to
// tfplugindocs).
//
// They exist because the unit tests cannot see plan validation at all. Driving
// Create and Update directly skips the step where Terraform compares the plan to
// the configuration, and a provider can be structurally unable to produce a
// valid plan while that entire suite stays green.
func planTest(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform CLI not on PATH — skipping plan-validation test")
	}
	srv := httptest.NewServer(http.HandlerFunc(respond))
	t.Cleanup(srv.Close)
	t.Setenv("FIVENINES_BASE_URL", srv.URL)
	t.Setenv("FIVENINES_API_KEY", "fn_test")
	t.Setenv("TF_ACC", "1") // hermetic: the fake server above is the whole API
	return srv
}

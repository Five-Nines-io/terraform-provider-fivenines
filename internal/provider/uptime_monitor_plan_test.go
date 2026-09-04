package provider_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The protocol cross-field rules live in ValidateConfig, which the unit tests
// call directly. Only a real plan proves Terraform actually invokes it and
// surfaces the message before anything reaches the API — the whole reason the
// rules exist rather than letting the server answer 422 mid-apply.
func TestUptimeMonitorPlan_DNSRequiresHostname(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_uptime_monitor" "dns" {
  name            = "apex record"
  protocol        = "dns"
  dns_record_type = "A"
}`,
			ExpectError: regexp.MustCompile(`(?s)"dns" monitors require "hostname"`),
		}},
	})
}

// The inverse rule: an attribute the protocol does not use is cleared by Update,
// so leaving it in HCL has to fail at plan time rather than as "Provider
// produced inconsistent result after apply".
func TestUptimeMonitorPlan_DNSRejectsKeyword(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_uptime_monitor" "dns" {
  name            = "apex record"
  protocol        = "dns"
  hostname        = "example.com"
  dns_record_type = "A"
  keyword         = "leftover from the https config"
}`,
			ExpectError: regexp.MustCompile(`(?s)"dns" monitors do not use "keyword"`),
		}},
	})
}

// custom_body and content_type only survive a POST — the API nils them for any
// other method. http_method defaults to GET, so the config below looks complete
// and would apply into a monitor with no body at all.
func TestUptimeMonitorPlan_BodyRequiresPost(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_uptime_monitor" "probe" {
  name         = "graphql probe"
  protocol     = "https"
  url          = "https://api.example.com/graphql"
  content_type = "application/json"
  custom_body  = jsonencode({ query = "{ __typename }" })
}`,
			ExpectError: regexp.MustCompile(`(?s)stores "custom_body" only for POST requests`),
		}},
	})
}

// order is enumerated server-side (created_at, updated_at, name), so a typo has
// to fail at plan time with the accepted values rather than reaching the API and
// coming back as an opaque 400 mid-refresh.
func TestUptimeMonitorsDataSourcePlan_RejectsInvalidOrderAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_uptime_monitors" "test" {
  order = "last_check_at"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute order value must be one of.*"created_at"`),
		}},
	})
}

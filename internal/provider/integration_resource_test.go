package provider_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCheckIntegrationDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_integration", func(ctx context.Context, id string) error {
		numericID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return err
		}
		_, err = testAccClient().GetIntegration(ctx, numericID)
		return err
	})
}

func testAccIntegrationWebhookConfig(name, url string) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_integration" "test" {
  type = "webhook"
  name = %[1]q
  url  = %[2]q
}
`, name, url)
}

// Webhook is the only creatable type that needs no third-party credential, so
// it is the only one whose full lifecycle an unattended run can drive: pagerduty
// validates its routing key by firing a live alert into someone's on-call
// rotation, and pushover needs a real application token.
//
// There is deliberately no ImportState step. Every argument that identifies a
// channel is write-only, so an imported resource plans an immediate
// destroy-and-recreate; the resource does not implement ImportState at all.
func TestAccIntegration_webhookLifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-hook")
	url := fmt.Sprintf("https://example.com/hooks/%s", name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckIntegrationDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccIntegrationWebhookConfig(name, url),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_integration.test", "type", "webhook"),
					resource.TestCheckResourceAttr("fivenines_integration.test", "name", name),
					resource.TestCheckResourceAttr("fivenines_integration.test", "url", url),
					resource.TestCheckResourceAttr("fivenines_integration.test", "verify_webhook", "false"),
					// Unverified until the endpoint echoes the token back, and a
					// workflow notification node refuses to deliver until then.
					resource.TestCheckResourceAttr("fivenines_integration.test", "verified", "false"),
					// Returned once, by this create, and never readable again —
					// if these are empty the secret is gone for good.
					resource.TestCheckResourceAttrSet("fivenines_integration.test", "webhook_signing_secret"),
					resource.TestCheckResourceAttrSet("fivenines_integration.test", "webhook_verification_token"),
					resource.TestCheckResourceAttrSet("fivenines_integration.test", "webhook_verification_header"),
					resource.TestCheckResourceAttrSet("fivenines_integration.test", "id"),
				),
			},
			{
				// No PATCH exists, so every argument is RequiresReplace. This
				// proves the plan replaces rather than trying an in-place update
				// that would land in the unreachable Update method.
				Config: testAccIntegrationWebhookConfig(name, url+"-changed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_integration.test", "url", url+"-changed"),
				),
			},
		},
	})
}

// The name the server falls back to when the configuration omits one is the
// channel's own identifier — the URL. That only works because `name` is
// Optional+Computed; as Optional-only the apply would fail with an
// inconsistent-result error the moment the server filled it in.
func TestAccIntegration_webhookNameDefaultsToURL(t *testing.T) {
	url := fmt.Sprintf("https://example.com/hooks/%s", acctest.RandomWithPrefix("tf-acc-unnamed"))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckIntegrationDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(providerConfig+`
resource "fivenines_integration" "test" {
  type = "webhook"
  url  = %[1]q
}
`, url),
				Check: resource.TestCheckResourceAttr("fivenines_integration.test", "name", url),
			},
		},
	})
}

// These never reach the API: ValidateConfig rejects them at plan time. That is
// the point — for pagerduty an apply that got as far as the API would have sent
// a live test alert before failing.
func TestAccIntegration_rejectsUncreatableTypes(t *testing.T) {
	for _, tc := range []struct {
		integrationType string
		wantErr         *regexp.Regexp
	}{
		{"slack", regexp.MustCompile(`Settings > Integrations`)},
		{"email", regexp.MustCompile(`6-digit code`)},
		{"carrier_pigeon", regexp.MustCompile(`not a known integration type`)},
	} {
		t.Run(tc.integrationType, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(providerConfig+`
resource "fivenines_integration" "test" {
  type = %[1]q
}
`, tc.integrationType),
					ExpectError: tc.wantErr,
				}},
			})
		})
	}
}

func TestAccIntegration_rejectsMissingAndForeignAttributes(t *testing.T) {
	for name, tc := range map[string]struct {
		config  string
		wantErr *regexp.Regexp
	}{
		"webhook without url": {
			config:  `type = "webhook"`,
			wantErr: regexpMissingRequired,
		},
		"pagerduty without name": {
			config:  `type = "pagerduty"` + "\n  routing_key = \"k\"",
			wantErr: regexpMissingRequired,
		},
		"routing_key on a webhook": {
			config:  `type = "webhook"` + "\n  url = \"https://example.com/h\"\n  routing_key = \"k\"",
			wantErr: regexp.MustCompile(`do not use "routing_key"`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(providerConfig+`
resource "fivenines_integration" "test" {
  %s
}
`, tc.config),
					ExpectError: tc.wantErr,
				}},
			})
		})
	}
}

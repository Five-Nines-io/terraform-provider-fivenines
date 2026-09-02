package provider_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Acceptance tests drive the provider through Terraform against a real
// FiveNines organisation. Unit tests pin the shapes the provider *believes*
// the API has; only these catch the API changing underneath it.
//
//	TF_ACC=1 FIVENINES_API_KEY=fn_... go test ./internal/provider/ -v -timeout 120m
//
// Without TF_ACC every test below skips. Point them at a dedicated staging
// organisation: each one creates and destroys real monitoring resources.

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"fivenines": providerserver.NewProtocol6WithError(provider.New()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("FIVENINES_API_KEY") == "" {
		t.Fatal("FIVENINES_API_KEY must be set to run acceptance tests")
	}
}

// testAccClient reaches the same API as the provider, for the destroy checks.
func testAccClient() *client.Client {
	baseURL := os.Getenv("FIVENINES_BASE_URL")
	if baseURL == "" {
		baseURL = "https://fivenines.io"
	}
	return client.NewClient(baseURL, os.Getenv("FIVENINES_API_KEY"))
}

func isNotFound(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// checkDestroyed asserts every resource of the given type is really gone from
// the API. For the resources whose DELETE is asynchronous this is what proves
// the provider waited for the teardown instead of dropping state early.
func checkDestroyed(resourceType string, get func(ctx context.Context, id string) error) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		for name, rs := range state.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			err := get(context.Background(), rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("%s (%s) still exists after destroy", name, rs.Primary.ID)
			}
			if !isNotFound(err) {
				return fmt.Errorf("checking %s (%s) was destroyed: %w", name, rs.Primary.ID, err)
			}
		}
		return nil
	}
}

// regexpMissingRequired matches the plan-time diagnostic both ValidateConfig
// implementations raise — tasks (#8) and uptime monitors (#9) — so the tests
// assert the failure lands before any apply touches the API.
var regexpMissingRequired = regexp.MustCompile(`Missing required attribute`)

// providerConfig prefixes every test configuration. The API key comes from the
// environment so it never reaches a test fixture.
const providerConfig = `
provider "fivenines" {}
`

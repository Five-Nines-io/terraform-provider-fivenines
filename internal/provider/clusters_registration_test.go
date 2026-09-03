package provider_test

import (
	"context"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

// A constructor that exists but is never appended to DataSources is a data source
// nobody can use, and nothing else in the suite would notice: the schema-validity
// sweep only inspects what IS registered, and the datasources package tests the
// table rather than the provider. #25 adds ten data sources through three
// separate append calls, so the wiring is worth naming.
func TestProvider_RegistersClusterAndStatusDataSources(t *testing.T) {
	ctx := context.Background()

	registered := map[string]bool{}
	for _, newDataSource := range provider.New().DataSources(ctx) {
		var nameResp datasource.MetadataResponse
		newDataSource().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "fivenines"}, &nameResp)
		registered[nameResp.TypeName] = true
	}

	for _, want := range []string{
		"fivenines_ceph_clusters",
		"fivenines_ceph_cluster",
		"fivenines_proxmox_clusters",
		"fivenines_proxmox_cluster",
		"fivenines_instance_capability_status",
		"fivenines_status_page_subscribers",
		// The four Proxmox child inventories come from their own table, which
		// is appended after the twenty per-instance collectors.
		"fivenines_proxmox_cluster_nodes",
		"fivenines_proxmox_cluster_guests",
		"fivenines_proxmox_cluster_storages",
		"fivenines_organization_proxmox_guests",
	} {
		if !registered[want] {
			t.Errorf("data source %s is not registered", want)
		}
	}

	// The per-instance collector inventories must survive the second append --
	// reassigning `ds` rather than appending is how a table gets dropped.
	for _, want := range []string{"fivenines_proxmox_guests", "fivenines_systemd_units"} {
		if !registered[want] {
			t.Errorf("the per-instance inventory %s was lost from the registration list", want)
		}
	}
}

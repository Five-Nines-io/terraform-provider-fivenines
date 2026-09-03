package datasources

import "fmt"

// The cluster-scoped and organization-wide Proxmox inventories serve the SAME
// rows as the per-instance collectors -- one cluster has one set of nodes,
// guests and storages however many hosts report it, because only the cluster's
// authoritative reporter writes them. So the field tables are read off
// inventoryCollectors rather than restated, and only the handful of
// descriptions whose wording is instance-scoped are overridden.
//
// Keeping one copy is the point: a field the server adds is declared once, and
// the four routes cannot drift into describing the same column differently.

// proxmoxRowFields returns a collector's field table with per-field description
// overrides applied. It panics on an unknown collector or an override naming a
// field that is not there, because both are typos in a package-level table and a
// silent miss would ship a data source documenting a column with the wrong
// scope.
func proxmoxRowFields(collector string, overrides map[string]string) []inventoryField {
	var source []inventoryField
	for _, c := range inventoryCollectors {
		if c.name == collector {
			source = c.fields
			break
		}
	}
	if source == nil {
		panic(fmt.Sprintf("proxmoxRowFields: no collector named %q", collector))
	}

	seen := make(map[string]bool, len(overrides))
	out := make([]inventoryField, len(source))
	for i, f := range source {
		if desc, ok := overrides[f.name]; ok {
			f.desc = desc
			seen[f.name] = true
		}
		out[i] = f
	}
	for name := range overrides {
		if !seen[name] {
			panic(fmt.Sprintf("proxmoxRowFields: override for unknown field %q on %q", name, collector))
		}
	}
	return out
}

// On a cluster-scoped route the cluster is the thing you asked for, so the
// per-instance table's "NOT the instance, deduplicate on id" warning is not just
// unnecessary but wrong -- there is nothing to deduplicate.
const clusterScopedClusterID = "The cluster these rows belong to. On this data source it is always the " +
	"`cluster_id` you asked for; it is published so a row stays legible after rows from several clusters " +
	"are merged."

// The organization-wide guest list DOES span clusters, so the field is the key
// you group by.
const orgScopedClusterID = "The cluster this guest belongs to -- the uuid `fivenines_proxmox_clusters` " +
	"publishes as `id`, and the key to group this fleet-wide list by. NOT the `cluster_key` that " +
	"`fivenines_metric_query` takes."

// proxmoxRowFilters returns a collector's filter table, with the same
// panic-on-typo contract proxmoxRowFields has. The filter sets are read off the
// collector table for the reason the fields are: the cluster-scoped routes
// accept the same query vocabulary as the per-instance ones, down to the two
// different staleness windows, and a hand-copied second set is how the same
// column starts being described two ways.
func proxmoxRowFilters(collector string) []inventoryFilter {
	for _, c := range inventoryCollectors {
		if c.name == collector {
			// Deep on `oneOf`: a plain copy would share the backing array with
			// the package-level collector table, so anything that later sorted
			// or appended to a returned filter's vocabulary would corrupt it
			// for all twenty per-instance data sources. The function advertises
			// isolation; it has to actually provide it.
			out := make([]inventoryFilter, len(c.filters))
			for i, f := range c.filters {
				if f.oneOf != nil {
					f.oneOf = append([]string(nil), f.oneOf...)
				}
				out[i] = f
			}
			return out
		}
	}
	panic(fmt.Sprintf("proxmoxRowFilters: no collector named %q", collector))
}

var (
	proxmoxGuestFilters   = proxmoxRowFilters("proxmox_guests")
	proxmoxNodeFilters    = proxmoxRowFilters("proxmox_nodes")
	proxmoxStorageFilters = proxmoxRowFilters("proxmox_storages")
)

// The shared preamble for the three cluster-scoped data sources: what makes them
// different from the per-instance ones with the same rows.
const clusterInventoryPreamble = "ONE ROW PER ENTITY however many hosts report the cluster, because only the " +
	"cluster's authoritative reporter writes them. These are the same rows the per-instance data source " +
	"of the same name serves -- that one is how you reach a cluster from a host you already know, this " +
	"one is how you enumerate a cluster you found through `fivenines_proxmox_clusters`. Reading it here " +
	"needs no deduplication, and it does not go empty when one host's reporter ages out.\n\n"

var proxmoxInventories = []proxmoxInventory{
	{
		name:    "proxmox_cluster_nodes",
		key:     "proxmox_nodes",
		segment: "nodes",
		desc: "The nodes in one Proxmox cluster, and which of them the cluster can no longer see.\n\n" +
			clusterInventoryPreamble +
			"`status = \"offline\"` IS THE CLUSTER'S VIEW of a node, not ours: the surviving nodes cannot " +
			"see it. That is the signal worth alerting on, and it is distinct from both \"the agent " +
			"stopped reporting\" (`stale`) and \"we cannot reach the Proxmox API at all\" (the cluster's " +
			"`unreachable_reporter_count`).\n\n" +
			"`stale` here is a FIXED 10-minute window -- not the organization threshold the guests data " +
			"source uses.",
		fields:  proxmoxRowFields("proxmox_nodes", map[string]string{"proxmox_cluster_id": clusterScopedClusterID}),
		filters: proxmoxNodeFilters,
	},
	{
		name:    "proxmox_cluster_guests",
		key:     "proxmox_guests",
		segment: "guests",
		desc: "The VMs and containers on one Proxmox cluster -- which guest is stopped and since when, " +
			"and which node currently holds it.\n\n" +
			clusterInventoryPreamble +
			"A GUEST MIGRATES: `vmid` is unique cluster-wide and the row is keyed on cluster + vmid, so " +
			"`node_name` is where the guest lives right now, not a stable attribute of it. " +
			"`state_changed_at` is what turns \"it is stopped\" into \"it has been stopped since 04:10\".\n\n" +
			"`stale` here is the ORGANIZATION's offline threshold, matching what every guest trigger, the " +
			"fleet grid and the workflow engine read -- NOT the fixed ten minutes the nodes and storages " +
			"data sources use.\n\n" +
			"Use `fivenines_organization_proxmox_guests` for the fleet-wide list across every cluster.",
		fields:  proxmoxRowFields("proxmox_guests", map[string]string{"proxmox_cluster_id": clusterScopedClusterID}),
		filters: proxmoxGuestFilters,
	},
	{
		name:    "proxmox_cluster_storages",
		key:     "proxmox_storages",
		segment: "storages",
		desc: "The storages on one Proxmox cluster.\n\n" +
			clusterInventoryPreamble +
			"ONE LOGICAL POOL APPEARS ONCE PER NODE, and that is the useful shape rather than " +
			"duplication: a datacenter-wide storage is listed by every node, and `active` is per node -- a " +
			"shared NFS mount really can be up on one node and down on another. Grouping by `name` alone " +
			"collapses exactly the failure worth seeing.\n\n" +
			"`pool` IS NOT `name`. `name` is the PVE storage id an operator types; `pool` is the backing " +
			"dataset (`rpool/data`), reported by agent >= 1.11.7 and null on older ones. `zpool_root` is " +
			"its first segment -- the actual zpool -- and it is what correlates this row to a ZFS pool on " +
			"the node's own host.\n\n" +
			"`stale` here is a FIXED 10-minute window, not the organization's offline threshold the " +
			"guests data source uses.",
		fields:  proxmoxRowFields("proxmox_storages", map[string]string{"proxmox_cluster_id": clusterScopedClusterID}),
		filters: proxmoxStorageFilters,
	},
	{
		name: "organization_proxmox_guests",
		key:  "proxmox_guests",
		// No segment: this is the unscoped fleet-wide route.
		desc: "Every Proxmox guest in the organization, across every cluster -- the fleet-wide " +
			"counterpart of `fivenines_proxmox_cluster_guests`.\n\n" +
			"Group by `proxmox_cluster_id` to split the list back into clusters, or narrow to one with " +
			"the filter of the same name. No deduplication is needed: guests are cluster-scoped and only " +
			"the authoritative reporter writes them, so each appears exactly once however many hosts " +
			"report its cluster.\n\n" +
			"A GUEST MIGRATES: `vmid` is unique cluster-wide and the row is keyed on cluster + vmid, so " +
			"`node_name` is where the guest lives right now, not a stable attribute of it.\n\n" +
			"`stale` here is the ORGANIZATION's offline threshold, matching what every guest trigger " +
			"reads.",
		fields: proxmoxRowFields("proxmox_guests", map[string]string{"proxmox_cluster_id": orgScopedClusterID}),
		filters: append([]inventoryFilter{{
			name: "proxmox_cluster_id",
			kind: fieldString,
			desc: "Narrow to one cluster. The uuid the guest rows publish as `proxmox_cluster_id` and " +
				"`fivenines_proxmox_clusters` publishes as `id` -- NOT the `cluster_key` that " +
				"`fivenines_metric_query` takes.\n\n" +
				"AN UNKNOWN CLUSTER ID IS AN ERROR, NEVER AN EMPTY LIST. The organization's clusters are " +
				"a closed vocabulary, so a uuid belonging to another organization -- or to a cluster that " +
				"no longer exists -- is rejected with a 400 naming it as unknown, and a malformed value " +
				"is a 400 asking for a UUID. That is deliberate: an empty list would be an all-clear for " +
				"a cluster nobody can see, and would disagree with `fivenines_proxmox_cluster_guests`, " +
				"which 404s for the same id.",
		}}, proxmoxGuestFilters...),
	},
}

package resources

import (
	"context"
	"regexp"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &instanceResource{}
	_ resource.ResourceWithImportState = &instanceResource{}
)

type instanceResource struct {
	client *client.Client
}

type instanceModel struct {
	ID              types.String `tfsdk:"id"`
	DisplayName     types.String `tfsdk:"display_name"`
	Description     types.String `tfsdk:"description"`
	HostGroupID     types.Int64  `tfsdk:"host_group_id"`
	ClusterName     types.String `tfsdk:"cluster_name"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	MaintenanceMode types.Bool   `tfsdk:"maintenance_mode"`

	// Agent-reported, read-only.
	Hostname            types.String `tfsdk:"hostname"`
	OperatingSystemName types.String `tfsdk:"operating_system_name"`
	KernelVersion       types.String `tfsdk:"kernel_version"`
	CPUArchitecture     types.String `tfsdk:"cpu_architecture"`
	CPUModel            types.String `tfsdk:"cpu_model"`
	CPUCount            types.Int64  `tfsdk:"cpu_count"`
	MemorySize          types.Int64  `tfsdk:"memory_size"`
	IPv4                types.String `tfsdk:"ipv4"`
	IPv6                types.String `tfsdk:"ipv6"`
	Source              types.String `tfsdk:"source"`
	ClientVersion       types.String `tfsdk:"client_version"`
	Status              types.String `tfsdk:"status"`
	FirstSyncAt         types.String `tfsdk:"first_sync_at"`
	LastSyncAt          types.String `tfsdk:"last_sync_at"`
	LastRequestAt       types.String `tfsdk:"last_request_at"`
	CreatedAt           types.String `tfsdk:"created_at"`
	UpdatedAt           types.String `tfsdk:"updated_at"`

	// Storage / virtualization
	SmartStorageHealthEnabled types.Bool   `tfsdk:"smart_storage_health_enabled"`
	RaidStorageHealthEnabled  types.Bool   `tfsdk:"raid_storage_health_enabled"`
	ZFSEnabled                types.Bool   `tfsdk:"zfs_enabled"`
	CephEnabled               types.Bool   `tfsdk:"ceph_enabled"`
	QemuEnabled               types.Bool   `tfsdk:"qemu_enabled"`
	QemuURI                   types.String `tfsdk:"qemu_uri"`
	ProxmoxEnabled            types.Bool   `tfsdk:"proxmox_enabled"`
	ProxmoxHost               types.String `tfsdk:"proxmox_host"`
	ProxmoxPort               types.Int64  `tfsdk:"proxmox_port"`
	ProxmoxTokenID            types.String `tfsdk:"proxmox_token_id"`
	ProxmoxTokenSecret        types.String `tfsdk:"proxmox_token_secret"`
	ProxmoxVerifySSL          types.Bool   `tfsdk:"proxmox_verify_ssl"`

	// Containers
	DockerEnabled   types.Bool   `tfsdk:"docker_enabled"`
	DockerSocketURL types.String `tfsdk:"docker_socket_url"`

	// Caches
	RedisEnabled     types.Bool   `tfsdk:"redis_enabled"`
	RedisPort        types.Int64  `tfsdk:"redis_port"`
	RedisPassword    types.String `tfsdk:"redis_password"`
	MemcachedEnabled types.Bool   `tfsdk:"memcached_enabled"`
	MemcachedHost    types.String `tfsdk:"memcached_host"`
	MemcachedPort    types.Int64  `tfsdk:"memcached_port"`

	// Databases
	PostgreSQLEnabled  types.Bool   `tfsdk:"postgresql_enabled"`
	PostgreSQLHost     types.String `tfsdk:"postgresql_host"`
	PostgreSQLPort     types.Int64  `tfsdk:"postgresql_port"`
	PostgreSQLUser     types.String `tfsdk:"postgresql_user"`
	PostgreSQLPassword types.String `tfsdk:"postgresql_password"`
	PostgreSQLDatabase types.String `tfsdk:"postgresql_database"`
	MySQLEnabled       types.Bool   `tfsdk:"mysql_enabled"`
	MySQLHost          types.String `tfsdk:"mysql_host"`
	MySQLPort          types.Int64  `tfsdk:"mysql_port"`
	MySQLUser          types.String `tfsdk:"mysql_user"`
	MySQLPassword      types.String `tfsdk:"mysql_password"`
	MySQLDatabase      types.String `tfsdk:"mysql_database"`
	MySQLSocket        types.String `tfsdk:"mysql_socket"`

	// Web / application servers
	NginxEnabled        types.Bool   `tfsdk:"nginx_enabled"`
	NginxStatusPageURL  types.String `tfsdk:"nginx_status_page_url"`
	ApacheEnabled       types.Bool   `tfsdk:"apache_enabled"`
	ApacheStatusPageURL types.String `tfsdk:"apache_status_page_url"`
	CaddyEnabled        types.Bool   `tfsdk:"caddy_enabled"`
	CaddyAdminAPIURL    types.String `tfsdk:"caddy_admin_api_url"`
	PHPFPMEnabled       types.Bool   `tfsdk:"php_fpm_enabled"`
	PHPFPMStatusPageURL types.String `tfsdk:"php_fpm_status_page_url"`
	HAProxyEnabled      types.Bool   `tfsdk:"haproxy_enabled"`
	HAProxyStatsURL     types.String `tfsdk:"haproxy_stats_url"`
	HAProxyStatsSocket  types.String `tfsdk:"haproxy_stats_socket"`
	HAProxyUsername     types.String `tfsdk:"haproxy_username"`
	HAProxyPassword     types.String `tfsdk:"haproxy_password"`

	// Messaging
	RabbitMQEnabled       types.Bool   `tfsdk:"rabbitmq_enabled"`
	RabbitMQManagementURL types.String `tfsdk:"rabbitmq_management_url"`
	RabbitMQUsername      types.String `tfsdk:"rabbitmq_username"`
	RabbitMQPassword      types.String `tfsdk:"rabbitmq_password"`
	RabbitMQVhostFilter   types.String `tfsdk:"rabbitmq_vhost_filter"`

	// Observability meta-monitoring + AI inference serving
	TSDBEnabled           types.Bool   `tfsdk:"tsdb_enabled"`
	TSDBURL               types.String `tfsdk:"tsdb_url"`
	TSDBAuthHeaderName    types.String `tfsdk:"tsdb_auth_header_name"`
	TSDBAuthHeaderValue   types.String `tfsdk:"tsdb_auth_header_value"`
	TSDBBasicAuthUsername types.String `tfsdk:"tsdb_basic_auth_username"`
	TSDBBasicAuthPassword types.String `tfsdk:"tsdb_basic_auth_password"`
	TSDBVerifySSL         types.Bool   `tfsdk:"tsdb_verify_ssl"`
	VLLMEnabled           types.Bool   `tfsdk:"vllm_enabled"`
	VLLMMetricsURL        types.String `tfsdk:"vllm_metrics_url"`
	VLLMAuthHeaderName    types.String `tfsdk:"vllm_auth_header_name"`
	VLLMAuthHeaderValue   types.String `tfsdk:"vllm_auth_header_value"`
	VLLMVerifySSL         types.Bool   `tfsdk:"vllm_verify_ssl"`
	SGLangEnabled         types.Bool   `tfsdk:"sglang_enabled"`
	SGLangMetricsURL      types.String `tfsdk:"sglang_metrics_url"`
	SGLangAuthHeaderName  types.String `tfsdk:"sglang_auth_header_name"`
	SGLangAuthHeaderValue types.String `tfsdk:"sglang_auth_header_value"`
	SGLangVerifySSL       types.Bool   `tfsdk:"sglang_verify_ssl"`

	// VPN / host-level collectors
	WireguardEnabled types.Bool   `tfsdk:"wireguard_enabled"`
	TailscaleEnabled types.Bool   `tfsdk:"tailscale_enabled"`
	SystemdEnabled   types.Bool   `tfsdk:"systemd_enabled"`
	Fail2banEnabled  types.Bool   `tfsdk:"fail2ban_enabled"`
	NvidiaGPUEnabled types.Bool   `tfsdk:"nvidia_gpu_enabled"`
	IPv6Enabled      types.Bool   `tfsdk:"ipv6_enabled"`
	LogsEnabled      types.Bool   `tfsdk:"logs_enabled"`
	LogsUnitsCSV     types.String `tfsdk:"logs_units_csv"`

	// Secret presence
	RedisPasswordSet         types.Bool `tfsdk:"redis_password_set"`
	PostgreSQLPasswordSet    types.Bool `tfsdk:"postgresql_password_set"`
	MySQLPasswordSet         types.Bool `tfsdk:"mysql_password_set"`
	RabbitMQPasswordSet      types.Bool `tfsdk:"rabbitmq_password_set"`
	HAProxyPasswordSet       types.Bool `tfsdk:"haproxy_password_set"`
	ProxmoxTokenSecretSet    types.Bool `tfsdk:"proxmox_token_secret_set"`
	TSDBAuthHeaderValueSet   types.Bool `tfsdk:"tsdb_auth_header_value_set"`
	TSDBBasicAuthPasswordSet types.Bool `tfsdk:"tsdb_basic_auth_password_set"`
	VLLMAuthHeaderValueSet   types.Bool `tfsdk:"vllm_auth_header_value_set"`
	SGLangAuthHeaderValueSet types.Bool `tfsdk:"sglang_auth_header_value_set"`
}

func NewInstanceResource() resource.Resource {
	return &instanceResource{}
}

func (r *instanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

// The host settings are Optional+Computed with NO provider-side default,
// because the server's defaults are not uniform (the collector toggles start
// false but tsdb/vllm/sglang verify SSL by default, and several URLs carry
// server-side values). An attribute left out of the configuration therefore
// keeps whatever the host already has — including choices made in the
// dashboard — and the applied value is read back into state.

func settingBool(description string) schema.BoolAttribute {
	return schema.BoolAttribute{Description: description, Optional: true, Computed: true}
}

func settingString(description string) schema.StringAttribute {
	return schema.StringAttribute{Description: description, Optional: true, Computed: true}
}

// settingStringTrimmed is settingString for the fields the server normalizes
// with strip on assignment. A padded value would be accepted, stored
// stripped, and read back different from what the plan promised — which
// Terraform rejects as an inconsistent result after apply — so the padding
// is refused up front instead. The edge classes mirror Ruby String#strip
// exactly: ASCII whitespace plus vertical tab and NUL (Go's \s misses the
// last two); internal whitespace is the server's business, not ours.
func settingStringTrimmed(description string) schema.StringAttribute {
	attribute := settingString(description)
	attribute.Validators = []validator.String{
		stringvalidator.RegexMatches(
			regexp.MustCompile(`(?s)^$|^[^\s\v\x{0}](.*[^\s\v\x{0}])?$`),
			"must not start or end with whitespace",
		),
	}
	return attribute
}

func settingPort(description string) schema.Int64Attribute {
	return schema.Int64Attribute{
		Description: description,
		Optional:    true,
		Computed:    true,
		Validators: []validator.Int64{
			int64validator.Between(1, 65535),
		},
	}
}

// instanceSecret is one of the ten write-only credentials. The API never
// returns the value — the paired `<name>_set` attribute reports whether one is
// stored — so the configured value stays in state as written.
func instanceSecret(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Description: description + " Write-only — the API never returns it; the paired `_set` attribute " +
			"reports whether one is stored. Setting it rotates the stored value; dropping it from the " +
			"configuration leaves that value alone rather than wiping a credential Terraform cannot " +
			"read back. Clear one from the dashboard.",
		Optional:  true,
		Sensitive: true,
		Validators: []validator.String{
			// A blank means "keep the stored value" to the API — Rails
			// `blank?`, which counts Unicode spaces (NBSP, vertical tab, NEL)
			// as blank where Go's ASCII-only \s does not — and state would
			// claim a credential the server silently discarded. The same
			// reason the MQTT broker credentials reject an empty string.
			stringvalidator.RegexMatches(
				regexp.MustCompile(`[^\s\v\p{Z}\x{85}]`),
				"must contain a non-whitespace character",
			),
		},
	}
}

func secretPresence(description string) schema.BoolAttribute {
	return schema.BoolAttribute{Description: description, Computed: true}
}

func (r *instanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines instance (monitored server).\n\n" +
			"Destroying an instance waits for the deletion to finish. The API answers 202 and tears " +
			"the host down asynchronously, so the provider polls until it is gone (up to five minutes) " +
			"before releasing state, which is what makes replacing a host in a single apply safe.\n\n" +
			"The collector settings (`*_enabled` toggles and their endpoints) have server-owned " +
			"defaults: an attribute left out of the configuration keeps whatever the host already " +
			"has, including choices made in the dashboard — set it explicitly to manage it. " +
			"Credentials (`*_password`, `proxmox_token_secret`, `*_auth_header_value`) are " +
			"write-only: the API reports only a computed `<name>_set` boolean, never the value. " +
			"Like every Terraform sensitive value they are still stored in state in cleartext — " +
			"write-only describes the API, so protect the state file as you would the credentials. " +
			"Linux-only collectors (ZFS, Proxmox, systemd, HAProxy, …) are dropped by the API on a " +
			"confirmed Windows host, so setting one there fails the apply — with one undetectable " +
			"corner: rotating a Linux-only credential the host already stores is refused while the " +
			"old value stays, and the presence-only `_set` boolean cannot tell the two apart.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier (UUID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"display_name": schema.StringAttribute{
				Description: "Display name of the instance.",
				Required:    true,
			},
			"description": settingString("Free-form description of the host."),
			"host_group_id": schema.Int64Attribute{
				Description: "ID of the host group this instance belongs to (see `fivenines_host_group`). " +
					"Must be a group in your organization. Removing it from the configuration removes " +
					"the instance from its group — and the API deletes a group its last host leaves, " +
					"so ungrouping the final member also destroys the group itself.",
				Optional: true,
			},
			"cluster_name": settingString("Cluster this host reports under (e.g. `eu-west-1`)."),
			"enabled": schema.BoolAttribute{
				Description: "Whether the instance is enabled for monitoring.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"maintenance_mode": schema.BoolAttribute{
				Description: "Whether the instance is in maintenance mode.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},

			// Agent-reported, read-only.
			"hostname": schema.StringAttribute{
				Description: "Hostname reported by the agent.",
				Computed:    true,
			},
			"operating_system_name": schema.StringAttribute{
				Description: "Operating system name.",
				Computed:    true,
			},
			"kernel_version": schema.StringAttribute{
				Description: "Kernel version.",
				Computed:    true,
			},
			"cpu_architecture": schema.StringAttribute{
				Description: "CPU architecture.",
				Computed:    true,
			},
			"cpu_model": schema.StringAttribute{
				Description: "CPU model.",
				Computed:    true,
			},
			"cpu_count": schema.Int64Attribute{
				Description: "Number of CPUs.",
				Computed:    true,
			},
			"memory_size": schema.Int64Attribute{
				Description: "Memory size in bytes.",
				Computed:    true,
			},
			"ipv4": schema.StringAttribute{
				Description: "IPv4 address.",
				Computed:    true,
			},
			"ipv6": schema.StringAttribute{
				Description: "IPv6 address.",
				Computed:    true,
			},
			"source": schema.StringAttribute{
				Description: "How the instance reports: `agent` (the FiveNines agent) or `prometheus_pull`.",
				Computed:    true,
			},
			"client_version": schema.StringAttribute{
				Description: "Agent client version.",
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Current status: `waiting`, `online`, `offline`, `warning`, `maintenance` " +
					"or `disabled`. `waiting` means the agent has never checked in.",
				Computed: true,
			},
			"first_sync_at": schema.StringAttribute{
				Description: "First agent sync time.",
				Computed:    true,
			},
			"last_sync_at": schema.StringAttribute{
				Description: "Last time the agent synced.",
				Computed:    true,
			},
			"last_request_at": schema.StringAttribute{
				Description: "Last API request time from the agent.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},

			// Storage / virtualization
			"smart_storage_health_enabled": settingBool("Whether S.M.A.R.T. disk health monitoring is enabled."),
			"raid_storage_health_enabled":  settingBool("Whether RAID array monitoring is enabled."),
			"zfs_enabled":                  settingBool("Whether ZFS pool monitoring is enabled."),
			"ceph_enabled":                 settingBool("Whether Ceph monitoring is enabled."),
			"qemu_enabled":                 settingBool("Whether QEMU/libvirt VM monitoring is enabled."),
			"qemu_uri":                     settingString("Libvirt connection URI (e.g. `qemu:///system`)."),
			"proxmox_enabled":              settingBool("Whether Proxmox VE monitoring is enabled."),
			"proxmox_host":                 settingString("Proxmox API host."),
			"proxmox_port":                 settingPort("Proxmox API port (usually 8006)."),
			"proxmox_token_id":             settingString("Proxmox API token ID (e.g. `monitoring@pve!fivenines`)."),
			"proxmox_token_secret":         instanceSecret("Proxmox API token secret."),
			"proxmox_verify_ssl":           settingBool("Whether to verify the Proxmox API TLS certificate."),

			// Containers
			"docker_enabled":    settingBool("Whether Docker container monitoring is enabled."),
			"docker_socket_url": settingString("Docker socket URL (e.g. `unix://var/run/docker.sock`)."),

			// Caches
			"redis_enabled":     settingBool("Whether Redis monitoring is enabled."),
			"redis_port":        settingPort("Redis port (usually 6379)."),
			"redis_password":    instanceSecret("Redis password."),
			"memcached_enabled": settingBool("Whether Memcached monitoring is enabled."),
			"memcached_host":    settingString("Memcached host."),
			"memcached_port":    settingPort("Memcached port (usually 11211)."),

			// Databases
			"postgresql_enabled":  settingBool("Whether PostgreSQL monitoring is enabled."),
			"postgresql_host":     settingString("PostgreSQL host."),
			"postgresql_port":     settingPort("PostgreSQL port (usually 5432)."),
			"postgresql_user":     settingString("PostgreSQL user."),
			"postgresql_password": instanceSecret("PostgreSQL password."),
			"postgresql_database": settingString("PostgreSQL database to connect to."),
			"mysql_enabled":       settingBool("Whether MySQL/MariaDB monitoring is enabled."),
			"mysql_host":          settingString("MySQL host."),
			"mysql_port":          settingPort("MySQL port (usually 3306)."),
			"mysql_user":          settingString("MySQL user."),
			"mysql_password":      instanceSecret("MySQL password."),
			"mysql_database":      settingString("MySQL database to connect to."),
			"mysql_socket":        settingString("MySQL Unix socket path (e.g. `/var/run/mysqld/mysqld.sock`)."),

			// Web / application servers
			"nginx_enabled":           settingBool("Whether nginx monitoring is enabled."),
			"nginx_status_page_url":   settingString("URL of the nginx stub_status page."),
			"apache_enabled":          settingBool("Whether Apache monitoring is enabled."),
			"apache_status_page_url":  settingString("URL of the Apache mod_status page (the `?auto` form)."),
			"caddy_enabled":           settingBool("Whether Caddy monitoring is enabled."),
			"caddy_admin_api_url":     settingString("URL of the Caddy admin API."),
			"php_fpm_enabled":         settingBool("Whether PHP-FPM monitoring is enabled."),
			"php_fpm_status_page_url": settingStringTrimmed("URL of the PHP-FPM status page."),
			"haproxy_enabled":         settingBool("Whether HAProxy monitoring is enabled."),
			"haproxy_stats_url":       settingStringTrimmed("URL of the HAProxy stats endpoint (the `;csv` form)."),
			"haproxy_stats_socket":    settingStringTrimmed("Path of the HAProxy admin socket."),
			"haproxy_username":        settingString("Username for the HAProxy stats endpoint."),
			"haproxy_password":        instanceSecret("Password for the HAProxy stats endpoint."),

			// Messaging
			"rabbitmq_enabled":        settingBool("Whether RabbitMQ monitoring is enabled."),
			"rabbitmq_management_url": settingStringTrimmed("URL of the RabbitMQ management API."),
			"rabbitmq_username":       settingString("RabbitMQ management username."),
			"rabbitmq_password":       instanceSecret("RabbitMQ management password."),
			"rabbitmq_vhost_filter":   settingString("Only monitor vhosts matching this filter."),

			// Observability meta-monitoring + AI inference serving
			"tsdb_enabled":             settingBool("Whether Prometheus/VictoriaMetrics (TSDB) meta-monitoring is enabled."),
			"tsdb_url":                 settingStringTrimmed("Base URL of the Prometheus / VictoriaMetrics server — the agent appends `/metrics`."),
			"tsdb_auth_header_name":    settingString("Name of the auth header sent to the TSDB."),
			"tsdb_auth_header_value":   instanceSecret("Value of the auth header sent to the TSDB."),
			"tsdb_basic_auth_username": settingString("Basic auth username for the TSDB."),
			"tsdb_basic_auth_password": instanceSecret("Basic auth password for the TSDB."),
			"tsdb_verify_ssl":          settingBool("Whether to verify the TSDB's TLS certificate."),
			"vllm_enabled":             settingBool("Whether vLLM monitoring is enabled."),
			"vllm_metrics_url":         settingStringTrimmed("Full metrics URL, not a base — vLLM moves the path with `--root-path`."),
			"vllm_auth_header_name":    settingStringTrimmed("Name of the auth header sent to vLLM."),
			"vllm_auth_header_value":   instanceSecret("Value of the auth header sent to vLLM."),
			"vllm_verify_ssl":          settingBool("Whether to verify vLLM's TLS certificate."),
			"sglang_enabled":           settingBool("Whether SGLang monitoring is enabled."),
			"sglang_metrics_url":       settingStringTrimmed("Full metrics URL of the SGLang server."),
			"sglang_auth_header_name":  settingStringTrimmed("Name of the auth header sent to SGLang."),
			"sglang_auth_header_value": instanceSecret("Value of the auth header sent to SGLang."),
			"sglang_verify_ssl":        settingBool("Whether to verify SGLang's TLS certificate."),

			// VPN / host-level collectors
			"wireguard_enabled":  settingBool("Whether WireGuard peer monitoring is enabled."),
			"tailscale_enabled":  settingBool("Whether Tailscale monitoring is enabled."),
			"systemd_enabled":    settingBool("Whether systemd unit monitoring is enabled."),
			"fail2ban_enabled":   settingBool("Whether Fail2ban jail monitoring is enabled."),
			"nvidia_gpu_enabled": settingBool("Whether NVIDIA GPU monitoring is enabled."),
			"ipv6_enabled":       settingBool("Whether IPv6 monitoring is enabled."),
			"logs_enabled":       settingBool("Whether log collection is enabled."),
			"logs_units_csv":     settingString("Comma-separated systemd units to collect logs from (e.g. `nginx.service, ssh.service`)."),

			// Secret presence
			"redis_password_set":           secretPresence("Whether a Redis password is stored. The value itself is never returned."),
			"postgresql_password_set":      secretPresence("Whether a PostgreSQL password is stored. The value itself is never returned."),
			"mysql_password_set":           secretPresence("Whether a MySQL password is stored. The value itself is never returned."),
			"rabbitmq_password_set":        secretPresence("Whether a RabbitMQ password is stored. The value itself is never returned."),
			"haproxy_password_set":         secretPresence("Whether an HAProxy password is stored. The value itself is never returned."),
			"proxmox_token_secret_set":     secretPresence("Whether a Proxmox token secret is stored. The value itself is never returned."),
			"tsdb_auth_header_value_set":   secretPresence("Whether a TSDB auth header value is stored. The value itself is never returned."),
			"tsdb_basic_auth_password_set": secretPresence("Whether a TSDB basic auth password is stored. The value itself is never returned."),
			"vllm_auth_header_value_set":   secretPresence("Whether a vLLM auth header value is stored. The value itself is never returned."),
			"sglang_auth_header_value_set": secretPresence("Whether an SGLang auth header value is stored. The value itself is never returned."),
		},
	}
}

func (r *instanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *client.Client, got unexpected type.")
		return
	}
	r.client = c
}

// instanceSettingsFromPlan builds the shared settings half of the create and
// update inputs. Attributes the plan holds no value for — null because they
// are not configured, or unknown because a fresh plan has not resolved a
// Computed value yet — become nil pointers, which the input structs omit, so
// the server keeps what it stores. host_group_id is the exception: nil
// marshals as an explicit null and removes the host from its group.
func instanceSettingsFromPlan(plan *instanceModel) client.InstanceSettingsInput {
	return client.InstanceSettingsInput{
		Description: stringPtr(plan.Description),
		HostGroupID: int64Ptr(plan.HostGroupID),
		ClusterName: stringPtr(plan.ClusterName),

		SmartStorageHealthEnabled: boolPtr(plan.SmartStorageHealthEnabled),
		RaidStorageHealthEnabled:  boolPtr(plan.RaidStorageHealthEnabled),
		ZFSEnabled:                boolPtr(plan.ZFSEnabled),
		CephEnabled:               boolPtr(plan.CephEnabled),
		QemuEnabled:               boolPtr(plan.QemuEnabled),
		QemuURI:                   stringPtr(plan.QemuURI),
		ProxmoxEnabled:            boolPtr(plan.ProxmoxEnabled),
		ProxmoxHost:               stringPtr(plan.ProxmoxHost),
		ProxmoxPort:               int64Ptr(plan.ProxmoxPort),
		ProxmoxTokenID:            stringPtr(plan.ProxmoxTokenID),
		ProxmoxTokenSecret:        stringPtr(plan.ProxmoxTokenSecret),
		ProxmoxVerifySSL:          boolPtr(plan.ProxmoxVerifySSL),

		DockerEnabled:   boolPtr(plan.DockerEnabled),
		DockerSocketURL: stringPtr(plan.DockerSocketURL),

		RedisEnabled:     boolPtr(plan.RedisEnabled),
		RedisPort:        int64Ptr(plan.RedisPort),
		RedisPassword:    stringPtr(plan.RedisPassword),
		MemcachedEnabled: boolPtr(plan.MemcachedEnabled),
		MemcachedHost:    stringPtr(plan.MemcachedHost),
		MemcachedPort:    int64Ptr(plan.MemcachedPort),

		PostgreSQLEnabled:  boolPtr(plan.PostgreSQLEnabled),
		PostgreSQLHost:     stringPtr(plan.PostgreSQLHost),
		PostgreSQLPort:     int64Ptr(plan.PostgreSQLPort),
		PostgreSQLUser:     stringPtr(plan.PostgreSQLUser),
		PostgreSQLPassword: stringPtr(plan.PostgreSQLPassword),
		PostgreSQLDatabase: stringPtr(plan.PostgreSQLDatabase),
		MySQLEnabled:       boolPtr(plan.MySQLEnabled),
		MySQLHost:          stringPtr(plan.MySQLHost),
		MySQLPort:          int64Ptr(plan.MySQLPort),
		MySQLUser:          stringPtr(plan.MySQLUser),
		MySQLPassword:      stringPtr(plan.MySQLPassword),
		MySQLDatabase:      stringPtr(plan.MySQLDatabase),
		MySQLSocket:        stringPtr(plan.MySQLSocket),

		NginxEnabled:        boolPtr(plan.NginxEnabled),
		NginxStatusPageURL:  stringPtr(plan.NginxStatusPageURL),
		ApacheEnabled:       boolPtr(plan.ApacheEnabled),
		ApacheStatusPageURL: stringPtr(plan.ApacheStatusPageURL),
		CaddyEnabled:        boolPtr(plan.CaddyEnabled),
		CaddyAdminAPIURL:    stringPtr(plan.CaddyAdminAPIURL),
		PHPFPMEnabled:       boolPtr(plan.PHPFPMEnabled),
		PHPFPMStatusPageURL: stringPtr(plan.PHPFPMStatusPageURL),
		HAProxyEnabled:      boolPtr(plan.HAProxyEnabled),
		HAProxyStatsURL:     stringPtr(plan.HAProxyStatsURL),
		HAProxyStatsSocket:  stringPtr(plan.HAProxyStatsSocket),
		HAProxyUsername:     stringPtr(plan.HAProxyUsername),
		HAProxyPassword:     stringPtr(plan.HAProxyPassword),

		RabbitMQEnabled:       boolPtr(plan.RabbitMQEnabled),
		RabbitMQManagementURL: stringPtr(plan.RabbitMQManagementURL),
		RabbitMQUsername:      stringPtr(plan.RabbitMQUsername),
		RabbitMQPassword:      stringPtr(plan.RabbitMQPassword),
		RabbitMQVhostFilter:   stringPtr(plan.RabbitMQVhostFilter),

		TSDBEnabled:           boolPtr(plan.TSDBEnabled),
		TSDBURL:               stringPtr(plan.TSDBURL),
		TSDBAuthHeaderName:    stringPtr(plan.TSDBAuthHeaderName),
		TSDBAuthHeaderValue:   stringPtr(plan.TSDBAuthHeaderValue),
		TSDBBasicAuthUsername: stringPtr(plan.TSDBBasicAuthUsername),
		TSDBBasicAuthPassword: stringPtr(plan.TSDBBasicAuthPassword),
		TSDBVerifySSL:         boolPtr(plan.TSDBVerifySSL),
		VLLMEnabled:           boolPtr(plan.VLLMEnabled),
		VLLMMetricsURL:        stringPtr(plan.VLLMMetricsURL),
		VLLMAuthHeaderName:    stringPtr(plan.VLLMAuthHeaderName),
		VLLMAuthHeaderValue:   stringPtr(plan.VLLMAuthHeaderValue),
		VLLMVerifySSL:         boolPtr(plan.VLLMVerifySSL),
		SGLangEnabled:         boolPtr(plan.SGLangEnabled),
		SGLangMetricsURL:      stringPtr(plan.SGLangMetricsURL),
		SGLangAuthHeaderName:  stringPtr(plan.SGLangAuthHeaderName),
		SGLangAuthHeaderValue: stringPtr(plan.SGLangAuthHeaderValue),
		SGLangVerifySSL:       boolPtr(plan.SGLangVerifySSL),

		WireguardEnabled: boolPtr(plan.WireguardEnabled),
		TailscaleEnabled: boolPtr(plan.TailscaleEnabled),
		SystemdEnabled:   boolPtr(plan.SystemdEnabled),
		Fail2banEnabled:  boolPtr(plan.Fail2banEnabled),
		NvidiaGPUEnabled: boolPtr(plan.NvidiaGPUEnabled),
		IPv6Enabled:      boolPtr(plan.IPv6Enabled),
		LogsEnabled:      boolPtr(plan.LogsEnabled),
		LogsUnitsCSV:     stringPtr(plan.LogsUnitsCSV),
	}
}

func (r *instanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	enabled := plan.Enabled.ValueBool()
	maintenance := plan.MaintenanceMode.ValueBool()
	input := client.CreateInstanceInput{
		DisplayName:           plan.DisplayName.ValueString(),
		Enabled:               &enabled,
		MaintenanceMode:       &maintenance,
		InstanceSettingsInput: instanceSettingsFromPlan(&plan),
	}

	tflog.Debug(ctx, "Creating instance", map[string]interface{}{"display_name": input.DisplayName})

	instance, err := r.client.CreateInstance(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating instance", err.Error())
		return
	}

	mapInstanceToState(instance, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *instanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	instance, _, err := r.client.GetInstance(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading instance", err.Error())
		return
	}

	mapInstanceToState(instance, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *instanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.UpdateInstanceInput{
		DisplayName:           stringPtr(plan.DisplayName),
		Enabled:               boolPtr(plan.Enabled),
		MaintenanceMode:       boolPtr(plan.MaintenanceMode),
		InstanceSettingsInput: instanceSettingsFromPlan(&plan),
	}

	var instance *client.Instance
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetInstance(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error reading instance for update", err.Error())
			return
		}
		instance, err = r.client.UpdateInstance(ctx, state.ID.ValueString(), etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on instance update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating instance", err.Error())
			return
		}
		break
	}

	mapInstanceToState(instance, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *instanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "Deleting instance", map[string]interface{}{"id": id})

	accepted, err := r.client.DeleteInstance(ctx, id)
	if err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting instance", err.Error())
		return
	}

	// A 202 means the host is only queued for deletion. Returning here would
	// drop it from state while it still exists, which breaks replacing a host
	// in a single apply.
	if accepted {
		tflog.Debug(ctx, "Waiting for asynchronous instance deletion", map[string]interface{}{"id": id})
		if err := r.client.WaitForInstanceDeletion(ctx, id, client.AsyncDeletionTimeout); err != nil {
			resp.Diagnostics.AddError("Error waiting for instance deletion", err.Error())
		}
	}
}

func (r *instanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapInstanceToState(i *client.Instance, state *instanceModel) {
	state.ID = types.StringValue(i.ID)
	state.DisplayName = types.StringValue(i.DisplayName)
	state.Description = optionalString(i.Description)
	state.HostGroupID = optionalInt64(i.HostGroupID)
	state.ClusterName = optionalString(i.ClusterName)
	state.Enabled = types.BoolValue(i.Enabled)
	state.MaintenanceMode = types.BoolValue(i.MaintenanceMode)
	// Everything the agent reports is null until it first syncs.
	state.Hostname = optionalString(i.Hostname)
	state.OperatingSystemName = optionalString(i.OperatingSystemName)
	state.KernelVersion = optionalString(i.KernelVersion)
	state.CPUArchitecture = optionalString(i.CPUArchitecture)
	state.CPUModel = optionalString(i.CPUModel)
	state.CPUCount = optionalInt64(i.CPUCount)
	state.MemorySize = optionalInt64(i.MemorySize)
	state.IPv4 = optionalString(i.IPv4)
	state.IPv6 = optionalString(i.IPv6)
	state.Source = optionalString(i.Source)
	state.ClientVersion = optionalString(i.ClientVersion)
	state.Status = types.StringValue(i.Status)
	state.FirstSyncAt = optionalString(i.FirstSyncAt)
	state.LastSyncAt = optionalString(i.LastSyncAt)
	state.LastRequestAt = optionalString(i.LastRequestAt)
	state.CreatedAt = types.StringValue(i.CreatedAt)
	state.UpdatedAt = types.StringValue(i.UpdatedAt)

	// Collector settings, exactly as the server stores them. The write-only
	// credentials (proxmox_token_secret, *_password, *_auth_header_value) are
	// never echoed by the API: while its `<name>_set` boolean reports one
	// stored, the configured value stays as it is; when it reports none, the
	// attribute goes null so the gap is visible (see storedSecret).
	state.SmartStorageHealthEnabled = optionalBool(i.SmartStorageHealthEnabled)
	state.RaidStorageHealthEnabled = optionalBool(i.RaidStorageHealthEnabled)
	state.ZFSEnabled = optionalBool(i.ZFSEnabled)
	state.CephEnabled = optionalBool(i.CephEnabled)
	state.QemuEnabled = optionalBool(i.QemuEnabled)
	state.QemuURI = optionalString(i.QemuURI)
	state.ProxmoxEnabled = optionalBool(i.ProxmoxEnabled)
	state.ProxmoxHost = optionalString(i.ProxmoxHost)
	state.ProxmoxPort = optionalInt64(i.ProxmoxPort)
	state.ProxmoxTokenID = optionalString(i.ProxmoxTokenID)
	state.ProxmoxVerifySSL = optionalBool(i.ProxmoxVerifySSL)

	state.DockerEnabled = optionalBool(i.DockerEnabled)
	state.DockerSocketURL = optionalString(i.DockerSocketURL)

	state.RedisEnabled = optionalBool(i.RedisEnabled)
	state.RedisPort = optionalInt64(i.RedisPort)
	state.MemcachedEnabled = optionalBool(i.MemcachedEnabled)
	state.MemcachedHost = optionalString(i.MemcachedHost)
	state.MemcachedPort = optionalInt64(i.MemcachedPort)

	state.PostgreSQLEnabled = optionalBool(i.PostgreSQLEnabled)
	state.PostgreSQLHost = optionalString(i.PostgreSQLHost)
	state.PostgreSQLPort = optionalInt64(i.PostgreSQLPort)
	state.PostgreSQLUser = optionalString(i.PostgreSQLUser)
	state.PostgreSQLDatabase = optionalString(i.PostgreSQLDatabase)
	state.MySQLEnabled = optionalBool(i.MySQLEnabled)
	state.MySQLHost = optionalString(i.MySQLHost)
	state.MySQLPort = optionalInt64(i.MySQLPort)
	state.MySQLUser = optionalString(i.MySQLUser)
	state.MySQLDatabase = optionalString(i.MySQLDatabase)
	state.MySQLSocket = optionalString(i.MySQLSocket)

	state.NginxEnabled = optionalBool(i.NginxEnabled)
	state.NginxStatusPageURL = optionalString(i.NginxStatusPageURL)
	state.ApacheEnabled = optionalBool(i.ApacheEnabled)
	state.ApacheStatusPageURL = optionalString(i.ApacheStatusPageURL)
	state.CaddyEnabled = optionalBool(i.CaddyEnabled)
	state.CaddyAdminAPIURL = optionalString(i.CaddyAdminAPIURL)
	state.PHPFPMEnabled = optionalBool(i.PHPFPMEnabled)
	state.PHPFPMStatusPageURL = optionalString(i.PHPFPMStatusPageURL)
	state.HAProxyEnabled = optionalBool(i.HAProxyEnabled)
	state.HAProxyStatsURL = optionalString(i.HAProxyStatsURL)
	state.HAProxyStatsSocket = optionalString(i.HAProxyStatsSocket)
	state.HAProxyUsername = optionalString(i.HAProxyUsername)

	state.RabbitMQEnabled = optionalBool(i.RabbitMQEnabled)
	state.RabbitMQManagementURL = optionalString(i.RabbitMQManagementURL)
	state.RabbitMQUsername = optionalString(i.RabbitMQUsername)
	state.RabbitMQVhostFilter = optionalString(i.RabbitMQVhostFilter)

	state.TSDBEnabled = optionalBool(i.TSDBEnabled)
	state.TSDBURL = optionalString(i.TSDBURL)
	state.TSDBAuthHeaderName = optionalString(i.TSDBAuthHeaderName)
	state.TSDBBasicAuthUsername = optionalString(i.TSDBBasicAuthUsername)
	state.TSDBVerifySSL = optionalBool(i.TSDBVerifySSL)
	state.VLLMEnabled = optionalBool(i.VLLMEnabled)
	state.VLLMMetricsURL = optionalString(i.VLLMMetricsURL)
	state.VLLMAuthHeaderName = optionalString(i.VLLMAuthHeaderName)
	state.VLLMVerifySSL = optionalBool(i.VLLMVerifySSL)
	state.SGLangEnabled = optionalBool(i.SGLangEnabled)
	state.SGLangMetricsURL = optionalString(i.SGLangMetricsURL)
	state.SGLangAuthHeaderName = optionalString(i.SGLangAuthHeaderName)
	state.SGLangVerifySSL = optionalBool(i.SGLangVerifySSL)

	state.WireguardEnabled = optionalBool(i.WireguardEnabled)
	state.TailscaleEnabled = optionalBool(i.TailscaleEnabled)
	state.SystemdEnabled = optionalBool(i.SystemdEnabled)
	state.Fail2banEnabled = optionalBool(i.Fail2banEnabled)
	state.NvidiaGPUEnabled = optionalBool(i.NvidiaGPUEnabled)
	state.IPv6Enabled = optionalBool(i.IPv6Enabled)
	state.LogsEnabled = optionalBool(i.LogsEnabled)
	// The server parses this list but re-renders it canonically (", "-joined),
	// so the configured spelling is kept whenever it names the same units.
	state.LogsUnitsCSV = csvOrKeep(i.LogsUnitsCSV, state.LogsUnitsCSV)

	state.ProxmoxTokenSecret = storedSecret(state.ProxmoxTokenSecret, i.ProxmoxTokenSecretSet)
	state.RedisPassword = storedSecret(state.RedisPassword, i.RedisPasswordSet)
	state.PostgreSQLPassword = storedSecret(state.PostgreSQLPassword, i.PostgreSQLPasswordSet)
	state.MySQLPassword = storedSecret(state.MySQLPassword, i.MySQLPasswordSet)
	state.HAProxyPassword = storedSecret(state.HAProxyPassword, i.HAProxyPasswordSet)
	state.RabbitMQPassword = storedSecret(state.RabbitMQPassword, i.RabbitMQPasswordSet)
	state.TSDBAuthHeaderValue = storedSecret(state.TSDBAuthHeaderValue, i.TSDBAuthHeaderValueSet)
	state.TSDBBasicAuthPassword = storedSecret(state.TSDBBasicAuthPassword, i.TSDBBasicAuthPasswordSet)
	state.VLLMAuthHeaderValue = storedSecret(state.VLLMAuthHeaderValue, i.VLLMAuthHeaderValueSet)
	state.SGLangAuthHeaderValue = storedSecret(state.SGLangAuthHeaderValue, i.SGLangAuthHeaderValueSet)

	state.RedisPasswordSet = types.BoolValue(i.RedisPasswordSet)
	state.PostgreSQLPasswordSet = types.BoolValue(i.PostgreSQLPasswordSet)
	state.MySQLPasswordSet = types.BoolValue(i.MySQLPasswordSet)
	state.RabbitMQPasswordSet = types.BoolValue(i.RabbitMQPasswordSet)
	state.HAProxyPasswordSet = types.BoolValue(i.HAProxyPasswordSet)
	state.ProxmoxTokenSecretSet = types.BoolValue(i.ProxmoxTokenSecretSet)
	state.TSDBAuthHeaderValueSet = types.BoolValue(i.TSDBAuthHeaderValueSet)
	state.TSDBBasicAuthPasswordSet = types.BoolValue(i.TSDBBasicAuthPasswordSet)
	state.VLLMAuthHeaderValueSet = types.BoolValue(i.VLLMAuthHeaderValueSet)
	state.SGLangAuthHeaderValueSet = types.BoolValue(i.SGLangAuthHeaderValueSet)
}

// Package provider implements the Gezor Terraform provider.
package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

var _ provider.Provider = &gezorProvider{}

type gezorProvider struct {
	version string
}

type providerModel struct {
	Endpoint    types.String `tfsdk:"endpoint"`
	Token       types.String `tfsdk:"token"`
	Workspace   types.String `tfsdk:"workspace"`
	MaxRetries  types.Int64  `tfsdk:"max_retries"`
	PollTimeout types.Int64  `tfsdk:"command_timeout_seconds"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &gezorProvider{version: version} }
}

func (p *gezorProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "gezor"
	resp.Version = p.version
}

func (p *gezorProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Configure Gezor Cloud with an API token. Everything except adding or removing people can be managed.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Gezor Cloud URL. Defaults to `GEZOR_ENDPOINT` or `https://app.gezor.cloud`.",
			},
			"token": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "API token (`gzr_sa_...`). Defaults to `GEZOR_TOKEN`.",
			},
			"workspace": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Default workspace id or slug. Defaults to `GEZOR_WORKSPACE`, then the organization's main workspace. Each resource can override it.",
			},
			"max_retries": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Retries for rate-limited or temporarily unavailable requests. Default 4.",
			},
			"command_timeout_seconds": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "How long to wait for the cluster operator to finish a command (Kafka topics, schemas). Default 300.",
			},
		},
	}
}

func (p *gezorProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := firstNonEmpty(cfg.Endpoint.ValueString(), os.Getenv("GEZOR_ENDPOINT"), client.DefaultEndpoint)
	token := firstNonEmpty(cfg.Token.ValueString(), os.Getenv("GEZOR_TOKEN"))
	workspace := firstNonEmpty(cfg.Workspace.ValueString(), os.Getenv("GEZOR_WORKSPACE"))
	if cfg.Token.IsUnknown() || cfg.Endpoint.IsUnknown() || cfg.Workspace.IsUnknown() {
		return
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token",
			"Set token in the provider block or the GEZOR_TOKEN environment variable. Create one under Organization → API tokens.")
		return
	}
	if !strings.HasPrefix(token, "gzr_sa_") {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Invalid API token",
			"Gezor API tokens start with gzr_sa_.")
		return
	}
	c := client.New(endpoint, token, workspace, "terraform-provider-gezor/"+p.version)
	if !cfg.MaxRetries.IsNull() && !cfg.MaxRetries.IsUnknown() {
		c.MaxRetries = int(cfg.MaxRetries.ValueInt64())
	}
	if !cfg.PollTimeout.IsNull() && !cfg.PollTimeout.IsUnknown() && cfg.PollTimeout.ValueInt64() > 0 {
		c.PollTimeout = secondsDuration(cfg.PollTimeout.ValueInt64())
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *gezorProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewWorkspaceResource,
		NewRoleResource,
		NewWorkspaceSettingsResource,
		NewSSOProviderResource,
		NewSSOGroupMappingsResource,
		NewSSODomainResource,
		NewKMSKeyResource,
		NewKMSDefaultKeyResource,
		NewAWSConnectionResource,
		NewAWSLakeResource,
		NewAWSCryptoModeResource,
		NewClusterResource,
		NewClusterInstallTokenResource,
		NewClusterAppResource,
		NewKafkaTopicResource,
		NewSchemaSubjectResource,
		NewCDCInstanceResource,
		NewCDCConnectorResource,
		NewSecureAccessConnectorResource,
		NewSecureAccessEndpointResource,
		NewDbtProjectResource,
		NewPipelineResource,
		NewPipelineNotebookResource,
	}
}

func (p *gezorProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCurrentIdentityDataSource,
		NewWorkspaceDataSource,
		NewWorkspacesDataSource,
		NewRolesDataSource,
		NewPermissionsDataSource,
		NewClusterDataSource,
		NewClustersDataSource,
		NewKMSKeysDataSource,
		NewAWSCloudFormationDataSource,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

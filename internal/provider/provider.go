package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

const defaultAPIURL = "https://api.4lock.net"

type vappcloudProvider struct {
	version string
}

type providerModel struct {
	Token  types.String `tfsdk:"token"`
	APIURL types.String `tfsdk:"api_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &vappcloudProvider{version: version}
	}
}

func (p *vappcloudProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "vappcloud"
	resp.Version = p.version
}

func (p *vappcloudProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		MarkdownDescription: "Manage VAppCloud resources. Organization service tokens are exchanged for short-lived API JWTs and are never persisted in state.",
		Attributes: map[string]providerschema.Attribute{
			"token": providerschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "VAppCloud organization service token. Defaults to `VAPPCLOUD_TOKEN`.",
			},
			"api_url": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "VAppCloud API base URL. Defaults to `VAPPCLOUD_API_URL`, then `https://api.4lock.net`.",
				Validators: []validator.String{
					stringvalidator.Any(
						stringvalidator.RegexMatches(apiURLPattern(), "must be an absolute HTTP(S) URL"),
					),
				},
			},
		},
	}
}

func (p *vappcloudProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.Token.IsUnknown() || config.APIURL.IsUnknown() {
		resp.Diagnostics.AddError("Unknown provider configuration", "Provider token and api_url must be known during configuration.")
		return
	}
	token := config.Token.ValueString()
	if token == "" {
		token = os.Getenv("VAPPCLOUD_TOKEN")
	}
	apiURL := config.APIURL.ValueString()
	if apiURL == "" {
		apiURL = os.Getenv("VAPPCLOUD_API_URL")
	}
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	c, err := client.New(apiURL, token, p.version)
	if err != nil {
		resp.Diagnostics.AddError("Unable to configure VAppCloud client", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *vappcloudProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewProjectResource,
		NewDeviceResource,
		NewComputeInstanceResource,
		NewVMMResource,
		NewApplicationInstanceResource,
	}
}

func (p *vappcloudProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewProjectsDataSource,
		NewProjectDataSource,
		NewDevicesDataSource,
		NewDeviceDataSource,
		NewComputeInstancesDataSource,
		NewComputeInstanceDataSource,
		NewVMMsDataSource,
		NewVMMDataSource,
		NewCloudConnectionsDataSource,
		NewCloudProvidersDataSource,
		NewCloudRegionsDataSource,
		NewCloudSizesDataSource,
		NewCloudImagesDataSource,
		NewMarketplaceApplicationsDataSource,
		NewMarketplaceVersionsDataSource,
		NewGitHubConnectionsDataSource,
		NewGitHubRepositoriesDataSource,
		NewApplicationInstancesDataSource,
		NewApplicationInstanceDataSource,
		NewOperationDataSource,
	}
}

func providerClient(data any, diagnostics *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil
	}
	c, ok := data.(*client.Client)
	if !ok {
		diagnostics.AddError("Unexpected provider data", "Expected a configured VAppCloud API client.")
		return nil
	}
	return c
}

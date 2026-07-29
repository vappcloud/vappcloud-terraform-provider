package provider

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
	Token              types.String  `tfsdk:"token"`
	APIURL             types.String  `tfsdk:"api_url"`
	EndpointOverrides  types.Map     `tfsdk:"endpoint_overrides"`
	MaxRetries         types.Int64   `tfsdk:"max_retries"`
	RequestTimeout     types.String  `tfsdk:"request_timeout"`
	RetryMaxWait       types.String  `tfsdk:"retry_max_wait"`
	RateLimitPerSecond types.Float64 `tfsdk:"rate_limit_per_second"`
	ProxyURL           types.String  `tfsdk:"proxy_url"`
	CACertificate      types.String  `tfsdk:"ca_certificate"`
	InsecureSkipVerify types.Bool    `tfsdk:"insecure_skip_verify"`
}

type operationalConfigValidator struct{}

func (operationalConfigValidator) Description(context.Context) string {
	return "Validates transport and retry provider configuration."
}

func (operationalConfigValidator) MarkdownDescription(ctx context.Context) string {
	return operationalConfigValidator{}.Description(ctx)
}

func (operationalConfigValidator) ValidateProvider(ctx context.Context, req provider.ValidateConfigRequest, resp *provider.ValidateConfigResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.Token.IsUnknown() &&
		strings.TrimSpace(config.Token.ValueString()) == "" &&
		strings.TrimSpace(os.Getenv("VAPPCLOUD_TOKEN")) == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing VAppCloud token",
			"Set the token provider attribute or the VAPPCLOUD_TOKEN environment variable.",
		)
	}
	if config.InsecureSkipVerify.ValueBool() && !config.CACertificate.IsNull() && config.CACertificate.ValueString() != "" {
		resp.Diagnostics.AddError(
			"Conflicting TLS configuration",
			"ca_certificate and insecure_skip_verify cannot be configured together.",
		)
	}
	for name, value := range map[string]types.String{
		"request_timeout": config.RequestTimeout,
		"retry_max_wait":  config.RetryMaxWait,
	} {
		if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
			continue
		}
		duration, err := time.ParseDuration(value.ValueString())
		if err != nil || duration <= 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root(name),
				"Invalid provider duration",
				name+" must be a positive Go duration such as 30s or 2m.",
			)
		}
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &vappcloudProvider{version: version}
	}
}

func (p *vappcloudProvider) ConfigValidators(context.Context) []provider.ConfigValidator {
	return []provider.ConfigValidator{operationalConfigValidator{}}
}

func (p *vappcloudProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "vappcloud"
	resp.Version = p.version
}

func (p *vappcloudProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		MarkdownDescription: "Manage VAppCloud resources. Named API keys for role-bound service accounts are exchanged for short-lived API JWTs and are never persisted in state.",
		Attributes: map[string]providerschema.Attribute{
			"token": providerschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Named API key for a role-bound VAppCloud service account. Defaults to `VAPPCLOUD_TOKEN`.",
			},
			"api_url": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "VAppCloud API base URL. Defaults to `VAPPCLOUD_API_URL`, then `https://api.4lock.net`.",
				Validators: []validator.String{
					apiURLValidator{},
				},
			},
			"endpoint_overrides": providerschema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Optional service-specific base URLs keyed by the first API path segment (for example `projects` or `vmms`). Intended for testing and staged rollouts.",
			},
			"max_retries": providerschema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Maximum retry count for retryable API failures. Defaults to 5.",
				Validators:          []validator.Int64{int64validator.Between(0, 20)},
			},
			"request_timeout": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Timeout for each HTTP request in Go duration syntax. Defaults to `30s`.",
			},
			"retry_max_wait": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Maximum delay between retries in Go duration syntax. Defaults to `30s`.",
			},
			"rate_limit_per_second": providerschema.Float64Attribute{
				Optional:            true,
				MarkdownDescription: "Optional client-side request rate limit. Zero disables limiting.",
				Validators:          []validator.Float64{float64validator.AtLeast(0)},
			},
			"proxy_url": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional HTTP(S) proxy URL. The standard proxy environment variables remain supported when this is unset.",
			},
			"ca_certificate": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Custom CA certificate PEM or path to a PEM file.",
			},
			"insecure_skip_verify": providerschema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Disable TLS certificate verification. Use only with controlled development endpoints.",
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
	requestTimeout, ok := configuredDuration(config.RequestTimeout, 30*time.Second, "request_timeout", &resp.Diagnostics)
	if !ok {
		return
	}
	retryMaxWait, ok := configuredDuration(config.RetryMaxWait, 30*time.Second, "retry_max_wait", &resp.Diagnostics)
	if !ok {
		return
	}
	endpointOverrides := map[string]string{}
	if !config.EndpointOverrides.IsNull() && !config.EndpointOverrides.IsUnknown() {
		resp.Diagnostics.Append(config.EndpointOverrides.ElementsAs(ctx, &endpointOverrides, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	maxRetries := 5
	if !config.MaxRetries.IsNull() {
		maxRetries = int(config.MaxRetries.ValueInt64())
	}
	c, err := client.NewWithConfig(client.Config{
		BaseURL:            apiURL,
		Token:              token,
		ProviderVersion:    p.version,
		TerraformVersion:   req.TerraformVersion,
		RequestTimeout:     requestTimeout,
		MaxRetries:         maxRetries,
		RetryMaxWait:       retryMaxWait,
		RateLimitPerSecond: config.RateLimitPerSecond.ValueFloat64(),
		ProxyURL:           config.ProxyURL.ValueString(),
		CACertificatePEM:   config.CACertificate.ValueString(),
		InsecureSkipVerify: config.InsecureSkipVerify.ValueBool(),
		EndpointOverrides:  endpointOverrides,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to configure VAppCloud client", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func configuredDuration(value types.String, fallback time.Duration, name string, diagnostics *diag.Diagnostics) (time.Duration, bool) {
	if value.IsNull() || value.ValueString() == "" {
		return fallback, true
	}
	if value.IsUnknown() {
		diagnostics.AddError("Unknown provider configuration", name+" must be known during configuration.")
		return 0, false
	}
	duration, err := time.ParseDuration(value.ValueString())
	if err != nil || duration <= 0 {
		diagnostics.AddAttributeError(
			path.Root(name),
			"Invalid provider duration",
			name+" must be a positive Go duration such as 30s or 2m.",
		)
		return 0, false
	}
	return duration, true
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

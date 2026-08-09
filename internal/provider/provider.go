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
	AccessKeyID          types.String  `tfsdk:"access_key_id"`
	SecretAccessKey      types.String  `tfsdk:"secret_access_key"`
	SessionToken         types.String  `tfsdk:"session_token"`
	CredentialProcess    types.String  `tfsdk:"credential_process"`
	WebIdentityTokenFile types.String  `tfsdk:"web_identity_token_file"`
	RoleARN              types.String  `tfsdk:"role_arn"`
	SessionName          types.String  `tfsdk:"session_name"`
	APIURL               types.String  `tfsdk:"api_url"`
	EndpointOverrides    types.Map     `tfsdk:"endpoint_overrides"`
	MaxRetries           types.Int64   `tfsdk:"max_retries"`
	RequestTimeout       types.String  `tfsdk:"request_timeout"`
	RetryMaxWait         types.String  `tfsdk:"retry_max_wait"`
	RateLimitPerSecond   types.Float64 `tfsdk:"rate_limit_per_second"`
	ProxyURL             types.String  `tfsdk:"proxy_url"`
	CACertificate        types.String  `tfsdk:"ca_certificate"`
	InsecureSkipVerify   types.Bool    `tfsdk:"insecure_skip_verify"`
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
	if config.AccessKeyID.IsUnknown() || config.SecretAccessKey.IsUnknown() || config.SessionToken.IsUnknown() ||
		config.CredentialProcess.IsUnknown() || config.WebIdentityTokenFile.IsUnknown() || config.RoleARN.IsUnknown() {
		return
	}
	accessKeyID := firstNonEmpty(config.AccessKeyID.ValueString(), os.Getenv("VAPPCLOUD_ACCESS_KEY_ID"))
	secretAccessKey := firstNonEmpty(config.SecretAccessKey.ValueString(), os.Getenv("VAPPCLOUD_SECRET_ACCESS_KEY"))
	sessionToken := firstNonEmpty(config.SessionToken.ValueString(), os.Getenv("VAPPCLOUD_SESSION_TOKEN"))
	credentialProcess := firstNonEmpty(config.CredentialProcess.ValueString(), os.Getenv("VAPPCLOUD_CREDENTIAL_PROCESS"))
	webIdentityTokenFile := firstNonEmpty(config.WebIdentityTokenFile.ValueString(), os.Getenv("VAPPCLOUD_WEB_IDENTITY_TOKEN_FILE"))
	roleARN := firstNonEmpty(config.RoleARN.ValueString(), os.Getenv("VAPPCLOUD_ROLE_ARN"))
	staticConfigured := accessKeyID != "" || secretAccessKey != "" || sessionToken != ""
	processConfigured := credentialProcess != ""
	webIdentityConfigured := webIdentityTokenFile != "" || roleARN != ""
	if !staticConfigured && !processConfigured && !webIdentityConfigured {
		resp.Diagnostics.AddAttributeError(
			path.Root("access_key_id"),
			"Missing VAppCloud credentials",
			"Configure temporary access_key_id, secret_access_key, and session_token; credential_process; or web_identity_token_file with role_arn.",
		)
	}
	if staticConfigured && (accessKeyID == "" || secretAccessKey == "" || sessionToken == "") {
		resp.Diagnostics.AddError("Incomplete VAppCloud temporary credentials", "access_key_id, secret_access_key, and session_token must be configured together.")
	}
	if webIdentityConfigured && (webIdentityTokenFile == "" || roleARN == "") {
		resp.Diagnostics.AddError("Incomplete VAppCloud web identity credentials", "web_identity_token_file and role_arn must be configured together.")
	}
	sources := 0
	for _, configured := range []bool{staticConfigured, processConfigured, webIdentityConfigured} {
		if configured {
			sources++
		}
	}
	if sources > 1 {
		resp.Diagnostics.AddError("Ambiguous VAppCloud credentials", "Configure exactly one credential source: temporary credentials, credential_process, or web identity.")
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
		MarkdownDescription: "Manage VAppCloud resources with short-lived, role-based credentials. Every API request is signed with SigV4 and credentials are never persisted in state.",
		Attributes: map[string]providerschema.Attribute{
			"access_key_id": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Temporary access key ID generated by the VAppCloud Access Portal. Defaults to `VAPPCLOUD_ACCESS_KEY_ID`.",
			},
			"secret_access_key": providerschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Temporary secret access key generated by the VAppCloud Access Portal. Defaults to `VAPPCLOUD_SECRET_ACCESS_KEY`.",
			},
			"session_token": providerschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Temporary session token generated with the access key pair. Defaults to `VAPPCLOUD_SESSION_TOKEN`.",
			},
			"credential_process": providerschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Command that returns AWS credential_process version 1 JSON. Defaults to `VAPPCLOUD_CREDENTIAL_PROCESS`.",
			},
			"web_identity_token_file": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to an OIDC token file used with role_arn. The file is re-read before each refresh. Defaults to `VAPPCLOUD_WEB_IDENTITY_TOKEN_FILE`.",
			},
			"role_arn": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "IAM role ARN used with web identity. Defaults to `VAPPCLOUD_ROLE_ARN`.",
			},
			"session_name": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Web identity session name used for audit records. Defaults to `VAPPCLOUD_SESSION_NAME`, then `terraform-provider`.",
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
	if config.AccessKeyID.IsUnknown() || config.SecretAccessKey.IsUnknown() || config.SessionToken.IsUnknown() ||
		config.CredentialProcess.IsUnknown() || config.WebIdentityTokenFile.IsUnknown() ||
		config.RoleARN.IsUnknown() || config.SessionName.IsUnknown() || config.APIURL.IsUnknown() {
		resp.Diagnostics.AddError("Unknown provider configuration", "Provider credentials, role/session settings, and api_url must be known during configuration.")
		return
	}
	accessKeyID := firstNonEmpty(config.AccessKeyID.ValueString(), os.Getenv("VAPPCLOUD_ACCESS_KEY_ID"))
	secretAccessKey := firstNonEmpty(config.SecretAccessKey.ValueString(), os.Getenv("VAPPCLOUD_SECRET_ACCESS_KEY"))
	sessionToken := firstNonEmpty(config.SessionToken.ValueString(), os.Getenv("VAPPCLOUD_SESSION_TOKEN"))
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
		BaseURL:              apiURL,
		AccessKeyID:          accessKeyID,
		SecretAccessKey:      secretAccessKey,
		SessionToken:         sessionToken,
		CredentialProcess:    firstNonEmpty(config.CredentialProcess.ValueString(), os.Getenv("VAPPCLOUD_CREDENTIAL_PROCESS")),
		WebIdentityTokenFile: firstNonEmpty(config.WebIdentityTokenFile.ValueString(), os.Getenv("VAPPCLOUD_WEB_IDENTITY_TOKEN_FILE")),
		RoleARN:              firstNonEmpty(config.RoleARN.ValueString(), os.Getenv("VAPPCLOUD_ROLE_ARN")),
		SessionName:          firstNonEmpty(config.SessionName.ValueString(), os.Getenv("VAPPCLOUD_SESSION_NAME")),
		ProviderVersion:      p.version,
		TerraformVersion:     req.TerraformVersion,
		RequestTimeout:       requestTimeout,
		MaxRetries:           maxRetries,
		RetryMaxWait:         retryMaxWait,
		RateLimitPerSecond:   config.RateLimitPerSecond.ValueFloat64(),
		ProxyURL:             config.ProxyURL.ValueString(),
		CACertificatePEM:     config.CACertificate.ValueString(),
		InsecureSkipVerify:   config.InsecureSkipVerify.ValueBool(),
		EndpointOverrides:    endpointOverrides,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to configure VAppCloud client", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
		NewIAMPolicyResource,
		NewIAMPolicyVersionResource,
		NewIAMPolicyAttachmentResource,
		NewIAMGroupResource,
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

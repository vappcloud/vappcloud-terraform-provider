package provider

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type namedListDataSource struct {
	dataSourceBase
	suffix   string
	endpoint string
}

type namedListModel struct {
	ProjectID          types.String `tfsdk:"project_id"`
	CloudConnectionID  types.String `tfsdk:"cloud_connection_id"`
	Region             types.String `tfsdk:"region"`
	ApplicationID      types.String `tfsdk:"application_id"`
	GitHubConnectionID types.String `tfsdk:"github_connection_id"`
	Items              types.List   `tfsdk:"items"`
}

var namedItemTypes = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "description": types.StringType,
	"state": types.StringType, "metadata_json": types.StringType,
}

func NewCloudConnectionsDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "cloud_connections", endpoint: "/v1/cloud-connections"}
}
func NewCloudProvidersDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "cloud_providers", endpoint: "/v1/cloud/providers"}
}
func NewCloudRegionsDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "cloud_regions", endpoint: "/v1/cloud/regions"}
}
func NewCloudSizesDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "cloud_sizes", endpoint: "/v1/cloud/sizes"}
}
func NewCloudImagesDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "cloud_images", endpoint: "/v1/cloud/images"}
}
func NewMarketplaceApplicationsDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "marketplace_applications", endpoint: "/v1/marketplace/applications"}
}
func NewMarketplaceVersionsDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "marketplace_versions", endpoint: "/v1/marketplace/versions"}
}
func NewGitHubConnectionsDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "github_connections", endpoint: "/v1/github/connections"}
}
func NewGitHubRepositoriesDataSource() datasource.DataSource {
	return &namedListDataSource{suffix: "github_repositories", endpoint: "/v1/github/repositories"}
}

func (d *namedListDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.suffix
}

func (d *namedListDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads VAppCloud catalog entries from `" + d.endpoint + "`.",
		Attributes: map[string]schema.Attribute{
			"project_id":           schema.StringAttribute{Optional: true},
			"cloud_connection_id":  schema.StringAttribute{Optional: true},
			"region":               schema.StringAttribute{Optional: true},
			"application_id":       schema.StringAttribute{Optional: true},
			"github_connection_id": schema.StringAttribute{Optional: true},
			"items": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true},
					"description": schema.StringAttribute{Computed: true}, "state": schema.StringAttribute{Computed: true},
					"metadata_json": schema.StringAttribute{
						Computed:            true,
						Sensitive:           true,
						MarkdownDescription: "Canonical catalog metadata. Marked sensitive because provider-defined metadata is not schema-constrained.",
					},
				},
			}},
		},
	}
}

func (d *namedListDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state namedListModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	query := url.Values{}
	addQuery(query, "project_id", state.ProjectID)
	addQuery(query, "cloud_connection_id", state.CloudConnectionID)
	addQuery(query, "region", state.Region)
	addQuery(query, "application_id", state.ApplicationID)
	addQuery(query, "github_connection_id", state.GitHubConnectionID)
	endpoint := d.endpoint
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	catalogItems, err := client.ListAll[client.NamedItem](ctx, d.client, endpoint)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read VAppCloud catalog", err.Error())
		return
	}
	values := make([]attr.Value, 0, len(catalogItems))
	for _, item := range catalogItems {
		metadataValue := item.Metadata
		if item.MetadataJSON != "" {
			if err := json.Unmarshal([]byte(item.MetadataJSON), &metadataValue); err != nil {
				resp.Diagnostics.AddError("Unable to decode catalog metadata", err.Error())
				return
			}
		}
		metadata, err := canonicalJSON(metadataValue)
		if err != nil {
			resp.Diagnostics.AddError("Unable to encode catalog metadata", err.Error())
			return
		}
		value, diags := types.ObjectValue(namedItemTypes, map[string]attr.Value{
			"id": types.StringValue(item.ID), "name": types.StringValue(item.Name),
			"description": types.StringValue(item.Description), "state": types.StringValue(item.State),
			"metadata_json": types.StringValue(metadata),
		})
		resp.Diagnostics.Append(diags...)
		values = append(values, value)
	}
	items, diags := types.ListValue(types.ObjectType{AttrTypes: namedItemTypes}, values)
	resp.Diagnostics.Append(diags...)
	state.Items = items
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func addQuery(query url.Values, key string, value types.String) {
	if !value.IsNull() && !value.IsUnknown() && value.ValueString() != "" {
		query.Set(key, value.ValueString())
	}
}

var _ datasource.DataSourceWithConfigure = &namedListDataSource{}

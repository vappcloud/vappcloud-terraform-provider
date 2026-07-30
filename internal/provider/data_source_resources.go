package provider

import (
	"context"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type dataSourceBase struct{ client *client.Client }

func (d *dataSourceBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

type projectsDataSource struct{ dataSourceBase }
type projectDataSource struct{ dataSourceBase }
type devicesDataSource struct{ dataSourceBase }
type deviceDataSource struct{ dataSourceBase }
type computeInstancesDataSource struct{ dataSourceBase }
type computeInstanceDataSource struct{ dataSourceBase }
type vmmsDataSource struct{ dataSourceBase }
type vmmDataSource struct{ dataSourceBase }
type applicationInstancesDataSource struct{ dataSourceBase }
type applicationInstanceDataSource struct{ dataSourceBase }
type operationDataSource struct{ dataSourceBase }

type projectListModel struct {
	Projects types.List `tfsdk:"projects"`
}

type projectDataModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	ResourceVersion types.Int64  `tfsdk:"resource_version"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

var projectObjectTypes = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "description": types.StringType,
	"resource_version": types.Int64Type, "created_at": types.StringType, "updated_at": types.StringType,
}

func NewProjectsDataSource() datasource.DataSource         { return &projectsDataSource{} }
func NewProjectDataSource() datasource.DataSource          { return &projectDataSource{} }
func NewDevicesDataSource() datasource.DataSource          { return &devicesDataSource{} }
func NewDeviceDataSource() datasource.DataSource           { return &deviceDataSource{} }
func NewComputeInstancesDataSource() datasource.DataSource { return &computeInstancesDataSource{} }
func NewComputeInstanceDataSource() datasource.DataSource  { return &computeInstanceDataSource{} }
func NewVMMsDataSource() datasource.DataSource             { return &vmmsDataSource{} }
func NewVMMDataSource() datasource.DataSource              { return &vmmDataSource{} }
func NewApplicationInstancesDataSource() datasource.DataSource {
	return &applicationInstancesDataSource{}
}
func NewApplicationInstanceDataSource() datasource.DataSource {
	return &applicationInstanceDataSource{}
}
func NewOperationDataSource() datasource.DataSource { return &operationDataSource{} }

func (d *projectsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_projects"
}
func (d *projectsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all projects visible to the service account.",
		Attributes: map[string]schema.Attribute{
			"projects": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true},
				"description": schema.StringAttribute{Computed: true}, "resource_version": schema.Int64Attribute{Computed: true},
				"created_at": schema.StringAttribute{Computed: true}, "updated_at": schema.StringAttribute{Computed: true},
			}}},
		},
	}
}
func (d *projectsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	projects, err := client.ListAll[client.Project](ctx, d.client, "/v1/projects")
	if err != nil {
		resp.Diagnostics.AddError("Unable to list VAppCloud projects", err.Error())
		return
	}
	values := make([]attr.Value, 0, len(projects))
	for _, project := range projects {
		value, diags := types.ObjectValue(projectObjectTypes, map[string]attr.Value{
			"id": types.StringValue(project.ID), "name": types.StringValue(project.Name),
			"description": types.StringValue(project.Description), "resource_version": types.Int64Value(project.ResourceVersion.Int64()),
			"created_at": formatTime(project.CreatedAt), "updated_at": formatTime(project.UpdatedAt),
		})
		resp.Diagnostics.Append(diags...)
		values = append(values, value)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: projectObjectTypes}, values)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &projectListModel{Projects: list})...)
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}
func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads a project by opaque public ID.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Required: true}, "name": schema.StringAttribute{Computed: true},
		"description": schema.StringAttribute{Computed: true}, "resource_version": schema.Int64Attribute{Computed: true},
		"created_at": schema.StringAttribute{Computed: true}, "updated_at": schema.StringAttribute{Computed: true},
	}}
}
func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state projectDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var project client.Project
	if err := d.client.Do(ctx, http.MethodGet, "/v1/projects/"+client.Escape(state.ID.ValueString()), nil, &project, ""); err != nil {
		resp.Diagnostics.AddError("Unable to read VAppCloud project", err.Error())
		return
	}
	state.Name = types.StringValue(project.Name)
	state.Description = types.StringValue(project.Description)
	state.ResourceVersion = types.Int64Value(project.ResourceVersion.Int64())
	state.CreatedAt = formatTime(project.CreatedAt)
	state.UpdatedAt = formatTime(project.UpdatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

type projectFilterModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	Items     types.List   `tfsdk:"items"`
}

var resourceSummaryTypes = map[string]attr.Type{
	"id": types.StringType, "project_id": types.StringType, "device_id": types.StringType,
	"name": types.StringType, "state": types.StringType, "default_vmm_id": types.StringType,
	"is_default": types.BoolType, "management": types.StringType,
}

func resourceListSchema(description string) schema.Schema {
	return schema.Schema{MarkdownDescription: description, Attributes: map[string]schema.Attribute{
		"project_id": schema.StringAttribute{Required: true},
		"items": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true}, "project_id": schema.StringAttribute{Computed: true},
			"device_id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true},
			"state": schema.StringAttribute{Computed: true}, "default_vmm_id": schema.StringAttribute{Computed: true},
			"is_default": schema.BoolAttribute{Computed: true}, "management": schema.StringAttribute{Computed: true},
		}}},
	}}
}

func (d *devicesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_devices"
}
func (d *devicesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = resourceListSchema("Lists devices in a project.")
}
func (d *devicesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state projectFilterModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	path := "/v1/devices?project_id=" + url.QueryEscape(state.ProjectID.ValueString())
	items, err := client.ListAll[client.Device](ctx, d.client, path)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list devices", err.Error())
		return
	}
	values := make([]attr.Value, 0, len(items))
	for _, item := range items {
		value, diags := summaryValue(item.ID, item.ProjectID, "", item.Name, item.State, item.DefaultVMMID, false, "")
		resp.Diagnostics.Append(diags...)
		values = append(values, value)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: resourceSummaryTypes}, values)
	resp.Diagnostics.Append(diags...)
	state.Items = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *computeInstancesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute_instances"
}
func (d *computeInstancesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = resourceListSchema("Lists compute instances in a project.")
}
func (d *computeInstancesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state projectFilterModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := "/v1/compute-instances?project_id=" + url.QueryEscape(state.ProjectID.ValueString())
	items, err := client.ListAll[client.ComputeInstance](ctx, d.client, endpoint)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list compute instances", err.Error())
		return
	}
	values := make([]attr.Value, 0, len(items))
	for _, item := range items {
		value, diags := summaryValue(item.ID, item.ProjectID, item.DeviceID, item.Name, item.State, item.DefaultVMMID, false, "")
		resp.Diagnostics.Append(diags...)
		values = append(values, value)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: resourceSummaryTypes}, values)
	resp.Diagnostics.Append(diags...)
	state.Items = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *vmmsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vmms"
}
func (d *vmmsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = resourceListSchema("Lists all VMMs in a project, including system-managed defaults.")
}
func (d *vmmsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state projectFilterModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := "/v1/vmms?project_id=" + url.QueryEscape(state.ProjectID.ValueString())
	items, err := client.ListAll[client.VMM](ctx, d.client, endpoint)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list VMMs", err.Error())
		return
	}
	values := make([]attr.Value, 0, len(items))
	for _, item := range items {
		value, diags := summaryValue(item.ID, item.ProjectID, item.DeviceID, item.Name, item.State, "", item.IsDefault, item.Management)
		resp.Diagnostics.Append(diags...)
		values = append(values, value)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: resourceSummaryTypes}, values)
	resp.Diagnostics.Append(diags...)
	state.Items = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *applicationInstancesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_instances"
}
func (d *applicationInstancesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = resourceListSchema("Lists application instances in a project.")
}
func (d *applicationInstancesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state projectFilterModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := "/v1/application-instances?project_id=" + url.QueryEscape(state.ProjectID.ValueString())
	items, err := client.ListAll[client.ApplicationInstance](ctx, d.client, endpoint)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list application instances", err.Error())
		return
	}
	values := make([]attr.Value, 0, len(items))
	for _, item := range items {
		value, diags := summaryValue(item.ID, item.ProjectID, "", item.Name, item.State, "", false, "")
		resp.Diagnostics.Append(diags...)
		values = append(values, value)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: resourceSummaryTypes}, values)
	resp.Diagnostics.Append(diags...)
	state.Items = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func summaryValue(id, projectID, deviceID, name, state, defaultVMMID string, isDefault bool, management string) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(resourceSummaryTypes, map[string]attr.Value{
		"id": types.StringValue(id), "project_id": types.StringValue(projectID), "device_id": stringOrNull(deviceID),
		"name": types.StringValue(name), "state": types.StringValue(state), "default_vmm_id": stringOrNull(defaultVMMID),
		"is_default": types.BoolValue(isDefault), "management": stringOrNull(management),
	})
}

var (
	_ datasource.DataSourceWithConfigure = &projectsDataSource{}
	_ datasource.DataSourceWithConfigure = &projectDataSource{}
	_ datasource.DataSourceWithConfigure = &devicesDataSource{}
	_ datasource.DataSourceWithConfigure = &deviceDataSource{}
	_ datasource.DataSourceWithConfigure = &computeInstancesDataSource{}
	_ datasource.DataSourceWithConfigure = &computeInstanceDataSource{}
	_ datasource.DataSourceWithConfigure = &vmmsDataSource{}
	_ datasource.DataSourceWithConfigure = &vmmDataSource{}
	_ datasource.DataSourceWithConfigure = &applicationInstancesDataSource{}
	_ datasource.DataSourceWithConfigure = &applicationInstanceDataSource{}
	_ datasource.DataSourceWithConfigure = &operationDataSource{}
)

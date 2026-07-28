package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type operationDataModel struct {
	ID            types.String `tfsdk:"id"`
	CorrelationID types.String `tfsdk:"correlation_id"`
	ResourceID    types.String `tfsdk:"resource_id"`
	Kind          types.String `tfsdk:"kind"`
	State         types.String `tfsdk:"state"`
	ErrorCode     types.String `tfsdk:"error_code"`
	ErrorMessage  types.String `tfsdk:"error_message"`
	RequestID     types.String `tfsdk:"request_id"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

type deviceDetailModel struct {
	ID              types.String `tfsdk:"id"`
	ProjectID       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	State           types.String `tfsdk:"state"`
	DefaultVMMID    types.String `tfsdk:"default_vmm_id"`
	ResourceVersion types.Int64  `tfsdk:"resource_version"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

type computeDetailModel struct {
	ID                types.String `tfsdk:"id"`
	ProjectID         types.String `tfsdk:"project_id"`
	DeviceID          types.String `tfsdk:"device_id"`
	DefaultVMMID      types.String `tfsdk:"default_vmm_id"`
	CloudConnectionID types.String `tfsdk:"cloud_connection_id"`
	Region            types.String `tfsdk:"region"`
	Size              types.String `tfsdk:"size"`
	Image             types.String `tfsdk:"image"`
	Name              types.String `tfsdk:"name"`
	State             types.String `tfsdk:"state"`
	ResourceVersion   types.Int64  `tfsdk:"resource_version"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

type vmmDetailModel struct {
	ID                 types.String `tfsdk:"id"`
	ProjectID          types.String `tfsdk:"project_id"`
	DeviceID           types.String `tfsdk:"device_id"`
	Name               types.String `tfsdk:"name"`
	CPUCores           types.Int64  `tfsdk:"cpu_cores"`
	MemoryMB           types.Int64  `tfsdk:"memory_mb"`
	DiskMB             types.Int64  `tfsdk:"disk_mb"`
	DeletionProtection types.Bool   `tfsdk:"deletion_protection"`
	RetainDisk         types.Bool   `tfsdk:"retain_disk"`
	IsDefault          types.Bool   `tfsdk:"is_default"`
	Management         types.String `tfsdk:"management"`
	State              types.String `tfsdk:"state"`
	Health             types.String `tfsdk:"health"`
	DesiredRevision    types.Int64  `tfsdk:"desired_revision"`
	ObservedRevision   types.Int64  `tfsdk:"observed_revision"`
	OperationStatus    types.String `tfsdk:"operation_status"`
	OperationID        types.String `tfsdk:"operation_id"`
	CorrelationID      types.String `tfsdk:"correlation_id"`
	ResourceVersion    types.Int64  `tfsdk:"resource_version"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

type applicationDetailModel struct {
	ID              types.String `tfsdk:"id"`
	ProjectID       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Source          types.Object `tfsdk:"source"`
	Placements      types.List   `tfsdk:"placement"`
	SecretIDs       types.Set    `tfsdk:"secret_ids"`
	State           types.String `tfsdk:"state"`
	ReadyReplicas   types.Int64  `tfsdk:"ready_replicas"`
	DesiredReplicas types.Int64  `tfsdk:"desired_replicas"`
	OperationStatus types.String `tfsdk:"operation_status"`
	OperationID     types.String `tfsdk:"operation_id"`
	CorrelationID   types.String `tfsdk:"correlation_id"`
	ResourceVersion types.Int64  `tfsdk:"resource_version"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func detailString(description string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: description}
}

func detailVersion() schema.Int64Attribute {
	return schema.Int64Attribute{Computed: true, MarkdownDescription: "Optimistic concurrency version."}
}

func (d *deviceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device"
}

func (d *deviceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a logical device by opaque public ID.",
		Attributes: map[string]schema.Attribute{
			"id":               schema.StringAttribute{Required: true},
			"project_id":       detailString("Owning project ID."),
			"name":             detailString("Device name."),
			"state":            detailString("Enrollment and connection state."),
			"default_vmm_id":   detailString("System-managed default VMM ID."),
			"resource_version": detailVersion(),
			"created_at":       detailString("Creation timestamp."),
			"updated_at":       detailString("Last update timestamp."),
		},
	}
}

func (d *deviceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state deviceDetailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var device client.Device
	if err := d.client.Do(ctx, http.MethodGet, "/v1/devices/"+client.Escape(state.ID.ValueString()), nil, &device, ""); err != nil {
		resp.Diagnostics.AddError("Unable to read VAppCloud device", err.Error())
		return
	}
	state.ID = types.StringValue(device.ID)
	state.ProjectID = types.StringValue(device.ProjectID)
	state.Name = types.StringValue(device.Name)
	state.State = types.StringValue(device.State)
	state.DefaultVMMID = stringOrNull(device.DefaultVMMID)
	state.ResourceVersion = types.Int64Value(device.ResourceVersion.Int64())
	state.CreatedAt = formatTime(device.CreatedAt)
	state.UpdatedAt = formatTime(device.UpdatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *computeInstanceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute_instance"
}

func (d *computeInstanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a cloud compute instance by opaque public ID.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Required: true},
			"project_id":          detailString("Owning project ID."),
			"device_id":           detailString("Attached device ID."),
			"default_vmm_id":      detailString("Default VMM ID."),
			"cloud_connection_id": detailString("Cloud connection ID."),
			"region":              detailString("Cloud region."),
			"size":                detailString("Cloud size."),
			"image":               detailString("Cloud image."),
			"name":                detailString("Compute instance name."),
			"state":               detailString("Provisioning and readiness state."),
			"resource_version":    detailVersion(),
			"created_at":          detailString("Creation timestamp."),
			"updated_at":          detailString("Last update timestamp."),
		},
	}
}

func (d *computeInstanceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state computeDetailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var compute client.ComputeInstance
	if err := d.client.Do(ctx, http.MethodGet, "/v1/compute-instances/"+client.Escape(state.ID.ValueString()), nil, &compute, ""); err != nil {
		resp.Diagnostics.AddError("Unable to read VAppCloud compute instance", err.Error())
		return
	}
	state.ID = types.StringValue(compute.ID)
	state.ProjectID = types.StringValue(compute.ProjectID)
	state.DeviceID = types.StringValue(compute.DeviceID)
	state.DefaultVMMID = stringOrNull(compute.DefaultVMMID)
	state.CloudConnectionID = types.StringValue(compute.CloudConnection)
	state.Region = types.StringValue(compute.Region)
	state.Size = types.StringValue(compute.Size)
	state.Image = types.StringValue(compute.Image)
	state.Name = types.StringValue(compute.Name)
	state.State = types.StringValue(compute.State)
	state.ResourceVersion = types.Int64Value(compute.ResourceVersion.Int64())
	state.CreatedAt = formatTime(compute.CreatedAt)
	state.UpdatedAt = formatTime(compute.UpdatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *vmmDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vmm"
}

func (d *vmmDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a VMM by opaque public ID, including system-managed default VMMs.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Required: true},
			"project_id":          detailString("Owning project ID."),
			"device_id":           detailString("Host device ID."),
			"name":                detailString("VMM name."),
			"cpu_cores":           schema.Int64Attribute{Computed: true},
			"memory_mb":           schema.Int64Attribute{Computed: true},
			"disk_mb":             schema.Int64Attribute{Computed: true},
			"deletion_protection": schema.BoolAttribute{Computed: true},
			"retain_disk":         schema.BoolAttribute{Computed: true},
			"is_default":          schema.BoolAttribute{Computed: true},
			"management":          detailString("Management owner."),
			"state":               detailString("Runtime state."),
			"health":              detailString("Agent-reported health."),
			"desired_revision":    schema.Int64Attribute{Computed: true},
			"observed_revision":   schema.Int64Attribute{Computed: true},
			"operation_status":    detailString("Latest operation state."),
			"operation_id":        detailString("Latest operation ID."),
			"correlation_id":      detailString("Latest correlation ID."),
			"resource_version":    detailVersion(),
			"created_at":          detailString("Creation timestamp."),
			"updated_at":          detailString("Last update timestamp."),
		},
	}
}

func (d *vmmDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state vmmDetailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var vmm client.VMM
	if err := d.client.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(state.ID.ValueString()), nil, &vmm, ""); err != nil {
		resp.Diagnostics.AddError("Unable to read VAppCloud VMM", err.Error())
		return
	}
	state.ID = types.StringValue(vmm.ID)
	state.ProjectID = types.StringValue(vmm.ProjectID)
	state.DeviceID = types.StringValue(vmm.DeviceID)
	state.Name = types.StringValue(vmm.Name)
	state.CPUCores = types.Int64Value(vmm.CPUCores)
	state.MemoryMB = types.Int64Value(vmm.MemoryMB)
	state.DiskMB = types.Int64Value(vmm.DiskMB)
	state.DeletionProtection = types.BoolValue(vmm.DeletionProtection)
	state.RetainDisk = types.BoolValue(vmm.RetainDisk)
	state.IsDefault = types.BoolValue(vmm.IsDefault)
	state.Management = types.StringValue(vmm.Management)
	state.State = types.StringValue(vmm.State)
	state.Health = types.StringValue(vmm.Health)
	state.DesiredRevision = types.Int64Value(vmm.DesiredRevision.Int64())
	state.ObservedRevision = types.Int64Value(vmm.ObservedRevision.Int64())
	state.OperationStatus = stringOrNull(vmm.Operation.State)
	state.OperationID = stringOrNull(vmm.Operation.ID)
	state.CorrelationID = stringOrNull(vmm.Operation.CorrelationID)
	state.ResourceVersion = types.Int64Value(vmm.ResourceVersion.Int64())
	state.CreatedAt = formatTime(vmm.CreatedAt)
	state.UpdatedAt = formatTime(vmm.UpdatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *applicationInstanceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_instance"
}

func (d *applicationInstanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads an application instance by opaque public ID.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Required: true},
			"project_id":  detailString("Owning project ID."),
			"name":        detailString("Application instance name."),
			"description": detailString("Application description."),
			"source": schema.SingleNestedAttribute{Computed: true, Attributes: map[string]schema.Attribute{
				"kind": detailString("Source kind."), "marketplace_application_id": detailString("Marketplace application ID."),
				"marketplace_version_id": detailString("Marketplace version ID."), "github_connection_id": detailString("GitHub connection ID."),
				"repository": detailString("Repository name."), "ref": detailString("Git ref."),
			}},
			"placement": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"vmm_id": detailString("Target VMM ID."), "replica_count": schema.Int64Attribute{Computed: true},
			}}},
			"secret_ids":       schema.SetAttribute{Computed: true, ElementType: types.StringType},
			"state":            detailString("Deployment state."),
			"ready_replicas":   schema.Int64Attribute{Computed: true},
			"desired_replicas": schema.Int64Attribute{Computed: true},
			"operation_status": detailString("Latest operation state."),
			"operation_id":     detailString("Latest operation ID."),
			"correlation_id":   detailString("Latest correlation ID."),
			"resource_version": detailVersion(),
			"created_at":       detailString("Creation timestamp."),
			"updated_at":       detailString("Last update timestamp."),
		},
	}
}

func (d *applicationInstanceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state applicationDetailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var application client.ApplicationInstance
	if err := d.client.Do(ctx, http.MethodGet, "/v1/application-instances/"+client.Escape(state.ID.ValueString()), nil, &application, ""); err != nil {
		resp.Diagnostics.AddError("Unable to read VAppCloud application instance", err.Error())
		return
	}
	var converted applicationInstanceResourceModel
	applicationToState(application, &converted, &resp.Diagnostics)
	state.ID = converted.ID
	state.ProjectID = converted.ProjectID
	state.Name = converted.Name
	state.Description = converted.Description
	state.Source = converted.Source
	state.Placements = converted.Placements
	state.SecretIDs = converted.SecretIDs
	state.State = converted.State
	state.ReadyReplicas = converted.ReadyReplicas
	state.DesiredReplicas = converted.DesiredReplicas
	state.OperationStatus = converted.OperationStatus
	state.OperationID = converted.OperationID
	state.CorrelationID = converted.CorrelationID
	state.ResourceVersion = converted.ResourceVersion
	state.CreatedAt = converted.CreatedAt
	state.UpdatedAt = converted.UpdatedAt
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (d *operationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_operation"
}

func (d *operationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a durable operation by opaque public ID and routes compute, application, and VMM operation IDs to their owning API.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Required: true},
			"correlation_id": detailString("Correlation ID."),
			"resource_id":    detailString("Mutated resource ID."),
			"kind":           detailString("Operation kind."),
			"state":          detailString("Operation state."),
			"error_code":     detailString("Terminal error code."),
			"error_message":  detailString("Terminal error message."),
			"request_id":     detailString("API request ID."),
			"created_at":     detailString("Creation timestamp."),
			"updated_at":     detailString("Last update timestamp."),
		},
	}
}

func (d *operationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state operationDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	operation, err := d.client.GetOperation(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read VAppCloud operation", err.Error())
		return
	}
	state.ID = types.StringValue(operation.ID)
	state.CorrelationID = stringOrNull(operation.CorrelationID)
	state.ResourceID = stringOrNull(operation.ResourceID)
	state.Kind = stringOrNull(operation.Kind)
	state.State = stringOrNull(operation.State)
	state.CreatedAt = formatTime(operation.CreatedAt)
	state.UpdatedAt = formatTime(operation.UpdatedAt)
	state.RequestID = stringOrNull(operation.RequestID)
	if operation.Error == nil {
		state.ErrorCode = types.StringNull()
		state.ErrorMessage = types.StringNull()
	} else {
		state.ErrorCode = stringOrNull(operation.Error.Code)
		state.ErrorMessage = stringOrNull(operation.Error.Message)
		if state.RequestID.IsNull() {
			state.RequestID = stringOrNull(operation.Error.RequestID)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

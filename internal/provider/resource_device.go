package provider

import (
	"context"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type deviceResource struct{ resourceBase }

type deviceResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ProjectID       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	State           types.String `tfsdk:"state"`
	DefaultVMMID    types.String `tfsdk:"default_vmm_id"`
	ResourceVersion types.Int64  `tfsdk:"resource_version"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func NewDeviceResource() resource.Resource { return &deviceResource{} }

func (r *deviceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device"
}

func (r *deviceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		MarkdownDescription: "A logical VAppCloud device created in pending enrollment state. Compute may later attach to it.",
		Attributes: withCommon(map[string]schema.Attribute{
			"project_id":     immutableString("Owning project ID."),
			"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Device name."},
			"state":          computedString("Enrollment and connection state."),
			"default_vmm_id": computedString("System-managed default VMM ID, populated after the agent becomes healthy."),
		}),
	}
}

func (r *deviceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.resourceBase.Configure(ctx, req, resp)
}

func (r *deviceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan deviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Device]
	err := r.client.Do(ctx, http.MethodPost, "/v1/devices", map[string]any{
		"project_id": plan.ProjectID.ValueString(),
		"name":       plan.Name.ValueString(),
	}, &result, client.IdempotencyKey())
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	waitMutation(ctx, r.client, result.Operation, result.OperationID, &resp.Diagnostics)
	deviceToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *deviceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state deviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var device client.Device
	if !readResource(ctx, r.client, "/v1/devices/"+client.Escape(state.ID.ValueString()), &device, &resp.State, &resp.Diagnostics) {
		return
	}
	deviceToState(device, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *deviceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan deviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Device]
	err := r.client.Do(ctx, http.MethodPatch, "/v1/devices/"+client.Escape(plan.ID.ValueString()), map[string]any{
		"name":             plan.Name.ValueString(),
		"resource_version": plan.ResourceVersion.ValueInt64(),
	}, &result, client.IdempotencyKey())
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	waitMutation(ctx, r.client, result.Operation, result.OperationID, &resp.Diagnostics)
	deviceToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *deviceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state deviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Device]
	endpoint := "/v1/devices/" + client.Escape(state.ID.ValueString()) +
		"?resource_version=" + strconv.FormatInt(state.ResourceVersion.ValueInt64(), 10)
	err := r.client.Do(ctx, http.MethodDelete, endpoint, nil, &result, client.IdempotencyKey())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "delete", err)
		return
	}
	waitMutation(ctx, r.client, result.Operation, result.OperationID, &resp.Diagnostics)
}

func (r *deviceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func deviceToState(device client.Device, state *deviceResourceModel) {
	state.ID = types.StringValue(device.ID)
	state.ProjectID = types.StringValue(device.ProjectID)
	state.Name = types.StringValue(device.Name)
	state.State = types.StringValue(device.State)
	if device.DefaultVMMID == "" {
		state.DefaultVMMID = types.StringNull()
	} else {
		state.DefaultVMMID = types.StringValue(device.DefaultVMMID)
	}
	state.ResourceVersion = types.Int64Value(device.ResourceVersion)
	state.CreatedAt = formatTime(device.CreatedAt)
	state.UpdatedAt = formatTime(device.UpdatedAt)
}

var (
	_ resource.Resource                = &deviceResource{}
	_ resource.ResourceWithConfigure   = &deviceResource{}
	_ resource.ResourceWithImportState = &deviceResource{}
)

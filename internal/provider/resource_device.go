package provider

import (
	"context"
	"errors"
	"fmt"
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
	ID              types.String      `tfsdk:"id"`
	ProjectID       types.String      `tfsdk:"project_id"`
	Name            types.String      `tfsdk:"name"`
	State           types.String      `tfsdk:"state"`
	DefaultVMMID    types.String      `tfsdk:"default_vmm_id"`
	ResourceVersion types.Int64       `tfsdk:"resource_version"`
	CreatedAt       types.String      `tfsdk:"created_at"`
	UpdatedAt       types.String      `tfsdk:"updated_at"`
	Timeouts        operationTimeouts `tfsdk:"timeouts"`
}

func NewDeviceResource() resource.Resource { return &deviceResource{} }

func (r *deviceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device"
}

func (r *deviceResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		MarkdownDescription: "A logical VAppCloud device created in pending enrollment state. Compute may later attach to it.",
		Attributes: withCommon(map[string]schema.Attribute{
			"project_id":     immutableString("Owning project ID."),
			"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Device name."},
			"state":          computedString("Enrollment and connection state."),
			"default_vmm_id": computedString("System-managed default VMM ID, populated after the agent becomes healthy."),
			"timeouts":       timeoutAttributes(ctx, deviceOperationTimeout),
		}),
	}
}

func (r *deviceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan deviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := createTimeout(ctx, plan.Timeouts, deviceOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{
		"project_id": plan.ProjectID.ValueString(),
		"name":       plan.Name.ValueString(),
	}
	key := mutationKey(&resp.Diagnostics, "vappcloud_device.create", "", payload)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Device]
	err := r.client.Do(ctx, http.MethodPost, "/v1/devices", payload, &result, key)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(device client.Device) string { return device.ID },
		func(id string) string { return "/v1/devices/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
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
	var state deviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := updateTimeout(ctx, plan.Timeouts, deviceOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Device]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{"name": plan.Name.ValueString(), "resource_version": version}
		key := mutationKey(&resp.Diagnostics, "vappcloud_device.update", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		return r.client.Do(ctx, http.MethodPatch, "/v1/devices/"+client.Escape(id), payload, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.Device
		err := r.client.Do(ctx, http.MethodGet, "/v1/devices/"+client.Escape(id), nil, &current, "")
		return current.ResourceVersion, err
	}, mutate)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(device client.Device) string { return device.ID },
		func(id string) string { return "/v1/devices/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	deviceToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *deviceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state deviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := deleteTimeout(ctx, state.Timeouts, deviceOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Device]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{"resource_version": version}
		key := mutationKey(&resp.Diagnostics, "vappcloud_device.delete", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		endpoint := "/v1/devices/" + client.Escape(id) +
			"?resource_version=" + strconv.FormatInt(version.Int64(), 10)
		return r.client.Do(ctx, http.MethodDelete, endpoint, nil, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.Device
		err := r.client.Do(ctx, http.MethodGet, "/v1/devices/"+client.Escape(id), nil, &current, "")
		return current.ResourceVersion, err
	}, mutate)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "delete", err)
		return
	}
	_, _ = waitMutation(ctx, r.client, result.Operation, result.OperationID, timeout, &resp.Diagnostics)
}

func (r *deviceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	projectID, deviceID, ok := compositeImportID(req.ID, "device", &resp.Diagnostics)
	if !ok {
		return
	}
	var device client.Device
	if err := r.client.Do(ctx, http.MethodGet, "/v1/devices/"+client.Escape(deviceID), nil, &device, ""); err != nil {
		resp.Diagnostics.AddError("Unable to import device", err.Error())
		return
	}
	if device.ProjectID != projectID {
		resp.Diagnostics.AddError(
			"Device project mismatch",
			fmt.Sprintf("Device %s belongs to project %s, not %s.", deviceID, device.ProjectID, projectID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), deviceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), projectID)...)
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
	state.ResourceVersion = types.Int64Value(device.ResourceVersion.Int64())
	state.CreatedAt = formatTime(device.CreatedAt)
	state.UpdatedAt = formatTime(device.UpdatedAt)
}

var (
	_ resource.Resource                = &deviceResource{}
	_ resource.ResourceWithConfigure   = &deviceResource{}
	_ resource.ResourceWithImportState = &deviceResource{}
)

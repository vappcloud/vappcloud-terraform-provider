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

type computeInstanceResource struct{ resourceBase }

type computeInstanceResourceModel struct {
	ID                types.String      `tfsdk:"id"`
	ProjectID         types.String      `tfsdk:"project_id"`
	DeviceID          types.String      `tfsdk:"device_id"`
	DefaultVMMID      types.String      `tfsdk:"default_vmm_id"`
	CloudConnectionID types.String      `tfsdk:"cloud_connection_id"`
	Region            types.String      `tfsdk:"region"`
	Size              types.String      `tfsdk:"size"`
	Image             types.String      `tfsdk:"image"`
	Name              types.String      `tfsdk:"name"`
	State             types.String      `tfsdk:"state"`
	ResourceVersion   types.Int64       `tfsdk:"resource_version"`
	CreatedAt         types.String      `tfsdk:"created_at"`
	UpdatedAt         types.String      `tfsdk:"updated_at"`
	Timeouts          operationTimeouts `tfsdk:"timeouts"`
}

func NewComputeInstanceResource() resource.Resource { return &computeInstanceResource{} }

func (r *computeInstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute_instance"
}

func (r *computeInstanceResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		MarkdownDescription: "A cloud compute instance attached to a pre-created VAppCloud device. Enrollment bootstrap material is injected server-side and never returned to Terraform.",
		Attributes: withCommon(map[string]schema.Attribute{
			"project_id":          immutableString("Owning project ID."),
			"device_id":           immutableString("Pre-created logical device ID."),
			"cloud_connection_id": immutableString("Preconfigured cloud connection ID."),
			"region":              immutableString("Cloud region slug."),
			"size":                immutableString("Cloud size slug."),
			"image":               immutableString("Cloud image slug or ID."),
			"name":                schema.StringAttribute{Required: true, MarkdownDescription: "Compute instance name."},
			"state":               computedString("Provisioning/readiness state."),
			"default_vmm_id":      computedString("Default VMM established by the connected agent."),
			"timeouts":            timeoutAttributes(ctx, computeOperationTimeout),
		}),
	}
}

func (r *computeInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan computeInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := createTimeout(ctx, plan.Timeouts, computeOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{
		"project_id":          plan.ProjectID.ValueString(),
		"device_id":           plan.DeviceID.ValueString(),
		"cloud_connection_id": plan.CloudConnectionID.ValueString(),
		"region":              plan.Region.ValueString(),
		"size":                plan.Size.ValueString(),
		"image":               plan.Image.ValueString(),
		"name":                plan.Name.ValueString(),
	}
	key := mutationKey(&resp.Diagnostics, "vappcloud_compute_instance.create", "", payload)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.ComputeInstance]
	err := r.client.Do(ctx, http.MethodPost, "/v1/compute-instances", payload, &result, key)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(compute client.ComputeInstance) string { return compute.ID },
		func(id string) string { return "/v1/compute-instances/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	computeToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *computeInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state computeInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var compute client.ComputeInstance
	if !readResource(ctx, r.client, "/v1/compute-instances/"+client.Escape(state.ID.ValueString()), &compute, &resp.State, &resp.Diagnostics) {
		return
	}
	computeToState(compute, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *computeInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan computeInstanceResourceModel
	var state computeInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := updateTimeout(ctx, plan.Timeouts, computeOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.ComputeInstance]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{"name": plan.Name.ValueString(), "resource_version": version}
		key := mutationKey(&resp.Diagnostics, "vappcloud_compute_instance.update", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		return r.client.Do(ctx, http.MethodPatch, "/v1/compute-instances/"+client.Escape(id), payload, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.ComputeInstance
		err := r.client.Do(ctx, http.MethodGet, "/v1/compute-instances/"+client.Escape(id), nil, &current, "")
		return current.ResourceVersion, err
	}, mutate)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(compute client.ComputeInstance) string { return compute.ID },
		func(id string) string { return "/v1/compute-instances/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	computeToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *computeInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state computeInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := deleteTimeout(ctx, state.Timeouts, computeOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.ComputeInstance]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{"resource_version": version}
		key := mutationKey(&resp.Diagnostics, "vappcloud_compute_instance.delete", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		endpoint := "/v1/compute-instances/" + client.Escape(id) +
			"?resource_version=" + strconv.FormatInt(version.Int64(), 10)
		return r.client.Do(ctx, http.MethodDelete, endpoint, nil, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.ComputeInstance
		err := r.client.Do(ctx, http.MethodGet, "/v1/compute-instances/"+client.Escape(id), nil, &current, "")
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

func (r *computeInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	projectID, computeID, ok := compositeImportID(req.ID, "compute instance", &resp.Diagnostics)
	if !ok {
		return
	}
	var compute client.ComputeInstance
	if err := r.client.Do(ctx, http.MethodGet, "/v1/compute-instances/"+client.Escape(computeID), nil, &compute, ""); err != nil {
		resp.Diagnostics.AddError("Unable to import compute instance", err.Error())
		return
	}
	if compute.ProjectID != projectID {
		resp.Diagnostics.AddError(
			"Compute instance project mismatch",
			fmt.Sprintf("Compute instance %s belongs to project %s, not %s.", computeID, compute.ProjectID, projectID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), computeID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), projectID)...)
}

func computeToState(compute client.ComputeInstance, state *computeInstanceResourceModel) {
	state.ID = types.StringValue(compute.ID)
	state.ProjectID = types.StringValue(compute.ProjectID)
	state.DeviceID = types.StringValue(compute.DeviceID)
	if compute.DefaultVMMID == "" {
		state.DefaultVMMID = types.StringNull()
	} else {
		state.DefaultVMMID = types.StringValue(compute.DefaultVMMID)
	}
	state.CloudConnectionID = types.StringValue(compute.CloudConnection)
	state.Region = types.StringValue(compute.Region)
	state.Size = types.StringValue(compute.Size)
	state.Image = types.StringValue(compute.Image)
	state.Name = types.StringValue(compute.Name)
	state.State = types.StringValue(compute.State)
	state.ResourceVersion = types.Int64Value(compute.ResourceVersion.Int64())
	state.CreatedAt = formatTime(compute.CreatedAt)
	state.UpdatedAt = formatTime(compute.UpdatedAt)
}

var (
	_ resource.Resource                = &computeInstanceResource{}
	_ resource.ResourceWithConfigure   = &computeInstanceResource{}
	_ resource.ResourceWithImportState = &computeInstanceResource{}
)

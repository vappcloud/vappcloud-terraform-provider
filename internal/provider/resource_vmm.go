package provider

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type vmmResource struct{ resourceBase }

type vmmResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	ProjectID          types.String `tfsdk:"project_id"`
	DeviceID           types.String `tfsdk:"device_id"`
	Name               types.String `tfsdk:"name"`
	CPUCores           types.Int64  `tfsdk:"cpu_cores"`
	MemoryMB           types.Int64  `tfsdk:"memory_mb"`
	DeletionProtection types.Bool   `tfsdk:"deletion_protection"`
	RetainDisk         types.Bool   `tfsdk:"retain_disk"`
	IsDefault          types.Bool   `tfsdk:"is_default"`
	Management         types.String `tfsdk:"management"`
	State              types.String `tfsdk:"state"`
	Health             types.String `tfsdk:"health"`
	DiskMB             types.Int64  `tfsdk:"disk_mb"`
	DesiredRevision    types.Int64  `tfsdk:"desired_revision"`
	ObservedRevision   types.Int64  `tfsdk:"observed_revision"`
	OperationStatus    types.String `tfsdk:"operation_status"`
	OperationID        types.String `tfsdk:"operation_id"`
	CorrelationID      types.String `tfsdk:"correlation_id"`
	ResourceVersion    types.Int64  `tfsdk:"resource_version"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewVMMResource() resource.Resource { return &vmmResource{} }

func (r *vmmResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vmm"
}

func (r *vmmResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	positive := []validator.Int64{int64validator.AtLeast(1)}
	resp.Schema = schema.Schema{
		Version: 1,
		MarkdownDescription: "A Terraform-managed secondary VMM. The system-managed default VMM is read-only and " +
			"is available through VMM data sources; it cannot be created, adopted, imported, or destroyed by this resource.",
		Attributes: withCommon(map[string]schema.Attribute{
			"project_id": immutableString("Immutable owning project ID."),
			"device_id":  immutableString("Immutable host device ID."),
			"name":       schema.StringAttribute{Required: true, MarkdownDescription: "VMM name."},
			"cpu_cores": schema.Int64Attribute{
				Required: true, MarkdownDescription: "Desired virtual CPU cores.", Validators: positive,
			},
			"memory_mb": schema.Int64Attribute{
				Required: true, MarkdownDescription: "Desired memory in MiB.", Validators: positive,
			},
			"deletion_protection": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "API-enforced deletion protection.",
			},
			"retain_disk": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Preserve the root disk as an auditable retained-storage record when the VMM is deleted.",
			},
			"is_default":        schema.BoolAttribute{Computed: true, MarkdownDescription: "Always false for managed resources."},
			"management":        computedString("Management owner: terraform, console, external, or system."),
			"state":             computedString("Runtime state."),
			"health":            computedString("Agent-reported health."),
			"disk_mb":           schema.Int64Attribute{Computed: true, MarkdownDescription: "Root disk capacity in MiB."},
			"desired_revision":  schema.Int64Attribute{Computed: true, MarkdownDescription: "Desired VMM specification revision."},
			"observed_revision": schema.Int64Attribute{Computed: true, MarkdownDescription: "Latest agent-observed revision."},
			"operation_status":  computedString("Latest asynchronous operation state."),
			"operation_id":      computedString("Latest asynchronous operation ID."),
			"correlation_id":    computedString("Latest operation correlation ID."),
		}),
	}
}

func (r *vmmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmmResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.VMM]
	err := r.client.Do(ctx, http.MethodPost, "/v1/vmms", map[string]any{
		"project_id":          plan.ProjectID.ValueString(),
		"device_id":           plan.DeviceID.ValueString(),
		"name":                plan.Name.ValueString(),
		"cpu_cores":           plan.CPUCores.ValueInt64(),
		"memory_mb":           plan.MemoryMB.ValueInt64(),
		"deletion_protection": plan.DeletionProtection.ValueBool(),
		"retain_disk":         plan.RetainDisk.ValueBool(),
		"terraform_owned":     true,
	}, &result, client.IdempotencyKey())
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	if result.Resource.IsDefault {
		resp.Diagnostics.AddError("Default VMM cannot be managed", "The API returned a system-managed default VMM for a create request.")
		return
	}
	waitMutation(ctx, r.client, result.Operation, result.OperationID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if result.Resource.ID == "" {
		var current client.VMM
		resourceID := result.Operation.ResourceID
		if err := r.client.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(resourceID), nil, &current, ""); err != nil {
			addMutationDiagnostic(&resp.Diagnostics, "read created", err)
			return
		}
		result.Resource = current
	}
	vmmToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var vmm client.VMM
	if !readResource(ctx, r.client, "/v1/vmms/"+client.Escape(state.ID.ValueString()), &vmm, &resp.State, &resp.Diagnostics) {
		return
	}
	if vmm.IsDefault {
		resp.Diagnostics.AddError("Default VMM cannot be managed", "This state points to a system-managed default VMM. Remove it from managed state and use data.vappcloud_vmm instead.")
		return
	}
	vmmToState(vmm, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vmmResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.VMM]
	err := r.client.Do(ctx, http.MethodPatch, "/v1/vmms/"+client.Escape(plan.ID.ValueString()), map[string]any{
		"name":                plan.Name.ValueString(),
		"cpu_cores":           plan.CPUCores.ValueInt64(),
		"memory_mb":           plan.MemoryMB.ValueInt64(),
		"deletion_protection": plan.DeletionProtection.ValueBool(),
		"retain_disk":         plan.RetainDisk.ValueBool(),
		"resource_version":    plan.ResourceVersion.ValueInt64(),
	}, &result, client.IdempotencyKey())
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	waitMutation(ctx, r.client, result.Operation, result.OperationID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if result.Resource.ID == "" {
		if err := r.client.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(plan.ID.ValueString()), nil, &result.Resource, ""); err != nil {
			addMutationDiagnostic(&resp.Diagnostics, "read updated", err)
			return
		}
	}
	vmmToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.IsDefault.ValueBool() {
		resp.Diagnostics.AddError("Default VMM cannot be destroyed", "Default VMM lifecycle is bound to its device.")
		return
	}
	var result client.Mutation[client.VMM]
	endpoint := "/v1/vmms/" + client.Escape(state.ID.ValueString()) +
		"?resource_version=" + strconv.FormatInt(state.ResourceVersion.ValueInt64(), 10) +
		"&retain_disk=" + strconv.FormatBool(state.RetainDisk.ValueBool())
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

func (r *vmmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid VMM import identifier", "Expected <project_id>/<vmm_id>.")
		return
	}
	var vmm client.VMM
	if err := r.client.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(parts[1]), nil, &vmm, ""); err != nil {
		resp.Diagnostics.AddError("Unable to import VMM", err.Error())
		return
	}
	if vmm.IsDefault {
		resp.Diagnostics.AddError("Default VMM cannot be imported", "System-managed default VMMs are read-only. Use data.vappcloud_vmm.")
		return
	}
	if vmm.ProjectID != parts[0] {
		resp.Diagnostics.AddError("VMM project mismatch", fmt.Sprintf("VMM %s belongs to project %s, not %s.", parts[1], vmm.ProjectID, parts[0]))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
}

func vmmToState(vmm client.VMM, state *vmmResourceModel) {
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
	state.DesiredRevision = types.Int64Value(vmm.DesiredRevision)
	state.ObservedRevision = types.Int64Value(vmm.ObservedRevision)
	state.ResourceVersion = types.Int64Value(vmm.ResourceVersion)
	state.OperationStatus = types.StringValue(vmm.Operation.State)
	state.OperationID = types.StringValue(vmm.Operation.ID)
	state.CorrelationID = types.StringValue(vmm.Operation.CorrelationID)
	state.CreatedAt = formatTime(vmm.CreatedAt)
	state.UpdatedAt = formatTime(vmm.UpdatedAt)
}

var (
	_ resource.Resource                = &vmmResource{}
	_ resource.ResourceWithConfigure   = &vmmResource{}
	_ resource.ResourceWithImportState = &vmmResource{}
)

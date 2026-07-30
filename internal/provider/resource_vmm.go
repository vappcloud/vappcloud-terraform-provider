package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
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
	ID                 types.String      `tfsdk:"id"`
	ProjectID          types.String      `tfsdk:"project_id"`
	DeviceID           types.String      `tfsdk:"device_id"`
	Name               types.String      `tfsdk:"name"`
	CPUCores           types.Int64       `tfsdk:"cpu_cores"`
	MemoryMB           types.Int64       `tfsdk:"memory_mb"`
	DeletionProtection types.Bool        `tfsdk:"deletion_protection"`
	RetainDisk         types.Bool        `tfsdk:"retain_disk"`
	IsDefault          types.Bool        `tfsdk:"is_default"`
	Management         types.String      `tfsdk:"management"`
	State              types.String      `tfsdk:"state"`
	Health             types.String      `tfsdk:"health"`
	DiskMB             types.Int64       `tfsdk:"disk_mb"`
	DesiredRevision    types.Int64       `tfsdk:"desired_revision"`
	ObservedRevision   types.Int64       `tfsdk:"observed_revision"`
	OperationStatus    types.String      `tfsdk:"operation_status"`
	OperationID        types.String      `tfsdk:"operation_id"`
	CorrelationID      types.String      `tfsdk:"correlation_id"`
	ResourceVersion    types.Int64       `tfsdk:"resource_version"`
	CreatedAt          timetypes.RFC3339 `tfsdk:"created_at"`
	UpdatedAt          timetypes.RFC3339 `tfsdk:"updated_at"`
	Timeouts           operationTimeouts `tfsdk:"timeouts"`
}

func NewVMMResource() resource.Resource { return &vmmResource{} }

func (r *vmmResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vmm"
}

func (r *vmmResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	positive := []validator.Int64{int64validator.AtLeast(1)}
	resp.Schema = schema.Schema{
		Version: 0,
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
				MarkdownDescription: "Delete policy persisted by the API. When true, the root disk is preserved as auditable retained storage on a future destroy; changing it does not alter a running VMM.",
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
			"timeouts":          timeoutAttributes(ctx, operationTimeout),
		}),
	}
}

func (r *vmmResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identitySchema(true)
}

func (r *vmmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmmResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := createTimeout(ctx, plan.Timeouts, operationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{
		"project_id":          plan.ProjectID.ValueString(),
		"device_id":           plan.DeviceID.ValueString(),
		"name":                plan.Name.ValueString(),
		"cpu_cores":           plan.CPUCores.ValueInt64(),
		"memory_mb":           plan.MemoryMB.ValueInt64(),
		"deletion_protection": plan.DeletionProtection.ValueBool(),
		"retain_disk":         plan.RetainDisk.ValueBool(),
		"terraform_owned":     true,
	}
	key := createMutationKey(&resp.Diagnostics, "vappcloud_vmm.create")
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.VMM]
	err := r.client.Do(ctx, http.MethodPost, "/v1/vmms", payload, &result, key)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(vmm client.VMM) string { return vmm.ID },
		func(id string) string { return "/v1/vmms/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	if result.Resource.IsDefault {
		resp.Diagnostics.AddError("Default VMM cannot be managed", "The API returned a system-managed default VMM for a create request.")
		return
	}
	vmmToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), plan.ProjectID.ValueString(), &resp.Diagnostics)
}

func (r *vmmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	setResourceIdentity(ctx, resp.Identity, state.ID.ValueString(), state.ProjectID.ValueString(), &resp.Diagnostics)
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
	setResourceIdentity(ctx, resp.Identity, state.ID.ValueString(), state.ProjectID.ValueString(), &resp.Diagnostics)
}

func (r *vmmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vmmResourceModel
	var state vmmResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := updateTimeout(ctx, plan.Timeouts, operationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.VMM]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{
			"name":                plan.Name.ValueString(),
			"cpu_cores":           plan.CPUCores.ValueInt64(),
			"memory_mb":           plan.MemoryMB.ValueInt64(),
			"deletion_protection": plan.DeletionProtection.ValueBool(),
			"retain_disk":         plan.RetainDisk.ValueBool(),
			"resource_version":    version,
		}
		key := mutationKey(&resp.Diagnostics, "vappcloud_vmm.update", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		return r.client.Do(ctx, http.MethodPatch, "/v1/vmms/"+client.Escape(id), payload, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.VMM
		err := r.client.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(id), nil, &current, "")
		return current.ResourceVersion, err
	}, mutate)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(vmm client.VMM) string { return vmm.ID },
		func(id string) string { return "/v1/vmms/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	vmmToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), plan.ProjectID.ValueString(), &resp.Diagnostics)
}

func (r *vmmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := deleteTimeout(ctx, state.Timeouts, operationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.IsDefault.ValueBool() {
		resp.Diagnostics.AddError("Default VMM cannot be destroyed", "Default VMM lifecycle is bound to its device.")
		return
	}
	var result client.Mutation[client.VMM]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{"resource_version": version, "retain_disk": state.RetainDisk.ValueBool()}
		key := mutationKey(&resp.Diagnostics, "vappcloud_vmm.delete", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		endpoint := "/v1/vmms/" + client.Escape(id) +
			"?resource_version=" + strconv.FormatInt(version.Int64(), 10) +
			"&retain_disk=" + strconv.FormatBool(state.RetainDisk.ValueBool())
		return r.client.Do(ctx, http.MethodDelete, endpoint, nil, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.VMM
		err := r.client.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(id), nil, &current, "")
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

func (r *vmmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		importCompositeIdentity(ctx, req, resp)
		return
	}
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
	state.DesiredRevision = types.Int64Value(vmm.DesiredRevision.Int64())
	state.ObservedRevision = types.Int64Value(vmm.ObservedRevision.Int64())
	state.ResourceVersion = types.Int64Value(vmm.ResourceVersion.Int64())
	state.OperationStatus = types.StringValue(vmm.Operation.State)
	state.OperationID = types.StringValue(vmm.Operation.ID)
	state.CorrelationID = types.StringValue(vmm.Operation.CorrelationID)
	state.CreatedAt = formatRFC3339(vmm.CreatedAt)
	state.UpdatedAt = formatRFC3339(vmm.UpdatedAt)
}

var (
	_ resource.Resource                = &vmmResource{}
	_ resource.ResourceWithConfigure   = &vmmResource{}
	_ resource.ResourceWithIdentity    = &vmmResource{}
	_ resource.ResourceWithImportState = &vmmResource{}
)

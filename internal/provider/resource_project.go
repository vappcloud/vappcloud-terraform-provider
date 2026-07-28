package provider

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

var (
	_ resource.Resource                = &projectResource{}
	_ resource.ResourceWithConfigure   = &projectResource{}
	_ resource.ResourceWithImportState = &projectResource{}
)

type projectResource struct {
	resourceBase
}

type projectResourceModel struct {
	ID              types.String      `tfsdk:"id"`
	Name            types.String      `tfsdk:"name"`
	Description     types.String      `tfsdk:"description"`
	ResourceVersion types.Int64       `tfsdk:"resource_version"`
	CreatedAt       types.String      `tfsdk:"created_at"`
	UpdatedAt       types.String      `tfsdk:"updated_at"`
	Timeouts        operationTimeouts `tfsdk:"timeouts"`
}

func NewProjectResource() resource.Resource {
	return &projectResource{}
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		MarkdownDescription: "A VAppCloud project. Projects are the tenant boundary for devices, VMMs, compute, and applications.",
		Attributes: withCommon(map[string]schema.Attribute{
			"name":        schema.StringAttribute{Required: true, MarkdownDescription: "Project name."},
			"description": schema.StringAttribute{Optional: true, MarkdownDescription: "Project description."},
			"timeouts":    timeoutAttributes(ctx, projectOperationTimeout),
		}),
	}
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := createTimeout(ctx, plan.Timeouts, projectOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{"name": plan.Name.ValueString(), "description": plan.Description.ValueString()}
	key := mutationKey(&resp.Diagnostics, "vappcloud_project.create", "", payload)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Project]
	err := r.client.Do(ctx, http.MethodPost, "/v1/projects", payload, &result, key)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(project client.Project) string { return project.ID },
		func(id string) string { return "/v1/projects/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	projectToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var project client.Project
	if !readResource(ctx, r.client, "/v1/projects/"+client.Escape(state.ID.ValueString()), &project, &resp.State, &resp.Diagnostics) {
		return
	}
	projectToState(project, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectResourceModel
	var state projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := updateTimeout(ctx, plan.Timeouts, projectOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Project]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{
			"name":             plan.Name.ValueString(),
			"description":      plan.Description.ValueString(),
			"resource_version": version,
		}
		key := mutationKey(&resp.Diagnostics, "vappcloud_project.update", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		return r.client.Do(ctx, http.MethodPatch, "/v1/projects/"+client.Escape(id), payload, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.Project
		err := r.client.Do(ctx, http.MethodGet, "/v1/projects/"+client.Escape(id), nil, &current, "")
		return current.ResourceVersion, err
	}, mutate)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(project client.Project) string { return project.ID },
		func(id string) string { return "/v1/projects/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	projectToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := deleteTimeout(ctx, state.Timeouts, projectOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Project]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{"resource_version": version}
		key := mutationKey(&resp.Diagnostics, "vappcloud_project.delete", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		endpoint := "/v1/projects/" + client.Escape(id) +
			"?resource_version=" + strconv.FormatInt(version.Int64(), 10)
		return r.client.Do(ctx, http.MethodDelete, endpoint, nil, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.Project
		err := r.client.Do(ctx, http.MethodGet, "/v1/projects/"+client.Escape(id), nil, &current, "")
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

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func projectToState(project client.Project, state *projectResourceModel) {
	state.ID = types.StringValue(project.ID)
	state.Name = types.StringValue(project.Name)
	if state.Description.IsNull() && project.Description == "" {
		state.Description = types.StringNull()
	} else {
		state.Description = types.StringValue(project.Description)
	}
	state.ResourceVersion = types.Int64Value(project.ResourceVersion.Int64())
	state.CreatedAt = formatTime(project.CreatedAt)
	state.UpdatedAt = formatTime(project.UpdatedAt)
}

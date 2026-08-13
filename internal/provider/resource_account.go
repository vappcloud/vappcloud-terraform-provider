package provider

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

var (
	_ resource.Resource                = &accountResource{}
	_ resource.ResourceWithConfigure   = &accountResource{}
	_ resource.ResourceWithIdentity    = &accountResource{}
	_ resource.ResourceWithImportState = &accountResource{}
)

type accountResource struct {
	resourceBase
}

type accountResourceModel struct {
	ID              types.String      `tfsdk:"id"`
	Name            types.String      `tfsdk:"name"`
	Description     types.String      `tfsdk:"description"`
	ResourceVersion types.Int64       `tfsdk:"resource_version"`
	CreatedAt       timetypes.RFC3339 `tfsdk:"created_at"`
	UpdatedAt       timetypes.RFC3339 `tfsdk:"updated_at"`
	Timeouts        operationTimeouts `tfsdk:"timeouts"`
}

func NewAccountResource() resource.Resource {
	return &accountResource{}
}

func (r *accountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (r *accountResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             0,
		MarkdownDescription: "A VAppCloud account. Accounts are the tenant boundary for devices, VMMs, compute, and applications.",
		Attributes: withCommon(map[string]schema.Attribute{
			"name":        schema.StringAttribute{Required: true, MarkdownDescription: "Account name."},
			"description": schema.StringAttribute{Optional: true, MarkdownDescription: "Account description."},
			"timeouts":    timeoutAttributes(ctx, accountOperationTimeout),
		}),
	}
}

func (r *accountResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identitySchema(false)
}

func (r *accountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan accountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := createTimeout(ctx, plan.Timeouts, accountOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{"name": plan.Name.ValueString(), "description": plan.Description.ValueString()}
	key := createMutationKey(&resp.Diagnostics, "vappcloud_account.create")
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Account]
	err := r.client.Do(ctx, http.MethodPost, "/v1/accounts", payload, &result, key)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(account client.Account) string { return account.ID },
		func(id string) string { return "/v1/accounts/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	accountToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *accountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state accountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	setResourceIdentity(ctx, resp.Identity, state.ID.ValueString(), "", &resp.Diagnostics)
	var account client.Account
	if !readResource(ctx, r.client, "/v1/accounts/"+client.Escape(state.ID.ValueString()), &account, &resp.State, &resp.Diagnostics) {
		return
	}
	accountToState(account, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setResourceIdentity(ctx, resp.Identity, state.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *accountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan accountResourceModel
	var state accountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := updateTimeout(ctx, plan.Timeouts, accountOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Account]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{
			"name":             plan.Name.ValueString(),
			"description":      plan.Description.ValueString(),
			"resource_version": version,
		}
		key := mutationKey(&resp.Diagnostics, "vappcloud_account.update", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		return r.client.Do(ctx, http.MethodPatch, "/v1/accounts/"+client.Escape(id), payload, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.Account
		err := r.client.Do(ctx, http.MethodGet, "/v1/accounts/"+client.Escape(id), nil, &current, "")
		return current.ResourceVersion, err
	}, mutate)
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	if !completeMutation(ctx, r.client, &result, timeout,
		func(account client.Account) string { return account.ID },
		func(id string) string { return "/v1/accounts/" + client.Escape(id) },
		&resp.Diagnostics,
	) {
		return
	}
	accountToState(result.Resource, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *accountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state accountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout := deleteTimeout(ctx, state.Timeouts, accountOperationTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.Account]
	id := state.ID.ValueString()
	mutate := func(version client.Version) error {
		payload := map[string]any{"resource_version": version}
		key := mutationKey(&resp.Diagnostics, "vappcloud_account.delete", id, payload)
		if resp.Diagnostics.HasError() {
			return errors.New("unable to derive idempotency key")
		}
		endpoint := "/v1/accounts/" + client.Escape(id) +
			"?resource_version=" + strconv.FormatInt(version.Int64(), 10)
		return r.client.Do(ctx, http.MethodDelete, endpoint, nil, &result, key)
	}
	err := mutateWithVersionRetry(client.Version(state.ResourceVersion.ValueInt64()), func() (client.Version, error) {
		var current client.Account
		err := r.client.Do(ctx, http.MethodGet, "/v1/accounts/"+client.Escape(id), nil, &current, "")
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

func (r *accountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("id"), path.Root("id"), req, resp)
}

func accountToState(account client.Account, state *accountResourceModel) {
	state.ID = types.StringValue(account.ID)
	state.Name = types.StringValue(account.Name)
	if state.Description.IsNull() && account.Description == "" {
		state.Description = types.StringNull()
	} else {
		state.Description = types.StringValue(account.Description)
	}
	state.ResourceVersion = types.Int64Value(account.ResourceVersion.Int64())
	state.CreatedAt = formatRFC3339(account.CreatedAt)
	state.UpdatedAt = formatRFC3339(account.UpdatedAt)
}

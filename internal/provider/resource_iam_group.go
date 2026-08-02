package provider

import (
	"context"
	"net/http"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type iamGroupResource struct{ resourceBase }

type iamGroupResourceModel struct {
	ID          types.String      `tfsdk:"id"`
	Name        types.String      `tfsdk:"name"`
	ARN         types.String      `tfsdk:"arn"`
	MemberIDs   types.Set         `tfsdk:"member_ids"`
	MemberCount types.Int64       `tfsdk:"member_count"`
	CreatedAt   timetypes.RFC3339 `tfsdk:"created_at"`
	UpdatedAt   timetypes.RFC3339 `tfsdk:"updated_at"`
}

func NewIAMGroupResource() resource.Resource { return &iamGroupResource{} }

func (r *iamGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_group"
}

func (r *iamGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             0,
		MarkdownDescription: "An IAM group and its complete user/service-account membership set.",
		Attributes: map[string]schema.Attribute{
			"id":   computedID(),
			"name": immutableString("Group name, unique within the organization."),
			"arn":  computedString("Group ARN."),
			"member_ids": schema.SetAttribute{
				Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Complete set of user or service-account principal IDs in the group.",
			},
			"member_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "Current group member count."},
			"created_at":   computedRFC3339("Creation timestamp in RFC3339 format.", true),
			"updated_at":   computedRFC3339("Last update timestamp in RFC3339 format.", false),
		},
	}
}

func (r *iamGroupResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identitySchema(false)
}

func (r *iamGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{"name": plan.Name.ValueString()}
	key := createMutationKey(&resp.Diagnostics, "vappcloud_iam_group.create")
	if resp.Diagnostics.HasError() {
		return
	}
	var group client.IAMGroup
	if err := r.client.Do(ctx, http.MethodPost, "/v1/iam/groups", payload, &group, key); err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create IAM group", err)
		return
	}
	desired := groupMembersFromState(ctx, plan.MemberIDs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.syncMembers(ctx, group.ID, nil, desired); err != nil {
		cleanupKey := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_group.rollback", group.ID, nil)
		if cleanupKey != "" {
			_ = r.client.Do(ctx, http.MethodDelete, "/v1/iam/groups/"+client.Escape(group.ID), nil, nil, cleanupKey)
		}
		resp.Diagnostics.AddError("Unable to create IAM group membership", err.Error())
		return
	}
	group.MemberCount = int64(len(desired))
	iamGroupToState(ctx, group, desired, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, group.ID, "", &resp.Diagnostics)
}

func (r *iamGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	groups, err := readIAMGroups(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read IAM groups", err.Error())
		return
	}
	var group client.IAMGroup
	found := false
	for _, item := range groups {
		if item.ID == state.ID.ValueString() {
			group = item
			found = true
			break
		}
	}
	if !found {
		removeIAMState(ctx, &resp.State)
		return
	}
	members, err := readIAMGroupMembers(ctx, r.client, group.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read IAM group members", err.Error())
		return
	}
	iamGroupToState(ctx, group, members, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setResourceIdentity(ctx, resp.Identity, group.ID, "", &resp.Diagnostics)
}

func (r *iamGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan iamGroupResourceModel
	var state iamGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current := groupMembersFromState(ctx, state.MemberIDs, &resp.Diagnostics)
	desired := groupMembersFromState(ctx, plan.MemberIDs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.syncMembers(ctx, state.ID.ValueString(), current, desired); err != nil {
		resp.Diagnostics.AddError("Unable to update IAM group membership", err.Error())
		return
	}
	plan.ID = state.ID
	plan.ARN = state.ARN
	plan.MemberCount = types.Int64Value(int64(len(desired)))
	plan.CreatedAt = state.CreatedAt
	plan.UpdatedAt = state.UpdatedAt
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *iamGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iamGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_group.delete", state.ID.ValueString(), nil)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, http.MethodDelete, "/v1/iam/groups/"+client.Escape(state.ID.ValueString()), nil, nil, key)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "delete IAM group", err)
	}
}

func (r *iamGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("id"), path.Root("id"), req, resp)
}

func (r *iamGroupResource) syncMembers(
	ctx context.Context,
	groupID string,
	current []string,
	desired []string,
) error {
	currentSet := make(map[string]struct{}, len(current))
	desiredSet := make(map[string]struct{}, len(desired))
	for _, id := range current {
		currentSet[id] = struct{}{}
	}
	for _, id := range desired {
		desiredSet[id] = struct{}{}
	}
	for _, principalID := range desired {
		if _, exists := currentSet[principalID]; exists {
			continue
		}
		payload := map[string]any{"group_id": groupID, "principal_id": principalID}
		key, err := client.StableIdempotencyKey("vappcloud_iam_group.add_member", groupID+"/"+principalID, payload)
		if err != nil {
			return err
		}
		endpoint := "/v1/iam/groups/" + client.Escape(groupID) + "/members/" + client.Escape(principalID)
		if err := r.client.Do(ctx, http.MethodPut, endpoint, payload, nil, key); err != nil {
			return err
		}
	}
	for _, principalID := range current {
		if _, exists := desiredSet[principalID]; exists {
			continue
		}
		payload := map[string]any{"group_id": groupID, "principal_id": principalID}
		key, err := client.StableIdempotencyKey("vappcloud_iam_group.remove_member", groupID+"/"+principalID, payload)
		if err != nil {
			return err
		}
		endpoint := "/v1/iam/groups/" + client.Escape(groupID) + "/members/" + client.Escape(principalID)
		if err := r.client.Do(ctx, http.MethodDelete, endpoint, nil, nil, key); err != nil && !client.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func groupMembersFromState(ctx context.Context, value types.Set, diagnostics *diag.Diagnostics) []string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	var members []string
	diagnostics.Append(value.ElementsAs(ctx, &members, false)...)
	sort.Strings(members)
	return members
}

func iamGroupToState(ctx context.Context, group client.IAMGroup, members []string, state *iamGroupResourceModel, diagnostics *diag.Diagnostics) {
	state.ID = types.StringValue(group.ID)
	state.Name = types.StringValue(group.Name)
	state.ARN = types.StringValue(group.ARN)
	if state.MemberIDs.IsNull() && len(members) == 0 {
		state.MemberIDs = types.SetNull(types.StringType)
	} else {
		state.MemberIDs = stringSet(ctx, members, diagnostics)
	}
	state.MemberCount = types.Int64Value(int64(len(members)))
	state.CreatedAt = formatRFC3339(group.CreatedAt)
	state.UpdatedAt = formatRFC3339(group.UpdatedAt)
}

var (
	_ resource.Resource                = &iamGroupResource{}
	_ resource.ResourceWithConfigure   = &iamGroupResource{}
	_ resource.ResourceWithIdentity    = &iamGroupResource{}
	_ resource.ResourceWithImportState = &iamGroupResource{}
)

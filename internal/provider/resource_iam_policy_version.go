package provider

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type iamPolicyVersionResource struct{ resourceBase }

type iamPolicyVersionResourceModel struct {
	ID           types.String      `tfsdk:"id"`
	PolicyID     types.String      `tfsdk:"policy_id"`
	Version      types.String      `tfsdk:"version"`
	Document     types.String      `tfsdk:"document"`
	SetAsDefault types.Bool        `tfsdk:"set_as_default"`
	IsDefault    types.Bool        `tfsdk:"is_default"`
	CreatedAt    timetypes.RFC3339 `tfsdk:"created_at"`
}

func NewIAMPolicyVersionResource() resource.Resource { return &iamPolicyVersionResource{} }

func (r *iamPolicyVersionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_policy_version"
}

func (r *iamPolicyVersionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Version:             0,
		MarkdownDescription: "An immutable version of a customer-managed IAM policy. A policy may retain at most five versions.",
		Attributes: map[string]schema.Attribute{
			"id":        computedID(),
			"policy_id": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Owning customer-managed policy ID."},
			"version":   immutableComputedString("Server-assigned immutable version such as `v2`."),
			"document": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "IAM policy JSON using Version `2026-08-01`. Changing it replaces this immutable version.",
				PlanModifiers:       []planmodifier.String{normalizedJSONPlanModifier{}, stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{iamPolicyDocumentValidator{}},
			},
			"set_as_default": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Promote this version on create or update. Setting false does not demote an active version; promote another version first.",
			},
			"is_default": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this is currently the policy default."},
			"created_at": computedRFC3339("Version creation timestamp in RFC3339 format.", true),
		},
	}
}

func (r *iamPolicyVersionResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identitySchema(false)
}

func (r *iamPolicyVersionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamPolicyVersionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	document, err := normalizeIAMJSON(plan.Document.ValueString(), true)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("document"), "Invalid IAM policy document", err.Error())
		return
	}
	payload := map[string]any{
		"policy_id":     plan.PolicyID.ValueString(),
		"document_json": document,
		"set_default":   plan.SetAsDefault.ValueBool(),
	}
	key := createMutationKey(&resp.Diagnostics, "vappcloud_iam_policy_version.create")
	if resp.Diagnostics.HasError() {
		return
	}
	var version client.IAMPolicyVersion
	endpoint := "/v1/iam/policies/" + client.Escape(plan.PolicyID.ValueString()) + "/versions"
	if err := r.client.Do(ctx, http.MethodPost, endpoint, payload, &version, key); err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create IAM policy version", err)
		return
	}
	iamPolicyVersionToState(version, &plan)
	plan.SetAsDefault = types.BoolValue(payload["set_default"].(bool))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *iamPolicyVersionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamPolicyVersionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	versions, err := readIAMPolicyVersions(ctx, r.client, state.PolicyID.ValueString())
	if client.IsNotFound(err) {
		removeIAMState(ctx, &resp.State)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read IAM policy versions", err.Error())
		return
	}
	for _, version := range versions {
		if version.Version == state.Version.ValueString() {
			iamPolicyVersionToState(version, &state)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			setResourceIdentity(ctx, resp.Identity, state.ID.ValueString(), "", &resp.Diagnostics)
			return
		}
	}
	removeIAMState(ctx, &resp.State)
}

func (r *iamPolicyVersionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan iamPolicyVersionResourceModel
	var state iamPolicyVersionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.SetAsDefault.ValueBool() && !state.SetAsDefault.ValueBool() {
		payload := map[string]any{"policy_id": state.PolicyID.ValueString(), "version": state.Version.ValueString()}
		key := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_policy_version.promote", state.ID.ValueString(), payload)
		if resp.Diagnostics.HasError() {
			return
		}
		endpoint := "/v1/iam/policies/" + client.Escape(state.PolicyID.ValueString()) + "/versions/" + client.Escape(state.Version.ValueString()) + "/default"
		if err := r.client.Do(ctx, http.MethodPut, endpoint, payload, nil, key); err != nil {
			addMutationDiagnostic(&resp.Diagnostics, "promote IAM policy version", err)
			return
		}
	}
	plan.ID = state.ID
	plan.PolicyID = state.PolicyID
	plan.Version = state.Version
	plan.IsDefault = types.BoolValue(plan.SetAsDefault.ValueBool() || state.IsDefault.ValueBool())
	plan.CreatedAt = state.CreatedAt
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *iamPolicyVersionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iamPolicyVersionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{"policy_id": state.PolicyID.ValueString(), "version": state.Version.ValueString()}
	if state.IsDefault.ValueBool() {
		versions, err := readIAMPolicyVersions(ctx, r.client, state.PolicyID.ValueString())
		if client.IsNotFound(err) {
			return
		}
		if err != nil {
			resp.Diagnostics.AddError("Unable to inspect IAM policy versions", err.Error())
			return
		}
		candidates := make([]client.IAMPolicyVersion, 0, len(versions)-1)
		for _, version := range versions {
			if version.Version != state.Version.ValueString() {
				candidates = append(candidates, version)
			}
		}
		if len(candidates) == 0 {
			resp.Diagnostics.AddError(
				"Cannot delete the only IAM policy version",
				"Create or retain another policy version before removing the current default version.",
			)
			return
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
				return candidates[i].Version < candidates[j].Version
			}
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		})
		replacement := candidates[len(candidates)-1].Version
		promotePayload := map[string]any{"policy_id": state.PolicyID.ValueString(), "version": replacement}
		promoteKey := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_policy_version.reassign_default", state.ID.ValueString(), promotePayload)
		if resp.Diagnostics.HasError() {
			return
		}
		promoteEndpoint := "/v1/iam/policies/" + client.Escape(state.PolicyID.ValueString()) + "/versions/" + client.Escape(replacement) + "/default"
		if err := r.client.Do(ctx, http.MethodPut, promoteEndpoint, promotePayload, nil, promoteKey); err != nil {
			addMutationDiagnostic(&resp.Diagnostics, "reassign default IAM policy version", err)
			return
		}
	}
	key := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_policy_version.delete", state.ID.ValueString(), payload)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := "/v1/iam/policies/" + client.Escape(state.PolicyID.ValueString()) + "/versions/" + client.Escape(state.Version.ValueString())
	err := r.client.Do(ctx, http.MethodDelete, endpoint, nil, nil, key)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "delete IAM policy version", err)
	}
}

func (r *iamPolicyVersionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitIAMImportID(req.ID, 2, "<policy_id>/<version>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid IAM policy version import identifier", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.Join(parts, "/"))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("version"), parts[1])...)
}

func iamPolicyVersionToState(version client.IAMPolicyVersion, state *iamPolicyVersionResourceModel) {
	state.ID = types.StringValue(version.PolicyID + "/" + version.Version)
	state.PolicyID = types.StringValue(version.PolicyID)
	state.Version = types.StringValue(version.Version)
	document, err := normalizeIAMJSON(version.DocumentJSON, true)
	if err == nil {
		state.Document = types.StringValue(document)
	} else {
		state.Document = types.StringValue(version.DocumentJSON)
	}
	state.IsDefault = types.BoolValue(version.IsDefault)
	state.CreatedAt = formatRFC3339(version.CreatedAt)
}

var (
	_ resource.Resource                = &iamPolicyVersionResource{}
	_ resource.ResourceWithConfigure   = &iamPolicyVersionResource{}
	_ resource.ResourceWithIdentity    = &iamPolicyVersionResource{}
	_ resource.ResourceWithImportState = &iamPolicyVersionResource{}
)

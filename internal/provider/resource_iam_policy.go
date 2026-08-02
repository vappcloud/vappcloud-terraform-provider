package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type iamPolicyResource struct{ resourceBase }

type iamPolicyResourceModel struct {
	ID             types.String      `tfsdk:"id"`
	Name           types.String      `tfsdk:"name"`
	Description    types.String      `tfsdk:"description"`
	Document       types.String      `tfsdk:"document"`
	ARN            types.String      `tfsdk:"arn"`
	Managed        types.Bool        `tfsdk:"managed"`
	DefaultVersion types.String      `tfsdk:"default_version"`
	CreatedAt      timetypes.RFC3339 `tfsdk:"created_at"`
	UpdatedAt      timetypes.RFC3339 `tfsdk:"updated_at"`
}

func NewIAMPolicyResource() resource.Resource { return &iamPolicyResource{} }

func (r *iamPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_policy"
}

func (r *iamPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Version:             0,
		MarkdownDescription: "A customer-managed VAppCloud IAM policy. Updating `document` creates and activates an immutable policy version.",
		Attributes: map[string]schema.Attribute{
			"id": computedID(),
			"name": schema.StringAttribute{
				Required: true, PlanModifiers: replace, MarkdownDescription: "Policy name, unique within the organization.",
			},
			"description": schema.StringAttribute{
				Optional: true, PlanModifiers: replace, MarkdownDescription: "Policy description. Changing it replaces the policy because the API keeps policy metadata immutable.",
			},
			"document": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "IAM policy JSON using Version `2026-08-01`. Semantic JSON equality suppresses formatting-only changes.",
				PlanModifiers:       []planmodifier.String{normalizedJSONPlanModifier{}},
				Validators:          []validator.String{iamPolicyDocumentValidator{}},
			},
			"arn":             computedString("Policy ARN."),
			"managed":         schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the policy is platform-managed. Terraform-created policies are always customer-managed."},
			"default_version": computedString("Active immutable policy version."),
			"created_at":      computedRFC3339("Creation timestamp in RFC3339 format.", true),
			"updated_at":      computedRFC3339("Last update timestamp in RFC3339 format.", false),
		},
	}
}

func (r *iamPolicyResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identitySchema(false)
}

func (r *iamPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamPolicyResourceModel
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
		"name":          plan.Name.ValueString(),
		"description":   plan.Description.ValueString(),
		"document_json": document,
	}
	key := createMutationKey(&resp.Diagnostics, "vappcloud_iam_policy.create")
	if resp.Diagnostics.HasError() {
		return
	}
	var policy client.IAMPolicy
	if err := r.client.Do(ctx, http.MethodPost, "/v1/iam/policies", payload, &policy, key); err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create IAM policy", err)
		return
	}
	if policy.Managed {
		resp.Diagnostics.AddError("Managed IAM policy cannot be adopted", "The create endpoint returned a platform-managed policy.")
		return
	}
	iamPolicyToState(policy, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, policy.ID, "", &resp.Diagnostics)
}

func (r *iamPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var policy client.IAMPolicy
	if !readResource(ctx, r.client, "/v1/iam/policies/"+client.Escape(state.ID.ValueString()), &policy, &resp.State, &resp.Diagnostics) {
		return
	}
	if policy.Managed {
		resp.Diagnostics.AddError("Managed IAM policy cannot be managed", "Use a data source or reference the managed policy ARN instead of importing it as a resource.")
		return
	}
	iamPolicyToState(policy, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setResourceIdentity(ctx, resp.Identity, policy.ID, "", &resp.Diagnostics)
}

func (r *iamPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan iamPolicyResourceModel
	var state iamPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	document, err := normalizeIAMJSON(plan.Document.ValueString(), true)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("document"), "Invalid IAM policy document", err.Error())
		return
	}
	payload := map[string]any{
		"policy_id":     state.ID.ValueString(),
		"document_json": document,
		"set_default":   true,
	}
	key := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_policy.update", state.ID.ValueString(), payload)
	if resp.Diagnostics.HasError() {
		return
	}
	var version client.IAMPolicyVersion
	if err := r.client.Do(ctx, http.MethodPost, "/v1/iam/policies/"+client.Escape(state.ID.ValueString())+"/versions", payload, &version, key); err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update IAM policy", err)
		return
	}
	var policy client.IAMPolicy
	if err := r.client.Do(ctx, http.MethodGet, "/v1/iam/policies/"+client.Escape(state.ID.ValueString()), nil, &policy, ""); err != nil {
		resp.Diagnostics.AddError("Unable to refresh IAM policy", err.Error())
		return
	}
	iamPolicyToState(policy, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, policy.ID, "", &resp.Diagnostics)
}

func (r *iamPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iamPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	key := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_policy.delete", id, nil)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, http.MethodDelete, "/v1/iam/policies/"+client.Escape(id), nil, nil, key)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "delete IAM policy", err)
	}
}

func (r *iamPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("id"), path.Root("id"), req, resp)
}

func iamPolicyToState(policy client.IAMPolicy, state *iamPolicyResourceModel) {
	state.ID = types.StringValue(policy.ID)
	state.Name = types.StringValue(policy.Name)
	if state.Description.IsNull() && policy.Description == "" {
		state.Description = types.StringNull()
	} else {
		state.Description = types.StringValue(policy.Description)
	}
	document, err := normalizeIAMJSON(policy.DocumentJSON, true)
	if err == nil {
		state.Document = types.StringValue(document)
	} else {
		state.Document = types.StringValue(policy.DocumentJSON)
	}
	state.ARN = types.StringValue(policy.ARN)
	state.Managed = types.BoolValue(policy.Managed)
	state.DefaultVersion = types.StringValue(policy.DefaultVersion)
	state.CreatedAt = formatRFC3339(policy.CreatedAt)
	state.UpdatedAt = formatRFC3339(policy.UpdatedAt)
}

var (
	_ resource.Resource                = &iamPolicyResource{}
	_ resource.ResourceWithConfigure   = &iamPolicyResource{}
	_ resource.ResourceWithIdentity    = &iamPolicyResource{}
	_ resource.ResourceWithImportState = &iamPolicyResource{}
)

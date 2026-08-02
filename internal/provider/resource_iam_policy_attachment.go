package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type iamPolicyAttachmentResource struct{ resourceBase }

type iamPolicyAttachmentResourceModel struct {
	ID         types.String      `tfsdk:"id"`
	PolicyID   types.String      `tfsdk:"policy_id"`
	PolicyARN  types.String      `tfsdk:"policy_arn"`
	PolicyName types.String      `tfsdk:"policy_name"`
	TargetType types.String      `tfsdk:"target_type"`
	TargetID   types.String      `tfsdk:"target_id"`
	CreatedBy  types.String      `tfsdk:"created_by"`
	CreatedAt  timetypes.RFC3339 `tfsdk:"created_at"`
}

func NewIAMPolicyAttachmentResource() resource.Resource { return &iamPolicyAttachmentResource{} }

func (r *iamPolicyAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_policy_attachment"
}

func (r *iamPolicyAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             0,
		MarkdownDescription: "Attaches an IAM policy to exactly one principal, group, or role. All attachment identity fields are immutable.",
		Attributes: map[string]schema.Attribute{
			"id":          computedID(),
			"policy_id":   immutableString("Policy ID."),
			"policy_arn":  computedString("Attached policy ARN."),
			"policy_name": computedString("Attached policy name."),
			"target_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Attachment target type: `principal`, `group`, or `role`.",
				Validators:          []validator.String{stringvalidator.OneOf("principal", "group", "role")},
				PlanModifiers:       immutableString("attachment target type").PlanModifiers,
			},
			"target_id":  immutableString("Target principal, group, or role ID."),
			"created_by": computedString("Principal that created the attachment."),
			"created_at": computedRFC3339("Attachment timestamp in RFC3339 format.", true),
		},
	}
}

func (r *iamPolicyAttachmentResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identitySchema(false)
}

func (r *iamPolicyAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamPolicyAttachmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{
		"policy_id":   plan.PolicyID.ValueString(),
		"target_type": plan.TargetType.ValueString(),
		"target_id":   plan.TargetID.ValueString(),
	}
	id := iamPolicyAttachmentID(plan.PolicyID.ValueString(), plan.TargetType.ValueString(), plan.TargetID.ValueString())
	key := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_policy_attachment.create", id, payload)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Do(ctx, http.MethodPost, "/v1/iam/attachments", payload, nil, key); err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create IAM policy attachment", err)
		return
	}
	attachment, found, err := findIAMPolicyAttachment(ctx, r.client, plan.PolicyID.ValueString(), plan.TargetType.ValueString(), plan.TargetID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to refresh IAM policy attachment", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("IAM policy attachment was not observable", "The API accepted the attachment but did not return it from the attachment list.")
		return
	}
	iamPolicyAttachmentToState(attachment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setResourceIdentity(ctx, resp.Identity, plan.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *iamPolicyAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamPolicyAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	attachment, found, err := findIAMPolicyAttachment(ctx, r.client, state.PolicyID.ValueString(), state.TargetType.ValueString(), state.TargetID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read IAM policy attachment", err.Error())
		return
	}
	if !found {
		removeIAMState(ctx, &resp.State)
		return
	}
	iamPolicyAttachmentToState(attachment, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setResourceIdentity(ctx, resp.Identity, state.ID.ValueString(), "", &resp.Diagnostics)
}

func (r *iamPolicyAttachmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Every configurable attachment field requires replacement.
}

func (r *iamPolicyAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iamPolicyAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := map[string]any{
		"policy_id": state.PolicyID.ValueString(), "target_type": state.TargetType.ValueString(), "target_id": state.TargetID.ValueString(),
	}
	key := iamMutationKey(&resp.Diagnostics, "vappcloud_iam_policy_attachment.delete", state.ID.ValueString(), payload)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := "/v1/iam/attachments/" + client.Escape(state.PolicyID.ValueString()) + "/" + client.Escape(state.TargetType.ValueString()) + "/" + client.Escape(state.TargetID.ValueString())
	err := r.client.Do(ctx, http.MethodDelete, endpoint, nil, nil, key)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "delete IAM policy attachment", err)
	}
}

func (r *iamPolicyAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitIAMImportID(req.ID, 3, "<policy_id>/<target_type>/<target_id>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid IAM policy attachment import identifier", err.Error())
		return
	}
	if parts[1] != "principal" && parts[1] != "group" && parts[1] != "role" {
		resp.Diagnostics.AddError("Invalid IAM policy attachment target", "target_type must be principal, group, or role.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.Join(parts, "/"))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target_type"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target_id"), parts[2])...)
}

func findIAMPolicyAttachment(ctx context.Context, c *client.Client, policyID, targetType, targetID string) (client.IAMPolicyAttachment, bool, error) {
	items, err := readIAMAttachments(ctx, c)
	if err != nil {
		return client.IAMPolicyAttachment{}, false, err
	}
	for _, item := range items {
		if item.PolicyID == policyID && item.TargetType == targetType && item.TargetID == targetID {
			return item, true, nil
		}
	}
	return client.IAMPolicyAttachment{}, false, nil
}

func iamPolicyAttachmentID(policyID, targetType, targetID string) string {
	return strings.Join([]string{policyID, targetType, targetID}, "/")
}

func iamPolicyAttachmentToState(attachment client.IAMPolicyAttachment, state *iamPolicyAttachmentResourceModel) {
	state.ID = types.StringValue(iamPolicyAttachmentID(attachment.PolicyID, attachment.TargetType, attachment.TargetID))
	state.PolicyID = types.StringValue(attachment.PolicyID)
	state.PolicyARN = types.StringValue(attachment.PolicyARN)
	state.PolicyName = types.StringValue(attachment.PolicyName)
	state.TargetType = types.StringValue(attachment.TargetType)
	state.TargetID = types.StringValue(attachment.TargetID)
	state.CreatedBy = types.StringValue(attachment.CreatedBy)
	state.CreatedAt = formatRFC3339(attachment.CreatedAt)
}

var (
	_ resource.Resource                = &iamPolicyAttachmentResource{}
	_ resource.ResourceWithConfigure   = &iamPolicyAttachmentResource{}
	_ resource.ResourceWithIdentity    = &iamPolicyAttachmentResource{}
	_ resource.ResourceWithImportState = &iamPolicyAttachmentResource{}
)

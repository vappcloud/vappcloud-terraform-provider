package provider

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type applicationInstanceResource struct{ resourceBase }

type applicationInstanceResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ProjectID       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Source          types.Object `tfsdk:"source"`
	Placements      types.List   `tfsdk:"placement"`
	SecretIDs       types.Set    `tfsdk:"secret_ids"`
	State           types.String `tfsdk:"state"`
	ReadyReplicas   types.Int64  `tfsdk:"ready_replicas"`
	DesiredReplicas types.Int64  `tfsdk:"desired_replicas"`
	OperationStatus types.String `tfsdk:"operation_status"`
	OperationID     types.String `tfsdk:"operation_id"`
	CorrelationID   types.String `tfsdk:"correlation_id"`
	ResourceVersion types.Int64  `tfsdk:"resource_version"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

type sourceModel struct {
	Kind                     types.String `tfsdk:"kind"`
	MarketplaceApplicationID types.String `tfsdk:"marketplace_application_id"`
	MarketplaceVersionID     types.String `tfsdk:"marketplace_version_id"`
	GitHubConnectionID       types.String `tfsdk:"github_connection_id"`
	Repository               types.String `tfsdk:"repository"`
	Ref                      types.String `tfsdk:"ref"`
}

type placementModel struct {
	VMMID        types.String `tfsdk:"vmm_id"`
	ReplicaCount types.Int64  `tfsdk:"replica_count"`
}

var sourceAttributeTypes = map[string]attr.Type{
	"kind":                       types.StringType,
	"marketplace_application_id": types.StringType,
	"marketplace_version_id":     types.StringType,
	"github_connection_id":       types.StringType,
	"repository":                 types.StringType,
	"ref":                        types.StringType,
}

var placementAttributeTypes = map[string]attr.Type{
	"vmm_id":        types.StringType,
	"replica_count": types.Int64Type,
}

func NewApplicationInstanceResource() resource.Resource { return &applicationInstanceResource{} }

func (r *applicationInstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_instance"
}

func (r *applicationInstanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		MarkdownDescription: "A marketplace or GitHub application deployed explicitly to one or more VMMs.",
		Attributes: withCommon(map[string]schema.Attribute{
			"project_id":  immutableString("Owning project ID."),
			"name":        schema.StringAttribute{Required: true, MarkdownDescription: "Application instance name."},
			"description": schema.StringAttribute{Optional: true, MarkdownDescription: "Mutable description."},
			"source": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "Exactly one marketplace or GitHub source. Source changes replace the deployment.",
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Required:   true,
						Validators: []validator.String{stringvalidator.OneOf("marketplace", "github")},
					},
					"marketplace_application_id": schema.StringAttribute{Optional: true},
					"marketplace_version_id":     schema.StringAttribute{Optional: true},
					"github_connection_id":       schema.StringAttribute{Optional: true},
					"repository":                 schema.StringAttribute{Optional: true},
					"ref":                        schema.StringAttribute{Optional: true},
				},
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
			},
			"placement": schema.ListNestedAttribute{
				Required:      true,
				Validators:    []validator.List{listvalidator.SizeAtLeast(1)},
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"vmm_id": schema.StringAttribute{Required: true, MarkdownDescription: "Target VMM ID."},
						"replica_count": schema.Int64Attribute{
							Required:            true,
							Validators:          []validator.Int64{int64validator.AtLeast(1)},
							MarkdownDescription: "Replicas placed on this VMM.",
						},
					},
				},
			},
			"secret_ids": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "References to preconfigured secret IDs. Secret values are never accepted or stored.",
			},
			"state":            computedString("Deployment state."),
			"ready_replicas":   schema.Int64Attribute{Computed: true},
			"desired_replicas": schema.Int64Attribute{Computed: true},
			"operation_status": computedString("Latest asynchronous operation state."),
			"operation_id":     computedString("Latest asynchronous operation ID."),
			"correlation_id":   computedString("Latest operation correlation ID."),
		}),
	}
}

func (r *applicationInstanceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config applicationInstanceResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Source.IsNull() || config.Source.IsUnknown() {
		return
	}
	var source sourceModel
	resp.Diagnostics.Append(config.Source.As(ctx, &source, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() || source.Kind.IsUnknown() {
		return
	}
	switch source.Kind.ValueString() {
	case "marketplace":
		if source.MarketplaceApplicationID.IsNull() || source.MarketplaceApplicationID.ValueString() == "" ||
			source.MarketplaceVersionID.IsNull() || source.MarketplaceVersionID.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(path.Root("source"), "Incomplete marketplace source", "marketplace_application_id and marketplace_version_id are required when kind is marketplace.")
		}
		if nonempty(source.GitHubConnectionID) || nonempty(source.Repository) || nonempty(source.Ref) {
			resp.Diagnostics.AddAttributeError(path.Root("source"), "Mixed source configuration", "GitHub fields cannot be set for a marketplace source.")
		}
	case "github":
		if !nonempty(source.GitHubConnectionID) || !nonempty(source.Repository) || !nonempty(source.Ref) {
			resp.Diagnostics.AddAttributeError(path.Root("source"), "Incomplete GitHub source", "github_connection_id, repository, and ref are required when kind is github.")
		}
		if nonempty(source.MarketplaceApplicationID) || nonempty(source.MarketplaceVersionID) {
			resp.Diagnostics.AddAttributeError(path.Root("source"), "Mixed source configuration", "Marketplace fields cannot be set for a GitHub source.")
		}
	}
}

func nonempty(v types.String) bool {
	return !v.IsNull() && !v.IsUnknown() && v.ValueString() != ""
}

func (r *applicationInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan applicationInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	source, placements, secretIDs := applicationPlanValues(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.ApplicationInstance]
	err := r.client.Do(ctx, http.MethodPost, "/v1/application-instances", map[string]any{
		"project_id":  plan.ProjectID.ValueString(),
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
		"source":      source,
		"placements":  placements,
		"secret_ids":  secretIDs,
	}, &result, client.IdempotencyKey())
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "create", err)
		return
	}
	waitMutation(ctx, r.client, result.Operation, result.OperationID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if result.Resource.ID == "" {
		if err := r.client.Do(ctx, http.MethodGet, "/v1/application-instances/"+client.Escape(result.Operation.ResourceID), nil, &result.Resource, ""); err != nil {
			addMutationDiagnostic(&resp.Diagnostics, "read created", err)
			return
		}
	}
	applicationToState(ctx, result.Resource, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *applicationInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var instance client.ApplicationInstance
	if !readResource(ctx, r.client, "/v1/application-instances/"+client.Escape(state.ID.ValueString()), &instance, &resp.State, &resp.Diagnostics) {
		return
	}
	applicationToState(ctx, instance, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *applicationInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan applicationInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, _, secretIDs := applicationPlanValues(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.ApplicationInstance]
	err := r.client.Do(ctx, http.MethodPatch, "/v1/application-instances/"+client.Escape(plan.ID.ValueString()), map[string]any{
		"name":             plan.Name.ValueString(),
		"description":      plan.Description.ValueString(),
		"secret_ids":       secretIDs,
		"resource_version": plan.ResourceVersion.ValueInt64(),
	}, &result, client.IdempotencyKey())
	if err != nil {
		addMutationDiagnostic(&resp.Diagnostics, "update", err)
		return
	}
	waitMutation(ctx, r.client, result.Operation, result.OperationID, &resp.Diagnostics)
	applicationToState(ctx, result.Resource, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *applicationInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var result client.Mutation[client.ApplicationInstance]
	endpoint := "/v1/application-instances/" + client.Escape(state.ID.ValueString()) +
		"?resource_version=" + strconv.FormatInt(state.ResourceVersion.ValueInt64(), 10)
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

func (r *applicationInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid application import identifier", "Expected <project_id>/<application_instance_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
}

func applicationPlanValues(ctx context.Context, plan applicationInstanceResourceModel, diagnostics interface {
	Append(...diag.Diagnostic)
}) (client.ApplicationSource, []client.Placement, []string) {
	var sourceModelValue sourceModel
	diagnostics.Append(plan.Source.As(ctx, &sourceModelValue, basetypes.ObjectAsOptions{})...)
	source := client.ApplicationSource{
		Kind:               sourceModelValue.Kind.ValueString(),
		MarketplaceAppID:   sourceModelValue.MarketplaceApplicationID.ValueString(),
		MarketplaceVersion: sourceModelValue.MarketplaceVersionID.ValueString(),
		GitHubConnectionID: sourceModelValue.GitHubConnectionID.ValueString(),
		Repository:         sourceModelValue.Repository.ValueString(),
		Ref:                sourceModelValue.Ref.ValueString(),
	}
	var placementValues []placementModel
	diagnostics.Append(plan.Placements.ElementsAs(ctx, &placementValues, false)...)
	placements := make([]client.Placement, 0, len(placementValues))
	for _, placement := range placementValues {
		placements = append(placements, client.Placement{
			VMMID: placement.VMMID.ValueString(), ReplicaCount: placement.ReplicaCount.ValueInt64(),
		})
	}
	var secretValues []string
	if !plan.SecretIDs.IsNull() && !plan.SecretIDs.IsUnknown() {
		diagnostics.Append(plan.SecretIDs.ElementsAs(ctx, &secretValues, false)...)
	}
	return source, placements, secretValues
}

func applicationToState(ctx context.Context, instance client.ApplicationInstance, state *applicationInstanceResourceModel, diagnostics interface {
	Append(...diag.Diagnostic)
}) {
	state.ID = types.StringValue(instance.ID)
	state.ProjectID = types.StringValue(instance.ProjectID)
	state.Name = types.StringValue(instance.Name)
	state.Description = types.StringValue(instance.Description)
	source, diags := types.ObjectValue(sourceAttributeTypes, map[string]attr.Value{
		"kind":                       types.StringValue(instance.Source.Kind),
		"marketplace_application_id": stringOrNull(instance.Source.MarketplaceAppID),
		"marketplace_version_id":     stringOrNull(instance.Source.MarketplaceVersion),
		"github_connection_id":       stringOrNull(instance.Source.GitHubConnectionID),
		"repository":                 stringOrNull(instance.Source.Repository),
		"ref":                        stringOrNull(instance.Source.Ref),
	})
	diagnostics.Append(diags...)
	state.Source = source
	placementValues := make([]attr.Value, 0, len(instance.Placements))
	for _, placement := range instance.Placements {
		object, objectDiags := types.ObjectValue(placementAttributeTypes, map[string]attr.Value{
			"vmm_id":        types.StringValue(placement.VMMID),
			"replica_count": types.Int64Value(placement.ReplicaCount),
		})
		diagnostics.Append(objectDiags...)
		placementValues = append(placementValues, object)
	}
	placements, placementDiags := types.ListValue(types.ObjectType{AttrTypes: placementAttributeTypes}, placementValues)
	diagnostics.Append(placementDiags...)
	state.Placements = placements
	secretValues := make([]attr.Value, 0, len(instance.SecretIDs))
	for _, id := range instance.SecretIDs {
		secretValues = append(secretValues, types.StringValue(id))
	}
	secrets, secretDiags := types.SetValue(types.StringType, secretValues)
	diagnostics.Append(secretDiags...)
	state.SecretIDs = secrets
	state.State = types.StringValue(instance.State)
	state.ReadyReplicas = types.Int64Value(instance.ReadyReplicas)
	state.DesiredReplicas = types.Int64Value(instance.DesiredReplicas)
	state.ResourceVersion = types.Int64Value(instance.ResourceVersion)
	state.OperationStatus = types.StringValue(instance.Operation.State)
	state.OperationID = types.StringValue(instance.Operation.ID)
	state.CorrelationID = types.StringValue(instance.Operation.CorrelationID)
	state.CreatedAt = formatTime(instance.CreatedAt)
	state.UpdatedAt = formatTime(instance.UpdatedAt)
	_ = ctx
}

func stringOrNull(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

var (
	_ resource.Resource                   = &applicationInstanceResource{}
	_ resource.ResourceWithConfigure      = &applicationInstanceResource{}
	_ resource.ResourceWithImportState    = &applicationInstanceResource{}
	_ resource.ResourceWithValidateConfig = &applicationInstanceResource{}
)

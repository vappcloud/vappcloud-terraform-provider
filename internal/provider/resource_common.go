package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

const operationTimeout = 20 * time.Minute

type resourceBase struct {
	client *client.Client
}

func (r *resourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func computedID() resourceschema.StringAttribute {
	return resourceschema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "Opaque public resource identifier.",
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
		},
	}
}

func immutableString(description string) resourceschema.StringAttribute {
	return resourceschema.StringAttribute{
		Required:            true,
		MarkdownDescription: description,
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
}

func computedString(description string) resourceschema.StringAttribute {
	return resourceschema.StringAttribute{
		Computed:            true,
		MarkdownDescription: description,
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
		},
	}
}

func commonComputedAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id":               computedID(),
		"resource_version": resourceschema.Int64Attribute{Computed: true, MarkdownDescription: "Optimistic concurrency version."},
		"created_at":       computedString("Creation timestamp in RFC3339 format."),
		"updated_at":       computedString("Last update timestamp in RFC3339 format."),
	}
}

func withCommon(attrs map[string]resourceschema.Attribute) map[string]resourceschema.Attribute {
	for k, v := range commonComputedAttributes() {
		attrs[k] = v
	}
	return attrs
}

func formatTime(t time.Time) types.String {
	if t.IsZero() {
		return types.StringNull()
	}
	return types.StringValue(t.UTC().Format(time.RFC3339))
}

func waitMutation(ctx context.Context, c *client.Client, operation client.Operation, operationID string, diagnostics *diag.Diagnostics) {
	id := operation.ID
	if id == "" {
		id = operationID
	}
	if id == "" {
		return
	}
	_, err := c.WaitOperation(ctx, id, operationTimeout)
	if err != nil {
		diagnostics.AddError("VAppCloud operation failed", err.Error())
	}
}

func readResource[T any](ctx context.Context, c *client.Client, path string, out *T, respState interface {
	RemoveResource(context.Context)
}, diagnostics *diag.Diagnostics) bool {
	err := c.Do(ctx, http.MethodGet, path, nil, out, "")
	if client.IsNotFound(err) {
		respState.RemoveResource(ctx)
		return false
	}
	if err != nil {
		diagnostics.AddError("Unable to read VAppCloud resource", err.Error())
		return false
	}
	return true
}

func addMutationDiagnostic(diagnostics *diag.Diagnostics, action string, err error) {
	if err == nil {
		return
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		diagnostics.AddError("Unable to "+action+" VAppCloud resource", apiErr.Error())
		return
	}
	diagnostics.AddError("Unable to "+action+" VAppCloud resource", fmt.Sprintf("%v", err))
}

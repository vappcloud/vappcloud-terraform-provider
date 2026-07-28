package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	resourceTimeouts "github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

const (
	operationTimeout        = 20 * time.Minute
	projectOperationTimeout = 5 * time.Minute
	deviceOperationTimeout  = 10 * time.Minute
	computeOperationTimeout = 30 * time.Minute
)

type operationTimeouts = resourceTimeouts.Value

type resourceBase struct {
	client *client.Client
}

func compositeImportID(id, resourceName string, diagnostics *diag.Diagnostics) (string, string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		diagnostics.AddError(
			"Invalid "+resourceName+" import identifier",
			"Expected <project_id>/<resource_id>.",
		)
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (r *resourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func computedID() resourceschema.StringAttribute {
	return immutableComputedString("Opaque public resource identifier.")
}

func immutableComputedString(description string) resourceschema.StringAttribute {
	return resourceschema.StringAttribute{
		Computed:            true,
		MarkdownDescription: description,
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
	}
}

func commonComputedAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id": computedID(),
		"resource_version": resourceschema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "Optimistic concurrency version.",
		},
		"created_at": immutableComputedString("Creation timestamp in RFC3339 format."),
		"updated_at": computedString("Last update timestamp in RFC3339 format."),
	}
}

func withCommon(attrs map[string]resourceschema.Attribute) map[string]resourceschema.Attribute {
	for k, v := range commonComputedAttributes() {
		attrs[k] = v
	}
	return attrs
}

func timeoutAttributes(ctx context.Context, fallback time.Duration) resourceschema.Attribute {
	description := func(operation string) string {
		return fmt.Sprintf(
			"Maximum time to wait for the %s operation. Accepts Go duration syntax such as `30s` or `2h45m`; defaults to `%s`.",
			operation,
			fallback,
		)
	}
	return resourceTimeouts.Attributes(ctx, resourceTimeouts.Opts{
		Create:            true,
		Update:            true,
		Delete:            true,
		CreateDescription: description("create"),
		UpdateDescription: description("update"),
		DeleteDescription: description("delete"),
	})
}

func createTimeout(ctx context.Context, value operationTimeouts, fallback time.Duration, diagnostics *diag.Diagnostics) time.Duration {
	timeout, diags := value.Create(ctx, fallback)
	diagnostics.Append(diags...)
	return timeout
}

func updateTimeout(ctx context.Context, value operationTimeouts, fallback time.Duration, diagnostics *diag.Diagnostics) time.Duration {
	timeout, diags := value.Update(ctx, fallback)
	diagnostics.Append(diags...)
	return timeout
}

func deleteTimeout(ctx context.Context, value operationTimeouts, fallback time.Duration, diagnostics *diag.Diagnostics) time.Duration {
	timeout, diags := value.Delete(ctx, fallback)
	diagnostics.Append(diags...)
	return timeout
}

func formatTime(t time.Time) types.String {
	if t.IsZero() {
		return types.StringNull()
	}
	return types.StringValue(t.UTC().Format(time.RFC3339))
}

func waitMutation(ctx context.Context, c *client.Client, operation client.Operation, operationID string, timeout time.Duration, diagnostics *diag.Diagnostics) (client.Operation, bool) {
	id := operation.ID
	if id == "" {
		id = operationID
	}
	if id == "" {
		return operation, true
	}
	completed, err := c.WaitOperation(ctx, id, timeout)
	if err != nil {
		diagnostics.AddError("VAppCloud operation failed", err.Error())
		return completed, false
	}
	return completed, true
}

func completeMutation[T any](
	ctx context.Context,
	c *client.Client,
	result *client.Mutation[T],
	timeout time.Duration,
	resourceID func(T) string,
	readPath func(string) string,
	diagnostics *diag.Diagnostics,
) bool {
	completed, ok := waitMutation(ctx, c, result.Operation, result.OperationID, timeout, diagnostics)
	if !ok || diagnostics.HasError() {
		return false
	}
	if resourceID(result.Resource) != "" {
		return true
	}
	id := completed.ResourceID
	if id == "" {
		id = result.Operation.ResourceID
	}
	if id == "" {
		diagnostics.AddError(
			"VAppCloud operation returned no resource",
			"The mutation completed without an inline resource or operation resource_id; state was not changed.",
		)
		return false
	}
	if err := c.Do(ctx, http.MethodGet, readPath(id), nil, &result.Resource, ""); err != nil {
		addMutationDiagnostic(diagnostics, "read mutated", err)
		return false
	}
	if resourceID(result.Resource) == "" {
		diagnostics.AddError("VAppCloud resource read returned no identifier", "State was not changed because the API response did not contain a resource ID.")
		return false
	}
	return true
}

func mutationKey(diagnostics *diag.Diagnostics, operation, resourceID string, payload any) string {
	key, err := client.StableIdempotencyKey(operation, resourceID, payload)
	if err != nil {
		diagnostics.AddError("Unable to derive stable idempotency key", err.Error())
		return ""
	}
	return key
}

func mutateWithVersionRetry(
	initial client.Version,
	readCurrent func() (client.Version, error),
	mutate func(client.Version) error,
) error {
	err := mutate(initial)
	if !client.IsVersionConflict(err) {
		return err
	}
	current, readErr := readCurrent()
	if readErr != nil {
		return fmt.Errorf("resource version conflict; refresh failed: %w", readErr)
	}
	err = mutate(current)
	if client.IsVersionConflict(err) {
		return fmt.Errorf("resource changed again while retrying optimistic concurrency: %w; run terraform refresh and re-apply", err)
	}
	return err
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

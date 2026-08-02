package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

const iamPolicyVersion = "2026-08-01"

type normalizedJSONPlanModifier struct{}

type iamPolicyDocumentValidator struct{}

func (iamPolicyDocumentValidator) Description(context.Context) string {
	return "Validates a VAppCloud IAM policy document."
}

func (v iamPolicyDocumentValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (iamPolicyDocumentValidator) ValidateString(
	_ context.Context,
	req validator.StringRequest,
	resp *validator.StringResponse,
) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := normalizeIAMJSON(req.ConfigValue.ValueString(), true); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid IAM policy document", err.Error())
	}
}

func (normalizedJSONPlanModifier) Description(context.Context) string {
	return "Treats semantically equivalent JSON documents as equal."
}

func (m normalizedJSONPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (normalizedJSONPlanModifier) PlanModifyString(
	_ context.Context,
	req planmodifier.StringRequest,
	resp *planmodifier.StringResponse,
) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	configured, configuredErr := normalizeIAMJSON(req.ConfigValue.ValueString(), false)
	current, currentErr := normalizeIAMJSON(req.StateValue.ValueString(), false)
	if configuredErr == nil && currentErr == nil && configured == current {
		resp.PlanValue = req.StateValue
	}
}

func normalizeIAMJSON(value string, requirePolicy bool) (string, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.UseNumber()
	var parsed map[string]any
	if err := decoder.Decode(&parsed); err != nil {
		return "", fmt.Errorf("document must be a JSON object: %w", err)
	}
	if len(parsed) == 0 {
		return "", errors.New("document must be a non-empty JSON object")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", errors.New("document must contain one JSON object")
	}
	if requirePolicy {
		if parsed["Version"] != iamPolicyVersion {
			return "", fmt.Errorf("policy Version must be %q", iamPolicyVersion)
		}
		statements, ok := parsed["Statement"].([]any)
		if !ok || len(statements) == 0 {
			return "", errors.New("policy Statement must be a non-empty array")
		}
	}
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return "", fmt.Errorf("normalize document: %w", err)
	}
	return string(encoded), nil
}

var _ validator.String = iamPolicyDocumentValidator{}

func readIAMPolicyVersions(ctx context.Context, c *client.Client, policyID string) ([]client.IAMPolicyVersion, error) {
	var page client.Page[client.IAMPolicyVersion]
	err := c.Do(ctx, http.MethodGet, "/v1/iam/policies/"+client.Escape(policyID)+"/versions", nil, &page, "")
	return page.Items, err
}

func readIAMAttachments(ctx context.Context, c *client.Client) ([]client.IAMPolicyAttachment, error) {
	var page client.Page[client.IAMPolicyAttachment]
	err := c.Do(ctx, http.MethodGet, "/v1/iam/attachments", nil, &page, "")
	return page.Items, err
}

func readIAMGroups(ctx context.Context, c *client.Client) ([]client.IAMGroup, error) {
	var page client.Page[client.IAMGroup]
	err := c.Do(ctx, http.MethodGet, "/v1/iam/groups", nil, &page, "")
	return page.Items, err
}

func readIAMGroupMembers(ctx context.Context, c *client.Client, groupID string) ([]string, error) {
	var result client.IAMGroupMembers
	err := c.Do(ctx, http.MethodGet, "/v1/iam/groups/"+client.Escape(groupID)+"/members", nil, &result, "")
	sort.Strings(result.PrincipalIDs)
	return result.PrincipalIDs, err
}

func removeIAMState(ctx context.Context, state interface{ RemoveResource(context.Context) }) {
	state.RemoveResource(ctx)
}

func iamMutationKey(diagnostics *diag.Diagnostics, operation, id string, payload any) string {
	key := mutationKey(diagnostics, operation, id, payload)
	if diagnostics.HasError() {
		return ""
	}
	return key
}

func splitIAMImportID(value string, count int, description string) ([]string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != count {
		return nil, fmt.Errorf("expected %s", description)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("expected %s", description)
		}
	}
	return parts, nil
}

func stringSet(ctx context.Context, values []string, diagnostics *diag.Diagnostics) types.Set {
	result, diags := types.SetValueFrom(ctx, types.StringType, values)
	diagnostics.Append(diags...)
	return result
}

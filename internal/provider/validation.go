package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type apiURLValidator struct{}

func (apiURLValidator) Description(context.Context) string {
	return "must be an absolute HTTPS URL; HTTP is allowed only for localhost and loopback addresses"
}

func (v apiURLValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v apiURLValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := client.ValidateBaseURL(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid VAppCloud API URL", err.Error())
	}
}

package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

func TestAccRealAPIProjectLifecycle(t *testing.T) {
	if os.Getenv("VAPPCLOUD_REAL_ACC") != "1" {
		t.Skip("set VAPPCLOUD_REAL_ACC=1 to run credentialed real API acceptance")
	}
	apiURL := os.Getenv("VAPPCLOUD_API_URL")
	token := os.Getenv("VAPPCLOUD_TOKEN")
	if apiURL == "" || token == "" {
		t.Fatal("VAPPCLOUD_API_URL and VAPPCLOUD_TOKEN are required")
	}
	api, err := client.New(apiURL, token, "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	var authorization struct {
		Principal map[string]any   `json:"principal"`
		Bindings  []map[string]any `json:"bindings"`
	}
	if err := api.Do(context.Background(), http.MethodGet, "/v1/iam/me", nil, &authorization, ""); err != nil {
		t.Fatalf("resolve role-bound service-account authorization: %v", err)
	}
	if valueFromJSON(authorization.Principal, "principalType", "principal_type") != "service_account" {
		t.Fatal("VAPPCLOUD_TOKEN must belong to a service account")
	}
	hasOrganizationEditor := false
	for _, binding := range authorization.Bindings {
		roleID := valueFromJSON(binding, "roleId", "role_id")
		roleName := valueFromJSON(binding, "roleName", "role_name")
		scopeType := valueFromJSON(binding, "scopeType", "scope_type")
		if scopeType == "organization" &&
			(roleName == "vapp-editor" || roleID == "00000000-0000-0000-0000-000000000102") {
			hasOrganizationEditor = true
			break
		}
	}
	if !hasOrganizationEditor {
		t.Fatal("VAPPCLOUD_TOKEN requires an organization-scoped vapp-editor binding")
	}
	vmmID := os.Getenv("VAPPCLOUD_REAL_ACC_VMM_ID")
	if vmmID == "" {
		t.Fatal("VAPPCLOUD_REAL_ACC_VMM_ID must identify an existing VMM for the service-account shell-denial check")
	}
	var existingVMM client.VMM
	if err := api.Do(
		context.Background(),
		http.MethodGet,
		"/v1/vmms/"+client.Escape(vmmID),
		nil,
		&existingVMM,
		"",
	); err != nil {
		t.Fatalf("read VMM used by service-account shell-denial check: %v", err)
	}
	if existingVMM.ID != vmmID {
		t.Fatalf("VMM denial fixture mismatch: requested %q, received %q", vmmID, existingVMM.ID)
	}
	idempotencyKey, err := client.NewIdempotencyKey("service-account-shell-denial")
	if err != nil {
		t.Fatalf("create shell-denial idempotency key: %v", err)
	}
	var shell any
	err = api.Do(
		context.Background(),
		http.MethodPost,
		"/v1/vmms/"+client.Escape(vmmID)+"/sessions",
		map[string]any{"purpose": "ssh", "keyId": "service-accounts-have-no-ssh-keys"},
		&shell,
		idempotencyKey,
	)
	if err == nil {
		t.Fatal("service-account token unexpectedly opened a VMM shell session")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("service-account shell denial returned a non-API error: %v", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized ||
		apiErr.Code != "UNAUTHENTICATED" ||
		apiErr.Message != "human authentication required" {
		t.Fatalf(
			"unexpected service-account shell denial: status=%d code=%q message=%q",
			apiErr.StatusCode,
			apiErr.Code,
			apiErr.Message,
		)
	}
	name := fmt.Sprintf("tf-nightly-%d", time.Now().UTC().Unix())
	config := fmt.Sprintf(`
provider "vappcloud" {
  api_url = %q
}
resource "vappcloud_project" "nightly" {
  name        = %q
  description = "nightly provider lifecycle verification"
}`, apiURL, name)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy: func(state *terraform.State) error {
			for _, managed := range state.RootModule().Resources {
				if managed.Type != "vappcloud_project" || managed.Primary.ID == "" {
					continue
				}
				var project client.Project
				err := api.Do(context.Background(), http.MethodGet, "/v1/projects/"+client.Escape(managed.Primary.ID), nil, &project, "")
				if err == nil {
					return fmt.Errorf("project %s still exists", managed.Primary.ID)
				}
				if !client.IsNotFound(err) {
					return err
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("vappcloud_project.nightly", "name", name),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

func valueFromJSON(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok {
			return text
		}
	}
	return ""
}

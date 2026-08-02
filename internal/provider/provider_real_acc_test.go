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
		Principal map[string]any `json:"principal"`
	}
	if err := api.Do(context.Background(), http.MethodGet, "/v1/iam/me", nil, &authorization, ""); err != nil {
		t.Fatalf("resolve service-account principal: %v", err)
	}
	if valueFromJSON(authorization.Principal, "principalType", "principal_type") != "service_account" {
		t.Fatal("VAPPCLOUD_TOKEN must belong to a service account")
	}
	principalID := valueFromJSON(authorization.Principal, "id")
	organizationID := valueFromJSON(authorization.Principal, "organizationId", "organization_id")
	var simulation struct {
		Decisions []struct {
			Allowed bool `json:"allowed"`
		} `json:"decisions"`
	}
	if err := api.Do(context.Background(), http.MethodPost, "/v1/iam/simulate", map[string]any{
		"principal_id": principalID,
		"entries": []map[string]any{{
			"action":       "project:Create",
			"resource_arn": fmt.Sprintf("arn:vapp:project::%s:project/new", organizationID),
			"context_json": "{}",
		}},
	}, &simulation, ""); err != nil {
		t.Fatalf("evaluate service-account project policy: %v", err)
	}
	if len(simulation.Decisions) != 1 || !simulation.Decisions[0].Allowed {
		t.Fatal("VAPPCLOUD_TOKEN must allow project:Create through IAM policy evaluation")
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
		if raw, ok := value[key]; ok && raw != nil {
			if text, ok := raw.(string); ok {
				return text
			}
			return fmt.Sprint(raw)
		}
	}
	return ""
}

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

func TestAccRealAPIVMMLifecycle(t *testing.T) {
	if os.Getenv("VAPPCLOUD_REAL_ACC") != "1" {
		t.Skip("set VAPPCLOUD_REAL_ACC=1 to run credentialed real API acceptance")
	}
	apiURL := os.Getenv("VAPPCLOUD_API_URL")
	token := os.Getenv("VAPPCLOUD_TOKEN")
	projectID := os.Getenv("VAPPCLOUD_REAL_PROJECT_ID")
	deviceID := os.Getenv("VAPPCLOUD_REAL_DEVICE_ID")
	if apiURL == "" || token == "" || projectID == "" || deviceID == "" {
		t.Fatal("VAPPCLOUD_API_URL, VAPPCLOUD_TOKEN, VAPPCLOUD_REAL_PROJECT_ID, and VAPPCLOUD_REAL_DEVICE_ID are required")
	}

	api, err := client.New(apiURL, token, "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("qa-e2e-provider-%d", time.Now().UTC().Unix())
	config := func(cpu int) string {
		return fmt.Sprintf(`
provider "vappcloud" {
  api_url = %q
}
resource "vappcloud_vmm" "qa" {
  project_id          = %q
  device_id           = %q
  name                = %q
  cpu_cores           = %d
  memory_mb           = 2048
  deletion_protection = false
  retain_disk         = false
}`, apiURL, projectID, deviceID, name, cpu)
	}

	var vmmID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy: func(state *terraform.State) error {
			if vmmID == "" {
				return nil
			}
			var vmm client.VMM
			err := api.Do(context.Background(), http.MethodGet, "/v1/vmms/"+client.Escape(vmmID), nil, &vmm, "")
			if err == nil {
				return fmt.Errorf("QA VMM %s still exists", vmmID)
			}
			if !client.IsNotFound(err) {
				return err
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_vmm.qa", "name", name),
					resource.TestCheckResourceAttr("vappcloud_vmm.qa", "is_default", "false"),
					func(state *terraform.State) error {
						managed := state.RootModule().Resources["vappcloud_vmm.qa"]
						if managed == nil || managed.Primary.ID == "" {
							return fmt.Errorf("QA VMM ID was not recorded")
						}
						vmmID = managed.Primary.ID
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					if err := injectLiveVMMDrift(api, vmmID, name+"-drift"); err != nil {
						t.Fatalf("inject controlled VMM drift: %v", err)
					}
				},
				Config: config(4),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_vmm.qa", "name", name),
					resource.TestCheckResourceAttr("vappcloud_vmm.qa", "cpu_cores", "4"),
				),
			},
			{Config: config(4), PlanOnly: true},
			{
				ResourceName:      "vappcloud_vmm.qa",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return projectID + "/" + vmmID, nil
				},
			},
		},
	})
}

func injectLiveVMMDrift(api *client.Client, vmmID, name string) error {
	if vmmID == "" {
		return fmt.Errorf("QA VMM ID is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var current client.VMM
	if err := api.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(vmmID), nil, &current, ""); err != nil {
		return err
	}
	payload := map[string]any{
		"name":                name,
		"cpu_cores":           current.CPUCores,
		"memory_mb":           current.MemoryMB,
		"deletion_protection": false,
		"retain_disk":         false,
		"resource_version":    current.ResourceVersion,
	}
	var mutation client.Mutation[client.VMM]
	key := fmt.Sprintf("qa-drift-%s-%d", vmmID, time.Now().UTC().UnixNano())
	if err := api.Do(ctx, http.MethodPatch, "/v1/vmms/"+client.Escape(vmmID), payload, &mutation, key); err != nil {
		return err
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var observed client.VMM
		if err := api.Do(ctx, http.MethodGet, "/v1/vmms/"+client.Escape(vmmID), nil, &observed, ""); err == nil && observed.Name == name {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for controlled drift: %w", ctx.Err())
		case <-ticker.C:
		}
	}
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

package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

func TestAccVMMResourceCRUDImportAndDrift(t *testing.T) {
	server, api := newAcceptanceServer(t)
	defer server.Close()
	config := func(cpu int, name string) string {
		return fmt.Sprintf(`
provider "vappcloud" {
  token   = "header.payload.signature"
  api_url = %q
}

resource "vappcloud_vmm" "test" {
  project_id          = "prj-test"
  device_id           = "dev-test"
  name                = %q
  cpu_cores           = %d
  memory_mb           = 2048
  deletion_protection = false
  retain_disk         = false
}`, server.URL, name, cpu)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy:             checkAcceptanceDestroy(api),
		Steps: []resource.TestStep{
			{
				Config: config(2, "secondary"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "id", "vmm-secondary"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "observed_revision", "1"),
				),
			},
			{
				PreConfig: func() {
					api.mu.Lock()
					api.vmm = client.VMM{}
					api.mu.Unlock()
				},
				Config: config(2, "secondary"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "id", "vmm-secondary"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "management", "terraform"),
				),
			},
			{
				PreConfig: func() {
					api.mu.Lock()
					api.conflictVMM = true
					api.mu.Unlock()
				},
				Config: config(4, "resized"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: identityStateChecks(
					statecheck.ExpectIdentityValueMatchesState("vappcloud_vmm.test", tfjsonpath.New("id")),
					statecheck.ExpectIdentityValueMatchesState("vappcloud_vmm.test", tfjsonpath.New("project_id")),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "cpu_cores", "4"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "desired_revision", "2"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "observed_revision", "2"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "resource_version", "3"),
				),
			},
			{Config: config(4, "resized"), PlanOnly: true},
			{
				ResourceName:      "vappcloud_vmm.test",
				ImportState:       true,
				ImportStateId:     "prj-test/vmm-secondary",
				ImportStateVerify: true,
			},
			{
				ResourceName:       "vappcloud_vmm.test",
				ImportState:        true,
				ImportStateId:      "prj-test/vmm-default",
				ImportStatePersist: false,
				ExpectError:        regexp.MustCompile("Default VMM cannot be imported"),
			},
		},
	})
}

func TestAccVMMInstanceProfileAttachment(t *testing.T) {
	server, api := newAcceptanceServer(t)
	defer server.Close()
	config := func(profileARN string) string {
		profile := ""
		if profileARN != "" {
			profile = fmt.Sprintf("  instance_profile_arn = %q\n", profileARN)
		}
		return fmt.Sprintf(`
provider "vappcloud" {
  token   = "header.payload.signature"
  api_url = %q
}
resource "vappcloud_vmm" "profile" {
  project_id  = "prj-test"
  device_id   = "dev-test"
  name        = "profile-test"
  cpu_cores   = 2
  memory_mb   = 2048
%s}`, server.URL, profile)
	}
	profileARN := "arn:vapp:iam::3:instance-profile/qa-profile"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy:             checkAcceptanceDestroy(api),
		Steps: []resource.TestStep{
			{
				Config: config(profileARN),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_vmm.profile", "instance_profile_arn", profileARN),
					resource.TestCheckResourceAttr("vappcloud_vmm.profile", "instance_role_arn", "arn:vapp:iam::3:role/qa-role"),
				),
			},
			{
				Config: config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("vappcloud_vmm.profile", "instance_profile_arn"),
					resource.TestCheckNoResourceAttr("vappcloud_vmm.profile", "instance_role_arn"),
				),
			},
		},
	})
}

func TestAccVMMStressTwentyLifecycles(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		t.Run(fmt.Sprintf("iteration-%02d", iteration+1), func(t *testing.T) {
			server, api := newAcceptanceServer(t)
			defer server.Close()
			config := fmt.Sprintf(`
provider "vappcloud" {
  token   = "header.payload.signature"
  api_url = %q
}
resource "vappcloud_vmm" "stress" {
  project_id          = "prj-test"
  device_id           = "dev-test"
  name                = "stress-%02d"
  cpu_cores           = 2
  memory_mb           = 2048
  deletion_protection = false
  retain_disk         = false
}`, server.URL, iteration+1)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: providerFactories(),
				CheckDestroy:             checkAcceptanceDestroy(api),
				Steps: []resource.TestStep{{
					Config: config,
					Check:  resource.TestCheckResourceAttr("vappcloud_vmm.stress", "state", "running"),
				}},
			})
		})
	}
}

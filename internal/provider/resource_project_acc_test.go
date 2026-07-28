package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccProjectResource(t *testing.T) {
	server, api := newAcceptanceServer(t)
	defer server.Close()
	config := fmt.Sprintf(`
provider "vappcloud" {
  token   = "header.payload.signature"
  api_url = %q
}
resource "vappcloud_project" "test" {
  name        = "acceptance"
  description = "created by acceptance"
}`, server.URL)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy:             checkAcceptanceDestroy(api),
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: identityStateChecks(
					statecheck.ExpectIdentityValueMatchesState("vappcloud_project.test", tfjsonpath.New("id")),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_project.test", "id", "prj-test"),
					resource.TestCheckResourceAttr("vappcloud_project.test", "resource_version", "1"),
				),
			},
			{Config: config, PlanOnly: true},
			{
				ResourceName:      "vappcloud_project.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccProjectIdenticalResourcesHaveDistinctIDs(t *testing.T) {
	server, api := newAcceptanceServer(t)
	defer server.Close()
	config := fmt.Sprintf(`
provider "vappcloud" {
  token   = "header.payload.signature"
  api_url = %q
}
resource "vappcloud_project" "identical" {
  count       = 2
  name        = "identical"
  description = "same payload"
}`, server.URL)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy:             checkAcceptanceDestroy(api),
		Steps: []resource.TestStep{{
			Config: config,
			Check: checkResourceAttributesDiffer(
				"vappcloud_project.identical.0",
				"vappcloud_project.identical.1",
				"id",
			),
		}},
	})
}

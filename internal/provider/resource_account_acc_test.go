package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccAccountResource(t *testing.T) {
	server, api := newAcceptanceServer(t)
	defer server.Close()
	config := fmt.Sprintf(`
%s
resource "vappcloud_account" "test" {
  name        = "acceptance"
  description = "created by acceptance"
}`, acceptanceProviderBlock(server.URL))
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
					statecheck.ExpectIdentityValueMatchesState("vappcloud_account.test", tfjsonpath.New("id")),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_account.test", "id", "prj-test"),
					resource.TestCheckResourceAttr("vappcloud_account.test", "resource_version", "1"),
				),
			},
			{Config: config, PlanOnly: true},
			{
				ResourceName:      "vappcloud_account.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAccountIdenticalResourcesHaveDistinctIDs(t *testing.T) {
	server, api := newAcceptanceServer(t)
	defer server.Close()
	config := fmt.Sprintf(`
%s
resource "vappcloud_account" "identical" {
  count       = 2
  name        = "identical"
  description = "same payload"
}`, acceptanceProviderBlock(server.URL))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy:             checkAcceptanceDestroy(api),
		Steps: []resource.TestStep{{
			Config: config,
			Check: checkResourceAttributesDiffer(
				"vappcloud_account.identical.0",
				"vappcloud_account.identical.1",
				"id",
			),
		}},
	})
}

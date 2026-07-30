package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccAllResourcesAndDataSources(t *testing.T) {
	server, api := newAcceptanceServer(t)
	defer server.Close()
	config := func(suffix string, replicas int, optionals bool) string {
		optionalProject := ""
		optionalApplication := ""
		if optionals {
			optionalProject = `description = "updated project"`
			optionalApplication = `
  description = "updated application"
  secret_ids  = ["secret-example"]`
		}
		return fmt.Sprintf(`
provider "vappcloud" {
  token   = "header.payload.signature"
  api_url = %q
}

resource "vappcloud_project" "test" {
  name = "project-%s"
  %s
}

resource "vappcloud_device" "test" {
  project_id = vappcloud_project.test.id
  name       = "device-%s"
}

resource "vappcloud_compute_instance" "test" {
  project_id          = vappcloud_project.test.id
  device_id           = vappcloud_device.test.id
  cloud_connection_id = "connection-test"
  region              = "region-test"
  size                = "size-test"
  image               = "image-test"
  name                = "compute-%s"
}

resource "vappcloud_vmm" "test" {
  project_id          = vappcloud_project.test.id
  device_id           = vappcloud_device.test.id
  name                = "vmm-%s"
  cpu_cores           = 2
  memory_mb           = 2048
  deletion_protection = false
  retain_disk         = false
}

resource "vappcloud_application_instance" "test" {
  project_id = vappcloud_project.test.id
  name       = "application-%s"
  %s
  source = {
    kind                       = "marketplace"
    marketplace_application_id = "catalog-test"
    marketplace_version_id     = "version-test"
  }
  placement = [{
    vmm_id        = vappcloud_vmm.test.id
    replica_count = %d
  }]
}

data "vappcloud_projects" "all" {
  depends_on = [vappcloud_project.test]
}
data "vappcloud_project" "selected" { id = vappcloud_project.test.id }
data "vappcloud_devices" "all" {
  project_id = vappcloud_project.test.id
  depends_on = [vappcloud_device.test]
}
data "vappcloud_device" "selected" { id = vappcloud_device.test.id }
data "vappcloud_compute_instances" "all" {
  project_id = vappcloud_project.test.id
  depends_on = [vappcloud_compute_instance.test]
}
data "vappcloud_compute_instance" "selected" { id = vappcloud_compute_instance.test.id }
data "vappcloud_vmms" "all" {
  project_id = vappcloud_project.test.id
  depends_on = [vappcloud_vmm.test]
}
data "vappcloud_vmm" "selected" { id = vappcloud_vmm.test.id }
data "vappcloud_application_instances" "all" {
  project_id = vappcloud_project.test.id
  depends_on = [vappcloud_application_instance.test]
}
data "vappcloud_application_instance" "selected" { id = vappcloud_application_instance.test.id }
data "vappcloud_operation" "selected" {
  id         = "op-vmm-create-vmm-secondary"
  depends_on = [vappcloud_vmm.test]
}
data "vappcloud_cloud_connections" "all" { project_id = vappcloud_project.test.id }
data "vappcloud_cloud_providers" "all" {}
data "vappcloud_cloud_regions" "all" { cloud_connection_id = "connection-test" }
data "vappcloud_cloud_sizes" "all" {
  cloud_connection_id = "connection-test"
  region              = "region-test"
}
data "vappcloud_cloud_images" "all" {
  cloud_connection_id = "connection-test"
  region              = "region-test"
}
data "vappcloud_marketplace_applications" "all" {}
data "vappcloud_marketplace_versions" "all" { application_id = "catalog-test" }
data "vappcloud_github_connections" "all" { project_id = vappcloud_project.test.id }
data "vappcloud_github_repositories" "all" { github_connection_id = "github-test" }
`, server.URL, suffix, optionalProject, suffix, suffix, suffix, suffix, optionalApplication, replicas)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy:             checkAcceptanceDestroy(api),
		Steps: []resource.TestStep{
			{
				Config: config("initial", 1, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_project.test", "resource_version", "1"),
					resource.TestCheckResourceAttr("vappcloud_device.test", "state", "pending"),
					resource.TestCheckResourceAttr("vappcloud_compute_instance.test", "state", "running"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "management", "terraform"),
					resource.TestCheckResourceAttr("vappcloud_application_instance.test", "desired_replicas", "1"),
					resource.TestCheckResourceAttr("data.vappcloud_projects.all", "projects.#", "1"),
					resource.TestCheckResourceAttr("data.vappcloud_devices.all", "items.#", "1"),
					resource.TestCheckResourceAttr("data.vappcloud_compute_instances.all", "items.#", "1"),
					resource.TestCheckResourceAttr("data.vappcloud_vmms.all", "items.#", "2"),
					resource.TestCheckResourceAttr("data.vappcloud_application_instances.all", "items.#", "1"),
					resource.TestCheckResourceAttr("data.vappcloud_cloud_connections.all", "items.#", "1"),
					resource.TestCheckResourceAttr("data.vappcloud_operation.selected", "state", "succeeded"),
				),
			},
			{
				Config: config("updated", 2, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: identityStateChecks(
					statecheck.ExpectIdentityValueMatchesState("vappcloud_project.test", tfjsonpath.New("id")),
					statecheck.ExpectIdentityValueMatchesState("vappcloud_device.test", tfjsonpath.New("id")),
					statecheck.ExpectIdentityValueMatchesState("vappcloud_compute_instance.test", tfjsonpath.New("id")),
					statecheck.ExpectIdentityValueMatchesState("vappcloud_vmm.test", tfjsonpath.New("id")),
					statecheck.ExpectIdentityValueMatchesState("vappcloud_application_instance.test", tfjsonpath.New("id")),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_project.test", "resource_version", "2"),
					resource.TestCheckResourceAttr("vappcloud_device.test", "resource_version", "2"),
					resource.TestCheckResourceAttr("vappcloud_compute_instance.test", "resource_version", "2"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "resource_version", "2"),
					resource.TestCheckResourceAttr("vappcloud_application_instance.test", "resource_version", "2"),
					resource.TestCheckResourceAttr("vappcloud_application_instance.test", "desired_replicas", "2"),
					resource.TestCheckResourceAttr("vappcloud_application_instance.test", "secret_ids.#", "1"),
				),
			},
			{Config: config("updated", 2, true), PlanOnly: true},
			{
				ResourceName:      "vappcloud_device.test",
				ImportState:       true,
				ImportStateId:     "prj-test/dev-test",
				ImportStateVerify: true,
			},
			{
				ResourceName:      "vappcloud_compute_instance.test",
				ImportState:       true,
				ImportStateId:     "prj-test/compute-test",
				ImportStateVerify: true,
			},
			{
				ResourceName:      "vappcloud_application_instance.test",
				ImportState:       true,
				ImportStateId:     "prj-test/app-test",
				ImportStateVerify: true,
			},
		},
	})
}

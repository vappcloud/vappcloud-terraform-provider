package provider

import (
	"context"
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

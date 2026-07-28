package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type acceptanceAPI struct {
	mu       sync.Mutex
	vmm      client.VMM
	projects map[string]client.Project
}

func newAcceptanceServer(t *testing.T) (*httptest.Server, *acceptanceAPI) {
	t.Helper()
	api := &acceptanceAPI{projects: make(map[string]client.Project)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer header.payload.signature" {
			http.Error(w, `{"code":"UNAUTHENTICATED","message":"missing token"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/projects" && r.Method == http.MethodPost:
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			project := client.Project{ID: "prj-test", Name: fmt.Sprint(in["name"]), Description: fmt.Sprint(in["description"]), ResourceVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			api.projects[project.ID] = project
			_ = json.NewEncoder(w).Encode(client.Mutation[client.Project]{Resource: project})
		case strings.HasPrefix(r.URL.Path, "/v1/projects/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/projects/")
			project, ok := api.projects[id]
			if !ok {
				http.Error(w, `{"code":"NOT_FOUND","message":"project not found"}`, http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(project)
			case http.MethodPatch:
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				project.Name = fmt.Sprint(in["name"])
				project.Description = fmt.Sprint(in["description"])
				project.ResourceVersion++
				project.UpdatedAt = time.Now()
				api.projects[id] = project
				_ = json.NewEncoder(w).Encode(client.Mutation[client.Project]{Resource: project})
			case http.MethodDelete:
				delete(api.projects, id)
				w.WriteHeader(http.StatusNoContent)
			}
		case r.URL.Path == "/v1/vmms" && r.Method == http.MethodPost:
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			api.vmm = client.VMM{
				ID: "vmm-secondary", ProjectID: fmt.Sprint(in["project_id"]), DeviceID: fmt.Sprint(in["device_id"]),
				Name: fmt.Sprint(in["name"]), CPUCores: int64(in["cpu_cores"].(float64)), MemoryMB: int64(in["memory_mb"].(float64)),
				DiskMB: 10240, State: "running", Health: "healthy", Management: "terraform",
				DesiredRevision: 1, ObservedRevision: 1, ResourceVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}
			_ = json.NewEncoder(w).Encode(client.Mutation[client.VMM]{Resource: api.vmm})
		case strings.HasPrefix(r.URL.Path, "/v1/vmms/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/vmms/")
			if id == "vmm-default" && r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(client.VMM{ID: id, ProjectID: "prj-test", DeviceID: "dev-test", IsDefault: true, Management: "system", State: "running"})
				return
			}
			if api.vmm.ID == "" || id != api.vmm.ID {
				http.Error(w, `{"code":"NOT_FOUND","message":"VMM not found"}`, http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(api.vmm)
			case http.MethodPatch:
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				api.vmm.Name = fmt.Sprint(in["name"])
				api.vmm.CPUCores = int64(in["cpu_cores"].(float64))
				api.vmm.MemoryMB = int64(in["memory_mb"].(float64))
				api.vmm.ResourceVersion++
				api.vmm.DesiredRevision++
				api.vmm.ObservedRevision = api.vmm.DesiredRevision
				api.vmm.UpdatedAt = time.Now()
				_ = json.NewEncoder(w).Encode(client.Mutation[client.VMM]{Resource: api.vmm})
			case http.MethodDelete:
				api.vmm = client.VMM{}
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	return server, api
}

func providerFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"vappcloud": providerserver.NewProtocol6WithError(New("test")()),
	}
}

func TestAccProjectResource(t *testing.T) {
	server, _ := newAcceptanceServer(t)
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
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_project.test", "id", "prj-test"),
					resource.TestCheckResourceAttr("vappcloud_project.test", "resource_version", "1"),
				),
			},
			{
				ResourceName:      "vappcloud_project.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

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
		CheckDestroy:             func(_ *terraform.State) error { return nil },
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
				Config: config(4, "resized"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "cpu_cores", "4"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "desired_revision", "2"),
					resource.TestCheckResourceAttr("vappcloud_vmm.test", "observed_revision", "2"),
				),
			},
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

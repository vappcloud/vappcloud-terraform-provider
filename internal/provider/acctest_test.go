package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type acceptanceAPI struct {
	mu          sync.Mutex
	vmm         client.VMM
	projects    map[string]client.Project
	devices     map[string]client.Device
	computes    map[string]client.ComputeInstance
	apps        map[string]client.ApplicationInstance
	operations  map[string]client.Operation
	clock       int64
	conflictVMM bool
	projectSeq  int64
}

func (a *acceptanceAPI) now() time.Time {
	a.clock++
	return time.Unix(1_700_000_000+a.clock, 0).UTC()
}

func requireIdempotency(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") == "" {
		http.Error(w, `{"code":"INVALID_ARGUMENT","message":"missing idempotency key"}`, http.StatusBadRequest)
		return false
	}
	return true
}

func requestVersion(w http.ResponseWriter, in map[string]any, current client.Version) bool {
	raw, ok := in["resource_version"].(string)
	if !ok {
		http.Error(w, `{"code":"INVALID_ARGUMENT","message":"resource_version must use protobuf JSON string encoding"}`, http.StatusBadRequest)
		return false
	}
	if raw != strconv.FormatInt(current.Int64(), 10) {
		http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
		return false
	}
	return true
}

func succeededOperation(api *acceptanceAPI, kind, resourceID string) client.Operation {
	now := api.now()
	op := client.Operation{
		ID:            "op-" + strings.ReplaceAll(kind, ".", "-") + "-" + resourceID,
		CorrelationID: "correlation-" + resourceID, ResourceID: resourceID,
		Kind: kind, State: "succeeded", RequestID: "request-" + resourceID,
		CreatedAt: now, UpdatedAt: now,
	}
	api.operations[op.ID] = op
	return op
}

func newAcceptanceServer(t *testing.T) (*httptest.Server, *acceptanceAPI) {
	t.Helper()
	api := &acceptanceAPI{
		projects: make(map[string]client.Project), devices: make(map[string]client.Device),
		computes: make(map[string]client.ComputeInstance), apps: make(map[string]client.ApplicationInstance),
		operations: make(map[string]client.Operation),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer header.payload.signature" {
			http.Error(w, `{"code":"UNAUTHENTICATED","message":"missing token"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/projects" && r.Method == http.MethodGet:
			items := make([]client.Project, 0, len(api.projects))
			for _, project := range api.projects {
				items = append(items, project)
			}
			if r.URL.Query().Get("page_token") == "" {
				_ = json.NewEncoder(w).Encode(client.Page[client.Project]{Items: items, NextCursor: "projects-page-2"})
			} else {
				_ = json.NewEncoder(w).Encode(client.Page[client.Project]{Items: []client.Project{}})
			}
		case r.URL.Path == "/v1/projects" && r.Method == http.MethodPost:
			if !requireIdempotency(w, r) {
				return
			}
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			now := api.now()
			api.projectSeq++
			id := "prj-test"
			if api.projectSeq > 1 {
				id = fmt.Sprintf("prj-test-%d", api.projectSeq)
			}
			project := client.Project{ID: id, Name: fmt.Sprint(in["name"]), Description: fmt.Sprint(in["description"]), ResourceVersion: 1, CreatedAt: now, UpdatedAt: now}
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
				if !requireIdempotency(w, r) {
					return
				}
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				if !requestVersion(w, in, project.ResourceVersion) {
					return
				}
				project.Name = fmt.Sprint(in["name"])
				project.Description = fmt.Sprint(in["description"])
				project.ResourceVersion++
				project.UpdatedAt = api.now()
				api.projects[id] = project
				_ = json.NewEncoder(w).Encode(client.Mutation[client.Project]{Resource: project})
			case http.MethodDelete:
				if !requireIdempotency(w, r) {
					return
				}
				if r.URL.Query().Get("resource_version") != strconv.FormatInt(project.ResourceVersion.Int64(), 10) {
					http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
					return
				}
				delete(api.projects, id)
				w.WriteHeader(http.StatusNoContent)
			}
		case r.URL.Path == "/v1/devices" && r.Method == http.MethodGet:
			items := make([]client.Device, 0, len(api.devices))
			for _, device := range api.devices {
				items = append(items, device)
			}
			_ = json.NewEncoder(w).Encode(client.Page[client.Device]{Items: items})
		case r.URL.Path == "/v1/devices" && r.Method == http.MethodPost:
			if !requireIdempotency(w, r) {
				return
			}
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			now := api.now()
			device := client.Device{
				ID: "dev-test", ProjectID: fmt.Sprint(in["project_id"]), Name: fmt.Sprint(in["name"]),
				State: "pending", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
			}
			api.devices[device.ID] = device
			_ = json.NewEncoder(w).Encode(client.Mutation[client.Device]{Resource: device})
		case strings.HasPrefix(r.URL.Path, "/v1/devices/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/devices/")
			device, ok := api.devices[id]
			if !ok {
				http.Error(w, `{"code":"NOT_FOUND","message":"device not found"}`, http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(device)
			case http.MethodPatch:
				if !requireIdempotency(w, r) {
					return
				}
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				if !requestVersion(w, in, device.ResourceVersion) {
					return
				}
				device.Name = fmt.Sprint(in["name"])
				device.ResourceVersion++
				device.UpdatedAt = api.now()
				api.devices[id] = device
				_ = json.NewEncoder(w).Encode(client.Mutation[client.Device]{Resource: device})
			case http.MethodDelete:
				if !requireIdempotency(w, r) {
					return
				}
				if r.URL.Query().Get("resource_version") != strconv.FormatInt(device.ResourceVersion.Int64(), 10) {
					http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
					return
				}
				delete(api.devices, id)
				w.WriteHeader(http.StatusNoContent)
			}
		case r.URL.Path == "/v1/compute-instances" && r.Method == http.MethodGet:
			items := make([]client.ComputeInstance, 0, len(api.computes))
			for _, compute := range api.computes {
				items = append(items, compute)
			}
			_ = json.NewEncoder(w).Encode(client.Page[client.ComputeInstance]{Items: items})
		case r.URL.Path == "/v1/compute-instances" && r.Method == http.MethodPost:
			if !requireIdempotency(w, r) {
				return
			}
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			now := api.now()
			compute := client.ComputeInstance{
				ID: "compute-test", ProjectID: fmt.Sprint(in["project_id"]), DeviceID: fmt.Sprint(in["device_id"]),
				CloudConnection: fmt.Sprint(in["cloud_connection_id"]), Region: fmt.Sprint(in["region"]),
				Size: fmt.Sprint(in["size"]), Image: fmt.Sprint(in["image"]), Name: fmt.Sprint(in["name"]),
				State: "running", DefaultVMMID: "vmm-default", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
			}
			api.computes[compute.ID] = compute
			op := succeededOperation(api, "compute.create", compute.ID)
			_ = json.NewEncoder(w).Encode(client.Mutation[client.ComputeInstance]{Resource: compute, Operation: op})
		case strings.HasPrefix(r.URL.Path, "/v1/compute-instances/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/compute-instances/")
			compute, ok := api.computes[id]
			if !ok {
				http.Error(w, `{"code":"NOT_FOUND","message":"compute instance not found"}`, http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(compute)
			case http.MethodPatch:
				if !requireIdempotency(w, r) {
					return
				}
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				if !requestVersion(w, in, compute.ResourceVersion) {
					return
				}
				compute.Name = fmt.Sprint(in["name"])
				compute.ResourceVersion++
				compute.UpdatedAt = api.now()
				api.computes[id] = compute
				op := succeededOperation(api, "compute.update", id)
				_ = json.NewEncoder(w).Encode(client.Mutation[client.ComputeInstance]{Resource: compute, Operation: op})
			case http.MethodDelete:
				if !requireIdempotency(w, r) {
					return
				}
				if r.URL.Query().Get("resource_version") != strconv.FormatInt(compute.ResourceVersion.Int64(), 10) {
					http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
					return
				}
				delete(api.computes, id)
				w.WriteHeader(http.StatusNoContent)
			}
		case r.URL.Path == "/v1/vmms" && r.Method == http.MethodGet:
			items := []client.VMM{{
				ID: "vmm-default", ProjectID: "prj-test", DeviceID: "dev-test", Name: "default",
				IsDefault: true, Management: "system", State: "running", ResourceVersion: 1,
			}}
			if api.vmm.ID != "" {
				items = append(items, api.vmm)
			}
			_ = json.NewEncoder(w).Encode(client.Page[client.VMM]{Items: items})
		case r.URL.Path == "/v1/vmms" && r.Method == http.MethodPost:
			if !requireIdempotency(w, r) {
				return
			}
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			now := api.now()
			api.vmm = client.VMM{
				ID: "vmm-secondary", ProjectID: fmt.Sprint(in["project_id"]), DeviceID: fmt.Sprint(in["device_id"]),
				Name: fmt.Sprint(in["name"]), CPUCores: int64(in["cpu_cores"].(float64)), MemoryMB: int64(in["memory_mb"].(float64)),
				DiskMB: 10240, State: "running", Health: "healthy", Management: "terraform",
				DesiredRevision: 1, ObservedRevision: 1, ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
			}
			op := succeededOperation(api, "vmm.create", api.vmm.ID)
			api.vmm.Operation = op
			_ = json.NewEncoder(w).Encode(client.Mutation[client.VMM]{Resource: api.vmm, Operation: op})
		case strings.HasPrefix(r.URL.Path, "/v1/vmms/") && strings.HasSuffix(r.URL.Path, "/instance-profile"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/vmms/"), "/instance-profile")
			if api.vmm.ID == "" || id != api.vmm.ID {
				http.Error(w, `{"code":"NOT_FOUND","message":"VMM not found"}`, http.StatusNotFound)
				return
			}
			if !requireIdempotency(w, r) {
				return
			}
			switch r.Method {
			case http.MethodPut:
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				if !requestVersion(w, in, api.vmm.ResourceVersion) {
					return
				}
				api.vmm.InstanceProfileID = "profile-test"
				api.vmm.InstanceProfileARN = fmt.Sprint(in["instance_profile_arn"])
				api.vmm.InstanceRoleID = "role-test"
				api.vmm.InstanceRoleARN = "arn:vapp:iam::3:role/qa-role"
				api.vmm.ResourceVersion++
				api.vmm.UpdatedAt = api.now()
				_ = json.NewEncoder(w).Encode(api.vmm)
			case http.MethodDelete:
				if r.URL.Query().Get("resource_version") != strconv.FormatInt(api.vmm.ResourceVersion.Int64(), 10) {
					http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
					return
				}
				api.vmm.InstanceProfileID = ""
				api.vmm.InstanceProfileARN = ""
				api.vmm.InstanceRoleID = ""
				api.vmm.InstanceRoleARN = ""
				api.vmm.ResourceVersion++
				api.vmm.UpdatedAt = api.now()
				_ = json.NewEncoder(w).Encode(api.vmm)
			}
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
				if !requireIdempotency(w, r) {
					return
				}
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				if api.conflictVMM {
					api.conflictVMM = false
					api.vmm.ResourceVersion++
					api.vmm.UpdatedAt = api.now()
				}
				if !requestVersion(w, in, api.vmm.ResourceVersion) {
					return
				}
				api.vmm.Name = fmt.Sprint(in["name"])
				api.vmm.CPUCores = int64(in["cpu_cores"].(float64))
				api.vmm.MemoryMB = int64(in["memory_mb"].(float64))
				api.vmm.ResourceVersion++
				api.vmm.DesiredRevision++
				api.vmm.ObservedRevision = api.vmm.DesiredRevision
				api.vmm.UpdatedAt = api.now()
				op := succeededOperation(api, "vmm.update", api.vmm.ID)
				api.vmm.Operation = op
				_ = json.NewEncoder(w).Encode(client.Mutation[client.VMM]{Resource: api.vmm, Operation: op})
			case http.MethodDelete:
				if !requireIdempotency(w, r) {
					return
				}
				if r.URL.Query().Get("resource_version") != strconv.FormatInt(api.vmm.ResourceVersion.Int64(), 10) {
					http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
					return
				}
				api.vmm = client.VMM{}
				w.WriteHeader(http.StatusNoContent)
			}
		case r.URL.Path == "/v1/application-instances" && r.Method == http.MethodGet:
			items := make([]client.ApplicationInstance, 0, len(api.apps))
			for _, app := range api.apps {
				items = append(items, app)
			}
			_ = json.NewEncoder(w).Encode(client.Page[client.ApplicationInstance]{Items: items})
		case r.URL.Path == "/v1/application-instances" && r.Method == http.MethodPost:
			if !requireIdempotency(w, r) {
				return
			}
			var in struct {
				ProjectID   string                   `json:"project_id"`
				Name        string                   `json:"name"`
				Description string                   `json:"description"`
				Source      client.ApplicationSource `json:"source"`
				Placements  []client.Placement       `json:"placements"`
				SecretIDs   []string                 `json:"secret_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			now := api.now()
			desired := int64(0)
			for _, placement := range in.Placements {
				desired += placement.ReplicaCount
			}
			app := client.ApplicationInstance{
				ID: "app-test", ProjectID: in.ProjectID, Name: in.Name, Description: in.Description,
				Source: in.Source, Placements: in.Placements, SecretIDs: in.SecretIDs,
				State: "running", ReadyReplicas: desired, DesiredReplicas: desired,
				ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
			}
			op := succeededOperation(api, "application.create", app.ID)
			app.Operation = op
			api.apps[app.ID] = app
			_ = json.NewEncoder(w).Encode(client.Mutation[client.ApplicationInstance]{Resource: app, Operation: op})
		case strings.HasPrefix(r.URL.Path, "/v1/application-instances/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/application-instances/")
			app, ok := api.apps[id]
			if !ok {
				http.Error(w, `{"code":"NOT_FOUND","message":"application instance not found"}`, http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(app)
			case http.MethodPatch:
				if !requireIdempotency(w, r) {
					return
				}
				var in struct {
					Name            string             `json:"name"`
					Description     string             `json:"description"`
					Placements      []client.Placement `json:"placements"`
					SecretIDs       []string           `json:"secret_ids"`
					ResourceVersion client.Version     `json:"resource_version"`
				}
				_ = json.NewDecoder(r.Body).Decode(&in)
				if in.ResourceVersion != app.ResourceVersion {
					http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
					return
				}
				app.Name = in.Name
				app.Description = in.Description
				app.Placements = in.Placements
				app.SecretIDs = in.SecretIDs
				app.DesiredReplicas = 0
				for _, placement := range app.Placements {
					app.DesiredReplicas += placement.ReplicaCount
				}
				app.ReadyReplicas = app.DesiredReplicas
				app.ResourceVersion++
				app.UpdatedAt = api.now()
				op := succeededOperation(api, "application.update", app.ID)
				app.Operation = op
				api.apps[id] = app
				_ = json.NewEncoder(w).Encode(client.Mutation[client.ApplicationInstance]{Resource: app, Operation: op})
			case http.MethodDelete:
				if !requireIdempotency(w, r) {
					return
				}
				if r.URL.Query().Get("resource_version") != strconv.FormatInt(app.ResourceVersion.Int64(), 10) {
					http.Error(w, `{"code":"ABORTED","message":"resource version conflict"}`, http.StatusConflict)
					return
				}
				delete(api.apps, id)
				w.WriteHeader(http.StatusNoContent)
			}
		case strings.HasPrefix(r.URL.Path, "/v1/operations/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(r.URL.Path, "/v1/operations/")
			op, ok := api.operations[id]
			if !ok {
				http.Error(w, `{"code":"NOT_FOUND","message":"operation not found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(op)
		case (strings.HasPrefix(r.URL.Path, "/v1/cloud") ||
			strings.HasPrefix(r.URL.Path, "/v1/marketplace") ||
			strings.HasPrefix(r.URL.Path, "/v1/github")) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.Page[client.NamedItem]{Items: []client.NamedItem{{
				ID: "catalog-test", Name: "catalog item", Description: "acceptance fixture",
				State: "available", Metadata: map[string]any{"architecture": "arm64"},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	return server, api
}

func checkResourceAttributesDiffer(first, second, attribute string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		firstResource, ok := state.RootModule().Resources[first]
		if !ok {
			return fmt.Errorf("resource %s not found", first)
		}
		secondResource, ok := state.RootModule().Resources[second]
		if !ok {
			return fmt.Errorf("resource %s not found", second)
		}
		firstValue := firstResource.Primary.Attributes[attribute]
		secondValue := secondResource.Primary.Attributes[attribute]
		if firstValue == "" || secondValue == "" || firstValue == secondValue {
			return fmt.Errorf("%s.%s=%q and %s.%s=%q must be distinct non-empty values", first, attribute, firstValue, second, attribute, secondValue)
		}
		return nil
	}
}

func checkAcceptanceDestroy(api *acceptanceAPI) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		api.mu.Lock()
		defer api.mu.Unlock()
		if len(api.projects) != 0 {
			return fmt.Errorf("%d project resources still exist", len(api.projects))
		}
		if len(api.devices) != 0 {
			return fmt.Errorf("%d device resources still exist", len(api.devices))
		}
		if len(api.computes) != 0 {
			return fmt.Errorf("%d compute resources still exist", len(api.computes))
		}
		if api.vmm.ID != "" {
			return fmt.Errorf("VMM resource %s still exists", api.vmm.ID)
		}
		if len(api.apps) != 0 {
			return fmt.Errorf("%d application resources still exist", len(api.apps))
		}
		return nil
	}
}

func identityStateChecks(checks ...statecheck.StateCheck) []statecheck.StateCheck {
	engine := os.Getenv("TF_ACC_TERRAFORM_PATH")
	if engine == "" {
		engine = "terraform"
	}
	output, err := exec.CommandContext(context.Background(), engine, "version").Output()
	if err != nil {
		return nil
	}
	match := regexp.MustCompile(`v1\.(\d+)`).FindSubmatch(output)
	if len(match) != 2 {
		return nil
	}
	minor, err := strconv.Atoi(string(match[1]))
	if err != nil || minor < 12 {
		return nil
	}
	return checks
}

func providerFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"vappcloud": providerserver.NewProtocol6WithError(New("test")()),
	}
}

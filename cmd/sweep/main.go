package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type sweepTarget struct {
	id       string
	readPath string
	delete   func(client.Version) string
}

func main() {
	if os.Getenv("VAPPCLOUD_SWEEP_CONFIRM") != "1" {
		panic("refusing to sweep: set VAPPCLOUD_SWEEP_CONFIRM=1 after reviewing VAPPCLOUD_SWEEP_IDS")
	}
	ids := strings.FieldsFunc(os.Getenv("VAPPCLOUD_SWEEP_IDS"), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	if len(ids) == 0 {
		panic("VAPPCLOUD_SWEEP_IDS must contain explicit resource IDs")
	}
	apiURL := os.Getenv("VAPPCLOUD_API_URL")
	if apiURL == "" {
		apiURL = "https://api.4lock.net"
	}
	api, err := client.New(apiURL, os.Getenv("VAPPCLOUD_TOKEN"), "sweeper")
	if err != nil {
		panic(err)
	}
	for _, id := range ids {
		target, err := targetForID(id)
		if err != nil {
			panic(err)
		}
		if err := sweep(context.Background(), api, target); err != nil {
			panic(err)
		}
	}
}

func targetForID(id string) (sweepTarget, error) {
	escaped := client.Escape(id)
	versionedDelete := func(path string) func(client.Version) string {
		return func(version client.Version) string {
			return path + "?resource_version=" + strconv.FormatInt(version.Int64(), 10)
		}
	}
	switch {
	case strings.HasPrefix(id, "acc_"):
		path := "/v1/projects/" + escaped
		return sweepTarget{id: id, readPath: path, delete: versionedDelete(path)}, nil
	case strings.HasPrefix(id, "dev_"):
		path := "/v1/devices/" + escaped
		return sweepTarget{id: id, readPath: path, delete: versionedDelete(path)}, nil
	case strings.HasPrefix(id, "vmm-"):
		path := "/v1/vmms/" + escaped
		return sweepTarget{
			id: id, readPath: path,
			delete: func(version client.Version) string {
				return path + "?resource_version=" + strconv.FormatInt(version.Int64(), 10) + "&retain_disk=false"
			},
		}, nil
	case strings.HasPrefix(id, "appinst_"):
		path := "/v1/application-instances/" + escaped
		return sweepTarget{id: id, readPath: path, delete: versionedDelete(path)}, nil
	default:
		return sweepTarget{}, fmt.Errorf("refusing unknown resource ID prefix: %s", id)
	}
}

func sweep(ctx context.Context, api *client.Client, target sweepTarget) error {
	var current struct {
		ResourceVersion client.Version `json:"resourceVersion"`
	}
	if err := api.Do(ctx, http.MethodGet, target.readPath, nil, &current, ""); err != nil {
		if client.IsNotFound(err) {
			fmt.Printf("already absent: %s\n", target.id)
			return nil
		}
		return fmt.Errorf("read %s: %w", target.id, err)
	}
	key, err := client.StableIdempotencyKey("sweep.delete", target.id, current)
	if err != nil {
		return err
	}
	var result client.Mutation[json.RawMessage]
	if err := api.Do(ctx, http.MethodDelete, target.delete(current.ResourceVersion), nil, &result, key); err != nil && !client.IsNotFound(err) {
		return fmt.Errorf("delete %s: %w", target.id, err)
	}
	operationID := result.Operation.ID
	if operationID == "" {
		operationID = result.OperationID
	}
	if operationID != "" {
		if _, err := api.WaitOperation(ctx, operationID, 10*time.Minute); err != nil {
			return fmt.Errorf("wait for deletion of %s: %w", target.id, err)
		}
	}
	fmt.Printf("deleted: %s\n", target.id)
	return nil
}

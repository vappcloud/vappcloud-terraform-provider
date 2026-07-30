package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func operationPath(operationID string) string {
	switch {
	case strings.HasPrefix(operationID, "op-cmp-"):
		return "/v1/compute-operations/" + Escape(operationID)
	case strings.HasPrefix(operationID, "op-app-"):
		return "/v1/application-operations/" + Escape(operationID)
	default:
		return "/v1/operations/" + Escape(operationID)
	}
}

func (c *Client) GetOperation(ctx context.Context, operationID string) (Operation, error) {
	var operation Operation
	err := c.Do(ctx, http.MethodGet, operationPath(operationID), nil, &operation, "")
	return operation, err
}

func (c *Client) WaitOperation(ctx context.Context, operationID string, timeout time.Duration) (Operation, error) {
	if operationID == "" {
		return Operation{State: "succeeded"}, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	delay := time.Second
	for {
		op, err := c.GetOperation(waitCtx, operationID)
		if err != nil {
			return op, err
		}
		tflog.Trace(ctx, "VAppCloud operation poll", map[string]any{
			"operation_id": operationID,
			"state":        op.State,
		})
		switch op.State {
		case "succeeded":
			return op, nil
		case "failed", "cancelled":
			if op.Error != nil {
				return op, op.Error
			}
			return op, fmt.Errorf("operation %s %s", operationID, op.State)
		case "pending", "running":
		default:
			return op, fmt.Errorf("operation %s returned unknown state %q", operationID, op.State)
		}
		if err := c.sleep(waitCtx, delay); err != nil {
			return op, fmt.Errorf("operation %s did not complete: %w", operationID, err)
		}
		if delay < 10*time.Second {
			delay *= 2
		}
	}
}

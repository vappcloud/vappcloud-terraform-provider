package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func (c *Client) WaitOperation(ctx context.Context, operationID string, timeout time.Duration) (Operation, error) {
	if operationID == "" {
		return Operation{State: "succeeded"}, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	delay := time.Second
	for {
		var op Operation
		err := c.Do(waitCtx, http.MethodGet, "/v1/operations/"+Escape(operationID), nil, &op, "")
		if err != nil {
			return op, err
		}
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

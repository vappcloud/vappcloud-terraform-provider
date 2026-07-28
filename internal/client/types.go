package client

import (
	"bytes"
	"encoding/json"
	"time"
)

type APIError struct {
	StatusCode int               `json:"-"`
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	RequestID  string            `json:"request_id"`
	Retryable  bool              `json:"retryable"`
	Details    map[string]string `json:"details,omitempty"`
	RetryAfter time.Duration     `json:"-"`
}

func (e *APIError) Error() string {
	if e.RequestID == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + ": " + e.Message + " (request_id=" + e.RequestID + ")"
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type Operation struct {
	ID            string         `json:"id"`
	CorrelationID string         `json:"correlation_id"`
	ResourceID    string         `json:"resource_id"`
	Kind          string         `json:"kind"`
	State         string         `json:"state"`
	RequestID     string         `json:"request_id,omitempty"`
	Error         *APIError      `json:"error,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Mutation[T any] struct {
	Resource      T         `json:"resource"`
	Operation     Operation `json:"operation"`
	OperationID   string    `json:"operation_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

// UnmarshalJSON accepts both asynchronous lifecycle envelopes and synchronous resource responses. Compute, VMM,
// and application work returns a durable operation; project and device metadata changes complete synchronously.
func (m *Mutation[T]) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if _, wrapped := probe["resource"]; !wrapped {
		return json.Unmarshal(bytes.TrimSpace(data), &m.Resource)
	}
	type mutationAlias Mutation[T]
	return json.Unmarshal(data, (*mutationAlias)(m))
}

type Project struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	ResourceVersion Version   `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Device struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	Name            string    `json:"name"`
	State           string    `json:"state"`
	DefaultVMMID    string    `json:"default_vmm_id,omitempty"`
	ResourceVersion Version   `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ComputeInstance struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	DeviceID        string    `json:"device_id"`
	DefaultVMMID    string    `json:"default_vmm_id,omitempty"`
	CloudConnection string    `json:"cloud_connection_id"`
	Region          string    `json:"region"`
	Size            string    `json:"size"`
	Image           string    `json:"image"`
	Name            string    `json:"name"`
	State           string    `json:"state"`
	ResourceVersion Version   `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type VMM struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	DeviceID           string    `json:"device_id"`
	Name               string    `json:"name"`
	CPUCores           int64     `json:"cpu_cores"`
	MemoryMB           int64     `json:"memory_mb"`
	DiskMB             int64     `json:"disk_mb"`
	DeletionProtection bool      `json:"deletion_protection"`
	RetainDisk         bool      `json:"retain_disk"`
	IsDefault          bool      `json:"is_default"`
	Management         string    `json:"management"`
	State              string    `json:"state"`
	Health             string    `json:"health"`
	DesiredRevision    Version   `json:"desired_revision"`
	ObservedRevision   Version   `json:"observed_revision"`
	ResourceVersion    Version   `json:"resource_version"`
	Operation          Operation `json:"operation"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Placement struct {
	VMMID        string `json:"vmm_id"`
	ReplicaCount int64  `json:"replica_count"`
}

type ApplicationSource struct {
	Kind               string `json:"kind"`
	MarketplaceAppID   string `json:"marketplace_application_id,omitempty"`
	MarketplaceVersion string `json:"marketplace_version_id,omitempty"`
	GitHubConnectionID string `json:"github_connection_id,omitempty"`
	Repository         string `json:"repository,omitempty"`
	Ref                string `json:"ref,omitempty"`
}

type ApplicationInstance struct {
	ID              string            `json:"id"`
	ProjectID       string            `json:"project_id"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Source          ApplicationSource `json:"source"`
	Placements      []Placement       `json:"placements"`
	SecretIDs       []string          `json:"secret_ids,omitempty"`
	State           string            `json:"state"`
	ReadyReplicas   int64             `json:"ready_replicas"`
	DesiredReplicas int64             `json:"desired_replicas"`
	ResourceVersion Version           `json:"resource_version"`
	Operation       Operation         `json:"operation"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type NamedItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	State        string `json:"state,omitempty"`
	Metadata     any    `json:"metadata,omitempty"`
	MetadataJSON string `json:"metadata_json,omitempty"`
}

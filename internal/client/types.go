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
	RequestID  string            `json:"requestId"`
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
	NextCursor string `json:"nextCursor,omitempty"`
}

type Operation struct {
	ID            string         `json:"id"`
	CorrelationID string         `json:"correlationId"`
	ResourceID    string         `json:"resourceId"`
	Kind          string         `json:"kind"`
	State         string         `json:"state"`
	RequestID     string         `json:"requestId,omitempty"`
	Error         *APIError      `json:"error,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type Mutation[T any] struct {
	Resource      T         `json:"resource"`
	Operation     Operation `json:"operation"`
	OperationID   string    `json:"operationId,omitempty"`
	CorrelationID string    `json:"correlationId,omitempty"`
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
	ResourceVersion Version   `json:"resourceVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Device struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	Name            string    `json:"name"`
	State           string    `json:"state"`
	DefaultVMMID    string    `json:"defaultVmmId,omitempty"`
	ResourceVersion Version   `json:"resourceVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type ComputeInstance struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	DeviceID        string    `json:"deviceId"`
	DefaultVMMID    string    `json:"defaultVmmId,omitempty"`
	CloudConnection string    `json:"cloudConnectionId"`
	Region          string    `json:"region"`
	Size            string    `json:"size"`
	Image           string    `json:"image"`
	Name            string    `json:"name"`
	State           string    `json:"state"`
	ResourceVersion Version   `json:"resourceVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type VMM struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"projectId"`
	DeviceID           string    `json:"deviceId"`
	Name               string    `json:"name"`
	CPUCores           int64     `json:"cpuCores"`
	MemoryMB           int64     `json:"memoryMb"`
	DiskMB             int64     `json:"diskMb"`
	DeletionProtection bool      `json:"deletionProtection"`
	RetainDisk         bool      `json:"retainDisk"`
	IsDefault          bool      `json:"isDefault"`
	Management         string    `json:"management"`
	State              string    `json:"state"`
	Health             string    `json:"health"`
	DesiredRevision    Version   `json:"desiredRevision"`
	ObservedRevision   Version   `json:"observedRevision"`
	ResourceVersion    Version   `json:"resourceVersion"`
	Operation          Operation `json:"operation"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type Placement struct {
	VMMID        string `json:"vmmId"`
	ReplicaCount int64  `json:"replicaCount"`
}

type ApplicationSource struct {
	Kind               string `json:"kind"`
	MarketplaceAppID   string `json:"marketplaceApplicationId,omitempty"`
	MarketplaceVersion string `json:"marketplaceVersionId,omitempty"`
	GitHubConnectionID string `json:"githubConnectionId,omitempty"`
	Repository         string `json:"repository,omitempty"`
	Ref                string `json:"ref,omitempty"`
}

type ApplicationInstance struct {
	ID              string            `json:"id"`
	ProjectID       string            `json:"projectId"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Source          ApplicationSource `json:"source"`
	Placements      []Placement       `json:"placements"`
	SecretIDs       []string          `json:"secretIds,omitempty"`
	State           string            `json:"state"`
	ReadyReplicas   int64             `json:"readyReplicas"`
	DesiredReplicas int64             `json:"desiredReplicas"`
	ResourceVersion Version           `json:"resourceVersion"`
	Operation       Operation         `json:"operation"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type NamedItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	State        string `json:"state,omitempty"`
	Metadata     any    `json:"metadata,omitempty"`
	MetadataJSON string `json:"metadataJson,omitempty"`
}

type IAMPolicy struct {
	ID             string    `json:"id"`
	OrganizationID int64     `json:"organizationId"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Managed        bool      `json:"managed"`
	ARN            string    `json:"arn"`
	DefaultVersion string    `json:"defaultVersion"`
	DocumentJSON   string    `json:"documentJson"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type IAMPolicyVersion struct {
	PolicyID     string    `json:"policyId"`
	Version      string    `json:"version"`
	DocumentJSON string    `json:"documentJson"`
	IsDefault    bool      `json:"isDefault"`
	CreatedAt    time.Time `json:"createdAt"`
}

type IAMPolicyAttachment struct {
	PolicyID   string    `json:"policyId"`
	PolicyARN  string    `json:"policyArn"`
	PolicyName string    `json:"policyName"`
	TargetType string    `json:"targetType"`
	TargetID   string    `json:"targetId"`
	CreatedBy  string    `json:"createdBy"`
	CreatedAt  time.Time `json:"createdAt"`
}

type IAMGroup struct {
	ID             string    `json:"id"`
	OrganizationID int64     `json:"organizationId"`
	Name           string    `json:"name"`
	ARN            string    `json:"arn"`
	MemberCount    int64     `json:"memberCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type IAMGroupMembers struct {
	PrincipalIDs []string `json:"principalIds"`
}

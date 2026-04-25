package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func NewValidationError(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

type SourceRef struct {
	HardwareID    string `json:"hardware_id"`
	SessionID     string `json:"session_id,omitempty"`
	SeqLocal      *int   `json:"seq_local,omitempty"`
	FabricShortID *int   `json:"fabric_short_id,omitempty"`
	NetworkRole   string `json:"network_role,omitempty"`
}

type TargetRef struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type DeliverySpec struct {
	RouteClass     string         `json:"route_class,omitempty"`
	AllowRelay     *bool          `json:"allow_relay,omitempty"`
	AllowRedundant *bool          `json:"allow_redundant,omitempty"`
	HopLimit       *int           `json:"hop_limit,omitempty"`
	ExpiresAt      string         `json:"expires_at,omitempty"`
	IngressMeta    map[string]any `json:"ingress_metadata,omitempty"`
}

type MeshMeta struct {
	MeshDomainID       string   `json:"mesh_domain_id,omitempty"`
	OnAirKey           string   `json:"onair_key,omitempty"`
	OriginShortID      *int     `json:"origin_short_id,omitempty"`
	PreviousHopShortID *int     `json:"previous_hop_short_id,omitempty"`
	TTL                *int     `json:"ttl,omitempty"`
	HopCount           *int     `json:"hop_count,omitempty"`
	RouteHint          *int     `json:"route_hint,omitempty"`
	LastHop            string   `json:"last_hop,omitempty"`
	IngressGatewayID   string   `json:"ingress_gateway_id,omitempty"`
	RelayTrace         []string `json:"relay_trace,omitempty"`
}

type AckInfo struct {
	AckPhase       string `json:"ack_phase,omitempty"`
	AckedMessageID string `json:"acked_message_id,omitempty"`
}

type Envelope struct {
	SchemaVersion string         `json:"schema_version"`
	MessageID     string         `json:"message_id"`
	Kind          string         `json:"kind"`
	Priority      string         `json:"priority"`
	EventID       string         `json:"event_id,omitempty"`
	CommandID     string         `json:"command_id,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	OccurredAt    string         `json:"occurred_at,omitempty"`
	Source        SourceRef      `json:"source"`
	Target        TargetRef      `json:"target"`
	Delivery      *DeliverySpec  `json:"delivery,omitempty"`
	MeshMeta      *MeshMeta      `json:"mesh_meta,omitempty"`
	Payload       map[string]any `json:"payload"`
	Ack           *AckInfo       `json:"ack,omitempty"`
}

type Manifest struct {
	HardwareID          string         `json:"hardware_id"`
	DeviceFamily        string         `json:"device_family"`
	DeviceClass         string         `json:"device_class,omitempty"`
	PowerClass          string         `json:"power_class"`
	WakeClass           string         `json:"wake_class"`
	SupportedBearers    []string       `json:"supported_bearers"`
	AllowedNetworkRoles []string       `json:"allowed_network_roles"`
	Firmware            map[string]any `json:"firmware"`
}

type Lease struct {
	RoleLeaseID         string   `json:"role_lease_id"`
	SiteID              string   `json:"site_id"`
	LogicalBindingID    string   `json:"logical_binding_id"`
	FabricShortID       *int     `json:"fabric_short_id,omitempty"`
	MeshDomainID        string   `json:"mesh_domain_id,omitempty"`
	EffectiveRole       string   `json:"effective_role"`
	PrimaryBearer       string   `json:"primary_bearer"`
	FallbackBearer      string   `json:"fallback_bearer,omitempty"`
	PreferredGateways   []string `json:"preferred_gateways,omitempty"`
	PreferredMeshRoots  []string `json:"preferred_mesh_roots,omitempty"`
	PreferredLoRaParent []string `json:"preferred_lora_parents,omitempty"`
}

var validKinds = map[string]struct{}{
	"event":          {},
	"state":          {},
	"command":        {},
	"command_result": {},
	"manifest":       {},
	"lease":          {},
	"file_chunk":     {},
	"fabric_summary": {},
	"heartbeat":      {},
}

var validPriorities = map[string]struct{}{
	"critical": {},
	"control":  {},
	"normal":   {},
	"bulk":     {},
}

var validTargetKinds = map[string]struct{}{
	"node":      {},
	"group":     {},
	"service":   {},
	"host":      {},
	"client":    {},
	"site":      {},
	"broadcast": {},
}

func LoadEnvelope(path string) (*Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func (e *Envelope) Validate() error {
	if e.SchemaVersion == "" {
		return NewValidationError("schema_version is required")
	}
	if e.MessageID == "" {
		return NewValidationError("message_id is required")
	}
	if e.Kind == "" {
		return NewValidationError("kind is required")
	}
	if _, ok := validKinds[e.Kind]; !ok {
		return NewValidationError("kind must be one of event/state/command/command_result/manifest/lease/fabric_summary/heartbeat/file_chunk")
	}
	if e.Priority == "" {
		return NewValidationError("priority is required")
	}
	if _, ok := validPriorities[e.Priority]; !ok {
		return NewValidationError("priority must be one of critical/control/normal/bulk")
	}
	if e.Source.HardwareID == "" {
		return NewValidationError("source.hardware_id is required")
	}
	if err := validateFabricShortID(e.Source.FabricShortID, "source.fabric_short_id"); err != nil {
		return err
	}
	if e.Target.Kind == "" || e.Target.Value == "" {
		return NewValidationError("target.kind and target.value are required")
	}
	if _, ok := validTargetKinds[e.Target.Kind]; !ok {
		return NewValidationError("target.kind must be one of node/group/service/host/client/site/broadcast")
	}
	if e.Payload == nil {
		return NewValidationError("payload is required")
	}
	if e.Kind == "command" && e.CommandID == "" {
		return NewValidationError("command.command_id is required")
	}
	if e.Kind == "command_result" && e.CommandID == "" {
		if value, ok := e.Payload["command_id"].(string); ok && value != "" {
		} else if _, ok := e.Payload["command_token"]; !ok {
			return NewValidationError("command_result.command_id or payload.command_token is required")
		}
	}
	if e.Kind == "file_chunk" {
		if _, ok := e.Payload["chunk_index"]; !ok {
			return NewValidationError("file_chunk.payload.chunk_index is required")
		}
		if _, ok := e.Payload["total_chunks"]; !ok {
			return NewValidationError("file_chunk.payload.total_chunks is required")
		}
	}
	if e.OccurredAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, e.OccurredAt); err != nil {
			return NewValidationError("occurred_at must be RFC3339 or RFC3339Nano")
		}
	}
	if e.Delivery != nil && e.Delivery.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, e.Delivery.ExpiresAt); err != nil {
			return NewValidationError("delivery.expires_at must be RFC3339 or RFC3339Nano")
		}
	}
	if e.Delivery != nil && e.Delivery.HopLimit != nil {
		if *e.Delivery.HopLimit < 0 || *e.Delivery.HopLimit > 8 {
			return NewValidationError("delivery.hop_limit must be between 0 and 8")
		}
	}
	if e.MeshMeta != nil && e.MeshMeta.HopCount != nil && *e.MeshMeta.HopCount < 0 {
		return NewValidationError("mesh_meta.hop_count must be >= 0")
	}
	if e.MeshMeta != nil && e.MeshMeta.TTL != nil && (*e.MeshMeta.TTL < 0 || *e.MeshMeta.TTL > 255) {
		return NewValidationError("mesh_meta.ttl must be between 0 and 255")
	}
	return nil
}

func validateFabricShortID(value *int, field string) error {
	if value == nil {
		return nil
	}
	if *value < 1 || *value > 65535 {
		return NewValidationError("%s must be between 1 and 65535", field)
	}
	return nil
}

func (e *Envelope) DedupeKey() string {
	switch {
	case e.Kind == "event" && e.EventID != "":
		return "event:" + e.EventID
	case e.Kind == "command" && e.CommandID != "":
		return "command:" + e.CommandID
	default:
		return "message:" + e.MessageID
	}
}

package sdk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

type CommandOptions struct {
	CommandID        string
	ServiceLevel     string
	ExpectedDelivery string
	ExpiresAt        time.Time
	IdempotencyKey   string
	RouteClass       string
	AllowRelay       *bool
	AllowRedundant   *bool
	HopLimit         *int
	Priority         string
}

type PersistAck struct {
	AckedMessageID string `json:"acked_message_id"`
	Status         string `json:"status"`
	Duplicate      bool   `json:"duplicate"`
}

type PendingCommandDigest struct {
	TargetHardwareID string `json:"target_hardware_id"`
	PendingCount     int    `json:"pending_count"`
	NewestCommandID  string `json:"newest_command_id,omitempty"`
	ExpiresSoon      bool   `json:"expires_soon"`
	Urgent           bool   `json:"urgent"`
}

type LocalSiteRouterClient struct {
	router   *siterouter.Router
	sourceID string
}

func NewLocalSiteRouterClient(router *siterouter.Router, sourceID string) *LocalSiteRouterClient {
	if sourceID == "" {
		sourceID = "local-client"
	}
	return &LocalSiteRouterClient{
		router:   router,
		sourceID: sourceID,
	}
}

func (c *LocalSiteRouterClient) PublishState(ctx context.Context, hardwareID, stateKey string, payload map[string]any, priority string) (*PersistAck, error) {
	if priority == "" {
		priority = "normal"
	}
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     newMessageID(),
		Kind:          "state",
		Priority:      priority,
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: hardwareID},
		Target:        contracts.TargetRef{Kind: "service", Value: "state"},
		Payload:       mergePayload(map[string]any{"state_key": stateKey}, payload),
	}
	ack, err := c.router.Ingest(ctx, envelope, "sdk-local")
	if err != nil {
		return nil, err
	}
	return fromRouterAck(ack), nil
}

func (c *LocalSiteRouterClient) EmitEvent(ctx context.Context, hardwareID, eventID, service string, payload map[string]any, priority string) (*PersistAck, error) {
	if priority == "" {
		priority = "critical"
	}
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     newMessageID(),
		Kind:          "event",
		Priority:      priority,
		EventID:       eventID,
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: hardwareID},
		Target:        contracts.TargetRef{Kind: "service", Value: service},
		Payload:       clonePayload(payload),
	}
	ack, err := c.router.Ingest(ctx, envelope, "sdk-local")
	if err != nil {
		return nil, err
	}
	return fromRouterAck(ack), nil
}

func (c *LocalSiteRouterClient) IssueCommand(ctx context.Context, targetNode string, payload map[string]any, options CommandOptions) (*PersistAck, int64, error) {
	priority := options.Priority
	if priority == "" {
		priority = "control"
	}
	commandID := options.CommandID
	if commandID == "" {
		commandID = options.IdempotencyKey
	}
	if commandID == "" {
		commandID = fmt.Sprintf("cmd-%d", time.Now().UTC().UnixNano())
	}
	commandToken, err := c.allocateCommandToken(ctx, targetNode, commandID)
	if err != nil {
		return nil, 0, err
	}
	delivery := &contracts.DeliverySpec{
		RouteClass:     options.RouteClass,
		AllowRelay:     options.AllowRelay,
		AllowRedundant: options.AllowRedundant,
		HopLimit:       options.HopLimit,
	}
	if !options.ExpiresAt.IsZero() {
		delivery.ExpiresAt = options.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	commandPayload := clonePayload(payload)
	if options.ServiceLevel != "" {
		commandPayload["service_level"] = options.ServiceLevel
	}
	if options.ExpectedDelivery != "" {
		commandPayload["expected_delivery"] = options.ExpectedDelivery
	}
	if options.IdempotencyKey != "" {
		commandPayload["idempotency_key"] = options.IdempotencyKey
	}
	commandPayload["command_token"] = commandToken
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     newMessageID(),
		Kind:          "command",
		Priority:      priority,
		CommandID:     commandID,
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: c.sourceID},
		Target:        contracts.TargetRef{Kind: "node", Value: targetNode},
		Delivery:      delivery,
		Payload:       commandPayload,
	}
	ack, queueID, err := c.router.IssueCommand(ctx, envelope, "sdk-local", "")
	if err != nil {
		return nil, 0, err
	}
	return fromRouterAck(ack), queueID, nil
}

func (c *LocalSiteRouterClient) RegisterManifest(ctx context.Context, hardwareID string, manifest *contracts.Manifest) error {
	return c.router.UpsertManifest(ctx, hardwareID, manifest)
}

func (c *LocalSiteRouterClient) RegisterLease(ctx context.Context, hardwareID string, lease *contracts.Lease) error {
	return c.router.UpsertLease(ctx, hardwareID, lease)
}

func (c *LocalSiteRouterClient) ObserveCommand(ctx context.Context, commandID string) (string, error) {
	return c.router.CommandState(ctx, commandID)
}

func (c *LocalSiteRouterClient) PendingCommandDigest(ctx context.Context, targetNode string, now time.Time) (*PendingCommandDigest, error) {
	digest, err := c.router.PendingCommandDigest(ctx, targetNode, now)
	if err != nil {
		return nil, err
	}
	return &PendingCommandDigest{
		TargetHardwareID: digest.TargetHardwareID,
		PendingCount:     digest.PendingCount,
		NewestCommandID:  digest.NewestCommandID,
		ExpiresSoon:      digest.ExpiresSoon,
		Urgent:           digest.Urgent,
	}, nil
}

func newMessageID() string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("msg-%d-fallback", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("msg-%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(suffix[:]))
}

func commandTokenForID(commandID string) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(commandID))
	token := int(hasher.Sum32() & 0xFFFF)
	if token == 0 {
		return 1
	}
	return token
}

func (c *LocalSiteRouterClient) allocateCommandToken(ctx context.Context, targetNode, commandID string) (int, error) {
	token := commandTokenForID(commandID)
	for attempts := 0; attempts < 0xFFFF; attempts++ {
		resolved, err := c.router.ResolveCommandIDByTokenForTarget(ctx, targetNode, uint16(token))
		if err != nil {
			return 0, err
		}
		if resolved == "" || resolved == commandID {
			return token, nil
		}
		token++
		if token > 0xFFFF {
			token = 1
		}
	}
	return 0, fmt.Errorf("unable to allocate target-scoped command token for %s on %s", commandID, targetNode)
}

func mergePayload(base map[string]any, payload map[string]any) map[string]any {
	merged := clonePayload(base)
	for key, value := range payload {
		merged[key] = value
	}
	return merged
}

func clonePayload(payload map[string]any) map[string]any {
	cloned := map[string]any{}
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func fromRouterAck(ack *siterouter.PersistAck) *PersistAck {
	if ack == nil {
		return nil
	}
	return &PersistAck{
		AckedMessageID: ack.AckedMessageID,
		Status:         ack.Status,
		Duplicate:      ack.Duplicate,
	}
}

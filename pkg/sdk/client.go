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

type RoutePlanSummary struct {
	QueueID            int64          `json:"queue_id"`
	RouteStatus        string         `json:"route_status"`
	SelectedBearer     string         `json:"selected_bearer,omitempty"`
	SelectedGatewayID  string         `json:"selected_gateway_id,omitempty"`
	NextHopID          string         `json:"next_hop_id,omitempty"`
	FinalTargetID      string         `json:"final_target_id,omitempty"`
	NextHopShortID     *int           `json:"next_hop_short_id,omitempty"`
	FinalTargetShortID *int           `json:"final_target_short_id,omitempty"`
	RouteReason        string         `json:"route_reason,omitempty"`
	PayloadFit         bool           `json:"payload_fit"`
	Detail             map[string]any `json:"detail,omitempty"`
}

type ClientBackend interface {
	IngestEnvelope(ctx context.Context, envelope *contracts.Envelope, ingressID string) (*PersistAck, error)
	IssueCommandEnvelope(ctx context.Context, envelope *contracts.Envelope, ingressID, queueKey string) (*PersistAck, int64, error)
	ExplainRouteEnvelope(ctx context.Context, envelope *contracts.Envelope) (*RoutePlanSummary, error)
	OutboxRoutePlan(ctx context.Context, queueID int64) (*RoutePlanSummary, error)
	UpsertManifest(ctx context.Context, hardwareID string, manifest *contracts.Manifest) error
	UpsertLease(ctx context.Context, hardwareID string, lease *contracts.Lease) error
	RegisterDevice(ctx context.Context, hardwareID string, manifest *contracts.Manifest, lease *contracts.Lease) error
	CommandState(ctx context.Context, commandID string) (string, error)
	PendingCommandDigest(ctx context.Context, targetHardwareID string, now time.Time) (*PendingCommandDigest, error)
	ResolveCommandIDByTokenForTarget(ctx context.Context, targetHardwareID string, token uint16) (string, error)
	Close() error
}

type LocalSiteRouterClient struct {
	backend  ClientBackend
	sourceID string
}

func NewClient(backend ClientBackend, sourceID string) *LocalSiteRouterClient {
	if sourceID == "" {
		sourceID = "local-client"
	}
	return &LocalSiteRouterClient{
		backend:  backend,
		sourceID: sourceID,
	}
}

func OpenLocalSite(dbPath, sourceID string) (*LocalSiteRouterClient, error) {
	router, err := siterouter.Open(dbPath, 3)
	if err != nil {
		return nil, err
	}
	return NewClient(newSiteRouterBackend(router), sourceID), nil
}

func (c *LocalSiteRouterClient) Close() error {
	if c == nil || c.backend == nil {
		return nil
	}
	return c.backend.Close()
}

func (c *LocalSiteRouterClient) SourceID() string {
	if c == nil || c.sourceID == "" {
		return "local-client"
	}
	return c.sourceID
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
	ack, err := c.backend.IngestEnvelope(ctx, envelope, "sdk-local")
	if err != nil {
		return nil, err
	}
	return ack, nil
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
	ack, err := c.backend.IngestEnvelope(ctx, envelope, "sdk-local")
	if err != nil {
		return nil, err
	}
	return ack, nil
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
	if routeClassRequiresCommandToken(options.RouteClass) {
		if _, ok := commandPayload["command_token"]; !ok {
			commandToken, err := c.allocateCommandToken(ctx, targetNode, commandID)
			if err != nil {
				return nil, 0, err
			}
			commandPayload["command_token"] = commandToken
		}
	}
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
	ack, queueID, err := c.backend.IssueCommandEnvelope(ctx, envelope, "sdk-local", "")
	if err != nil {
		return nil, 0, err
	}
	return ack, queueID, nil
}

func (c *LocalSiteRouterClient) RegisterManifest(ctx context.Context, hardwareID string, manifest *contracts.Manifest) error {
	return c.backend.UpsertManifest(ctx, hardwareID, manifest)
}

func (c *LocalSiteRouterClient) RegisterLease(ctx context.Context, hardwareID string, lease *contracts.Lease) error {
	return c.backend.UpsertLease(ctx, hardwareID, lease)
}

func (c *LocalSiteRouterClient) RegisterDevice(ctx context.Context, hardwareID string, manifest *contracts.Manifest, lease *contracts.Lease) error {
	return c.backend.RegisterDevice(ctx, hardwareID, manifest, lease)
}

func (c *LocalSiteRouterClient) ObserveCommand(ctx context.Context, commandID string) (string, error) {
	return c.backend.CommandState(ctx, commandID)
}

func (c *LocalSiteRouterClient) PendingCommandDigest(ctx context.Context, targetNode string, now time.Time) (*PendingCommandDigest, error) {
	return c.backend.PendingCommandDigest(ctx, targetNode, now)
}

func (c *LocalSiteRouterClient) OutboxRoutePlan(ctx context.Context, queueID int64) (*RoutePlanSummary, error) {
	return c.backend.OutboxRoutePlan(ctx, queueID)
}

func (c *LocalSiteRouterClient) ExplainRoute(ctx context.Context, envelope *contracts.Envelope) (*RoutePlanSummary, error) {
	return c.backend.ExplainRouteEnvelope(ctx, envelope)
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

func routeClassRequiresCommandToken(routeClass string) bool {
	return routeClass == "sleepy_tiny_control"
}

func (c *LocalSiteRouterClient) allocateCommandToken(ctx context.Context, targetNode, commandID string) (int, error) {
	token := commandTokenForID(commandID)
	for attempts := 0; attempts < 0xFFFF; attempts++ {
		resolved, err := c.backend.ResolveCommandIDByTokenForTarget(ctx, targetNode, uint16(token))
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

type siteRouterBackend struct {
	router *siterouter.Router
}

func newSiteRouterBackend(router *siterouter.Router) *siteRouterBackend {
	return &siteRouterBackend{router: router}
}

func (b *siteRouterBackend) IngestEnvelope(ctx context.Context, envelope *contracts.Envelope, ingressID string) (*PersistAck, error) {
	ack, err := b.router.Ingest(ctx, envelope, ingressID)
	if err != nil {
		return nil, err
	}
	return fromRouterAck(ack), nil
}

func (b *siteRouterBackend) IssueCommandEnvelope(ctx context.Context, envelope *contracts.Envelope, ingressID, queueKey string) (*PersistAck, int64, error) {
	ack, queueID, err := b.router.IssueCommand(ctx, envelope, ingressID, queueKey)
	if err != nil {
		return nil, 0, err
	}
	return fromRouterAck(ack), queueID, nil
}

func (b *siteRouterBackend) ExplainRouteEnvelope(ctx context.Context, envelope *contracts.Envelope) (*RoutePlanSummary, error) {
	record, err := b.router.PlanOutboundRouteSummary(ctx, envelope)
	if err != nil || record == nil {
		return nil, err
	}
	return routePlanSummaryFromRecord(record), nil
}

func (b *siteRouterBackend) OutboxRoutePlan(ctx context.Context, queueID int64) (*RoutePlanSummary, error) {
	record, err := b.router.OutboxRoutePlan(ctx, queueID)
	if err != nil || record == nil {
		return nil, err
	}
	return routePlanSummaryFromRecord(record), nil
}

func routePlanSummaryFromRecord(record *siterouter.OutboxRoutePlanRecord) *RoutePlanSummary {
	return &RoutePlanSummary{
		QueueID:            record.QueueID,
		RouteStatus:        record.RouteStatus,
		SelectedBearer:     record.SelectedBearer,
		SelectedGatewayID:  record.SelectedGatewayID,
		NextHopID:          record.NextHopID,
		FinalTargetID:      record.FinalTargetID,
		NextHopShortID:     record.NextHopShortID,
		FinalTargetShortID: record.FinalTargetShortID,
		RouteReason:        record.RouteReason,
		PayloadFit:         record.PayloadFit,
		Detail:             record.Detail,
	}
}

func (b *siteRouterBackend) UpsertManifest(ctx context.Context, hardwareID string, manifest *contracts.Manifest) error {
	return b.router.UpsertManifest(ctx, hardwareID, manifest)
}

func (b *siteRouterBackend) UpsertLease(ctx context.Context, hardwareID string, lease *contracts.Lease) error {
	return b.router.UpsertLease(ctx, hardwareID, lease)
}

func (b *siteRouterBackend) RegisterDevice(ctx context.Context, hardwareID string, manifest *contracts.Manifest, lease *contracts.Lease) error {
	return b.router.RegisterDevice(ctx, hardwareID, manifest, lease)
}

func (b *siteRouterBackend) CommandState(ctx context.Context, commandID string) (string, error) {
	return b.router.CommandState(ctx, commandID)
}

func (b *siteRouterBackend) PendingCommandDigest(ctx context.Context, targetHardwareID string, now time.Time) (*PendingCommandDigest, error) {
	digest, err := b.router.PendingCommandDigest(ctx, targetHardwareID, now)
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

func (b *siteRouterBackend) ResolveCommandIDByTokenForTarget(ctx context.Context, targetHardwareID string, token uint16) (string, error) {
	return b.router.ResolveCommandIDByTokenForTarget(ctx, targetHardwareID, token)
}

func (b *siteRouterBackend) Close() error {
	if b == nil || b.router == nil {
		return nil
	}
	return b.router.Close()
}

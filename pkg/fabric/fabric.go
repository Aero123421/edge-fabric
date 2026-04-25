package fabric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Aero123421/edge-fabric/pkg/contracts"
	"github.com/Aero123421/edge-fabric/pkg/sdk"
)

type JSON map[string]any

type Client struct {
	low *sdk.LocalSiteRouterClient
}

type State struct {
	Source   string
	Key      string
	Value    any
	Payload  JSON
	Priority string
}

type EventType string

const (
	EventMotionDetected   EventType = "motion_detected"
	EventLeakDetected     EventType = "leak_detected"
	EventBatteryLow       EventType = "battery_low"
	EventTamper           EventType = "tamper"
	EventThresholdCrossed EventType = "threshold_crossed"
)

type Severity string

const (
	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

type Event struct {
	EventID        string
	IdempotencyKey string
	Source         string
	Type           EventType
	Severity       Severity
	Bucket         int
	Flags          int
	Payload        JSON
	Priority       string
	Service        string
}

type SleepyCommandBuilder struct {
	client    *Client
	target    string
	payload   JSON
	commandID string
	expiresIn time.Duration
}

type SendResult struct {
	MessageID          string
	CommandID          string
	QueueID            int64
	Persisted          bool
	Duplicate          bool
	Status             string
	RouteStatus        string
	SelectedBearer     string
	SelectedGatewayID  string
	NextHopID          string
	FinalTargetID      string
	NextHopShortID     *int
	FinalTargetShortID *int
	RouteReason        string
	PayloadFit         bool
	ReadyToSend        bool
	Warnings           []string
}

type PublishResult struct {
	MessageID string
	Persisted bool
	Duplicate bool
	Status    string
}

type DeviceRegistration struct {
	HardwareID     string
	Profile        DeviceProfile
	ShortID        int
	SiteID         string
	Role           string
	PrimaryBearer  string
	FallbackBearer string
}

type RegistrationOption func(*DeviceRegistration)

func WithRole(role string) RegistrationOption {
	return func(registration *DeviceRegistration) {
		registration.Role = role
	}
}

func WithPrimaryBearer(bearer string) RegistrationOption {
	return func(registration *DeviceRegistration) {
		registration.PrimaryBearer = bearer
	}
}

func WithFallbackBearer(bearer string) RegistrationOption {
	return func(registration *DeviceRegistration) {
		registration.FallbackBearer = bearer
	}
}

func WithSiteID(siteID string) RegistrationOption {
	return func(registration *DeviceRegistration) {
		registration.SiteID = siteID
	}
}

func OpenLocal(dbPath, sourceID string) (*Client, error) {
	low, err := sdk.OpenLocalSite(dbPath, sourceID)
	if err != nil {
		return nil, err
	}
	return &Client{low: low}, nil
}

func (c *Client) Close() error {
	if c == nil || c.low == nil {
		return nil
	}
	return c.low.Close()
}

func (c *Client) PublishState(ctx context.Context, state State) (*sdk.PersistAck, error) {
	result, err := c.PublishStateResult(ctx, state)
	if err != nil {
		return nil, err
	}
	return &sdk.PersistAck{
		AckedMessageID: result.MessageID,
		Status:         result.Status,
		Duplicate:      result.Duplicate,
	}, nil
}

func (c *Client) PublishStateResult(ctx context.Context, state State) (*PublishResult, error) {
	if c == nil || c.low == nil {
		return nil, fmt.Errorf("fabric client is closed")
	}
	if state.Source == "" || state.Key == "" {
		return nil, fmt.Errorf("state source and key are required")
	}
	payload := cloneJSON(state.Payload)
	if state.Value != nil {
		payload["value"] = state.Value
	}
	ack, err := c.low.PublishState(ctx, state.Source, state.Key, payload, state.Priority)
	if err != nil {
		return nil, err
	}
	return publishResultFromAck(ack), nil
}

func (c *Client) EmitEvent(ctx context.Context, event Event) (*sdk.PersistAck, error) {
	result, err := c.EmitEventResult(ctx, event)
	if err != nil {
		return nil, err
	}
	return &sdk.PersistAck{
		AckedMessageID: result.MessageID,
		Status:         result.Status,
		Duplicate:      result.Duplicate,
	}, nil
}

func (c *Client) EmitEventResult(ctx context.Context, event Event) (*PublishResult, error) {
	if c == nil || c.low == nil {
		return nil, fmt.Errorf("fabric client is closed")
	}
	if event.Source == "" || event.Type == "" {
		return nil, fmt.Errorf("event source and type are required")
	}
	service := event.Service
	if service == "" {
		service = "event"
	}
	payload := cloneJSON(event.Payload)
	payload["event_type"] = string(event.Type)
	if event.Severity != "" {
		payload["severity"] = string(event.Severity)
	}
	if event.Bucket != 0 {
		payload["value_bucket"] = event.Bucket
	}
	if event.Flags != 0 {
		payload["flags"] = event.Flags
	}
	if event.IdempotencyKey != "" {
		payload["idempotency_key"] = event.IdempotencyKey
	}
	eventID := event.EventID
	if eventID == "" && event.IdempotencyKey != "" {
		eventID = fmt.Sprintf("%s:%s:%s", event.Source, event.Type, event.IdempotencyKey)
	}
	if eventID == "" {
		eventID = fmt.Sprintf("%s:%s:%d", event.Source, event.Type, time.Now().UTC().UnixNano())
	}
	ack, err := c.low.EmitEvent(ctx, event.Source, eventID, service, payload, event.Priority)
	if err != nil {
		return nil, err
	}
	return publishResultFromAck(ack), nil
}

func (c *Client) SleepyCommand(target string) *SleepyCommandBuilder {
	return &SleepyCommandBuilder{
		client:  c,
		target:  target,
		payload: JSON{},
	}
}

func (b *SleepyCommandBuilder) CommandID(commandID string) *SleepyCommandBuilder {
	b.commandID = commandID
	return b
}

func (b *SleepyCommandBuilder) ThresholdSet(value int) *SleepyCommandBuilder {
	b.payload["command_name"] = "threshold.set"
	b.payload["value"] = value
	return b
}

func (b *SleepyCommandBuilder) MaintenanceAwake() *SleepyCommandBuilder {
	b.payload["command_name"] = "mode.set"
	b.payload["mode"] = "maintenance_awake"
	return b
}

func (b *SleepyCommandBuilder) Deployed() *SleepyCommandBuilder {
	b.payload["command_name"] = "mode.set"
	b.payload["mode"] = "deployed"
	return b
}

func (b *SleepyCommandBuilder) ExpiresIn(duration time.Duration) *SleepyCommandBuilder {
	b.expiresIn = duration
	return b
}

func (b *SleepyCommandBuilder) Send(ctx context.Context) (*sdk.PersistAck, int64, error) {
	result, err := b.SendResult(ctx)
	if err != nil {
		return nil, 0, err
	}
	return &sdk.PersistAck{
		AckedMessageID: result.MessageID,
		Status:         result.Status,
		Duplicate:      result.Duplicate,
	}, result.QueueID, nil
}

func (b *SleepyCommandBuilder) SendResult(ctx context.Context) (*SendResult, error) {
	if b == nil || b.client == nil || b.client.low == nil {
		return nil, fmt.Errorf("fabric sleepy command builder is not attached to a client")
	}
	if b.target == "" {
		return nil, fmt.Errorf("sleepy command target is required")
	}
	if b.payload["command_name"] == nil {
		return nil, fmt.Errorf("sleepy command requires a command builder method")
	}
	options := sdk.CommandOptions{
		CommandID:        b.commandID,
		ServiceLevel:     "eventual_next_poll",
		ExpectedDelivery: "next_poll",
		RouteClass:       "sleepy_tiny_control",
		Priority:         "control",
	}
	if options.CommandID == "" {
		options.CommandID = fmt.Sprintf("cmd-%s-%d", b.target, time.Now().UTC().UnixNano())
	}
	if b.expiresIn > 0 {
		options.ExpiresAt = time.Now().UTC().Add(b.expiresIn)
	}
	ack, queueID, err := b.client.low.IssueCommand(ctx, b.target, map[string]any(b.payload), options)
	if err != nil {
		return nil, classifyFabricError(err)
	}
	result := &SendResult{
		MessageID: ack.AckedMessageID,
		CommandID: options.CommandID,
		QueueID:   queueID,
		Persisted: ack.Status == "persisted",
		Duplicate: ack.Duplicate,
		Status:    ack.Status,
	}
	if queueID != 0 {
		route, err := b.client.low.OutboxRoutePlan(ctx, queueID)
		if err != nil {
			return nil, err
		}
		if route != nil {
			result.RouteStatus = route.RouteStatus
			result.SelectedBearer = route.SelectedBearer
			result.SelectedGatewayID = route.SelectedGatewayID
			result.NextHopID = route.NextHopID
			result.FinalTargetID = route.FinalTargetID
			result.NextHopShortID = route.NextHopShortID
			result.FinalTargetShortID = route.FinalTargetShortID
			result.RouteReason = route.RouteReason
			result.PayloadFit = route.PayloadFit
			result.ReadyToSend = route.RouteStatus == "ready_to_send"
		}
	}
	if err := routeStatusError(result); err != nil {
		return result, err
	}
	return result, nil
}

func (b *SleepyCommandBuilder) Explain(ctx context.Context) (*SendResult, error) {
	envelope, options, err := b.commandEnvelope()
	if err != nil {
		return nil, err
	}
	route, err := b.client.low.ExplainRoute(ctx, envelope)
	if err != nil {
		return nil, classifyFabricError(err)
	}
	result := &SendResult{
		MessageID: envelope.MessageID,
		CommandID: options.CommandID,
	}
	applyRouteSummary(result, route)
	if err := routeStatusError(result); err != nil {
		return result, err
	}
	return result, nil
}

func RegisterDeviceProfile(ctx context.Context, client *Client, hardwareID string, profile DeviceProfile, shortID int, opts ...RegistrationOption) error {
	registration := DeviceRegistration{
		HardwareID: hardwareID,
		Profile:    profile,
		ShortID:    shortID,
	}
	for _, opt := range opts {
		opt(&registration)
	}
	return RegisterDevice(ctx, client, registration)
}

func RegisterDevice(ctx context.Context, client *Client, registration DeviceRegistration) error {
	if client == nil || client.low == nil {
		return fmt.Errorf("fabric client is closed")
	}
	if registration.HardwareID == "" {
		return fmt.Errorf("device hardware_id is required")
	}
	if registration.ShortID < 0 || registration.ShortID > 0xFFFF {
		return fmt.Errorf("fabric short ID must be between 1 and 65535 when set")
	}
	profile := registration.Profile
	manifest := &contracts.Manifest{
		HardwareID:          registration.HardwareID,
		DeviceFamily:        profile.DeviceFamily,
		PowerClass:          profile.PowerClass,
		WakeClass:           profile.WakeClass,
		SupportedBearers:    append([]string(nil), profile.SupportedBearers...),
		AllowedNetworkRoles: append([]string(nil), profile.AllowedRoles...),
		Firmware:            map[string]any{"device_profile": profile.ID},
	}
	if len(profile.AllowedRoles) == 0 || len(profile.SupportedBearers) == 0 {
		return client.low.RegisterDevice(ctx, registration.HardwareID, manifest, nil)
	}
	role := registration.Role
	if role == "" {
		role = profile.AllowedRoles[0]
	}
	primaryBearer := registration.PrimaryBearer
	if primaryBearer == "" {
		primaryBearer = profile.SupportedBearers[0]
	}
	siteID := registration.SiteID
	if siteID == "" {
		siteID = "local"
	}
	lease := &contracts.Lease{
		RoleLeaseID:      "lease-" + registration.HardwareID,
		SiteID:           siteID,
		LogicalBindingID: "binding-" + registration.HardwareID,
		EffectiveRole:    role,
		PrimaryBearer:    primaryBearer,
		FallbackBearer:   registration.FallbackBearer,
	}
	if registration.ShortID > 0 {
		lease.FabricShortID = &registration.ShortID
	}
	return client.low.RegisterDevice(ctx, registration.HardwareID, manifest, lease)
}

func cloneJSON(payload JSON) JSON {
	cloned := JSON{}
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func publishResultFromAck(ack *sdk.PersistAck) *PublishResult {
	if ack == nil {
		return &PublishResult{}
	}
	return &PublishResult{
		MessageID: ack.AckedMessageID,
		Persisted: ack.Status == "persisted",
		Duplicate: ack.Duplicate,
		Status:    ack.Status,
	}
}

func routeStatusError(result *SendResult) error {
	if result == nil || result.RouteStatus == "" || result.RouteStatus == "ready_to_send" {
		return nil
	}
	base := ErrRouteUnavailable
	if result.RouteStatus == "route_pending" {
		base = ErrRoutePending
	}
	reason := classifyRouteReason(result.RouteReason)
	if !errors.Is(reason, base) {
		return errors.Join(base, reason, fmt.Errorf("%s: %s", result.RouteStatus, result.RouteReason))
	}
	return errors.Join(base, fmt.Errorf("%s: %s", result.RouteStatus, result.RouteReason))
}

func (b *SleepyCommandBuilder) commandEnvelope() (*contracts.Envelope, sdk.CommandOptions, error) {
	if b == nil || b.client == nil || b.client.low == nil {
		return nil, sdk.CommandOptions{}, fmt.Errorf("fabric sleepy command builder is not attached to a client")
	}
	if b.target == "" {
		return nil, sdk.CommandOptions{}, fmt.Errorf("sleepy command target is required")
	}
	if b.payload["command_name"] == nil {
		return nil, sdk.CommandOptions{}, fmt.Errorf("sleepy command requires a command builder method")
	}
	options := sdk.CommandOptions{
		CommandID:        b.commandID,
		ServiceLevel:     "eventual_next_poll",
		ExpectedDelivery: "next_poll",
		RouteClass:       "sleepy_tiny_control",
		Priority:         "control",
	}
	if options.CommandID == "" {
		options.CommandID = fmt.Sprintf("cmd-%s-%d", b.target, time.Now().UTC().UnixNano())
	}
	if b.expiresIn > 0 {
		options.ExpiresAt = time.Now().UTC().Add(b.expiresIn)
	}
	delivery := &contracts.DeliverySpec{RouteClass: options.RouteClass}
	if !options.ExpiresAt.IsZero() {
		delivery.ExpiresAt = options.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	payload := cloneJSON(b.payload)
	payload["service_level"] = options.ServiceLevel
	payload["expected_delivery"] = options.ExpectedDelivery
	if _, ok := payload["command_token"]; !ok {
		payload["command_token"] = 1
	}
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     fmt.Sprintf("msg-explain-%d", time.Now().UTC().UnixNano()),
		Kind:          "command",
		Priority:      options.Priority,
		CommandID:     options.CommandID,
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: b.client.lowSourceID()},
		Target:        contracts.TargetRef{Kind: "node", Value: b.target},
		Delivery:      delivery,
		Payload:       map[string]any(payload),
	}
	return envelope, options, nil
}

func (c *Client) lowSourceID() string {
	if c == nil || c.low == nil {
		return "fabric-client"
	}
	return c.low.SourceID()
}

func applyRouteSummary(result *SendResult, route *sdk.RoutePlanSummary) {
	if result == nil || route == nil {
		return
	}
	result.QueueID = route.QueueID
	result.RouteStatus = route.RouteStatus
	result.SelectedBearer = route.SelectedBearer
	result.SelectedGatewayID = route.SelectedGatewayID
	result.NextHopID = route.NextHopID
	result.FinalTargetID = route.FinalTargetID
	result.NextHopShortID = route.NextHopShortID
	result.FinalTargetShortID = route.FinalTargetShortID
	result.RouteReason = route.RouteReason
	result.PayloadFit = route.PayloadFit
	result.ReadyToSend = route.RouteStatus == "ready_to_send"
}

func classifyFabricError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(classifyRouteReason(err.Error()), err)
}

func classifyRouteReason(reason string) error {
	switch {
	case strings.Contains(reason, "payload_too_large") || strings.Contains(reason, "payload exceeds"):
		return ErrPayloadTooLarge
	case strings.Contains(reason, "lease"):
		return ErrLeaseMissing
	case strings.Contains(reason, "manifest"):
		return ErrManifestMissing
	case strings.Contains(reason, "role"):
		return ErrRoleDenied
	case strings.Contains(reason, "forbidden"):
		return ErrBearerForbidden
	case strings.Contains(reason, "representable") || strings.Contains(reason, "compact"):
		return ErrCommandNotRepresentable
	case strings.Contains(reason, "gateway"):
		return ErrGatewayUnavailable
	default:
		return ErrRouteUnavailable
	}
}

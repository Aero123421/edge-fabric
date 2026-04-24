package fabric

import (
	"context"
	"fmt"
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
	Source   string
	Type     EventType
	Severity Severity
	Bucket   int
	Flags    int
	Payload  JSON
	Priority string
	Service  string
}

type SleepyCommandBuilder struct {
	client    *Client
	target    string
	payload   JSON
	commandID string
	expiresIn time.Duration
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
	return c.low.PublishState(ctx, state.Source, state.Key, payload, state.Priority)
}

func (c *Client) EmitEvent(ctx context.Context, event Event) (*sdk.PersistAck, error) {
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
	eventID := fmt.Sprintf("%s:%s:%d", event.Source, event.Type, time.Now().UTC().UnixNano())
	return c.low.EmitEvent(ctx, event.Source, eventID, service, payload, event.Priority)
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
	if b == nil || b.client == nil || b.client.low == nil {
		return nil, 0, fmt.Errorf("fabric sleepy command builder is not attached to a client")
	}
	if b.target == "" {
		return nil, 0, fmt.Errorf("sleepy command target is required")
	}
	if b.payload["command_name"] == nil {
		return nil, 0, fmt.Errorf("sleepy command requires a command builder method")
	}
	options := sdk.CommandOptions{
		CommandID:        b.commandID,
		ServiceLevel:     "eventual_next_poll",
		ExpectedDelivery: "next_poll",
		RouteClass:       "sleepy_tiny_control",
		Priority:         "control",
	}
	if b.expiresIn > 0 {
		options.ExpiresAt = time.Now().UTC().Add(b.expiresIn)
	}
	return b.client.low.IssueCommand(ctx, b.target, map[string]any(b.payload), options)
}

func RegisterDeviceProfile(ctx context.Context, client *Client, hardwareID string, profile DeviceProfile, shortID int) error {
	if client == nil || client.low == nil {
		return fmt.Errorf("fabric client is closed")
	}
	manifest := &contracts.Manifest{
		HardwareID:          hardwareID,
		DeviceFamily:        profile.DeviceFamily,
		PowerClass:          profile.PowerClass,
		WakeClass:           profile.WakeClass,
		SupportedBearers:    append([]string(nil), profile.SupportedBearers...),
		AllowedNetworkRoles: append([]string(nil), profile.AllowedRoles...),
		Firmware:            map[string]any{"device_profile": profile.ID},
	}
	if err := client.low.RegisterManifest(ctx, hardwareID, manifest); err != nil {
		return err
	}
	if len(profile.AllowedRoles) == 0 || len(profile.SupportedBearers) == 0 {
		return nil
	}
	lease := &contracts.Lease{
		RoleLeaseID:      "lease-" + hardwareID,
		SiteID:           "local",
		LogicalBindingID: "binding-" + hardwareID,
		EffectiveRole:    profile.AllowedRoles[0],
		PrimaryBearer:    profile.SupportedBearers[0],
	}
	if shortID > 0 {
		lease.FabricShortID = &shortID
	}
	return client.low.RegisterLease(ctx, hardwareID, lease)
}

func cloneJSON(payload JSON) JSON {
	cloned := JSON{}
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

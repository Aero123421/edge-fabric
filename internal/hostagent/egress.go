package hostagent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

const (
	TransportStatusSent             = "sent"
	TransportStatusAcked            = "acked"
	TransportStatusRetry            = "retry"
	TransportStatusPermanentFailure = "permanent_failure"
)

type OutboxRouter interface {
	LeaseOutbound(context.Context, string, int, time.Duration) ([]siterouter.QueueLease, error)
	MarkSending(context.Context, int64, string) error
	MarkSentOK(context.Context, int64, string) error
	AckOutbound(context.Context, int64, string) error
	RetryOutbound(context.Context, int64, string, string) (string, error)
	MoveToDead(context.Context, int64, string, string) error
	RecoverExpiredLeases(context.Context, time.Time) (int64, error)
	OutboxRoutePlan(context.Context, int64) (*siterouter.OutboxRoutePlanRecord, error)
	RecordOutboundAttempt(context.Context, int64, string, string, string, map[string]any) (*siterouter.OutboundAttempt, error)
	UpdateOutboundAttempt(context.Context, int64, string, map[string]any) error
}

type Transport interface {
	Name() string
	Send(context.Context, OutboundPacket) (*TransportResult, error)
}

type OutboundPacket struct {
	QueueID    int64
	MessageID  string
	WorkerID   string
	Route      *siterouter.OutboxRoutePlanRecord
	Envelope   *contracts.Envelope
	FrameType  byte
	WireFrame  []byte
	EnqueuedBy string
}

type TransportResult struct {
	Status           string         `json:"status"`
	AckPhase         string         `json:"ack_phase,omitempty"`
	Acked            bool           `json:"acked,omitempty"`
	Retryable        bool           `json:"retryable,omitempty"`
	PermanentFailure bool           `json:"permanent_failure,omitempty"`
	Detail           map[string]any `json:"detail,omitempty"`
}

type EgressConfig struct {
	WorkerID      string
	Limit         int
	LeaseDuration time.Duration
	Now           func() time.Time
}

type EgressRunResult struct {
	WorkerID   string             `json:"worker_id"`
	Transport  string             `json:"transport"`
	Recovered  int64              `json:"recovered"`
	Leased     int                `json:"leased"`
	Dispatched int                `json:"dispatched"`
	Sent       int                `json:"sent"`
	Acked      int                `json:"acked"`
	Retrying   int                `json:"retrying"`
	Dead       int                `json:"dead"`
	Items      []EgressItemResult `json:"items"`
}

type EgressItemResult struct {
	QueueID         int64  `json:"queue_id"`
	MessageID       string `json:"message_id"`
	AttemptID       int64  `json:"attempt_id,omitempty"`
	Status          string `json:"status"`
	TransportStatus string `json:"transport_status,omitempty"`
	Error           string `json:"error,omitempty"`
}

func (a *Agent) DispatchOutboundOnce(ctx context.Context, transport Transport, config EgressConfig) (*EgressRunResult, error) {
	router, ok := a.router.(OutboxRouter)
	if !ok {
		return nil, errors.New("host agent router does not support outbound dispatch")
	}
	return DispatchOutboundOnce(ctx, router, transport, config)
}

func DispatchOutboundOnce(ctx context.Context, router OutboxRouter, transport Transport, config EgressConfig) (*EgressRunResult, error) {
	if router == nil {
		return nil, errors.New("outbox router is required")
	}
	if transport == nil {
		return nil, errors.New("transport is required")
	}
	config = normalizeEgressConfig(config)
	result := &EgressRunResult{
		WorkerID:  config.WorkerID,
		Transport: transport.Name(),
	}
	recovered, err := router.RecoverExpiredLeases(ctx, config.Now())
	if err != nil {
		return result, err
	}
	result.Recovered = recovered

	leases, err := router.LeaseOutbound(ctx, config.WorkerID, config.Limit, config.LeaseDuration)
	if err != nil {
		return result, err
	}
	result.Leased = len(leases)
	for _, lease := range leases {
		item := dispatchLease(ctx, router, transport, config.WorkerID, lease)
		result.Items = append(result.Items, item)
		switch item.Status {
		case "acked":
			result.Dispatched++
			result.Sent++
			result.Acked++
		case "sent":
			result.Dispatched++
			result.Sent++
		case "retry":
			result.Dispatched++
			result.Retrying++
		case "dead":
			result.Dispatched++
			result.Dead++
		}
	}
	return result, nil
}

func dispatchLease(ctx context.Context, router OutboxRouter, transport Transport, workerID string, lease siterouter.QueueLease) EgressItemResult {
	item := EgressItemResult{QueueID: lease.QueueID, MessageID: lease.MessageID}
	route, err := router.OutboxRoutePlan(ctx, lease.QueueID)
	if err != nil {
		item.Status = "failed"
		item.Error = err.Error()
		return item
	}
	attempt, err := router.RecordOutboundAttempt(ctx, lease.QueueID, routeBearer(route), routeGateway(route), routePathLabel(route), map[string]any{
		"message_id": lease.MessageID,
		"transport":  transport.Name(),
	})
	if err != nil {
		item.Status = "failed"
		item.Error = err.Error()
		return item
	}
	item.AttemptID = attempt.AttemptID

	if err := router.MarkSending(ctx, lease.QueueID, workerID); err != nil {
		_ = router.UpdateOutboundAttempt(ctx, attempt.AttemptID, "dispatch_failed", map[string]any{"error": err.Error()})
		item.Status = "failed"
		item.Error = err.Error()
		return item
	}
	wireFrame, err := EncodeEnvelopeFrame(lease.Envelope)
	if err != nil {
		detail := map[string]any{"error": err.Error(), "stage": "encode"}
		_ = router.UpdateOutboundAttempt(ctx, attempt.AttemptID, TransportStatusPermanentFailure, detail)
		_ = router.MoveToDead(ctx, lease.QueueID, workerID, "encode_failed")
		item.Status = "dead"
		item.TransportStatus = TransportStatusPermanentFailure
		item.Error = err.Error()
		return item
	}
	transportResult, sendErr := transport.Send(ctx, OutboundPacket{
		QueueID:   lease.QueueID,
		MessageID: lease.MessageID,
		WorkerID:  workerID,
		Route:     route,
		Envelope:  lease.Envelope,
		FrameType: FrameEnvelopeJSON,
		WireFrame: wireFrame,
	})
	return finishDispatch(ctx, router, workerID, lease, attempt.AttemptID, transportResult, sendErr)
}

func finishDispatch(
	ctx context.Context,
	router OutboxRouter,
	workerID string,
	lease siterouter.QueueLease,
	attemptID int64,
	transportResult *TransportResult,
	sendErr error,
) EgressItemResult {
	item := EgressItemResult{QueueID: lease.QueueID, MessageID: lease.MessageID, AttemptID: attemptID}
	status := classifyTransportResult(transportResult, sendErr)
	item.TransportStatus = status
	detail := transportDetail(transportResult, sendErr)
	switch status {
	case TransportStatusAcked:
		if err := router.MarkSentOK(ctx, lease.QueueID, workerID); err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			_ = router.UpdateOutboundAttempt(ctx, attemptID, "dispatch_failed", map[string]any{"error": err.Error()})
			return item
		}
		if err := router.AckOutbound(ctx, lease.QueueID, workerID); err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			_ = router.UpdateOutboundAttempt(ctx, attemptID, "ack_update_failed", map[string]any{"error": err.Error()})
			return item
		}
		_ = router.UpdateOutboundAttempt(ctx, attemptID, TransportStatusAcked, detail)
		item.Status = "acked"
	case TransportStatusSent:
		if err := router.MarkSentOK(ctx, lease.QueueID, workerID); err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			_ = router.UpdateOutboundAttempt(ctx, attemptID, "dispatch_failed", map[string]any{"error": err.Error()})
			return item
		}
		_ = router.UpdateOutboundAttempt(ctx, attemptID, "sent_ok", detail)
		item.Status = "sent"
	case TransportStatusPermanentFailure:
		reason := "transport_permanent_failure"
		if transportResult != nil && transportResult.Detail != nil {
			if value, ok := transportResult.Detail["reason"].(string); ok && value != "" {
				reason = value
			}
		}
		_ = router.UpdateOutboundAttempt(ctx, attemptID, TransportStatusPermanentFailure, detail)
		if err := router.MoveToDead(ctx, lease.QueueID, workerID, reason); err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			return item
		}
		item.Status = "dead"
	case TransportStatusRetry:
		queueStatus, err := router.RetryOutbound(ctx, lease.QueueID, workerID, transportRetryReason(transportResult, sendErr))
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			_ = router.UpdateOutboundAttempt(ctx, attemptID, "dispatch_failed", map[string]any{"error": err.Error()})
			return item
		}
		attemptStatus := TransportStatusRetry
		item.Status = "retry"
		if queueStatus == "dead" {
			attemptStatus = "dead"
			item.Status = "dead"
		}
		_ = router.UpdateOutboundAttempt(ctx, attemptID, attemptStatus, detail)
	default:
		item.Status = "failed"
		item.Error = fmt.Sprintf("unknown transport status: %s", status)
		_ = router.UpdateOutboundAttempt(ctx, attemptID, "dispatch_failed", map[string]any{"error": item.Error})
	}
	if sendErr != nil {
		item.Error = sendErr.Error()
	}
	return item
}

func classifyTransportResult(result *TransportResult, err error) string {
	if result == nil {
		if err != nil {
			return TransportStatusRetry
		}
		return TransportStatusSent
	}
	if result.PermanentFailure || result.Status == TransportStatusPermanentFailure {
		return TransportStatusPermanentFailure
	}
	if result.Retryable || result.Status == TransportStatusRetry || err != nil {
		return TransportStatusRetry
	}
	if result.Acked || result.Status == TransportStatusAcked {
		return TransportStatusAcked
	}
	if result.Status == "" {
		return TransportStatusSent
	}
	return result.Status
}

func transportDetail(result *TransportResult, err error) map[string]any {
	detail := map[string]any{}
	if result != nil {
		for key, value := range result.Detail {
			detail[key] = value
		}
		if result.AckPhase != "" {
			detail["ack_phase"] = result.AckPhase
		}
		if result.Status != "" {
			detail["transport_status"] = result.Status
		}
	}
	if err != nil {
		detail["error"] = err.Error()
	}
	return detail
}

func transportRetryReason(result *TransportResult, err error) string {
	if result != nil {
		if result.Detail != nil {
			if reason, ok := result.Detail["reason"].(string); ok && reason != "" {
				return reason
			}
		}
		if result.Status != "" {
			return result.Status
		}
	}
	if err != nil {
		return "transport_error"
	}
	return "dispatch_retry"
}

func normalizeEgressConfig(config EgressConfig) EgressConfig {
	if config.WorkerID == "" {
		config.WorkerID = "host-agent"
	}
	if config.Limit <= 0 {
		config.Limit = 1
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return config
}

func routeBearer(route *siterouter.OutboxRoutePlanRecord) string {
	if route == nil {
		return ""
	}
	return route.SelectedBearer
}

func routeGateway(route *siterouter.OutboxRoutePlanRecord) string {
	if route == nil {
		return ""
	}
	return route.SelectedGatewayID
}

func routePathLabel(route *siterouter.OutboxRoutePlanRecord) string {
	if route == nil || route.RoutePlan == nil {
		return ""
	}
	return route.RoutePlan.PathLabel
}

type FakeTransport struct {
	mu      sync.Mutex
	name    string
	results []FakeTransportStep
	sent    []OutboundPacket
}

type FakeTransportStep struct {
	Result *TransportResult
	Err    error
}

func NewFakeTransport(steps ...FakeTransportStep) *FakeTransport {
	return &FakeTransport{name: "fake", results: append([]FakeTransportStep(nil), steps...)}
}

func (t *FakeTransport) Name() string {
	if t == nil || t.name == "" {
		return "fake"
	}
	return t.name
}

func (t *FakeTransport) Send(ctx context.Context, packet OutboundPacket) (*TransportResult, error) {
	if err := ctx.Err(); err != nil {
		return &TransportResult{Status: TransportStatusRetry, Retryable: true}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	packet.WireFrame = append([]byte(nil), packet.WireFrame...)
	t.sent = append(t.sent, packet)
	if len(t.results) == 0 {
		return &TransportResult{Status: TransportStatusAcked, Acked: true, AckPhase: "fake_ack"}, nil
	}
	step := t.results[0]
	t.results = t.results[1:]
	return step.Result, step.Err
}

func (t *FakeTransport) SentPackets() []OutboundPacket {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]OutboundPacket, len(t.sent))
	for i, packet := range t.sent {
		packet.WireFrame = append([]byte(nil), packet.WireFrame...)
		out[i] = packet
	}
	return out
}

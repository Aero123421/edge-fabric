package siterouter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

type OutboxItem struct {
	QueueID     int64               `json:"queue_id"`
	MessageID   string              `json:"message_id"`
	TargetKind  string              `json:"target_kind"`
	TargetValue string              `json:"target_value"`
	Priority    string              `json:"priority"`
	Envelope    *contracts.Envelope `json:"envelope"`
}

type DispatchResult struct {
	Status        string         `json:"status"`
	Transport     string         `json:"transport,omitempty"`
	AttemptDetail map[string]any `json:"attempt_detail,omitempty"`
	ErrorReason   string         `json:"error_reason,omitempty"`
	AckID         string         `json:"ack_id,omitempty"`
}

type OutboundDispatcher interface {
	Dispatch(ctx context.Context, item OutboxItem, plan RoutePlan) DispatchResult
}

type DispatchWorker struct {
	Router        *Router
	WorkerID      string
	Dispatcher    OutboundDispatcher
	BatchSize     int
	LeaseDuration time.Duration
}

type DispatchWorkerResult struct {
	RecoveredExpired int64 `json:"recovered_expired"`
	Leased           int64 `json:"leased"`
	Sent             int64 `json:"sent"`
	Acked            int64 `json:"acked"`
	Retried          int64 `json:"retried"`
	Dead             int64 `json:"dead"`
	Failed           int64 `json:"failed"`
}

func (w DispatchWorker) RunOnce(ctx context.Context) (*DispatchWorkerResult, error) {
	if w.Router == nil {
		return nil, errors.New("router is required")
	}
	if w.Dispatcher == nil {
		return nil, errors.New("dispatcher is required")
	}
	workerID := w.WorkerID
	if workerID == "" {
		workerID = "site-router-dispatch"
	}
	batchSize := w.BatchSize
	if batchSize <= 0 {
		batchSize = 8
	}
	leaseDuration := w.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = 15 * time.Second
	}
	result := &DispatchWorkerResult{}
	recovered, err := w.Router.RecoverExpiredLeases(ctx, time.Now().UTC())
	if err != nil {
		return result, err
	}
	result.RecoveredExpired = recovered
	leases, err := w.Router.LeaseOutbound(ctx, workerID, batchSize, leaseDuration)
	if err != nil {
		return result, err
	}
	result.Leased = int64(len(leases))
	for _, lease := range leases {
		if err := w.dispatchLease(ctx, workerID, lease, result); err != nil {
			result.Failed++
		}
	}
	return result, nil
}

func (w DispatchWorker) dispatchLease(ctx context.Context, workerID string, lease QueueLease, result *DispatchWorkerResult) error {
	route, err := w.Router.OutboxRoutePlan(ctx, lease.QueueID)
	if err != nil {
		return err
	}
	if route == nil || route.RoutePlan == nil {
		_, retryErr := w.Router.RetryOutbound(ctx, lease.QueueID, workerID, "route_plan_missing")
		if retryErr != nil {
			return retryErr
		}
		result.Retried++
		return errors.New("route plan missing")
	}
	if err := w.Router.MarkSending(ctx, lease.QueueID, workerID); err != nil {
		return err
	}
	item := outboxItemFromLease(lease)
	attempt, err := w.Router.RecordOutboundAttempt(ctx, lease.QueueID, route.SelectedBearer, route.SelectedGatewayID, route.RoutePlan.PathLabel, map[string]any{
		"worker_id":   workerID,
		"route_class": route.RoutePlan.RouteClass,
	})
	if err != nil {
		return err
	}
	dispatchResult := w.Dispatcher.Dispatch(ctx, item, *route.RoutePlan)
	detail := mergeAttemptDetail(dispatchResult.AttemptDetail, map[string]any{
		"worker_id": workerID,
		"transport": dispatchResult.Transport,
		"ack_id":    dispatchResult.AckID,
	})
	if dispatchResult.ErrorReason != "" {
		detail["error_reason"] = dispatchResult.ErrorReason
	}
	status := dispatchResult.Status
	if status == "" {
		status = "sent"
	}
	switch status {
	case "sent", "sent_ok":
		if err := w.Router.UpdateOutboundAttempt(ctx, attempt.AttemptID, "sent_ok", detail); err != nil {
			return err
		}
		if err := w.Router.MarkSentOK(ctx, lease.QueueID, workerID); err != nil {
			return err
		}
		result.Sent++
		return nil
	case "acked", "acknowledged":
		if err := w.Router.UpdateOutboundAttempt(ctx, attempt.AttemptID, "acked", detail); err != nil {
			return err
		}
		if err := w.Router.AckOutbound(ctx, lease.QueueID, workerID); err != nil {
			return err
		}
		result.Acked++
		return nil
	case "retry":
		queueStatus, err := w.Router.RetryOutbound(ctx, lease.QueueID, workerID, retryReason(dispatchResult))
		if err != nil {
			return err
		}
		attemptStatus := "retry"
		if queueStatus == "dead" {
			attemptStatus = "dead"
			result.Dead++
		} else {
			result.Retried++
		}
		return w.Router.UpdateOutboundAttempt(ctx, attempt.AttemptID, attemptStatus, detail)
	case "permanent_failure", "dead":
		if err := w.Router.UpdateOutboundAttempt(ctx, attempt.AttemptID, "dead", detail); err != nil {
			return err
		}
		if err := w.Router.MoveToDead(ctx, lease.QueueID, workerID, retryReason(dispatchResult)); err != nil {
			return err
		}
		result.Dead++
		return nil
	default:
		queueStatus, err := w.Router.RetryOutbound(ctx, lease.QueueID, workerID, "unknown_dispatch_status:"+status)
		if err != nil {
			return fmt.Errorf("unknown dispatch status %q: %w", status, err)
		}
		if queueStatus == "dead" {
			result.Dead++
			return w.Router.UpdateOutboundAttempt(ctx, attempt.AttemptID, "dead", detail)
		}
		result.Retried++
		return w.Router.UpdateOutboundAttempt(ctx, attempt.AttemptID, "retry", detail)
	}
}

func (r *Router) RetryOutbound(ctx context.Context, queueID int64, workerID, reason string) (string, error) {
	if queueID <= 0 {
		return "", errors.New("queue_id must be > 0")
	}
	if workerID == "" {
		return "", errors.New("worker_id is required")
	}
	if reason == "" {
		reason = "dispatch_retry"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	dead, err := tx.ExecContext(ctx, `
		UPDATE outbox_queue
		SET status = 'dead',
			retry_count = retry_count + 1,
			dead_reason = ?,
			updated_at = ?
		WHERE id = ?
		  AND lease_owner = ?
		  AND status IN ('leased', 'sending', 'sent_ok')
		  AND retry_count + 1 >= ?
	`, reason, now, queueID, workerID, r.maxRetryCount)
	if err != nil {
		return "", err
	}
	if changed, _ := dead.RowsAffected(); changed == 1 {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "dead", nil
	}
	queued, err := tx.ExecContext(ctx, `
		UPDATE outbox_queue
		SET status = 'queued',
			lease_owner = NULL,
			lease_until = NULL,
			retry_count = retry_count + 1,
			dead_reason = NULL,
			updated_at = ?
		WHERE id = ?
		  AND lease_owner = ?
		  AND status IN ('leased', 'sending', 'sent_ok')
		  AND retry_count + 1 < ?
	`, now, queueID, workerID, r.maxRetryCount)
	if err != nil {
		return "", err
	}
	if changed, _ := queued.RowsAffected(); changed != 1 {
		return "", errors.New("queue item is not retryable by this worker")
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return "queued", nil
}

func outboxItemFromLease(lease QueueLease) OutboxItem {
	item := OutboxItem{
		QueueID:   lease.QueueID,
		MessageID: lease.MessageID,
		Envelope:  lease.Envelope,
	}
	if lease.Envelope != nil {
		item.TargetKind = lease.Envelope.Target.Kind
		item.TargetValue = lease.Envelope.Target.Value
		item.Priority = lease.Envelope.Priority
	}
	return item
}

func mergeAttemptDetail(primary, extra map[string]any) map[string]any {
	merged := cloneMap(primary)
	for key, value := range extra {
		if value != "" {
			merged[key] = value
		}
	}
	return merged
}

func retryReason(result DispatchResult) string {
	if result.ErrorReason != "" {
		return result.ErrorReason
	}
	if result.Status != "" {
		return result.Status
	}
	return "dispatch_retry"
}

package siterouter

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

type fakeDispatcher struct {
	result DispatchResult
	fn     func(context.Context, OutboxItem, RoutePlan) DispatchResult
}

func (f fakeDispatcher) Dispatch(ctx context.Context, item OutboxItem, plan RoutePlan) DispatchResult {
	if f.fn != nil {
		return f.fn(ctx, item, plan)
	}
	return f.result
}

func TestDispatchWorkerMarksSendingAndSent(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	queueID := enqueueReadyServiceItem(t, router, "msg-worker-sent", "evt-worker-sent")
	worker := DispatchWorker{
		Router:        router,
		WorkerID:      "worker-dispatch-sent",
		BatchSize:     1,
		LeaseDuration: time.Minute,
		Dispatcher: fakeDispatcher{fn: func(ctx context.Context, item OutboxItem, plan RoutePlan) DispatchResult {
			if item.QueueID != queueID || plan.Bearer != "host_local" {
				t.Fatalf("unexpected dispatch input item=%+v plan=%+v", item, plan)
			}
			if status := queueStatus(t, router, queueID); status != "sending" {
				t.Fatalf("dispatcher must observe sending status, got %s", status)
			}
			return DispatchResult{Status: "sent", Transport: "host_local", AttemptDetail: map[string]any{"handoff": "ok"}}
		}},
	}
	result, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Leased != 1 || result.Sent != 1 || result.Failed != 0 {
		t.Fatalf("unexpected worker result: %+v", result)
	}
	if status := queueStatus(t, router, queueID); status != "sent_ok" {
		t.Fatalf("expected sent_ok queue status, got %s", status)
	}
	attempts, err := router.ListOutboundAttempts(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != "sent_ok" || attempts[0].Bearer != "host_local" {
		t.Fatalf("unexpected attempts: %+v", attempts)
	}
}

func TestDispatchWorkerRetriesThenSends(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	queueID := enqueueReadyServiceItem(t, router, "msg-worker-retry", "evt-worker-retry")
	worker := DispatchWorker{
		Router:        router,
		WorkerID:      "worker-dispatch-retry",
		BatchSize:     1,
		LeaseDuration: time.Minute,
		Dispatcher: fakeDispatcher{result: DispatchResult{
			Status:      "retry",
			Transport:   "host_local",
			ErrorReason: "transient_link_loss",
		}},
	}
	result, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Retried != 1 || result.Dead != 0 {
		t.Fatalf("unexpected retry result: %+v", result)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["queued_count"] != 1 || metrics["retry_count"] != 1 {
		t.Fatalf("expected queued retry_count=1, got %+v", metrics)
	}

	worker.Dispatcher = fakeDispatcher{result: DispatchResult{Status: "sent", Transport: "host_local"}}
	result, err = worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 1 {
		t.Fatalf("expected second run to send, got %+v", result)
	}
	attempts, err := router.ListOutboundAttempts(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].Status != "retry" || attempts[1].Status != "sent_ok" {
		t.Fatalf("unexpected retry attempts: %+v", attempts)
	}
}

func TestDispatchWorkerDeadLettersAfterRetryExhaustion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-router.db")
	router, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	ctx := context.Background()
	queueID := enqueueReadyServiceItem(t, router, "msg-worker-dead", "evt-worker-dead")
	worker := DispatchWorker{
		Router:        router,
		WorkerID:      "worker-dispatch-dead",
		BatchSize:     1,
		LeaseDuration: time.Minute,
		Dispatcher: fakeDispatcher{result: DispatchResult{
			Status:      "retry",
			Transport:   "host_local",
			ErrorReason: "target_unreachable_permanent",
		}},
	}
	result, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dead != 1 || result.Retried != 0 {
		t.Fatalf("expected dead-letter result, got %+v", result)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["dead_count"] != 1 || metrics["dead_reason_target_unreachable_permanent_count"] != 1 {
		t.Fatalf("unexpected dead metrics: %+v", metrics)
	}
	if status := queueStatus(t, router, queueID); status != "dead" {
		t.Fatalf("expected dead queue status, got %s", status)
	}
}

func enqueueReadyServiceItem(t *testing.T, router *Router, messageID, eventID string) int64 {
	t.Helper()
	queueID, err := router.EnqueueOutbound(context.Background(), &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     messageID,
		Kind:          "event",
		Priority:      "normal",
		EventID:       eventID,
		Source:        contracts.SourceRef{HardwareID: "sensor-" + eventID},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"event_type": "worker_test"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	return queueID
}

func queueStatus(t *testing.T, router *Router, queueID int64) string {
	t.Helper()
	var status string
	if err := router.db.QueryRowContext(context.Background(), `SELECT status FROM outbox_queue WHERE id = ?`, queueID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

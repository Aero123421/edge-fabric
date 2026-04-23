package sdk

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Aero123421/edge-fabric/internal/siterouter"
)

func openRouter(t *testing.T) *siterouter.Router {
	t.Helper()
	router, err := siterouter.Open(filepath.Join(t.TempDir(), "site-router.db"), 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = router.Close()
	})
	return router
}

func TestIssueCommandBuildsPendingDigest(t *testing.T) {
	router := openRouter(t)
	client := NewLocalSiteRouterClient(router, "controller-sdk")
	ack, queueID, err := client.IssueCommand(context.Background(), "sleepy-sdk-01", map[string]any{
		"command_name": "mode.set",
		"mode":         "eco",
	}, CommandOptions{
		ServiceLevel:     "eventual_next_poll",
		ExpectedDelivery: "next_poll",
		IdempotencyKey:   "cmd-sdk-001",
		ExpiresAt:        time.Now().UTC().Add(10 * time.Minute),
		RouteClass:       "sleepy_tiny_control",
		Priority:         "control",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "persisted" || queueID == 0 {
		t.Fatalf("unexpected issue result: ack=%+v queueID=%d", ack, queueID)
	}
	state, err := client.ObserveCommand(context.Background(), "cmd-sdk-001")
	if err != nil {
		t.Fatal(err)
	}
	if state != "issued" {
		t.Fatalf("expected issued state, got %s", state)
	}
	digest, err := client.PendingCommandDigest(context.Background(), "sleepy-sdk-01", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if digest.PendingCount != 1 || digest.NewestCommandID != "cmd-sdk-001" {
		t.Fatalf("unexpected digest: %+v", digest)
	}
}

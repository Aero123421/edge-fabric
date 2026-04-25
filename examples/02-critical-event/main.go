package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Aero123421/edge-fabric/pkg/fabric"
)

func main() {
	ctx := context.Background()
	dbPath := filepath.Join(os.TempDir(), "edge-fabric-critical-event.db")
	_ = os.Remove(dbPath)
	client, err := fabric.OpenLocal(dbPath, "example-critical-event")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	result, err := client.EmitEventResult(ctx, fabric.Event{
		EventID:  "evt-example-motion-01",
		Source:   "motion-example-01",
		Type:     fabric.EventMotionDetected,
		Severity: fabric.Critical,
		Bucket:   3,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("event persisted: message_id=%s duplicate=%t\n", result.MessageID, result.Duplicate)
}

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
	dbPath := filepath.Join(os.TempDir(), "edge-fabric-basic-state.db")
	_ = os.Remove(dbPath)
	client, err := fabric.OpenLocal(dbPath, "example-basic-state")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	result, err := client.PublishStateResult(ctx, fabric.State{
		Source: "temp-example-01",
		Key:    "temperature.c",
		Value:  24.5,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("state persisted: message_id=%s status=%s\n", result.MessageID, result.Status)
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Aero123421/edge-fabric/internal/hostagent"
	"github.com/Aero123421/edge-fabric/internal/protocol/usbcdc"
	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func main() {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "edge-fabric-sleepy-demo-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir)

	router, err := siterouter.Open(filepath.Join(tempDir, "sleepy-cycle-demo.db"), 3)
	if err != nil {
		panic(err)
	}
	defer router.Close()

	agent := hostagent.New(router, filepath.Join(tempDir, "sleepy-cycle-demo.spool.jsonl"))
	command, err := contracts.LoadEnvelope(filepath.Join("contracts", "fixtures", "command-sleepy-threshold-set.json"))
	if err != nil {
		panic(err)
	}
	ack, queueID, err := router.IssueCommand(ctx, command, "local", "")
	if err != nil {
		panic(err)
	}
	digest, err := router.PendingCommandDigest(ctx, "sleepy-leaf-01", time.Now().UTC())
	if err != nil {
		panic(err)
	}
	digestFrame, err := usbcdc.EncodeFrame(hostagent.FrameSummaryBinary, []byte("D|sleepy-leaf-01|1"))
	if err != nil {
		panic(err)
	}
	pollFrame, err := usbcdc.EncodeFrame(hostagent.FrameSummaryBinary, []byte("P|sleepy-leaf-01|TP|ENP"))
	if err != nil {
		panic(err)
	}
	resultFrame, err := usbcdc.EncodeFrame(
		hostagent.FrameSummaryBinary,
		[]byte("R|sleepy-leaf-01|cmd-sleepy-threshold-001|succeeded|ok"),
	)
	if err != nil {
		panic(err)
	}
	digestResult, err := agent.RelayUSBFrame(ctx, "gateway-usb-01", "sleepy-session-01", digestFrame, nil)
	if err != nil {
		panic(err)
	}
	pollResult, err := agent.RelayUSBFrame(ctx, "gateway-usb-01", "sleepy-session-01", pollFrame, nil)
	if err != nil {
		panic(err)
	}
	commandResult, err := agent.RelayUSBFrame(ctx, "gateway-usb-01", "sleepy-session-01", resultFrame, nil)
	if err != nil {
		panic(err)
	}
	commandState, err := router.CommandState(ctx, "cmd-sleepy-threshold-001")
	if err != nil {
		panic(err)
	}

	fmt.Println("issued command ack:", ack.Status, "queue_id:", queueID)
	fmt.Printf("pending digest: %+v\n", digest)
	fmt.Println("digest frame relay:", digestResult.Status)
	fmt.Println("poll frame relay:", pollResult.Status)
	fmt.Println("command result relay:", commandResult.Status)
	fmt.Println("command state:", commandState)
}

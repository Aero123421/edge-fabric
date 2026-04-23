package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Aero123421/edge-fabric/internal/hostagent"
	"github.com/Aero123421/edge-fabric/internal/protocol/onair"
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
	manifestData, err := os.ReadFile(filepath.Join("contracts", "fixtures", "manifest-sleepy-leaf.json"))
	if err != nil {
		panic(err)
	}
	var manifest contracts.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		panic(err)
	}
	if err := router.UpsertManifest(ctx, manifest.HardwareID, &manifest); err != nil {
		panic(err)
	}
	leaseData, err := os.ReadFile(filepath.Join("contracts", "fixtures", "lease-sleepy-leaf.json"))
	if err != nil {
		panic(err)
	}
	var lease contracts.Lease
	if err := json.Unmarshal(leaseData, &lease); err != nil {
		panic(err)
	}
	if err := router.UpsertLease(ctx, manifest.HardwareID, &lease); err != nil {
		panic(err)
	}
	command, err := contracts.LoadEnvelope(filepath.Join("contracts", "fixtures", "command-sleepy-threshold-set.json"))
	if err != nil {
		panic(err)
	}
	ack, queueID, err := router.IssueCommand(ctx, command, "local", "")
	if err != nil {
		panic(err)
	}
	digest, err := router.PendingCommandDigest(ctx, manifest.HardwareID, time.Now().UTC())
	if err != nil {
		panic(err)
	}
	shortID := uint16(*lease.FabricShortID)
	commandToken, ok := command.Payload["command_token"].(float64)
	if !ok {
		panic("command fixture is missing payload.command_token")
	}
	digestWire, err := onair.EncodePendingDigest(shortID, true, onair.PendingDigestBody{
		PendingCount: uint8(digest.PendingCount),
		Flags:        pendingDigestFlags(digest),
	})
	if err != nil {
		panic(err)
	}
	digestFrame, err := usbcdc.EncodeFrame(hostagent.FrameSummaryBinary, digestWire)
	if err != nil {
		panic(err)
	}
	pollWire, err := onair.EncodeTinyPoll(shortID, onair.TinyPollBody{
		ServiceLevel: onair.ServiceLevelEventualNextPoll,
	})
	if err != nil {
		panic(err)
	}
	pollFrame, err := usbcdc.EncodeFrame(hostagent.FrameCompactBinary, pollWire)
	if err != nil {
		panic(err)
	}
	resultWire, err := onair.EncodeCommandResult(shortID, false, onair.CommandResultBody{
		CommandToken: uint16(commandToken),
		PhaseToken:   onair.PhaseSucceeded,
		ReasonToken:  onair.ReasonOK,
	})
	if err != nil {
		panic(err)
	}
	resultFrame, err := usbcdc.EncodeFrame(hostagent.FrameCompactBinary, resultWire)
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
	commandState, err := router.CommandState(ctx, command.CommandID)
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

func pendingDigestFlags(digest *siterouter.PendingCommandDigest) byte {
	if digest == nil {
		return 0
	}
	flags := byte(0)
	if digest.Urgent {
		flags |= onair.PendingFlagUrgent
	}
	if digest.ExpiresSoon {
		flags |= onair.PendingFlagExpiresSoon
	}
	return flags
}

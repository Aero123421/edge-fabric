package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Aero123421/edge-fabric/internal/hostagent"
	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func main() {
	ctx := context.Background()
	router, err := siterouter.Open(filepath.Join(".", "direct-slice-demo.db"), 3)
	if err != nil {
		panic(err)
	}
	defer router.Close()
	agent := hostagent.New(router, filepath.Join(".", "direct-slice-demo.spool.jsonl"))

	event, err := contracts.LoadEnvelope(filepath.Join("contracts", "fixtures", "event-battery-alert.json"))
	if err != nil {
		panic(err)
	}
	state, err := contracts.LoadEnvelope(filepath.Join("contracts", "fixtures", "state-powered-level.json"))
	if err != nil {
		panic(err)
	}
	frame, err := hostagent.EncodeEnvelopeFrame(event)
	if err != nil {
		panic(err)
	}
	usbResult, err := agent.RelayUSBFrame(ctx, "gateway-usb-01", "usb-session-01", frame, map[string]any{
		"rssi":      -110,
		"snr":       7.5,
		"hop_count": 0,
	})
	if err != nil {
		panic(err)
	}
	wifiResult, err := agent.RelayDirectIP(ctx, "wifi-direct-01", "wifi-session-01", state, map[string]any{"rssi": -48})
	if err != nil {
		panic(err)
	}
	latestState, err := router.LatestState(ctx, "powered-leaf-01", "tank.level")
	if err != nil {
		panic(err)
	}
	eventCount, err := router.CountEvents(ctx)
	if err != nil {
		panic(err)
	}
	diag, err := agent.Diagnostics()
	if err != nil {
		panic(err)
	}
	fmt.Println("usb relay:", usbResult.Status, usbResult.Ack)
	fmt.Println("wifi relay:", wifiResult.Status, wifiResult.Ack)
	fmt.Println("latest state:", latestState)
	fmt.Println("event count:", eventCount)
	fmt.Println("agent diagnostics:", diag)
}

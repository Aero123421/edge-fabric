package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Aero123421/edge-fabric/internal/hostagent"
	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "host-agent: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dbPath    = flag.String("db", "site-router.db", "SQLite database path")
		spoolPath = flag.String("spool", "host-agent-spool.jsonl", "host-agent spool path")
		maxRetry  = flag.Int("max-retry", 3, "max outbound retry count before dead-letter")
		mode      = flag.String("mode", "direct-json", "relay mode: direct-json|usb-envelope-json|usb-frame-binary|heartbeat-json|flush-spool|diagnostics")
		inputPath = flag.String("input", "", "input file path")
		ingressID = flag.String("ingress-id", "local-host-agent", "ingress id")
		sessionID = flag.String("session-id", "session-local-001", "session id")
	)
	flag.Parse()

	if *inputPath == "" && *mode != "flush-spool" && *mode != "diagnostics" {
		return fmt.Errorf("-input is required")
	}

	ctx := context.Background()
	if *mode == "diagnostics" {
		agent := hostagent.New(nil, *spoolPath)
		diagnostics, err := agent.Diagnostics()
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"diagnostics": diagnostics,
		})
	}

	router, err := siterouter.Open(*dbPath, *maxRetry)
	if err != nil {
		return err
	}
	defer router.Close()

	agent := hostagent.New(router, *spoolPath)

	var relayResult *hostagent.RelayResult
	switch *mode {
	case "direct-json":
		envelope, err := contracts.LoadEnvelope(*inputPath)
		if err != nil {
			return err
		}
		relayResult, err = agent.RelayDirectIP(ctx, *ingressID, *sessionID, envelope, nil)
		if err != nil {
			return err
		}
	case "usb-envelope-json":
		envelope, err := contracts.LoadEnvelope(*inputPath)
		if err != nil {
			return err
		}
		frame, err := hostagent.EncodeEnvelopeFrame(envelope)
		if err != nil {
			return err
		}
		relayResult, err = agent.RelayUSBFrame(ctx, *ingressID, *sessionID, frame, nil)
		if err != nil {
			return err
		}
	case "usb-frame-binary":
		frame, err := os.ReadFile(*inputPath)
		if err != nil {
			return err
		}
		relayResult, err = agent.RelayUSBFrame(ctx, *ingressID, *sessionID, frame, nil)
		if err != nil {
			return err
		}
	case "heartbeat-json":
		payload, err := loadGenericJSON(*inputPath)
		if err != nil {
			return err
		}
		frame, err := hostagent.EncodeHeartbeatFrame(payload)
		if err != nil {
			return err
		}
		relayResult, err = agent.RelayUSBFrame(ctx, *ingressID, *sessionID, frame, nil)
		if err != nil {
			return err
		}
	case "flush-spool":
		flushed, err := agent.FlushSpool(ctx)
		if err != nil {
			return err
		}
		diagnostics, err := agent.Diagnostics()
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"flushed":     flushed,
			"diagnostics": diagnostics,
		})
	default:
		return fmt.Errorf("unsupported mode: %s", *mode)
	}

	diagnostics, err := agent.Diagnostics()
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"result":      relayResult,
		"diagnostics": diagnostics,
	})
}

func loadGenericJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}

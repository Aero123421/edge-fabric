package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Aero123421/edge-fabric/internal/protocol/onair"
	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "edge-fabric: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "doctor":
		return printJSON(map[string]any{
			"status": "ok",
			"commands": []string{
				"edge-fabric explain-route -fixture contracts/fixtures/command-sleepy-threshold-set.json",
				"edge-fabric decode-onair -hex <hex-frame>",
			},
		})
	case "explain-route":
		return explainRoute(args[1:])
	case "decode-onair":
		return decodeOnAir(args[1:])
	default:
		return usage()
	}
}

func usage() error {
	return fmt.Errorf("usage: edge-fabric doctor | explain-route -fixture <envelope.json> [-db site-router.db] | decode-onair -hex <hex-frame>")
}

func explainRoute(args []string) error {
	fs := flag.NewFlagSet("explain-route", flag.ContinueOnError)
	dbPath := fs.String("db", "site-router.db", "SQLite database path")
	fixturePath := fs.String("fixture", "", "envelope fixture path")
	maxRetry := fs.Int("max-retry", 3, "max outbound retry count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fixturePath == "" {
		return fmt.Errorf("-fixture is required")
	}
	envelope, err := contracts.LoadEnvelope(*fixturePath)
	if err != nil {
		return err
	}
	router, err := siterouter.Open(*dbPath, *maxRetry)
	if err != nil {
		return err
	}
	defer router.Close()
	plan, err := router.PlanOutboundRoute(context.Background(), envelope)
	if err != nil {
		return printJSON(map[string]any{
			"message_id":  envelope.MessageID,
			"target":      envelope.Target,
			"route_class": routeClass(envelope),
			"allowed":     false,
			"error":       err.Error(),
		})
	}
	return printJSON(map[string]any{
		"message_id":   envelope.MessageID,
		"target":       envelope.Target,
		"route_class":  plan.RouteClass,
		"allowed":      plan.PayloadFit && plan.Bearer != "" && plan.Bearer != "unplanned",
		"route_status": routeStatus(plan),
		"plan":         plan,
	})
}

func decodeOnAir(args []string) error {
	fs := flag.NewFlagSet("decode-onair", flag.ContinueOnError)
	hexFrame := fs.String("hex", "", "hex encoded on-air frame")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *hexFrame == "" {
		return fmt.Errorf("-hex is required")
	}
	frame, err := hex.DecodeString(strings.TrimSpace(*hexFrame))
	if err != nil {
		return err
	}
	packet, err := onair.Decode(frame)
	if err != nil {
		return err
	}
	body, err := decodeBody(packet)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"version":         packet.Version,
		"logical_type":    packet.LogicalType,
		"type_name":       typeName(packet.LogicalType),
		"summary":         packet.Summary(),
		"sequence":        packet.Sequence,
		"source_short_id": packet.SourceShortID,
		"target_short_id": packet.TargetShortID,
		"body":            body,
	})
}

func decodeBody(packet *onair.Packet) (map[string]any, error) {
	switch packet.LogicalType {
	case onair.TypeState:
		body, err := onair.DecodeState(packet)
		if err != nil {
			return nil, err
		}
		return map[string]any{"key_token": body.KeyToken, "value_token": body.ValueToken, "event_wake": body.EventWake}, nil
	case onair.TypeEvent:
		body, err := onair.DecodeEvent(packet)
		if err != nil {
			return nil, err
		}
		return map[string]any{"event_code": body.EventCode, "severity": body.Severity, "value_bucket": body.ValueBucket, "flags": body.Flags}, nil
	case onair.TypeCommandResult:
		body, err := onair.DecodeCommandResult(packet)
		if err != nil {
			return nil, err
		}
		return map[string]any{"command_token": body.CommandToken, "phase": body.PhaseToken, "reason": body.ReasonToken}, nil
	case onair.TypePendingDigest:
		body, err := onair.DecodePendingDigest(packet)
		if err != nil {
			return nil, err
		}
		return map[string]any{"pending_count": body.PendingCount, "flags": body.Flags}, nil
	case onair.TypeTinyPoll:
		body, err := onair.DecodeTinyPoll(packet)
		if err != nil {
			return nil, err
		}
		return map[string]any{"service_level": body.ServiceLevel}, nil
	case onair.TypeCompactCommand:
		body, err := onair.DecodeCompactCommand(packet)
		if err != nil {
			return nil, err
		}
		return map[string]any{"command_token": body.CommandToken, "command_kind": body.CommandKind, "argument": body.Argument, "expires_in_sec": body.ExpiresInSec}, nil
	case onair.TypeHeartbeat:
		body, err := onair.DecodeHeartbeat(packet)
		if err != nil {
			return nil, err
		}
		return map[string]any{"health": body.Health, "battery_bucket": body.BatteryBucket, "link_quality": body.LinkQuality, "uptime_bucket": body.UptimeBucket, "flags": body.Flags}, nil
	default:
		return nil, fmt.Errorf("unsupported logical_type: %d", packet.LogicalType)
	}
}

func routeClass(envelope *contracts.Envelope) string {
	if envelope == nil || envelope.Delivery == nil {
		return ""
	}
	return envelope.Delivery.RouteClass
}

func routeStatus(plan *siterouter.RoutePlan) string {
	if plan == nil || plan.Bearer == "" || plan.Bearer == "unplanned" {
		return "route_pending"
	}
	if !plan.PayloadFit {
		return "route_blocked"
	}
	return "ready_to_send"
}

func typeName(logicalType byte) string {
	switch logicalType {
	case onair.TypeState:
		return "state"
	case onair.TypeEvent:
		return "event"
	case onair.TypeCommandResult:
		return "command_result"
	case onair.TypePendingDigest:
		return "pending_digest"
	case onair.TypeTinyPoll:
		return "tiny_poll"
	case onair.TypeCompactCommand:
		return "compact_command"
	case onair.TypeHeartbeat:
		return "heartbeat"
	default:
		return "unknown"
	}
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}

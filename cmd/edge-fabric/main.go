package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Aero123421/edge-fabric/internal/devfixtures"
	"github.com/Aero123421/edge-fabric/internal/protocol/onair"
	"github.com/Aero123421/edge-fabric/internal/protocol/usbcdc"
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
				"edge-fabric seed-fixtures",
				"edge-fabric explain-route -seed-fixtures -fixture contracts/fixtures/command-sleepy-threshold-set.json",
				"edge-fabric decode-onair -hex <hex-frame>",
				"edge-fabric decode-usb-frame -hex <hex-frame>",
			},
		})
	case "seed-fixtures":
		return seedFixtures(args[1:])
	case "explain-route":
		return explainRoute(args[1:])
	case "decode-onair":
		return decodeOnAir(args[1:])
	case "decode-usb-frame":
		return decodeUSBFrame(args[1:])
	default:
		return usage()
	}
}

func usage() error {
	return fmt.Errorf("usage: edge-fabric doctor | seed-fixtures [-db site-router.db] | explain-route -fixture <envelope.json> [-seed-fixtures|-manifest file -lease file] [-db site-router.db] | decode-onair -hex <hex-frame> | decode-usb-frame -hex <hex-frame>")
}

func seedFixtures(args []string) error {
	fs := flag.NewFlagSet("seed-fixtures", flag.ContinueOnError)
	dbPath := fs.String("db", "site-router.db", "SQLite database path")
	maxRetry := fs.Int("max-retry", 3, "max outbound retry count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := siterouter.Open(*dbPath, *maxRetry)
	if err != nil {
		return err
	}
	defer router.Close()
	seeded, err := devfixtures.SeedBuiltIn(context.Background(), router, ".")
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"db_path": *dbPath, "seeded": seeded})
}

func explainRoute(args []string) error {
	fs := flag.NewFlagSet("explain-route", flag.ContinueOnError)
	dbPath := fs.String("db", "site-router.db", "SQLite database path")
	fixturePath := fs.String("fixture", "", "envelope fixture path")
	seedBuiltIns := fs.Bool("seed-fixtures", false, "seed built-in sleepy manifest/lease fixtures before planning")
	manifestPath := fs.String("manifest", "", "raw manifest fixture to upsert before planning")
	leasePath := fs.String("lease", "", "raw lease fixture to upsert for the envelope target before planning")
	strict := fs.Bool("strict", false, "return a non-zero status when the route is not sendable")
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
	ctx := context.Background()
	var seeded []string
	if *seedBuiltIns {
		items, err := devfixtures.SeedBuiltIn(ctx, router, ".")
		if err != nil {
			return err
		}
		seeded = append(seeded, items...)
	}
	if *manifestPath != "" {
		manifest, err := devfixtures.LoadManifest(*manifestPath)
		if err != nil {
			return err
		}
		if err := router.UpsertManifest(ctx, manifest.HardwareID, manifest); err != nil {
			return err
		}
		seeded = append(seeded, "manifest:"+manifest.HardwareID)
	}
	if *leasePath != "" {
		if envelope.Target.Kind != "node" || envelope.Target.Value == "" {
			return fmt.Errorf("-lease requires an envelope with node target")
		}
		lease, err := devfixtures.LoadLease(*leasePath)
		if err != nil {
			return err
		}
		if err := router.UpsertLease(ctx, envelope.Target.Value, lease); err != nil {
			return err
		}
		seeded = append(seeded, "lease:"+envelope.Target.Value)
	}
	plan, err := router.PlanOutboundRoute(ctx, envelope)
	if err != nil {
		outputErr := printJSON(map[string]any{
			"message_id":  envelope.MessageID,
			"target":      envelope.Target,
			"route_class": routeClass(envelope),
			"allowed":     false,
			"seeded":      seeded,
			"error":       err.Error(),
		})
		if outputErr != nil {
			return outputErr
		}
		if *strict {
			return err
		}
		return nil
	}
	allowed := plan.PayloadFit && plan.Bearer != "" && plan.Bearer != "unplanned"
	if err := printJSON(map[string]any{
		"message_id":   envelope.MessageID,
		"target":       envelope.Target,
		"route_class":  plan.RouteClass,
		"allowed":      allowed,
		"route_status": routeStatus(plan),
		"seeded":       seeded,
		"plan":         plan,
	}); err != nil {
		return err
	}
	if *strict && !allowed {
		return fmt.Errorf("route is not sendable: %s", plan.Reason)
	}
	return nil
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

func decodeUSBFrame(args []string) error {
	fs := flag.NewFlagSet("decode-usb-frame", flag.ContinueOnError)
	hexFrame := fs.String("hex", "", "hex encoded USB CDC frame")
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
	frameType, payload, err := usbcdc.DecodeFrame(frame)
	if err != nil {
		return err
	}
	result := map[string]any{
		"frame_type":  frameType,
		"payload_hex": hex.EncodeToString(payload),
	}
	if json.Valid(payload) {
		var payloadJSON any
		if err := json.Unmarshal(payload, &payloadJSON); err == nil {
			result["payload_json"] = payloadJSON
		}
	}
	if packet, err := onair.Decode(payload); err == nil {
		body, bodyErr := decodeBody(packet)
		if bodyErr == nil {
			result["onair"] = map[string]any{
				"version":         packet.Version,
				"logical_type":    packet.LogicalType,
				"type_name":       typeName(packet.LogicalType),
				"summary":         packet.Summary(),
				"sequence":        packet.Sequence,
				"source_short_id": packet.SourceShortID,
				"target_short_id": packet.TargetShortID,
				"body":            body,
			}
		}
	}
	return printJSON(result)
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

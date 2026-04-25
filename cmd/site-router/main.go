package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Aero123421/edge-fabric/internal/devfixtures"
	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "site-router: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dbPath       = flag.String("db", "site-router.db", "SQLite database path")
		maxRetry     = flag.Int("max-retry", 3, "max outbound retry count before dead-letter")
		op           = flag.String("op", "doctor", "operation: doctor|seed-fixtures|queue-metrics|ingest-fixture|issue-command|latest-state|command-state|pending-digest|pending-list|rebuild-latest-state")
		fixturePath  = flag.String("fixture", "", "envelope fixture path for ingest-fixture")
		seedFixtures = flag.Bool("seed-fixtures", false, "seed built-in manifest/lease fixtures before the operation")
		ingressID    = flag.String("ingress-id", "local-cli", "ingress id for ingest-fixture")
		hardwareID   = flag.String("hardware-id", "", "hardware id for latest-state")
		stateKey     = flag.String("state-key", "", "state key for latest-state")
		commandID    = flag.String("command-id", "", "command id for command-state")
		limit        = flag.Int("limit", 8, "result limit for pending-list")
	)
	flag.Parse()

	router, err := siterouter.Open(*dbPath, *maxRetry)
	if err != nil {
		return err
	}
	defer router.Close()

	ctx := context.Background()
	if *seedFixtures && *op != "seed-fixtures" {
		if _, err := devfixtures.SeedBuiltIn(ctx, router, "."); err != nil {
			return err
		}
	}
	switch *op {
	case "doctor":
		return printJSON(map[string]any{
			"db_path":    *dbPath,
			"max_retry":  *maxRetry,
			"queue_info": mustQueueMetrics(ctx, router),
		})
	case "seed-fixtures":
		seeded, err := devfixtures.SeedBuiltIn(ctx, router, ".")
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"seeded": seeded})
	case "queue-metrics":
		metrics, err := router.QueueMetrics(ctx)
		if err != nil {
			return err
		}
		return printJSON(metrics)
	case "ingest-fixture":
		if *fixturePath == "" {
			return fmt.Errorf("-fixture is required for op=%s", *op)
		}
		envelope, err := contracts.LoadEnvelope(*fixturePath)
		if err != nil {
			return err
		}
		ack, err := router.Ingest(ctx, envelope, *ingressID)
		if err != nil {
			return err
		}
		return printJSON(ack)
	case "issue-command":
		if *fixturePath == "" {
			return fmt.Errorf("-fixture is required for op=%s", *op)
		}
		envelope, err := contracts.LoadEnvelope(*fixturePath)
		if err != nil {
			return err
		}
		ack, queueID, err := router.IssueCommand(ctx, envelope, *ingressID, "")
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"ack":      ack,
			"queue_id": queueID,
		})
	case "latest-state":
		if *hardwareID == "" || *stateKey == "" {
			return fmt.Errorf("-hardware-id and -state-key are required for op=%s", *op)
		}
		payload, err := router.LatestState(ctx, *hardwareID, *stateKey)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"hardware_id": *hardwareID,
			"state_key":   *stateKey,
			"payload":     payload,
		})
	case "command-state":
		if *commandID == "" {
			return fmt.Errorf("-command-id is required for op=%s", *op)
		}
		state, err := router.CommandState(ctx, *commandID)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"command_id": *commandID,
			"state":      state,
		})
	case "pending-digest":
		if *hardwareID == "" {
			return fmt.Errorf("-hardware-id is required for op=%s", *op)
		}
		digest, err := router.PendingCommandDigest(ctx, *hardwareID, time.Now().UTC())
		if err != nil {
			return err
		}
		return printJSON(digest)
	case "pending-list":
		if *hardwareID == "" {
			return fmt.Errorf("-hardware-id is required for op=%s", *op)
		}
		items, err := router.PendingCommandsForNode(ctx, *hardwareID, *limit, time.Now().UTC())
		if err != nil {
			return err
		}
		return printJSON(items)
	case "rebuild-latest-state":
		if err := router.RebuildLatestState(ctx); err != nil {
			return err
		}
		return printJSON(map[string]any{"status": "rebuilt"})
	default:
		return fmt.Errorf("unsupported op: %s", *op)
	}
}

func mustQueueMetrics(ctx context.Context, router *siterouter.Router) map[string]int64 {
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		return map[string]int64{"error": 1}
	}
	return metrics
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}

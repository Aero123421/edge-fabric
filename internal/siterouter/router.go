package siterouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Aero123421/edge-fabric/internal/protocol/jp"
	"github.com/Aero123421/edge-fabric/internal/protocol/onair"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

type PersistAck struct {
	AckedMessageID string
	Status         string
	Duplicate      bool
}

type QueueLease struct {
	QueueID    int64
	MessageID  string
	LeaseOwner string
	LeaseUntil string
	Envelope   *contracts.Envelope
}

type PendingCommandDigest struct {
	TargetHardwareID string `json:"target_hardware_id"`
	PendingCount     int    `json:"pending_count"`
	NewestCommandID  string `json:"newest_command_id,omitempty"`
	ExpiresSoon      bool   `json:"expires_soon"`
	Urgent           bool   `json:"urgent"`
}

type PendingCommand struct {
	QueueID    int64               `json:"queue_id"`
	MessageID  string              `json:"message_id"`
	CommandID  string              `json:"command_id,omitempty"`
	Priority   string              `json:"priority"`
	ExpiresAt  string              `json:"expires_at,omitempty"`
	Envelope   *contracts.Envelope `json:"envelope"`
	CreatedAt  string              `json:"created_at"`
	Status     string              `json:"status"`
	LeaseUntil string              `json:"lease_until,omitempty"`
}

type NodeRuntimeInfo struct {
	HardwareID string              `json:"hardware_id"`
	Manifest   *contracts.Manifest `json:"manifest,omitempty"`
	Lease      *contracts.Lease    `json:"lease,omitempty"`
}

type Router struct {
	db            *sql.DB
	maxRetryCount int
}

type stateCandidate struct {
	Envelope    contracts.Envelope
	PersistedAt string
}

var validCommandResultPhases = map[string]struct{}{
	"accepted":  {},
	"executing": {},
	"succeeded": {},
	"failed":    {},
	"rejected":  {},
	"expired":   {},
}

var validCommandTransitions = map[string]map[string]struct{}{
	"issued": {
		"accepted":  {},
		"executing": {},
		"succeeded": {},
		"failed":    {},
		"rejected":  {},
		"expired":   {},
	},
	"accepted": {
		"executing": {},
		"succeeded": {},
		"failed":    {},
		"rejected":  {},
		"expired":   {},
	},
	"executing": {
		"succeeded": {},
		"failed":    {},
		"expired":   {},
	},
}

func Open(path string, maxRetryCount int) (*Router, error) {
	if maxRetryCount <= 0 {
		maxRetryCount = 5
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	router := &Router{db: db, maxRetryCount: maxRetryCount}
	if err := router.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, _ = router.RecoverExpiredLeases(context.Background(), time.Now().UTC())
	return router, nil
}

func (r *Router) Close() error {
	return r.db.Close()
}

func (r *Router) init(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA synchronous=FULL;`,
		`CREATE TABLE IF NOT EXISTS messages (
			message_id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			dedupe_key TEXT NOT NULL,
			event_id TEXT,
			command_id TEXT,
			occurred_at TEXT,
			source_hardware_id TEXT NOT NULL,
			ingress_id TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			persisted_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_dedupe_key ON messages(dedupe_key);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_kind ON messages(kind);`,
		`CREATE TABLE IF NOT EXISTS event_ledger (
			event_id TEXT PRIMARY KEY,
			message_id TEXT NOT NULL UNIQUE,
			occurred_at TEXT,
			priority TEXT NOT NULL,
			source_hardware_id TEXT NOT NULL,
			envelope_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS latest_state (
			source_hardware_id TEXT NOT NULL,
			state_key TEXT NOT NULL,
			message_id TEXT NOT NULL,
			occurred_at TEXT,
			session_id TEXT,
			seq_local INTEGER,
			payload_json TEXT NOT NULL,
			PRIMARY KEY(source_hardware_id, state_key)
		);`,
		`CREATE TABLE IF NOT EXISTS command_ledger (
			command_id TEXT PRIMARY KEY,
			command_token INTEGER,
			message_id TEXT NOT NULL UNIQUE,
			state TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_command_ledger_command_token ON command_ledger(command_token) WHERE command_token IS NOT NULL;`,
		`CREATE TABLE IF NOT EXISTS command_execution (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			command_id TEXT NOT NULL,
			phase TEXT NOT NULL,
			message_id TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS outbox_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			queue_key TEXT NOT NULL UNIQUE,
			message_id TEXT,
			target_kind TEXT NOT NULL,
			target_value TEXT NOT NULL,
			priority TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			status TEXT NOT NULL,
			lease_owner TEXT,
			lease_until TEXT,
			retry_count INTEGER NOT NULL DEFAULT 0,
			dead_reason TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_outbox_status_priority_created ON outbox_queue(status, priority, created_at);`,
		`CREATE TABLE IF NOT EXISTS node_manifest (
			hardware_id TEXT PRIMARY KEY,
			power_class TEXT NOT NULL,
			wake_class TEXT NOT NULL,
			allowed_network_roles_json TEXT NOT NULL,
			supported_bearers_json TEXT NOT NULL,
			manifest_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS node_lease (
			hardware_id TEXT PRIMARY KEY,
			logical_binding_id TEXT NOT NULL,
			fabric_short_id INTEGER,
			effective_role TEXT NOT NULL,
			primary_bearer TEXT NOT NULL,
			fallback_bearer TEXT,
			lease_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_node_lease_fabric_short_id ON node_lease(fabric_short_id) WHERE fabric_short_id IS NOT NULL;`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (r *Router) Ingest(ctx context.Context, envelope *contracts.Envelope, ingressID string) (*PersistAck, error) {
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	duplicate, err := r.lookupDuplicate(ctx, tx, envelope)
	if err != nil {
		return nil, err
	}
	if duplicate != "" {
		return &PersistAck{AckedMessageID: duplicate, Status: "duplicate", Duplicate: true}, nil
	}

	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	persistedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages (
			message_id, kind, dedupe_key, event_id, command_id, occurred_at,
			source_hardware_id, ingress_id, envelope_json, persisted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, envelope.MessageID, envelope.Kind, envelope.DedupeKey(), envelope.EventID, envelope.CommandID,
		envelope.OccurredAt, envelope.Source.HardwareID, ingressID, string(rawEnvelope), persistedAt); err != nil {
		if isConstraintError(err) {
			return r.ingestDuplicateAck(ctx, envelope)
		}
		return nil, err
	}

	switch envelope.Kind {
	case "event":
		if envelope.EventID != "" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO event_ledger (
					event_id, message_id, occurred_at, priority, source_hardware_id, envelope_json
				) VALUES (?, ?, ?, ?, ?, ?)
			`, envelope.EventID, envelope.MessageID, envelope.OccurredAt, envelope.Priority,
				envelope.Source.HardwareID, string(rawEnvelope)); err != nil {
				if isConstraintError(err) {
					return r.ingestDuplicateAck(ctx, envelope)
				}
				return nil, err
			}
		}
	case "state":
		if err := r.upsertLatestState(ctx, tx, envelope); err != nil {
			return nil, err
		}
	case "command":
		if envelope.CommandID != "" {
			commandToken := payloadInt64(envelope.Payload, "command_token")
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO command_ledger (command_id, command_token, message_id, state, envelope_json, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, envelope.CommandID, nullableInt64(commandToken), envelope.MessageID, "issued", string(rawEnvelope), persistedAt); err != nil {
				if isConstraintError(err) {
					return r.ingestDuplicateAck(ctx, envelope)
				}
				return nil, err
			}
		}
	case "command_result":
		if err := r.recordCommandResult(ctx, tx, envelope, persistedAt); err != nil {
			return nil, err
		}
	case "manifest":
		if err := r.upsertManifestEnvelope(ctx, tx, envelope, persistedAt); err != nil {
			return nil, err
		}
	case "lease":
		if err := r.upsertLeaseEnvelope(ctx, tx, envelope, persistedAt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PersistAck{AckedMessageID: envelope.MessageID, Status: "persisted", Duplicate: false}, nil
}

func (r *Router) ingestDuplicateAck(ctx context.Context, envelope *contracts.Envelope) (*PersistAck, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	duplicate, err := r.lookupDuplicate(ctx, tx, envelope)
	if err != nil {
		return nil, err
	}
	if duplicate == "" {
		return nil, errors.New("constraint error without duplicate resolution")
	}
	return &PersistAck{AckedMessageID: duplicate, Status: "duplicate", Duplicate: true}, nil
}

func (r *Router) lookupDuplicate(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope) (string, error) {
	var duplicate string
	switch {
	case envelope.Kind == "event" && envelope.EventID != "":
		err := tx.QueryRowContext(ctx, `SELECT message_id FROM event_ledger WHERE event_id = ?`, envelope.EventID).Scan(&duplicate)
		if err == nil {
			return duplicate, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	case envelope.Kind == "command" && envelope.CommandID != "":
		err := tx.QueryRowContext(ctx, `SELECT message_id FROM command_ledger WHERE command_id = ?`, envelope.CommandID).Scan(&duplicate)
		if err == nil {
			return duplicate, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	err := tx.QueryRowContext(ctx, `SELECT message_id FROM messages WHERE message_id = ?`, envelope.MessageID).Scan(&duplicate)
	if err == nil {
		return duplicate, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return "", err
}

func (r *Router) upsertLatestState(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope) error {
	stateKey := "__default__"
	if value, ok := envelope.Payload["state_key"].(string); ok && value != "" {
		stateKey = value
	} else if value, ok := envelope.Payload["key"].(string); ok && value != "" {
		stateKey = value
	} else if value, ok := envelope.Payload["metric"].(string); ok && value != "" {
		stateKey = value
	}

	var occurredAt sql.NullString
	var sessionID sql.NullString
	var seqLocal sql.NullInt64
	var messageID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT occurred_at, session_id, seq_local, message_id
		FROM latest_state
		WHERE source_hardware_id = ? AND state_key = ?
	`, envelope.Source.HardwareID, stateKey).Scan(&occurredAt, &sessionID, &seqLocal, &messageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		if compareStateOrder(envelope, occurredAt.String, sessionID.String, int(seqLocal.Int64), messageID.String) <= 0 {
			return nil
		}
	}
	payloadJSON, err := json.Marshal(envelope.Payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO latest_state (
			source_hardware_id, state_key, message_id, occurred_at, session_id, seq_local, payload_json
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_hardware_id, state_key) DO UPDATE SET
			message_id = excluded.message_id,
			occurred_at = excluded.occurred_at,
			session_id = excluded.session_id,
			seq_local = excluded.seq_local,
			payload_json = excluded.payload_json
	`, envelope.Source.HardwareID, stateKey, envelope.MessageID, envelope.OccurredAt,
		envelope.Source.SessionID, derefInt(envelope.Source.SeqLocal), string(payloadJSON))
	return err
}

func compareStateOrder(envelope *contracts.Envelope, occurredAt, sessionID string, seqLocal int, messageID string) int {
	leftOccurred := mustParseOrderTime(envelope.OccurredAt)
	rightOccurred := mustParseOrderTime(occurredAt)
	if leftOccurred.After(rightOccurred) {
		return 1
	}
	if leftOccurred.Before(rightOccurred) {
		return -1
	}
	if envelope.Source.SessionID != "" && sessionID != "" && envelope.Source.SessionID == sessionID {
		leftSeq := derefInt(envelope.Source.SeqLocal)
		if leftSeq > seqLocal {
			return 1
		}
		if leftSeq < seqLocal {
			return -1
		}
	}
	if envelope.MessageID > messageID {
		return 1
	}
	if envelope.MessageID < messageID {
		return -1
	}
	return 0
}

func (r *Router) recordCommandResult(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, createdAt string) error {
	commandID := envelope.CommandID
	if commandID == "" {
		if fromPayload, ok := envelope.Payload["command_id"].(string); ok {
			commandID = fromPayload
		}
	}
	if commandID == "" {
		commandToken := payloadInt64(envelope.Payload, "command_token")
		if commandToken != nil {
			var resolved string
			err := tx.QueryRowContext(ctx, `SELECT command_id FROM command_ledger WHERE command_token = ?`, *commandToken).Scan(&resolved)
			if err == nil {
				commandID = resolved
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
	}
	if commandID == "" {
		return contracts.NewValidationError("command_result.command_id or payload.command_token could not be resolved")
	}
	phase := "accepted"
	if value, ok := envelope.Payload["phase"].(string); ok && value != "" {
		phase = value
	}
	if !isValidCommandResultPhase(phase) {
		return contracts.NewValidationError("invalid command_result phase: %s", phase)
	}
	var currentState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM command_ledger WHERE command_id = ?`, commandID).Scan(&currentState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contracts.NewValidationError("command_result references unknown command_id: %s", commandID)
		}
		return err
	}
	if currentState == phase {
		return nil
	}
	if !isValidCommandTransition(currentState, phase) {
		return contracts.NewValidationError("invalid command transition: %s -> %s", currentState, phase)
	}
	payloadJSON, err := json.Marshal(envelope.Payload)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO command_execution (command_id, phase, message_id, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, commandID, phase, envelope.MessageID, string(payloadJSON), createdAt); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE command_ledger SET state = ? WHERE command_id = ?`, phase, commandID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return contracts.NewValidationError("command_result update did not match command_id: %s", commandID)
	}
	return nil
}

func (r *Router) upsertManifestEnvelope(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, updatedAt string) error {
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		return err
	}
	var manifest contracts.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if manifest.HardwareID == "" {
		manifest.HardwareID = envelope.Source.HardwareID
	}
	return r.upsertManifestTx(ctx, tx, manifest.HardwareID, &manifest, updatedAt)
}

func (r *Router) upsertLeaseEnvelope(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, updatedAt string) error {
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		return err
	}
	var lease contracts.Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		return err
	}
	return r.upsertLeaseTx(ctx, tx, envelope.Target.Value, &lease, updatedAt)
}

func (r *Router) LatestState(ctx context.Context, hardwareID, stateKey string) (map[string]any, error) {
	var payloadJSON string
	err := r.db.QueryRowContext(ctx, `
		SELECT payload_json FROM latest_state
		WHERE source_hardware_id = ? AND state_key = ?
	`, hardwareID, stateKey).Scan(&payloadJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (r *Router) CountEvents(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_ledger`).Scan(&count)
	return count, err
}

func (r *Router) RebuildLatestState(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM latest_state`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT envelope_json, persisted_at FROM messages WHERE kind = 'state'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var candidates []stateCandidate
	for rows.Next() {
		var raw string
		var persistedAt string
		if err := rows.Scan(&raw, &persistedAt); err != nil {
			return err
		}
		var envelope contracts.Envelope
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			return err
		}
		candidates = append(candidates, stateCandidate{Envelope: envelope, PersistedAt: persistedAt})
	}
	sortStateCandidates(candidates)
	for _, item := range candidates {
		envelope := item.Envelope
		if envelope.OccurredAt == "" {
			envelope.OccurredAt = item.PersistedAt
		}
		if err := r.upsertLatestState(ctx, tx, &envelope); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func sortStateCandidates(items []stateCandidate) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			left := items[i]
			right := items[j]
			leftOccurred := left.Envelope.OccurredAt
			if leftOccurred == "" {
				leftOccurred = left.PersistedAt
			}
			rightOccurred := right.Envelope.OccurredAt
			if rightOccurred == "" {
				rightOccurred = right.PersistedAt
			}
			if compareStateOrder(&left.Envelope, rightOccurred, right.Envelope.Source.SessionID, derefInt(right.Envelope.Source.SeqLocal), right.Envelope.MessageID) > 0 {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func (r *Router) CommandState(ctx context.Context, commandID string) (string, error) {
	var state string
	err := r.db.QueryRowContext(ctx, `SELECT state FROM command_ledger WHERE command_id = ?`, commandID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return state, err
}

func (r *Router) UpsertManifest(ctx context.Context, hardwareID string, manifest *contracts.Manifest) error {
	if manifest == nil {
		return errors.New("manifest is required")
	}
	if hardwareID == "" {
		hardwareID = manifest.HardwareID
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.upsertManifestTx(ctx, tx, hardwareID, manifest, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Router) UpsertLease(ctx context.Context, hardwareID string, lease *contracts.Lease) error {
	if lease == nil {
		return errors.New("lease is required")
	}
	if hardwareID == "" {
		return errors.New("hardware_id is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.upsertLeaseTx(ctx, tx, hardwareID, lease, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Router) RuntimeInfoForNode(ctx context.Context, hardwareID string) (*NodeRuntimeInfo, error) {
	info := &NodeRuntimeInfo{HardwareID: hardwareID}
	if hardwareID == "" {
		return info, nil
	}
	var manifestJSON string
	err := r.db.QueryRowContext(ctx, `SELECT manifest_json FROM node_manifest WHERE hardware_id = ?`, hardwareID).Scan(&manifestJSON)
	if err == nil {
		var manifest contracts.Manifest
		if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
			return nil, err
		}
		info.Manifest = &manifest
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var leaseJSON string
	err = r.db.QueryRowContext(ctx, `SELECT lease_json FROM node_lease WHERE hardware_id = ?`, hardwareID).Scan(&leaseJSON)
	if err == nil {
		var lease contracts.Lease
		if err := json.Unmarshal([]byte(leaseJSON), &lease); err != nil {
			return nil, err
		}
		info.Lease = &lease
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return info, nil
}

func (r *Router) ResolveHardwareIDByShortID(ctx context.Context, shortID uint16) (string, error) {
	var hardwareID string
	err := r.db.QueryRowContext(ctx, `SELECT hardware_id FROM node_lease WHERE fabric_short_id = ?`, int(shortID)).Scan(&hardwareID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return hardwareID, err
}

func (r *Router) ResolveCommandIDByToken(ctx context.Context, token uint16) (string, error) {
	var commandID string
	err := r.db.QueryRowContext(ctx, `SELECT command_id FROM command_ledger WHERE command_token = ?`, int(token)).Scan(&commandID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return commandID, err
}

func (r *Router) IssueCommand(ctx context.Context, envelope *contracts.Envelope, ingressID, queueKey string) (*PersistAck, int64, error) {
	if envelope.Kind != "command" {
		return nil, 0, contracts.NewValidationError("IssueCommand requires kind=command")
	}
	ack, err := r.Ingest(ctx, envelope, ingressID)
	if err != nil {
		return nil, 0, err
	}
	if queueKey == "" {
		if envelope.CommandID != "" {
			queueKey = "command:" + envelope.CommandID
		} else {
			queueKey = "message:" + envelope.MessageID
		}
	}
	queueID, err := r.EnqueueOutbound(ctx, envelope, queueKey)
	if err != nil {
		return nil, 0, err
	}
	return ack, queueID, nil
}

func (r *Router) validateOutboundEnvelope(ctx context.Context, envelope *contracts.Envelope) error {
	if envelope == nil || envelope.Kind != "command" || envelope.Target.Kind != "node" {
		return nil
	}
	info, err := r.RuntimeInfoForNode(ctx, envelope.Target.Value)
	if err != nil {
		return err
	}
	if info.Lease != nil && info.Manifest != nil {
		if err := validateLeaseAgainstManifest(info.Lease, info.Manifest); err != nil {
			return err
		}
	}
	routeClass := ""
	if envelope.Delivery != nil {
		routeClass = envelope.Delivery.RouteClass
	}
	if routeClass == "" {
		return nil
	}
	switch routeClass {
	case "sleepy_tiny_control":
		_, err := planSleepyCompactCommand(envelope, info)
		return err
	case "maintenance_sync":
		if info.Lease == nil || info.Lease.EffectiveRole != "sleepy_leaf" {
			return nil
		}
		if info.Manifest != nil && !manifestAllowsBearer(info.Manifest, "ble_maintenance") {
			return contracts.NewValidationError("maintenance_sync requires maintenance bearer for target %s", envelope.Target.Value)
		}
		return nil
	default:
		return nil
	}
}

func (r *Router) EnqueueOutbound(ctx context.Context, envelope *contracts.Envelope, queueKey string) (int64, error) {
	if err := envelope.Validate(); err != nil {
		return 0, err
	}
	if err := r.validateOutboundEnvelope(ctx, envelope); err != nil {
		return 0, err
	}
	if queueKey == "" {
		queueKey = "message:" + envelope.MessageID
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO outbox_queue (
			queue_key, message_id, target_kind, target_value, priority, envelope_json,
			status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?)
		ON CONFLICT(queue_key) DO NOTHING
	`, queueKey, envelope.MessageID, envelope.Target.Kind, envelope.Target.Value,
		envelope.Priority, string(raw), now, now)
	if err != nil {
		return 0, err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		var id int64
		if err := r.db.QueryRowContext(ctx, `SELECT id FROM outbox_queue WHERE queue_key = ?`, queueKey).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	return result.LastInsertId()
}

func (r *Router) LeaseOutbound(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]QueueLease, error) {
	if limit <= 0 {
		limit = 1
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT id, message_id, envelope_json
		FROM outbox_queue
		WHERE status = 'queued'
		ORDER BY CASE priority
			WHEN 'critical' THEN 0
			WHEN 'control' THEN 1
			WHEN 'normal' THEN 2
			ELSE 3
		END, created_at
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseDuration).Format(time.RFC3339Nano)
	var leases []QueueLease
	for rows.Next() {
		var id int64
		var messageID string
		var raw string
		if err := rows.Scan(&id, &messageID, &raw); err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE outbox_queue
			SET status = 'leased', lease_owner = ?, lease_until = ?, updated_at = ?
			WHERE id = ? AND status = 'queued'
		`, workerID, leaseUntil, now.Format(time.RFC3339Nano), id)
		if err != nil {
			return nil, err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			continue
		}
		var envelope contracts.Envelope
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			return nil, err
		}
		leases = append(leases, QueueLease{
			QueueID:    id,
			MessageID:  messageID,
			LeaseOwner: workerID,
			LeaseUntil: leaseUntil,
			Envelope:   &envelope,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return leases, nil
}

func (r *Router) MarkSending(ctx context.Context, queueID int64, workerID string) error {
	return r.updateQueueState(ctx, queueID, workerID, "sending", "")
}

func (r *Router) MarkSentOK(ctx context.Context, queueID int64, workerID string) error {
	return r.updateQueueState(ctx, queueID, workerID, "sent_ok", "")
}

func (r *Router) AckOutbound(ctx context.Context, queueID int64, workerID string) error {
	return r.updateQueueState(ctx, queueID, workerID, "acked", "")
}

func (r *Router) MoveToDead(ctx context.Context, queueID int64, workerID, reason string) error {
	return r.updateQueueState(ctx, queueID, workerID, "dead", reason)
}

func (r *Router) updateQueueState(ctx context.Context, queueID int64, workerID, status, deadReason string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_queue
		SET status = ?, dead_reason = NULLIF(?, ''), updated_at = ?
		WHERE id = ? AND lease_owner = ?
	`, status, deadReason, time.Now().UTC().Format(time.RFC3339Nano), queueID, workerID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errors.New("queue item is not leased by this worker")
	}
	return nil
}

func (r *Router) RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error) {
	expiry := now.UTC().Format(time.RFC3339Nano)
	if _, err := r.db.ExecContext(ctx, `
		UPDATE outbox_queue
		SET status = 'dead', dead_reason = 'retry_exhausted', updated_at = ?
		WHERE status IN ('leased', 'sending', 'sent_ok')
		  AND lease_until IS NOT NULL
		  AND lease_until < ?
		  AND retry_count + 1 >= ?
	`, expiry, expiry, r.maxRetryCount); err != nil {
		return 0, err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_queue
		SET status = 'queued',
			lease_owner = NULL,
			lease_until = NULL,
			retry_count = retry_count + 1,
			updated_at = ?
		WHERE status IN ('leased', 'sending', 'sent_ok')
		  AND lease_until IS NOT NULL
		  AND lease_until < ?
		  AND retry_count + 1 < ?
	`, expiry, expiry, r.maxRetryCount)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *Router) QueueMetrics(ctx context.Context) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM outbox_queue GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics := map[string]int64{
		"queued_count":  0,
		"leased_count":  0,
		"sending_count": 0,
		"sent_ok_count": 0,
		"acked_count":   0,
		"dead_count":    0,
	}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		metrics[status+"_count"] = count
	}
	metrics["retry_count"] = 0
	var retryCount int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(retry_count), 0) FROM outbox_queue`).Scan(&retryCount); err != nil {
		return nil, err
	}
	metrics["retry_count"] = retryCount
	var oldestQueuedAgeMS int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(CAST((julianday('now') - julianday(MIN(created_at))) * 86400000 AS INTEGER), 0)
		FROM outbox_queue
		WHERE status = 'queued'
	`).Scan(&oldestQueuedAgeMS); err != nil {
		return nil, err
	}
	metrics["oldest_queued_age_ms"] = oldestQueuedAgeMS
	queueAges, err := r.queueAgeSamples(ctx)
	if err != nil {
		return nil, err
	}
	metrics["queue_lag_p95_ms"] = percentile95(queueAges)
	return metrics, nil
}

func (r *Router) PendingCommandDigest(ctx context.Context, targetHardwareID string, now time.Time) (*PendingCommandDigest, error) {
	pending, err := r.PendingCommandsForNode(ctx, targetHardwareID, 32, now)
	if err != nil {
		return nil, err
	}
	digest := &PendingCommandDigest{
		TargetHardwareID: targetHardwareID,
		PendingCount:     len(pending),
	}
	if len(pending) == 0 {
		return digest, nil
	}
	digest.NewestCommandID = pending[0].CommandID
	for _, item := range pending {
		if item.Priority == "critical" || item.Priority == "control" {
			digest.Urgent = true
		}
		if item.ExpiresAt != "" {
			expiresAt, err := time.Parse(time.RFC3339Nano, item.ExpiresAt)
			if err == nil && expiresAt.Before(now.UTC().Add(5*time.Minute)) {
				digest.ExpiresSoon = true
			}
		}
	}
	return digest, nil
}

func (r *Router) PendingCommandsForNode(ctx context.Context, targetHardwareID string, limit int, now time.Time) ([]PendingCommand, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, message_id, priority, envelope_json, created_at, status, COALESCE(lease_until, '')
		FROM outbox_queue
		WHERE target_kind = 'node'
		  AND target_value = ?
		  AND status IN ('queued', 'leased', 'sending')
		ORDER BY created_at DESC
		LIMIT ?
	`, targetHardwareID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []PendingCommand
	for rows.Next() {
		var (
			queueID    int64
			messageID  string
			priority   string
			raw        string
			createdAt  string
			status     string
			leaseUntil string
		)
		if err := rows.Scan(&queueID, &messageID, &priority, &raw, &createdAt, &status, &leaseUntil); err != nil {
			return nil, err
		}
		var envelope contracts.Envelope
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			return nil, err
		}
		if envelope.Kind != "command" {
			continue
		}
		if envelope.Delivery != nil && envelope.Delivery.ExpiresAt != "" {
			expiresAt, err := time.Parse(time.RFC3339Nano, envelope.Delivery.ExpiresAt)
			if err == nil && expiresAt.Before(now.UTC()) {
				continue
			}
		}
		items = append(items, PendingCommand{
			QueueID:    queueID,
			MessageID:  messageID,
			CommandID:  envelope.CommandID,
			Priority:   priority,
			ExpiresAt:  deliveryExpiresAt(envelope.Delivery),
			Envelope:   &envelope,
			CreatedAt:  createdAt,
			Status:     status,
			LeaseUntil: leaseUntil,
		})
	}
	return items, rows.Err()
}

func (r *Router) upsertManifestTx(ctx context.Context, tx *sql.Tx, hardwareID string, manifest *contracts.Manifest, updatedAt string) error {
	if manifest == nil {
		return errors.New("manifest is required")
	}
	if hardwareID == "" {
		return contracts.NewValidationError("manifest.hardware_id is required")
	}
	manifest.HardwareID = hardwareID
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	allowedRolesJSON, err := json.Marshal(manifest.AllowedNetworkRoles)
	if err != nil {
		return err
	}
	bearersJSON, err := json.Marshal(manifest.SupportedBearers)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO node_manifest (
			hardware_id, power_class, wake_class, allowed_network_roles_json, supported_bearers_json, manifest_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hardware_id) DO UPDATE SET
			power_class = excluded.power_class,
			wake_class = excluded.wake_class,
			allowed_network_roles_json = excluded.allowed_network_roles_json,
			supported_bearers_json = excluded.supported_bearers_json,
			manifest_json = excluded.manifest_json,
			updated_at = excluded.updated_at
	`, hardwareID, manifest.PowerClass, manifest.WakeClass, string(allowedRolesJSON), string(bearersJSON), string(raw), updatedAt)
	return err
}

func (r *Router) upsertLeaseTx(ctx context.Context, tx *sql.Tx, hardwareID string, lease *contracts.Lease, updatedAt string) error {
	if lease == nil {
		return errors.New("lease is required")
	}
	if hardwareID == "" {
		return contracts.NewValidationError("lease target hardware_id is required")
	}
	manifest, err := r.manifestForNodeTx(ctx, tx, hardwareID)
	if err != nil {
		return err
	}
	if err := validateLeaseAgainstManifest(lease, manifest); err != nil {
		return err
	}
	raw, err := json.Marshal(lease)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO node_lease (
			hardware_id, logical_binding_id, fabric_short_id, effective_role, primary_bearer, fallback_bearer, lease_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hardware_id) DO UPDATE SET
			logical_binding_id = excluded.logical_binding_id,
			fabric_short_id = excluded.fabric_short_id,
			effective_role = excluded.effective_role,
			primary_bearer = excluded.primary_bearer,
			fallback_bearer = excluded.fallback_bearer,
			lease_json = excluded.lease_json,
			updated_at = excluded.updated_at
	`, hardwareID, lease.LogicalBindingID, nullableInt(lease.FabricShortID), lease.EffectiveRole, lease.PrimaryBearer, lease.FallbackBearer, string(raw), updatedAt)
	return err
}

func (r *Router) manifestForNodeTx(ctx context.Context, tx *sql.Tx, hardwareID string) (*contracts.Manifest, error) {
	var manifestJSON string
	err := tx.QueryRowContext(ctx, `SELECT manifest_json FROM node_manifest WHERE hardware_id = ?`, hardwareID).Scan(&manifestJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest contracts.Manifest
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func validateLeaseAgainstManifest(lease *contracts.Lease, manifest *contracts.Manifest) error {
	if lease == nil {
		return nil
	}
	if manifest == nil {
		return nil
	}
	if len(manifest.AllowedNetworkRoles) > 0 && !containsString(manifest.AllowedNetworkRoles, lease.EffectiveRole) {
		return contracts.NewValidationError("lease role %s is not allowed for hardware_id %s", lease.EffectiveRole, manifest.HardwareID)
	}
	if isSleepyPowerClass(manifest) && leaseRoleRequiresAlwaysOn(lease.EffectiveRole) {
		return contracts.NewValidationError("sleepy/battery node %s must not take always-on role %s", manifest.HardwareID, lease.EffectiveRole)
	}
	return nil
}

func planSleepyCompactCommand(envelope *contracts.Envelope, info *NodeRuntimeInfo) ([]byte, error) {
	if envelope == nil {
		return nil, contracts.NewValidationError("command envelope is required")
	}
	if info == nil || info.Lease == nil {
		return nil, contracts.NewValidationError("sleepy_tiny_control requires lease with fabric_short_id for target %s", envelope.Target.Value)
	}
	if info.Lease.FabricShortID == nil || *info.Lease.FabricShortID <= 0 {
		return nil, contracts.NewValidationError("sleepy_tiny_control requires fabric_short_id for target %s", envelope.Target.Value)
	}
	if info.Lease.EffectiveRole != "sleepy_leaf" {
		return nil, contracts.NewValidationError("sleepy_tiny_control targets must be sleepy_leaf, got %s", info.Lease.EffectiveRole)
	}
	commandToken := payloadInt64(envelope.Payload, "command_token")
	if commandToken == nil || *commandToken <= 0 || *commandToken > 0xFFFF {
		return nil, contracts.NewValidationError("sleepy_tiny_control requires command_token in payload")
	}
	var (
		commandKind byte
		argument    byte
	)
	commandName, _ := envelope.Payload["command_name"].(string)
	switch commandName {
	case "mode.set":
		mode, _ := envelope.Payload["mode"].(string)
		switch mode {
		case "maintenance_awake":
			commandKind = onair.CommandKindMaintenanceOn
		case "deployed":
			commandKind = onair.CommandKindMaintenanceOff
		default:
			return nil, contracts.NewValidationError("mode.set(%s) is not representable as sleepy_tiny_control", mode)
		}
	case "threshold.set":
		value := payloadInt64(envelope.Payload, "value")
		if value == nil || *value < 0 || *value > 0xFF {
			return nil, contracts.NewValidationError("threshold.set requires uint8 value for sleepy_tiny_control")
		}
		commandKind = onair.CommandKindThresholdSet
		argument = byte(*value)
	case "quiet.set":
		value := payloadInt64(envelope.Payload, "value")
		if value == nil || *value < 0 || *value > 0xFF {
			return nil, contracts.NewValidationError("quiet.set requires uint8 value for sleepy_tiny_control")
		}
		commandKind = onair.CommandKindQuietSet
		argument = byte(*value)
	case "alarm.clear":
		commandKind = onair.CommandKindAlarmClear
	case "sampling.set":
		value := payloadInt64(envelope.Payload, "value")
		if value == nil || *value < 0 || *value > 0xFF {
			return nil, contracts.NewValidationError("sampling.set requires uint8 value for sleepy_tiny_control")
		}
		commandKind = onair.CommandKindSamplingSet
		argument = byte(*value)
	default:
		return nil, contracts.NewValidationError("command %s is not representable as sleepy_tiny_control", commandName)
	}
	expiresInSec := byte(30)
	if envelope.Delivery != nil && envelope.Delivery.ExpiresAt != "" {
		if expiresAt, err := time.Parse(time.RFC3339Nano, envelope.Delivery.ExpiresAt); err == nil {
			remaining := time.Until(expiresAt.UTC()).Seconds()
			if remaining <= 0 {
				expiresInSec = 0
			} else if remaining < 255 {
				expiresInSec = byte(remaining)
			} else {
				expiresInSec = 255
			}
		}
	}
	wire, err := onair.EncodeCompactCommand(uint16(*info.Lease.FabricShortID), false, onair.CompactCommandBody{
		CommandToken: uint16(*commandToken),
		CommandKind:  commandKind,
		Argument:     argument,
		ExpiresInSec: expiresInSec,
	})
	if err != nil {
		return nil, err
	}
	profiles, err := jp.LoadProfileFile(jpProfilePath())
	if err != nil {
		return nil, err
	}
	totalCap, err := findJPProfileCap(profiles, "JP125_LONG_SF10")
	if err != nil {
		return nil, err
	}
	if len(wire) > totalCap {
		return nil, contracts.NewValidationError("sleepy_tiny_control payload exceeds JP125_LONG_SF10 cap: %d > %d", len(wire), totalCap)
	}
	return wire, nil
}

func isValidCommandResultPhase(phase string) bool {
	_, ok := validCommandResultPhases[phase]
	return ok
}

func isValidCommandTransition(currentState, nextState string) bool {
	allowed, ok := validCommandTransitions[currentState]
	if !ok {
		return false
	}
	_, ok = allowed[nextState]
	return ok
}

func isConstraintError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "constraint")
}

func payloadInt64(payload map[string]any, key string) *int64 {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case int:
		result := int64(value)
		return &result
	case int32:
		result := int64(value)
		return &result
	case int64:
		result := value
		return &result
	case float64:
		result := int64(value)
		return &result
	default:
		return nil
	}
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func manifestAllowsBearer(manifest *contracts.Manifest, bearer string) bool {
	if manifest == nil {
		return false
	}
	return containsString(manifest.SupportedBearers, bearer)
}

func isSleepyPowerClass(manifest *contracts.Manifest) bool {
	if manifest == nil {
		return false
	}
	return strings.Contains(strings.ToLower(manifest.PowerClass), "battery") ||
		strings.Contains(strings.ToLower(manifest.WakeClass), "sleep")
}

func jpProfilePath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "contracts", "protocol", "jp-safe-profiles.json")
}

func findJPProfileCap(file *jp.ProfileFile, name string) (int, error) {
	if file == nil {
		return 0, errors.New("profile file is required")
	}
	for _, profile := range file.Profiles {
		if profile.Name == name {
			return profile.TotalPayloadCap, nil
		}
	}
	return 0, fmt.Errorf("unknown JP-safe profile: %s", name)
}

func leaseRoleRequiresAlwaysOn(role string) bool {
	switch role {
	case "lora_relay", "mesh_router", "mesh_root", "ack_owner", "powered_leaf":
		return true
	default:
		return false
	}
}

func derefInt(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func mustParseOrderTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func deliveryExpiresAt(delivery *contracts.DeliverySpec) string {
	if delivery == nil {
		return ""
	}
	return delivery.ExpiresAt
}

func (r *Router) queueAgeSamples(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT CAST((julianday('now') - julianday(created_at)) * 86400000 AS INTEGER)
		FROM outbox_queue
		WHERE status IN ('queued', 'leased', 'sending')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ages []int64
	for rows.Next() {
		var age int64
		if err := rows.Scan(&age); err != nil {
			return nil, err
		}
		ages = append(ages, age)
	}
	return ages, rows.Err()
}

func percentile95(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[i] > values[j] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
	index := (len(values)*95 - 1) / 100
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

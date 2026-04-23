package siterouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

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
			message_id TEXT NOT NULL UNIQUE,
			state TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
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
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO command_ledger (command_id, message_id, state, envelope_json, created_at)
				VALUES (?, ?, ?, ?, ?)
			`, envelope.CommandID, envelope.MessageID, "issued", string(rawEnvelope), persistedAt); err != nil {
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
	if envelope.Source.SessionID > sessionID {
		return 1
	}
	if envelope.Source.SessionID < sessionID {
		return -1
	}
	leftSeq := derefInt(envelope.Source.SeqLocal)
	if leftSeq > seqLocal {
		return 1
	}
	if leftSeq < seqLocal {
		return -1
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
		return nil
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

func (r *Router) EnqueueOutbound(ctx context.Context, envelope *contracts.Envelope, queueKey string) (int64, error) {
	if err := envelope.Validate(); err != nil {
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

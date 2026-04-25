package siterouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	contractpolicy "github.com/Aero123421/edge-fabric/contracts/policy"
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

type HeartbeatRecord struct {
	HeartbeatKey     string         `json:"heartbeat_key"`
	GatewayID        string         `json:"gateway_id,omitempty"`
	SubjectKind      string         `json:"subject_kind,omitempty"`
	SubjectID        string         `json:"subject_id,omitempty"`
	SourceHardwareID string         `json:"source_hardware_id"`
	IngressID        string         `json:"ingress_id"`
	HostLink         string         `json:"host_link,omitempty"`
	Bearer           string         `json:"bearer,omitempty"`
	Status           string         `json:"status,omitempty"`
	Live             bool           `json:"live"`
	MessageID        string         `json:"message_id"`
	UpdatedAt        string         `json:"updated_at"`
	Payload          map[string]any `json:"payload"`
}

type RetentionPolicy struct {
	HeartbeatRetentionDays       int `json:"heartbeat_retention_days,omitempty"`
	RadioObservationRetentionHrs int `json:"radio_observation_retention_hours,omitempty"`
	DeadQueueRetentionDays       int `json:"dead_queue_retention_days,omitempty"`
	FileChunkRetentionDays       int `json:"file_chunk_retention_days,omitempty"`
}

type RetentionResult struct {
	DeletedHeartbeats        int64 `json:"deleted_heartbeats"`
	DeletedRadioObservations int64 `json:"deleted_radio_observations"`
	DeletedDeadQueueItems    int64 `json:"deleted_dead_queue_items"`
	DeletedDeadQueueAttempts int64 `json:"deleted_dead_queue_attempts"`
	DeletedFileChunks        int64 `json:"deleted_file_chunks"`
}

type RouterSchemaInfo struct {
	SchemaVersion int    `json:"schema_version"`
	OpenedAt      string `json:"opened_at"`
}

type FabricSummaryRecord struct {
	SummaryKey       string         `json:"summary_key"`
	SourceHardwareID string         `json:"source_hardware_id"`
	MessageID        string         `json:"message_id"`
	UpdatedAt        string         `json:"updated_at"`
	Payload          map[string]any `json:"payload"`
}

type FileChunkStatus struct {
	FileID            string `json:"file_id"`
	ReceivedChunks    int    `json:"received_chunks"`
	TotalChunks       int    `json:"total_chunks"`
	HighestChunkIndex int    `json:"highest_chunk_index"`
	LastMessageID     string `json:"last_message_id,omitempty"`
	LastUpdatedAt     string `json:"last_updated_at,omitempty"`
	Complete          bool   `json:"complete"`
}

type OutboundAttempt struct {
	AttemptID int64          `json:"attempt_id"`
	QueueID   int64          `json:"queue_id"`
	AttemptNo int            `json:"attempt_no"`
	Bearer    string         `json:"bearer,omitempty"`
	GatewayID string         `json:"gateway_id,omitempty"`
	PathLabel string         `json:"path_label,omitempty"`
	Status    string         `json:"status"`
	Detail    map[string]any `json:"detail,omitempty"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

type RoutePlan struct {
	RouteClass         string         `json:"route_class,omitempty"`
	Bearer             string         `json:"bearer,omitempty"`
	GatewayID          string         `json:"gateway_id,omitempty"`
	NextHopID          string         `json:"next_hop_id,omitempty"`
	FinalTargetID      string         `json:"final_target_id,omitempty"`
	NextHopShortID     *int           `json:"next_hop_short_id,omitempty"`
	FinalTargetShortID *int           `json:"final_target_short_id,omitempty"`
	PathLabel          string         `json:"path_label,omitempty"`
	AllowRelay         bool           `json:"allow_relay"`
	AllowRedundant     bool           `json:"allow_redundant"`
	HopLimit           *int           `json:"hop_limit,omitempty"`
	PayloadFit         bool           `json:"payload_fit"`
	Reason             string         `json:"reason"`
	Detail             map[string]any `json:"detail,omitempty"`
}

type OutboxRoutePlanRecord struct {
	QueueID            int64          `json:"queue_id"`
	RouteStatus        string         `json:"route_status"`
	SelectedBearer     string         `json:"selected_bearer,omitempty"`
	SelectedGatewayID  string         `json:"selected_gateway_id,omitempty"`
	NextHopID          string         `json:"next_hop_id,omitempty"`
	FinalTargetID      string         `json:"final_target_id,omitempty"`
	NextHopShortID     *int           `json:"next_hop_short_id,omitempty"`
	FinalTargetShortID *int           `json:"final_target_short_id,omitempty"`
	RouteReason        string         `json:"route_reason,omitempty"`
	PayloadFit         bool           `json:"payload_fit"`
	RoutePlan          *RoutePlan     `json:"route_plan,omitempty"`
	Detail             map[string]any `json:"detail,omitempty"`
}

type NodeRuntimeInfo struct {
	HardwareID string              `json:"hardware_id"`
	Manifest   *contracts.Manifest `json:"manifest,omitempty"`
	Lease      *contracts.Lease    `json:"lease,omitempty"`
}

type nodeRuntimeLookup func(context.Context, string) (*NodeRuntimeInfo, error)

type Router struct {
	db            *sql.DB
	maxRetryCount int
}

const radioPacketObservationWindow = 10 * time.Minute

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
		`PRAGMA user_version = 1;`,
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
		`CREATE TABLE IF NOT EXISTS radio_packet_observation (
			packet_key TEXT PRIMARY KEY,
			event_id TEXT NOT NULL,
			first_message_id TEXT NOT NULL,
			last_message_id TEXT NOT NULL,
			source_hardware_id TEXT NOT NULL,
			last_ingress_id TEXT NOT NULL,
			observed_count INTEGER NOT NULL DEFAULT 1,
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL
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
			command_token_scope TEXT NOT NULL DEFAULT '',
			command_token_epoch TEXT NOT NULL DEFAULT '',
			command_token_valid_from TEXT,
			command_token_valid_until TEXT,
			message_id TEXT NOT NULL UNIQUE,
			state TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`DROP INDEX IF EXISTS idx_command_ledger_command_token;`,
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
			route_status TEXT NOT NULL DEFAULT 'route_pending',
			selected_bearer TEXT,
			selected_gateway_id TEXT,
			route_reason TEXT,
			payload_fit INTEGER NOT NULL DEFAULT 0,
			route_plan_json TEXT,
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
		`CREATE TABLE IF NOT EXISTS heartbeat_ledger (
			heartbeat_key TEXT PRIMARY KEY,
			gateway_id TEXT,
			subject_kind TEXT,
			subject_id TEXT,
			source_hardware_id TEXT NOT NULL,
			ingress_id TEXT NOT NULL,
			host_link TEXT,
			bearer TEXT,
			status TEXT,
			live INTEGER NOT NULL DEFAULT 0,
			message_id TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS fabric_summary_latest (
			summary_key TEXT PRIMARY KEY,
			source_hardware_id TEXT NOT NULL,
			message_id TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS file_chunk_ledger (
			file_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			total_chunks INTEGER NOT NULL,
			source_hardware_id TEXT NOT NULL,
			message_id TEXT NOT NULL UNIQUE,
			payload_json TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(file_id, chunk_index)
		);`,
		`CREATE TABLE IF NOT EXISTS outbox_attempt (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			queue_id INTEGER NOT NULL,
			attempt_no INTEGER NOT NULL,
			bearer TEXT,
			gateway_id TEXT,
			path_label TEXT,
			status TEXT NOT NULL,
			detail_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(queue_id, attempt_no)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_outbox_attempt_queue_id ON outbox_attempt(queue_id, attempt_no);`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := r.ensureOperationalSchema(ctx); err != nil {
		return err
	}
	return nil
}

func (r *Router) SchemaInfo(ctx context.Context) (*RouterSchemaInfo, error) {
	var version int
	if err := r.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return nil, err
	}
	return &RouterSchemaInfo{
		SchemaVersion: version,
		OpenedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func (r *Router) ensureOperationalSchema(ctx context.Context) error {
	columns := []struct {
		table string
		def   string
	}{
		{"command_ledger", `command_token_scope TEXT NOT NULL DEFAULT ''`},
		{"command_ledger", `command_token_epoch TEXT NOT NULL DEFAULT ''`},
		{"command_ledger", `command_token_valid_from TEXT`},
		{"command_ledger", `command_token_valid_until TEXT`},
		{"heartbeat_ledger", `subject_kind TEXT`},
		{"heartbeat_ledger", `subject_id TEXT`},
		{"outbox_queue", `route_status TEXT NOT NULL DEFAULT 'route_pending'`},
		{"outbox_queue", `selected_bearer TEXT`},
		{"outbox_queue", `selected_gateway_id TEXT`},
		{"outbox_queue", `route_reason TEXT`},
		{"outbox_queue", `payload_fit INTEGER NOT NULL DEFAULT 0`},
		{"outbox_queue", `route_plan_json TEXT`},
	}
	for _, column := range columns {
		if err := r.addColumnIfMissing(ctx, column.table, column.def); err != nil {
			return err
		}
	}
	if _, err := r.db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_command_ledger_command_token`); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_command_ledger_command_token_scope`); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE command_ledger
		SET command_token_scope = COALESCE(json_extract(envelope_json, '$.target.value'), '')
		WHERE command_token_scope = ''
		  AND command_token IS NOT NULL
		  AND json_extract(envelope_json, '$.target.kind') = 'node'
	`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_command_ledger_command_token_scope_epoch
		ON command_ledger(command_token_scope, command_token_epoch, command_token)
		WHERE command_token IS NOT NULL
	`)
	return err
}

func (r *Router) addColumnIfMissing(ctx context.Context, table, definition string) error {
	if _, err := r.db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s`, table, definition)); err != nil {
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "duplicate column") {
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
	if packetDuplicate, err := r.recordDuplicateRadioObservation(ctx, tx, envelope, ingressID); err != nil {
		return nil, err
	} else if packetDuplicate != "" {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &PersistAck{AckedMessageID: packetDuplicate, Status: "duplicate", Duplicate: true}, nil
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
			_ = tx.Rollback()
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
					_ = tx.Rollback()
					return r.ingestDuplicateAck(ctx, envelope)
				}
				return nil, err
			}
		}
		if err := r.recordRadioObservation(ctx, tx, envelope, ingressID, persistedAt); err != nil {
			return nil, err
		}
	case "state":
		if err := r.upsertLatestState(ctx, tx, envelope); err != nil {
			return nil, err
		}
	case "command":
		if envelope.CommandID != "" {
			commandToken := payloadInt64(envelope.Payload, "command_token")
			commandTokenScope := commandTokenScopeForEnvelope(envelope)
			commandTokenEpoch := commandTokenEpochForEnvelope(envelope)
			commandTokenValidFrom, commandTokenValidUntil := commandTokenValidityForEnvelope(envelope)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO command_ledger (
					command_id, command_token, command_token_scope, command_token_epoch,
					command_token_valid_from, command_token_valid_until,
					message_id, state, envelope_json, created_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, envelope.CommandID, nullableInt64(commandToken), commandTokenScope, commandTokenEpoch,
				nullableString(commandTokenValidFrom), nullableString(commandTokenValidUntil),
				envelope.MessageID, "issued", string(rawEnvelope), persistedAt); err != nil {
				if isConstraintError(err) {
					_ = tx.Rollback()
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
	case "heartbeat":
		if err := r.upsertHeartbeatEnvelope(ctx, tx, envelope, ingressID, persistedAt); err != nil {
			return nil, err
		}
	case "fabric_summary":
		if err := r.upsertFabricSummaryEnvelope(ctx, tx, envelope, persistedAt); err != nil {
			return nil, err
		}
	case "file_chunk":
		if err := r.upsertFileChunkEnvelope(ctx, tx, envelope, persistedAt); err != nil {
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

func (r *Router) recordDuplicateRadioObservation(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, ingressID string) (string, error) {
	packetKey := envelopeOnAirKey(envelope)
	if envelope.Kind != "event" || packetKey == "" {
		return "", nil
	}
	var duplicate string
	var lastSeenAt string
	err := tx.QueryRowContext(ctx, `SELECT first_message_id, last_seen_at FROM radio_packet_observation WHERE packet_key = ?`, packetKey).Scan(&duplicate, &lastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lastSeen, err := time.Parse(time.RFC3339Nano, lastSeenAt)
	if err != nil || time.Since(lastSeen.UTC()) > radioPacketObservationWindow {
		_, err := tx.ExecContext(ctx, `DELETE FROM radio_packet_observation WHERE packet_key = ?`, packetKey)
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radio_packet_observation
		SET last_ingress_id = ?, observed_count = observed_count + 1, last_seen_at = ?
		WHERE packet_key = ?
	`, ingressID, now, packetKey); err != nil {
		return "", err
	}
	return duplicate, nil
}

func (r *Router) recordRadioObservation(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, ingressID, observedAt string) error {
	packetKey := envelopeOnAirKey(envelope)
	if envelope.Kind != "event" || packetKey == "" {
		return nil
	}
	eventID := envelope.EventID
	if eventID == "" {
		eventID = envelope.MessageID
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO radio_packet_observation (
			packet_key, event_id, first_message_id, last_message_id, source_hardware_id,
			last_ingress_id, observed_count, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(packet_key) DO UPDATE SET
			last_message_id = excluded.last_message_id,
			last_ingress_id = excluded.last_ingress_id,
			observed_count = observed_count + 1,
			last_seen_at = excluded.last_seen_at
	`, packetKey, eventID, envelope.MessageID, envelope.MessageID, envelope.Source.HardwareID, ingressID, observedAt, observedAt)
	return err
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

func compareStateOrderWithOccurred(left *contracts.Envelope, leftOccurredAt string, right *contracts.Envelope, rightOccurredAt string) int {
	leftOccurred := mustParseOrderTime(leftOccurredAt)
	rightOccurred := mustParseOrderTime(rightOccurredAt)
	if leftOccurred.After(rightOccurred) {
		return 1
	}
	if leftOccurred.Before(rightOccurred) {
		return -1
	}
	if left.Source.SessionID != "" && right.Source.SessionID != "" && left.Source.SessionID == right.Source.SessionID {
		leftSeq := derefInt(left.Source.SeqLocal)
		rightSeq := derefInt(right.Source.SeqLocal)
		if leftSeq > rightSeq {
			return 1
		}
		if leftSeq < rightSeq {
			return -1
		}
	}
	if left.MessageID > right.MessageID {
		return 1
	}
	if left.MessageID < right.MessageID {
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
			scope := envelope.Source.HardwareID
			err := tx.QueryRowContext(ctx, `
				SELECT command_id FROM command_ledger
				WHERE command_token_scope = ? AND command_token = ?
			`, scope, *commandToken).Scan(&resolved)
			if errors.Is(err, sql.ErrNoRows) && scope == "" {
				err = tx.QueryRowContext(ctx, `
					SELECT command_id FROM command_ledger
					WHERE command_token = ?
					ORDER BY created_at DESC
					LIMIT 1
				`, *commandToken).Scan(&resolved)
			}
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

func (r *Router) upsertHeartbeatEnvelope(
	ctx context.Context,
	tx *sql.Tx,
	envelope *contracts.Envelope,
	ingressID string,
	updatedAt string,
) error {
	if err := validateHeartbeatSubject(envelope); err != nil {
		return err
	}
	heartbeatKey := heartbeatKeyForEnvelope(envelope)
	subjectKind := heartbeatSubjectKind(envelope)
	subjectID := heartbeatSubjectID(envelope)
	status := payloadString(envelope.Payload, "status")
	live := payloadBool(envelope.Payload, "live") || heartbeatStatusImpliesLive(status)
	payloadJSON, err := json.Marshal(envelope.Payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO heartbeat_ledger (
			heartbeat_key, gateway_id, subject_kind, subject_id, source_hardware_id, ingress_id, host_link, bearer, status, live, message_id, payload_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(heartbeat_key) DO UPDATE SET
			gateway_id = excluded.gateway_id,
			subject_kind = excluded.subject_kind,
			subject_id = excluded.subject_id,
			source_hardware_id = excluded.source_hardware_id,
			ingress_id = excluded.ingress_id,
			host_link = excluded.host_link,
			bearer = excluded.bearer,
			status = excluded.status,
			live = excluded.live,
			message_id = excluded.message_id,
			payload_json = excluded.payload_json,
			updated_at = excluded.updated_at
	`,
		heartbeatKey,
		payloadString(envelope.Payload, "gateway_id"),
		subjectKind,
		subjectID,
		envelope.Source.HardwareID,
		ingressID,
		deliveryIngressValue(envelope.Delivery, "host_link"),
		deliveryIngressValue(envelope.Delivery, "bearer"),
		status,
		boolToInt(live),
		envelope.MessageID,
		string(payloadJSON),
		updatedAt,
	)
	return err
}

func (r *Router) upsertFabricSummaryEnvelope(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, updatedAt string) error {
	summaryKey := fabricSummaryKeyForEnvelope(envelope)
	payloadJSON, err := json.Marshal(envelope.Payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO fabric_summary_latest (
			summary_key, source_hardware_id, message_id, payload_json, updated_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(summary_key) DO UPDATE SET
			source_hardware_id = excluded.source_hardware_id,
			message_id = excluded.message_id,
			payload_json = excluded.payload_json,
			updated_at = excluded.updated_at
	`, summaryKey, envelope.Source.HardwareID, envelope.MessageID, string(payloadJSON), updatedAt)
	return err
}

func (r *Router) upsertFileChunkEnvelope(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, updatedAt string) error {
	fileID := fileChunkIDForEnvelope(envelope)
	chunkIndex := payloadInt64(envelope.Payload, "chunk_index")
	totalChunks := payloadInt64(envelope.Payload, "total_chunks")
	if chunkIndex == nil || totalChunks == nil {
		return contracts.NewValidationError("file_chunk requires payload.chunk_index and payload.total_chunks")
	}
	var existingTotal int
	err := tx.QueryRowContext(ctx, `
		SELECT total_chunks FROM file_chunk_ledger
		WHERE file_id = ?
		LIMIT 1
	`, fileID).Scan(&existingTotal)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingTotal != int(*totalChunks) {
		return contracts.NewValidationError("file_chunk total_chunks mismatch for file_id %s: existing=%d incoming=%d", fileID, existingTotal, *totalChunks)
	}
	payloadJSON, err := json.Marshal(envelope.Payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO file_chunk_ledger (
			file_id, chunk_index, total_chunks, source_hardware_id, message_id, payload_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_id, chunk_index) DO UPDATE SET
			total_chunks = excluded.total_chunks,
			source_hardware_id = excluded.source_hardware_id,
			message_id = excluded.message_id,
			payload_json = excluded.payload_json,
			updated_at = excluded.updated_at
	`, fileID, int(*chunkIndex), int(*totalChunks), envelope.Source.HardwareID, envelope.MessageID, string(payloadJSON), updatedAt)
	return err
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
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		return compareStateOrderWithOccurred(
			&left.Envelope,
			stateCandidateOccurredAt(left),
			&right.Envelope,
			stateCandidateOccurredAt(right),
		) < 0
	})
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

func (r *Router) RegisterDevice(ctx context.Context, hardwareID string, manifest *contracts.Manifest, lease *contracts.Lease) error {
	if manifest == nil {
		return errors.New("manifest is required")
	}
	if hardwareID == "" {
		hardwareID = manifest.HardwareID
	}
	if hardwareID == "" {
		return errors.New("hardware_id is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := r.upsertManifestTx(ctx, tx, hardwareID, manifest, updatedAt); err != nil {
		return err
	}
	if lease != nil {
		if err := r.upsertLeaseTx(ctx, tx, hardwareID, lease, updatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Router) RuntimeInfoForNode(ctx context.Context, hardwareID string) (*NodeRuntimeInfo, error) {
	return runtimeInfoForNode(ctx, r.db, hardwareID)
}

func (r *Router) runtimeInfoForNodeTx(ctx context.Context, tx *sql.Tx, hardwareID string) (*NodeRuntimeInfo, error) {
	return runtimeInfoForNode(ctx, tx, hardwareID)
}

type runtimeInfoQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func runtimeInfoForNode(ctx context.Context, querier runtimeInfoQuerier, hardwareID string) (*NodeRuntimeInfo, error) {
	info := &NodeRuntimeInfo{HardwareID: hardwareID}
	if hardwareID == "" {
		return info, nil
	}
	var manifestJSON string
	err := querier.QueryRowContext(ctx, `SELECT manifest_json FROM node_manifest WHERE hardware_id = ?`, hardwareID).Scan(&manifestJSON)
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
	err = querier.QueryRowContext(ctx, `SELECT lease_json FROM node_lease WHERE hardware_id = ?`, hardwareID).Scan(&leaseJSON)
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
	return r.ResolveCommandIDByTokenForTarget(ctx, "", token)
}

func (r *Router) ResolveCommandIDByTokenForTarget(ctx context.Context, targetHardwareID string, token uint16) (string, error) {
	var commandID string
	var err error
	if targetHardwareID != "" {
		err = r.db.QueryRowContext(ctx, `
			SELECT command_id FROM command_ledger
			WHERE command_token_scope = ? AND command_token = ?
			ORDER BY created_at DESC, rowid DESC
			LIMIT 1
		`, targetHardwareID, int(token)).Scan(&commandID)
	} else {
		err = r.db.QueryRowContext(ctx, `
			SELECT command_id FROM command_ledger
			WHERE command_token = ?
			ORDER BY created_at DESC, rowid DESC
			LIMIT 1
		`, int(token)).Scan(&commandID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return commandID, err
}

func (r *Router) IssueCommand(ctx context.Context, envelope *contracts.Envelope, ingressID, queueKey string) (*PersistAck, int64, error) {
	if envelope.Kind != "command" {
		return nil, 0, contracts.NewValidationError("IssueCommand requires kind=command")
	}
	if err := envelope.Validate(); err != nil {
		return nil, 0, err
	}
	queueKey = commandQueueKey(envelope, queueKey)
	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return nil, 0, err
	}
	persistedAt := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	duplicate, err := r.lookupDuplicate(ctx, tx, envelope)
	if err != nil {
		return nil, 0, err
	}
	if duplicate != "" {
		queueID, err := r.outboxQueueIDTx(ctx, tx, queueKey)
		if err != nil {
			return nil, 0, err
		}
		if err := tx.Commit(); err != nil {
			return nil, 0, err
		}
		return &PersistAck{AckedMessageID: duplicate, Status: "duplicate", Duplicate: true}, queueID, nil
	}
	plan, err := r.planOutboundRoute(ctx, envelope, func(ctx context.Context, hardwareID string) (*NodeRuntimeInfo, error) {
		return r.runtimeInfoForNodeTx(ctx, tx, hardwareID)
	})
	if err != nil {
		return nil, 0, err
	}
	if status := routeStatusForPlan(plan); status != "ready_to_send" {
		reason := "route_pending"
		if plan != nil && plan.Reason != "" {
			reason = plan.Reason
		}
		return nil, 0, contracts.NewValidationError("command route is not sendable: status=%s reason=%s", status, reason)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages (
			message_id, kind, dedupe_key, event_id, command_id, occurred_at,
			source_hardware_id, ingress_id, envelope_json, persisted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, envelope.MessageID, envelope.Kind, envelope.DedupeKey(), envelope.EventID, envelope.CommandID,
		envelope.OccurredAt, envelope.Source.HardwareID, ingressID, string(rawEnvelope), persistedAt); err != nil {
		if isConstraintError(err) {
			_ = tx.Rollback()
			ack, dupErr := r.ingestDuplicateAck(ctx, envelope)
			return ack, 0, dupErr
		}
		return nil, 0, err
	}
	if envelope.CommandID != "" {
		commandToken := payloadInt64(envelope.Payload, "command_token")
		commandTokenScope := commandTokenScopeForEnvelope(envelope)
		commandTokenEpoch := commandTokenEpochForEnvelope(envelope)
		commandTokenValidFrom, commandTokenValidUntil := commandTokenValidityForEnvelope(envelope)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO command_ledger (
				command_id, command_token, command_token_scope, command_token_epoch,
				command_token_valid_from, command_token_valid_until,
				message_id, state, envelope_json, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, envelope.CommandID, nullableInt64(commandToken), commandTokenScope, commandTokenEpoch,
			nullableString(commandTokenValidFrom), nullableString(commandTokenValidUntil),
			envelope.MessageID, "issued", string(rawEnvelope), persistedAt); err != nil {
			if isConstraintError(err) {
				_ = tx.Rollback()
				ack, dupErr := r.ingestDuplicateAck(ctx, envelope)
				return ack, 0, dupErr
			}
			return nil, 0, err
		}
	}
	queueID, err := r.enqueueOutboundTx(ctx, tx, envelope, queueKey, plan, string(rawEnvelope), persistedAt)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return &PersistAck{AckedMessageID: envelope.MessageID, Status: "persisted", Duplicate: false}, queueID, nil
}

func (r *Router) validateOutboundEnvelope(ctx context.Context, envelope *contracts.Envelope) error {
	if envelope == nil || envelope.Kind != "command" || envelope.Target.Kind != "node" {
		return nil
	}
	_, err := r.PlanOutboundRoute(ctx, envelope)
	return err
}

func (r *Router) PlanOutboundRoute(ctx context.Context, envelope *contracts.Envelope) (*RoutePlan, error) {
	return r.planOutboundRoute(ctx, envelope, r.RuntimeInfoForNode)
}

func (r *Router) PlanOutboundRouteSummary(ctx context.Context, envelope *contracts.Envelope) (*OutboxRoutePlanRecord, error) {
	plan, err := r.PlanOutboundRoute(ctx, envelope)
	if err != nil {
		return nil, err
	}
	record := &OutboxRoutePlanRecord{
		RouteStatus: routeStatusForPlan(plan),
		PayloadFit:  false,
		RoutePlan:   plan,
		Detail:      map[string]any{},
	}
	if plan != nil {
		record.SelectedBearer = plan.Bearer
		record.SelectedGatewayID = plan.GatewayID
		record.RouteReason = plan.Reason
		record.PayloadFit = plan.PayloadFit
		record.Detail = plan.Detail
		record.NextHopID = plan.NextHopID
		record.FinalTargetID = plan.FinalTargetID
		record.NextHopShortID = plan.NextHopShortID
		record.FinalTargetShortID = plan.FinalTargetShortID
	}
	return record, nil
}

func (r *Router) planOutboundRoute(ctx context.Context, envelope *contracts.Envelope, lookup nodeRuntimeLookup) (*RoutePlan, error) {
	if envelope == nil {
		return nil, contracts.NewValidationError("route planning requires envelope")
	}
	if lookup == nil {
		lookup = r.RuntimeInfoForNode
	}
	plan := &RoutePlan{
		RouteClass:     deliveryRouteClass(envelope.Delivery),
		AllowRelay:     deliveryBool(envelope.Delivery, "allow_relay"),
		AllowRedundant: deliveryBool(envelope.Delivery, "allow_redundant"),
		HopLimit:       deliveryHopLimit(envelope.Delivery),
		PayloadFit:     true,
		Reason:         "default",
		Detail:         map[string]any{"message_id": envelope.MessageID, "target_kind": envelope.Target.Kind},
	}
	if envelope.Target.Kind != "node" {
		plan.Bearer = "host_local"
		plan.PathLabel = "non_node_target"
		return plan, nil
	}
	info, err := lookup(ctx, envelope.Target.Value)
	if err != nil {
		return nil, err
	}
	if info.Lease != nil && info.Manifest != nil {
		if err := validateLeaseAgainstManifest(info.Lease, info.Manifest); err != nil {
			return nil, err
		}
	}
	if plan.RouteClass == "" {
		plan.Bearer = bearerLabel(info.Lease)
		plan.PathLabel = "lease_primary"
		plan.Reason = "lease_primary_bearer"
		if bearerIsLoRa(plan.Bearer) {
			plan.PayloadFit = false
			plan.Reason = "lora_requires_explicit_route_class"
			return nil, contracts.NewValidationError("LoRa target %s requires explicit compact/summary route_class before enqueue", envelope.Target.Value)
		}
		return plan, nil
	}
	applyRouteClassDefaults(plan, envelope.Delivery)
	switch plan.RouteClass {
	case "sleepy_tiny_control":
		if info.Manifest != nil && !manifestAllowsBearer(info.Manifest, "lora") {
			return nil, contracts.NewValidationError("sleepy_tiny_control requires lora bearer support for target %s", envelope.Target.Value)
		}
		if info.Lease != nil && !bearerAllowedByPolicy(bearerLabel(info.Lease), []string{"lora_direct"}, false) {
			return nil, contracts.NewValidationError("sleepy_tiny_control requires lora_direct-compatible primary bearer for target %s", envelope.Target.Value)
		}
		wire, err := planSleepyCompactCommand(envelope, info)
		if err != nil {
			return nil, err
		}
		plan.Bearer = "lora_direct"
		plan.PathLabel = "sleepy_tiny_control/direct"
		plan.Reason = "sleepy_leaf_short_id_payload_fit"
		plan.Detail["payload_bytes"] = len(wire)
		plan.Detail["profile"] = "JP125_LONG_SF10"
		applyRadioBudgetToPlan(plan, len(wire), false)
		if info.Lease != nil && info.Lease.FabricShortID != nil {
			plan.Detail["target_short_id"] = *info.Lease.FabricShortID
		}
		return plan, nil
	case "maintenance_sync":
		if info.Lease == nil || info.Lease.EffectiveRole != "sleepy_leaf" {
			plan.Bearer = bearerLabel(info.Lease)
			plan.PathLabel = "maintenance/non_sleepy"
			plan.PayloadFit = false
			plan.Reason = "target_not_sleepy_leaf"
			return plan, nil
		}
		if info.Manifest != nil && !manifestAllowsBearer(info.Manifest, "ble_maintenance") {
			return nil, contracts.NewValidationError("maintenance_sync requires maintenance bearer for target %s", envelope.Target.Value)
		}
		plan.Bearer = "ble_maintenance"
		plan.PathLabel = "sleepy_maintenance_window"
		plan.Reason = "maintenance_bearer_required"
		return plan, nil
	case "local_control":
		return r.planPolicyRoute(envelope, plan, info, "local_control", []string{"wifi", "wifi_ip", "usb_cdc"}, false), nil
	case "bulk_wifi_only":
		return r.planPolicyRoute(envelope, plan, info, "bulk_wifi_only", []string{"wifi", "wifi_ip", "wifi_mesh"}, false), nil
	case "critical_alert":
		return r.planPolicyRoute(envelope, plan, info, "critical_alert", []string{"wifi", "wifi_ip", "lora_direct"}, true), nil
	case "redundant_critical":
		return r.planPolicyRoute(envelope, plan, info, "redundant_critical", []string{"wifi", "wifi_ip", "wifi_mesh", "lora_direct", "lora_relay"}, true), nil
	case "lora_relay_1":
		return r.planLoRaRelayOneHop(ctx, envelope, plan, info, lookup)
	case "wifi_mesh_backbone":
		return r.planPolicyRoute(envelope, plan, info, "wifi_mesh_backbone", []string{"wifi_mesh"}, false), nil
	case "sparse_summary":
		return r.planPolicyRoute(envelope, plan, info, "sparse_summary", []string{"lora_direct", "wifi", "wifi_ip"}, true), nil
	case "sleepy_heartbeat":
		return r.planPolicyRoute(envelope, plan, info, "sleepy_heartbeat", []string{"lora_direct", "wifi", "wifi_ip"}, true), nil
	case "normal_state":
		return r.planPolicyRoute(envelope, plan, info, "normal_state", []string{"wifi", "wifi_ip", "lora_direct"}, true), nil
	case "control_event":
		return r.planPolicyRoute(envelope, plan, info, "control_event", []string{"wifi", "wifi_ip"}, false), nil
	default:
		return nil, contracts.NewValidationError("unsupported route_class %s for target %s", plan.RouteClass, envelope.Target.Value)
	}
}

func applyRouteClassDefaults(plan *RoutePlan, delivery *contracts.DeliverySpec) {
	if plan == nil {
		return
	}
	if policy, ok := routeClassPolicy(plan.RouteClass); ok {
		if delivery == nil || delivery.AllowRedundant == nil {
			plan.AllowRedundant = policy.AllowRedundant
		}
		if delivery == nil || delivery.AllowRelay == nil {
			plan.AllowRelay = policy.AllowRelay
		}
		if delivery == nil || delivery.HopLimit == nil {
			if policy.HopLimit != nil {
				hopLimit := *policy.HopLimit
				plan.HopLimit = &hopLimit
			}
		}
		if policy.ServiceLevel != "" {
			plan.Detail["service_level"] = policy.ServiceLevel
		}
		if policy.PendingPolicy != "" {
			plan.Detail["pending_policy"] = policy.PendingPolicy
		}
		return
	}
	if delivery == nil || delivery.AllowRedundant == nil {
		switch plan.RouteClass {
		case "critical_alert", "redundant_critical":
			plan.AllowRedundant = true
		}
	}
	if delivery == nil || delivery.AllowRelay == nil {
		switch plan.RouteClass {
		case "lora_relay_1", "wifi_mesh_backbone", "redundant_critical":
			plan.AllowRelay = true
		}
	}
	if delivery == nil || delivery.HopLimit == nil {
		switch plan.RouteClass {
		case "lora_relay_1":
			hopLimit := 1
			plan.HopLimit = &hopLimit
		case "redundant_critical":
			hopLimit := 2
			plan.HopLimit = &hopLimit
		}
	}
}

func (r *Router) planLoRaRelayOneHop(ctx context.Context, envelope *contracts.Envelope, plan *RoutePlan, finalInfo *NodeRuntimeInfo, lookup nodeRuntimeLookup) (*RoutePlan, error) {
	relayID := payloadString(envelope.Payload, "relay_hardware_id")
	if relayID == "" {
		relayID = deliveryIngressValue(envelope.Delivery, "relay_hardware_id")
	}
	relayInfo := finalInfo
	if relayID != "" && relayID != envelope.Target.Value {
		var err error
		relayInfo, err = lookup(ctx, relayID)
		if err != nil {
			return nil, err
		}
		if relayInfo.Lease != nil && relayInfo.Manifest != nil {
			if err := validateLeaseAgainstManifest(relayInfo.Lease, relayInfo.Manifest); err != nil {
				return nil, err
			}
		}
		plan.NextHopID = relayID
		plan.FinalTargetID = envelope.Target.Value
		plan.Detail["next_hop_hardware_id"] = relayID
		plan.Detail["final_target_hardware_id"] = envelope.Target.Value
		if finalInfo != nil && finalInfo.Lease != nil && finalInfo.Lease.FabricShortID != nil {
			finalShortID := *finalInfo.Lease.FabricShortID
			plan.FinalTargetShortID = &finalShortID
			plan.Detail["final_target_short_id"] = finalShortID
		}
	}
	return r.planPolicyRoute(envelope, plan, relayInfo, "lora_relay_1", []string{"lora_relay"}, true), nil
}

func (r *Router) planPolicyRoute(envelope *contracts.Envelope, plan *RoutePlan, info *NodeRuntimeInfo, routeClass string, allowedBearers []string, allowLoRaIfRepresentable bool) *RoutePlan {
	policy, hasPolicy := routeClassPolicy(routeClass)
	if hasPolicy {
		if len(policy.AllowedBearers) > 0 {
			allowedBearers = append([]string(nil), policy.AllowedBearers...)
		}
		allowLoRaIfRepresentable = routeClassPolicyAllowsLoRa(policy)
		if policy.ServiceLevel != "" {
			plan.Detail["service_level"] = policy.ServiceLevel
		}
		if policy.PendingPolicy != "" {
			plan.Detail["pending_policy"] = policy.PendingPolicy
		}
		if len(policy.ForbiddenBearers) > 0 {
			plan.Detail["forbidden_bearers"] = append([]string(nil), policy.ForbiddenBearers...)
		}
		if policy.MaxLoRaBodyBytes != nil {
			plan.Detail["policy_max_lora_body_bytes"] = *policy.MaxLoRaBodyBytes
		}
	}
	bearer := bearerLabel(nil)
	if info != nil {
		bearer = bearerLabel(info.Lease)
	}
	plan.Bearer = bearer
	plan.PathLabel = routeClass + "/policy"
	plan.Reason = routeClass + "_policy"
	if bearer == "unplanned" {
		plan.PayloadFit = false
		plan.Reason = "lease_missing"
		plan.Detail["allowed_bearers"] = allowedBearers
		return plan
	}
	if !routeClassAllowsTargetRole(routeClass, info) {
		plan.PayloadFit = false
		plan.Reason = "target_role_forbidden_by_route_class"
		plan.Detail["route_class"] = routeClass
		plan.Detail["target_role"] = leaseRole(info)
		plan.Detail["requires_target_role"] = requiredRolesForRouteClass(routeClass)
		return plan
	}
	if envelope.MeshMeta != nil && envelope.MeshMeta.HopCount != nil {
		plan.Detail["current_hop_count"] = *envelope.MeshMeta.HopCount
	}
	applyRouteIntentToPlan(plan, bearer)
	if !plan.PayloadFit {
		return plan
	}
	if hasPolicy && bearerForbiddenByPolicy(bearer, policy.ForbiddenBearers) {
		plan.PayloadFit = false
		plan.Reason = "bearer_forbidden_by_route_class"
		plan.Detail["bearer"] = bearer
		plan.Detail["allowed_bearers"] = allowedBearers
		return plan
	}
	if !bearerAllowedByPolicy(bearer, allowedBearers, plan.AllowRelay) {
		plan.PayloadFit = false
		plan.Reason = "bearer_forbidden_by_route_class"
		plan.Detail["bearer"] = bearer
		plan.Detail["allowed_bearers"] = allowedBearers
		return plan
	}
	if bearerIsLoRa(bearer) && !allowLoRaIfRepresentable {
		plan.PayloadFit = false
		plan.Reason = "lora_forbidden_by_route_class"
		plan.Detail["bearer"] = bearer
		return plan
	}
	if bearerIsLoRa(bearer) && allowLoRaIfRepresentable {
		bytes, source, blockedReason := compactPayloadBodyBytes(envelope)
		if blockedReason != "" {
			plan.PayloadFit = false
			plan.Reason = blockedReason
			plan.Detail["runtime_mode"] = runtimeSecurityMode(envelope)
			return plan
		}
		if bytes <= 0 {
			plan.PayloadFit = false
			plan.Reason = "lora_requires_compact_payload"
			return plan
		}
		relayed := bearerIsRelay(bearer)
		cap, err := jp.BodyCapForProfile(jpProfilePath(), "JP125_LONG_SF10", relayed)
		if err == nil && bytes > cap {
			plan.PayloadFit = false
			plan.Reason = "lora_payload_too_large"
			plan.Detail["payload_bytes"] = bytes
			plan.Detail["cap_bytes"] = cap
			plan.Detail["relayed"] = relayed
			return plan
		}
		if hasPolicy && policy.MaxLoRaBodyBytes != nil && bytes > *policy.MaxLoRaBodyBytes {
			plan.PayloadFit = false
			plan.Reason = "lora_payload_too_large_by_route_policy"
			plan.Detail["payload_bytes"] = bytes
			plan.Detail["cap_bytes"] = *policy.MaxLoRaBodyBytes
			plan.Detail["jp_cap_bytes"] = cap
			plan.Detail["relayed"] = relayed
			return plan
		}
		plan.Detail["payload_bytes"] = bytes
		plan.Detail["payload_fit_source"] = source
		plan.Detail["profile"] = "JP125_LONG_SF10"
		plan.Detail["relayed"] = relayed
		applyRadioBudgetToPlan(plan, bytes, relayed)
		if !plan.PayloadFit {
			return plan
		}
		if relayed {
			if info == nil || info.Lease == nil || info.Lease.FabricShortID == nil {
				plan.PayloadFit = false
				plan.Reason = "lora_relay_requires_next_hop_short_id"
				return plan
			}
			nextHopShortID := *info.Lease.FabricShortID
			plan.NextHopShortID = &nextHopShortID
			if plan.NextHopID == "" {
				plan.NextHopID = info.HardwareID
			}
			if plan.FinalTargetID == "" {
				if finalTargetID := payloadString(envelope.Payload, "final_target_hardware_id"); finalTargetID != "" {
					plan.FinalTargetID = finalTargetID
					plan.Detail["final_target_hardware_id"] = finalTargetID
				}
			}
			plan.Detail["next_hop_short_id"] = nextHopShortID
			plan.Detail["relay_short_id"] = nextHopShortID
			if plan.FinalTargetShortID != nil {
				plan.Detail["final_target_short_id"] = *plan.FinalTargetShortID
			} else if finalTarget := payloadInt64(envelope.Payload, "final_target_short_id"); finalTarget != nil {
				if *finalTarget <= 0 || *finalTarget > 0xFFFF {
					plan.PayloadFit = false
					plan.Reason = "lora_relay_invalid_final_target_short_id"
					plan.Detail["final_target_short_id"] = *finalTarget
					return plan
				}
				finalShortID := int(*finalTarget)
				plan.FinalTargetShortID = &finalShortID
				plan.Detail["final_target_short_id"] = finalShortID
			}
			if plan.FinalTargetShortID == nil {
				plan.PayloadFit = false
				plan.Reason = "lora_relay_requires_final_target_short_id"
				return plan
			}
		}
	}
	return plan
}

func (r *Router) EnqueueOutbound(ctx context.Context, envelope *contracts.Envelope, queueKey string) (int64, error) {
	if err := envelope.Validate(); err != nil {
		return 0, err
	}
	plan, err := r.PlanOutboundRoute(ctx, envelope)
	if err != nil {
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := r.enqueueOutboundTx(ctx, tx, envelope, queueKey, plan, string(raw), now)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (r *Router) enqueueOutboundTx(ctx context.Context, tx *sql.Tx, envelope *contracts.Envelope, queueKey string, plan *RoutePlan, rawEnvelope string, now string) (int64, error) {
	routePlanJSON, err := json.Marshal(plan)
	if err != nil {
		return 0, err
	}
	routeStatus := routeStatusForPlan(plan)
	selectedBearer := ""
	selectedGatewayID := ""
	routeReason := ""
	payloadFit := false
	if plan != nil {
		selectedBearer = plan.Bearer
		selectedGatewayID = plan.GatewayID
		routeReason = plan.Reason
		payloadFit = plan.PayloadFit
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_queue (
			queue_key, message_id, target_kind, target_value, priority, envelope_json,
			status, route_status, selected_bearer, selected_gateway_id, route_reason, payload_fit,
			route_plan_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(queue_key) DO NOTHING
	`, queueKey, envelope.MessageID, envelope.Target.Kind, envelope.Target.Value,
		envelope.Priority, rawEnvelope, routeStatus, selectedBearer, selectedGatewayID, routeReason,
		boolToInt(payloadFit), string(routePlanJSON), now, now)
	if err != nil {
		return 0, err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return r.outboxQueueIDTx(ctx, tx, queueKey)
	}
	return result.LastInsertId()
}

func (r *Router) outboxQueueIDTx(ctx context.Context, tx *sql.Tx, queueKey string) (int64, error) {
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM outbox_queue WHERE queue_key = ?`, queueKey).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

func (r *Router) OutboxRoutePlan(ctx context.Context, queueID int64) (*OutboxRoutePlanRecord, error) {
	var (
		routeStatus       string
		selectedBearer    string
		selectedGatewayID string
		routeReason       string
		payloadFit        int
		routePlanJSON     string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(route_status, ''), COALESCE(selected_bearer, ''), COALESCE(selected_gateway_id, ''),
			COALESCE(route_reason, ''), payload_fit, COALESCE(route_plan_json, '')
		FROM outbox_queue
		WHERE id = ?
	`, queueID).Scan(&routeStatus, &selectedBearer, &selectedGatewayID, &routeReason, &payloadFit, &routePlanJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record := &OutboxRoutePlanRecord{
		QueueID:           queueID,
		RouteStatus:       routeStatus,
		SelectedBearer:    selectedBearer,
		SelectedGatewayID: selectedGatewayID,
		RouteReason:       routeReason,
		PayloadFit:        payloadFit != 0,
		Detail:            map[string]any{},
	}
	if routePlanJSON != "" {
		var plan RoutePlan
		if err := json.Unmarshal([]byte(routePlanJSON), &plan); err != nil {
			return nil, err
		}
		record.RoutePlan = &plan
		record.Detail = plan.Detail
		record.NextHopID = plan.NextHopID
		record.FinalTargetID = plan.FinalTargetID
		record.NextHopShortID = plan.NextHopShortID
		record.FinalTargetShortID = plan.FinalTargetShortID
	}
	return record, nil
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
		  AND route_status = 'ready_to_send'
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
			WHERE id = ? AND status = 'queued' AND route_status = 'ready_to_send'
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
	routeMetrics, err := r.queueRouteStatusMetrics(ctx)
	if err != nil {
		return nil, err
	}
	for key, value := range routeMetrics {
		metrics[key] = value
	}
	queueAges, err := r.queueAgeSamples(ctx)
	if err != nil {
		return nil, err
	}
	metrics["queue_lag_p95_ms"] = percentile95(queueAges)
	return metrics, nil
}

func (r *Router) queueRouteStatusMetrics(ctx context.Context) (map[string]int64, error) {
	metrics := map[string]int64{
		"queued_ready_count":          0,
		"queued_route_pending_count":  0,
		"queued_route_blocked_count":  0,
		"oldest_ready_to_send_age_ms": 0,
		"oldest_route_pending_age_ms": 0,
		"oldest_route_blocked_age_ms": 0,
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(route_status, 'route_pending'), COUNT(*)
		FROM outbox_queue
		WHERE status = 'queued'
		GROUP BY route_status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var routeStatus string
		var count int64
		if err := rows.Scan(&routeStatus, &count); err != nil {
			return nil, err
		}
		switch routeStatus {
		case "ready_to_send":
			metrics["queued_ready_count"] = count
		case "route_blocked":
			metrics["queued_route_blocked_count"] = count
		default:
			metrics["queued_route_pending_count"] += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	crossRows, err := r.db.QueryContext(ctx, `
		SELECT status, COALESCE(route_status, 'route_pending'), COUNT(*)
		FROM outbox_queue
		GROUP BY status, route_status
	`)
	if err != nil {
		return nil, err
	}
	defer crossRows.Close()
	for crossRows.Next() {
		var status string
		var routeStatus string
		var count int64
		if err := crossRows.Scan(&status, &routeStatus, &count); err != nil {
			return nil, err
		}
		metrics[status+"_"+routeStatus+"_count"] = count
	}
	if err := crossRows.Err(); err != nil {
		return nil, err
	}
	for _, item := range []struct {
		status string
		key    string
	}{
		{"ready_to_send", "oldest_ready_to_send_age_ms"},
		{"route_pending", "oldest_route_pending_age_ms"},
		{"route_blocked", "oldest_route_blocked_age_ms"},
	} {
		var ageMS int64
		if err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(CAST((julianday('now') - julianday(MIN(created_at))) * 86400000 AS INTEGER), 0)
			FROM outbox_queue
			WHERE status = 'queued' AND route_status = ?
		`, item.status).Scan(&ageMS); err != nil {
			return nil, err
		}
		metrics[item.key] = ageMS
	}
	reasonRows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(route_status, 'route_pending'), COALESCE(NULLIF(route_reason, ''), 'unknown'), COUNT(*)
		FROM outbox_queue
		WHERE status = 'queued'
		GROUP BY route_status, route_reason
	`)
	if err != nil {
		return nil, err
	}
	defer reasonRows.Close()
	for reasonRows.Next() {
		var routeStatus string
		var reason string
		var count int64
		if err := reasonRows.Scan(&routeStatus, &reason, &count); err != nil {
			return nil, err
		}
		metrics["queued_"+metricToken(routeStatus)+"_reason_"+metricToken(reason)+"_count"] = count
	}
	if err := reasonRows.Err(); err != nil {
		return nil, err
	}
	deadRows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(dead_reason, ''), 'unknown'), COUNT(*)
		FROM outbox_queue
		WHERE status = 'dead'
		GROUP BY dead_reason
	`)
	if err != nil {
		return nil, err
	}
	defer deadRows.Close()
	for deadRows.Next() {
		var reason string
		var count int64
		if err := deadRows.Scan(&reason, &count); err != nil {
			return nil, err
		}
		metrics["dead_reason_"+metricToken(reason)+"_count"] = count
	}
	return metrics, deadRows.Err()
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
		  AND route_status = 'ready_to_send'
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

func (r *Router) LatestHeartbeat(ctx context.Context, heartbeatKey string) (*HeartbeatRecord, error) {
	record, err := r.latestHeartbeatByKey(ctx, heartbeatKey)
	if err != nil || record != nil {
		return record, err
	}
	if strings.HasPrefix(heartbeatKey, "short:") {
		return r.latestHeartbeatByKey(ctx, "node:"+heartbeatKey)
	}
	if strings.Contains(heartbeatKey, ":") {
		return nil, nil
	}
	// Backwards-compatible lookup for callers that still pass a bare gateway/node id.
	if record, err = r.latestHeartbeatByKey(ctx, "gateway:"+heartbeatKey); err != nil || record != nil {
		return record, err
	}
	return r.latestHeartbeatByKey(ctx, "node:"+heartbeatKey)
}

func (r *Router) LatestHeartbeatBySubject(ctx context.Context, subjectKind, subjectID string) (*HeartbeatRecord, error) {
	if subjectKind == "" || subjectID == "" {
		return nil, contracts.NewValidationError("heartbeat subject_kind and subject_id are required")
	}
	return r.latestHeartbeatByKey(ctx, subjectKind+":"+subjectID)
}

func (r *Router) ListHeartbeats(ctx context.Context, subjectKind string, limit int) ([]HeartbeatRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var (
		rows *sql.Rows
		err  error
	)
	if subjectKind == "" {
		rows, err = r.db.QueryContext(ctx, `
			SELECT heartbeat_key, COALESCE(gateway_id, ''), COALESCE(subject_kind, ''), COALESCE(subject_id, ''),
				source_hardware_id, ingress_id, COALESCE(host_link, ''), COALESCE(bearer, ''),
				COALESCE(status, ''), live, message_id, payload_json, updated_at
			FROM heartbeat_ledger
			ORDER BY updated_at DESC, heartbeat_key ASC
			LIMIT ?
		`, limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT heartbeat_key, COALESCE(gateway_id, ''), COALESCE(subject_kind, ''), COALESCE(subject_id, ''),
				source_hardware_id, ingress_id, COALESCE(host_link, ''), COALESCE(bearer, ''),
				COALESCE(status, ''), live, message_id, payload_json, updated_at
			FROM heartbeat_ledger
			WHERE subject_kind = ?
			ORDER BY updated_at DESC, heartbeat_key ASC
			LIMIT ?
		`, subjectKind, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []HeartbeatRecord{}
	for rows.Next() {
		record, err := scanHeartbeatRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, rows.Err()
}

func (r *Router) latestHeartbeatByKey(ctx context.Context, heartbeatKey string) (*HeartbeatRecord, error) {
	var (
		gatewayID   string
		subjectKind string
		subjectID   string
		sourceID    string
		ingressID   string
		hostLink    string
		bearer      string
		status      string
		live        int
		messageID   string
		payload     string
		updatedAt   string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(gateway_id, ''), COALESCE(subject_kind, ''), COALESCE(subject_id, ''),
			source_hardware_id, ingress_id, COALESCE(host_link, ''), COALESCE(bearer, ''),
			COALESCE(status, ''), live, message_id, payload_json, updated_at
		FROM heartbeat_ledger
		WHERE heartbeat_key = ?
	`, heartbeatKey).Scan(&gatewayID, &subjectKind, &subjectID, &sourceID, &ingressID, &hostLink, &bearer, &status, &live, &messageID, &payload, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return heartbeatRecordFromValues(heartbeatKey, gatewayID, subjectKind, subjectID, sourceID, ingressID, hostLink, bearer, status, live, messageID, payload, updatedAt)
}

type heartbeatScanner interface {
	Scan(dest ...any) error
}

func scanHeartbeatRecord(scanner heartbeatScanner) (*HeartbeatRecord, error) {
	var (
		heartbeatKey string
		gatewayID    string
		subjectKind  string
		subjectID    string
		sourceID     string
		ingressID    string
		hostLink     string
		bearer       string
		status       string
		live         int
		messageID    string
		payload      string
		updatedAt    string
	)
	if err := scanner.Scan(&heartbeatKey, &gatewayID, &subjectKind, &subjectID, &sourceID, &ingressID, &hostLink, &bearer, &status, &live, &messageID, &payload, &updatedAt); err != nil {
		return nil, err
	}
	return heartbeatRecordFromValues(heartbeatKey, gatewayID, subjectKind, subjectID, sourceID, ingressID, hostLink, bearer, status, live, messageID, payload, updatedAt)
}

func heartbeatRecordFromValues(heartbeatKey, gatewayID, subjectKind, subjectID, sourceID, ingressID, hostLink, bearer, status string, live int, messageID, payload, updatedAt string) (*HeartbeatRecord, error) {
	var payloadMap map[string]any
	if err := json.Unmarshal([]byte(payload), &payloadMap); err != nil {
		return nil, err
	}
	return &HeartbeatRecord{
		HeartbeatKey:     heartbeatKey,
		GatewayID:        gatewayID,
		SubjectKind:      subjectKind,
		SubjectID:        subjectID,
		SourceHardwareID: sourceID,
		IngressID:        ingressID,
		HostLink:         hostLink,
		Bearer:           bearer,
		Status:           status,
		Live:             live != 0,
		MessageID:        messageID,
		UpdatedAt:        updatedAt,
		Payload:          payloadMap,
	}, nil
}

func (r *Router) Compact(ctx context.Context, policy RetentionPolicy, now time.Time) (*RetentionResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := &RetentionResult{}
	var err error
	if policy.HeartbeatRetentionDays > 0 {
		result.DeletedHeartbeats, err = r.deleteOlderThan(ctx, "heartbeat_ledger", "updated_at", now.AddDate(0, 0, -policy.HeartbeatRetentionDays), "")
		if err != nil {
			return nil, err
		}
	}
	if policy.RadioObservationRetentionHrs > 0 {
		result.DeletedRadioObservations, err = r.deleteOlderThan(ctx, "radio_packet_observation", "last_seen_at", now.Add(-time.Duration(policy.RadioObservationRetentionHrs)*time.Hour), "")
		if err != nil {
			return nil, err
		}
	}
	if policy.DeadQueueRetentionDays > 0 {
		deadQueues, deadAttempts, err := r.deleteDeadQueueOlderThan(ctx, now.AddDate(0, 0, -policy.DeadQueueRetentionDays))
		if err != nil {
			return nil, err
		}
		result.DeletedDeadQueueItems = deadQueues
		result.DeletedDeadQueueAttempts = deadAttempts
	}
	if policy.FileChunkRetentionDays > 0 {
		result.DeletedFileChunks, err = r.deleteOlderThan(ctx, "file_chunk_ledger", "updated_at", now.AddDate(0, 0, -policy.FileChunkRetentionDays), "")
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *Router) deleteDeadQueueOlderThan(ctx context.Context, cutoff time.Time) (int64, int64, error) {
	cutoffValue := cutoff.UTC().Format(time.RFC3339Nano)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	attempts, err := tx.ExecContext(ctx, `
		DELETE FROM outbox_attempt
		WHERE queue_id IN (
			SELECT id FROM outbox_queue
			WHERE updated_at < ? AND status = 'dead'
		)
	`, cutoffValue)
	if err != nil {
		return 0, 0, err
	}
	queues, err := tx.ExecContext(ctx, `
		DELETE FROM outbox_queue
		WHERE updated_at < ? AND status = 'dead'
	`, cutoffValue)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	deletedAttempts, _ := attempts.RowsAffected()
	deletedQueues, _ := queues.RowsAffected()
	return deletedQueues, deletedAttempts, nil
}

func (r *Router) deleteOlderThan(ctx context.Context, table, column string, cutoff time.Time, extraPredicate string) (int64, error) {
	query := fmt.Sprintf("DELETE FROM %s WHERE %s < ?", table, column)
	if extraPredicate != "" {
		query += " AND " + extraPredicate
	}
	result, err := r.db.ExecContext(ctx, query, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *Router) LatestFabricSummary(ctx context.Context, summaryKey string) (*FabricSummaryRecord, error) {
	var (
		sourceID  string
		messageID string
		payload   string
		updatedAt string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT source_hardware_id, message_id, payload_json, updated_at
		FROM fabric_summary_latest
		WHERE summary_key = ?
	`, summaryKey).Scan(&sourceID, &messageID, &payload, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payloadMap map[string]any
	if err := json.Unmarshal([]byte(payload), &payloadMap); err != nil {
		return nil, err
	}
	return &FabricSummaryRecord{
		SummaryKey:       summaryKey,
		SourceHardwareID: sourceID,
		MessageID:        messageID,
		UpdatedAt:        updatedAt,
		Payload:          payloadMap,
	}, nil
}

func (r *Router) FileChunkProgress(ctx context.Context, fileID string) (*FileChunkStatus, error) {
	var status FileChunkStatus
	var minChunkIndex int
	status.FileID = fileID
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(total_chunks), 0), COALESCE(MAX(chunk_index), -1), COALESCE(MIN(chunk_index), -1)
		FROM file_chunk_ledger
		WHERE file_id = ?
	`, fileID).Scan(&status.ReceivedChunks, &status.TotalChunks, &status.HighestChunkIndex, &minChunkIndex)
	if err != nil {
		return nil, err
	}
	err = r.db.QueryRowContext(ctx, `
		SELECT COALESCE(message_id, ''), COALESCE(updated_at, '')
		FROM file_chunk_ledger
		WHERE file_id = ?
		ORDER BY updated_at DESC, chunk_index DESC
		LIMIT 1
	`, fileID).Scan(&status.LastMessageID, &status.LastUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		status.LastMessageID = ""
		status.LastUpdatedAt = ""
	} else if err != nil {
		return nil, err
	}
	status.Complete = status.TotalChunks > 0 &&
		status.ReceivedChunks == status.TotalChunks &&
		minChunkIndex == 0 &&
		status.HighestChunkIndex == status.TotalChunks-1
	return &status, nil
}

func (r *Router) RecordOutboundAttempt(
	ctx context.Context,
	queueID int64,
	bearer, gatewayID, pathLabel string,
	detail map[string]any,
) (*OutboundAttempt, error) {
	if queueID <= 0 {
		return nil, errors.New("queue_id must be > 0")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	detailJSON, err := json.Marshal(cloneMap(detail))
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var attemptNo int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt_no), 0) + 1 FROM outbox_attempt WHERE queue_id = ?`, queueID).Scan(&attemptNo); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_attempt (
			queue_id, attempt_no, bearer, gateway_id, path_label, status, detail_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'planned', ?, ?, ?)
	`, queueID, attemptNo, bearer, gatewayID, pathLabel, string(detailJSON), now, now)
	if err != nil {
		return nil, err
	}
	attemptID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &OutboundAttempt{
		AttemptID: attemptID,
		QueueID:   queueID,
		AttemptNo: attemptNo,
		Bearer:    bearer,
		GatewayID: gatewayID,
		PathLabel: pathLabel,
		Status:    "planned",
		Detail:    cloneMap(detail),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (r *Router) UpdateOutboundAttempt(ctx context.Context, attemptID int64, status string, detail map[string]any) error {
	if attemptID <= 0 {
		return errors.New("attempt_id must be > 0")
	}
	detailJSON, err := json.Marshal(cloneMap(detail))
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_attempt
		SET status = ?, detail_json = ?, updated_at = ?
		WHERE id = ?
	`, status, string(detailJSON), time.Now().UTC().Format(time.RFC3339Nano), attemptID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errors.New("outbound attempt not found")
	}
	return nil
}

func (r *Router) ListOutboundAttempts(ctx context.Context, queueID int64) ([]OutboundAttempt, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, attempt_no, COALESCE(bearer, ''), COALESCE(gateway_id, ''), COALESCE(path_label, ''), status, detail_json, created_at, updated_at
		FROM outbox_attempt
		WHERE queue_id = ?
		ORDER BY attempt_no
	`, queueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attempts []OutboundAttempt
	for rows.Next() {
		var attempt OutboundAttempt
		var detailJSON string
		attempt.QueueID = queueID
		if err := rows.Scan(&attempt.AttemptID, &attempt.AttemptNo, &attempt.Bearer, &attempt.GatewayID, &attempt.PathLabel, &attempt.Status, &detailJSON, &attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
			return nil, err
		}
		if detailJSON != "" {
			if err := json.Unmarshal([]byte(detailJSON), &attempt.Detail); err != nil {
				return nil, err
			}
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
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
	if err := validateOptionalFabricShortID(lease.FabricShortID, "lease.fabric_short_id"); err != nil {
		return err
	}
	if manifest == nil {
		return nil
	}
	if len(manifest.AllowedNetworkRoles) > 0 && !containsString(manifest.AllowedNetworkRoles, lease.EffectiveRole) {
		return contracts.NewValidationError("lease role %s is not allowed for hardware_id %s", lease.EffectiveRole, manifest.HardwareID)
	}
	if lease.PrimaryBearer != "" && !manifestAllowsBearer(manifest, manifestBearerName(lease.PrimaryBearer)) {
		return contracts.NewValidationError("lease primary_bearer %s is not supported by hardware_id %s", lease.PrimaryBearer, manifest.HardwareID)
	}
	if lease.FallbackBearer != "" && !manifestAllowsBearer(manifest, manifestBearerName(lease.FallbackBearer)) {
		return contracts.NewValidationError("lease fallback_bearer %s is not supported by hardware_id %s", lease.FallbackBearer, manifest.HardwareID)
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
	if info.Lease.FabricShortID == nil {
		return nil, contracts.NewValidationError("sleepy_tiny_control requires fabric_short_id for target %s", envelope.Target.Value)
	}
	if err := validateOptionalFabricShortID(info.Lease.FabricShortID, "lease.fabric_short_id"); err != nil {
		return nil, err
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
	wire, err := onair.EncodeCompactCommand(uint16(*info.Lease.FabricShortID), false, 0, onair.CompactCommandBody{
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
		if value != float64(int64(value)) {
			return nil
		}
		result := int64(value)
		return &result
	default:
		return nil
	}
}

func deliveryRouteClass(delivery *contracts.DeliverySpec) string {
	if delivery == nil {
		return ""
	}
	return delivery.RouteClass
}

func deliveryBool(delivery *contracts.DeliverySpec, key string) bool {
	if delivery == nil {
		return false
	}
	switch key {
	case "allow_relay":
		return delivery.AllowRelay != nil && *delivery.AllowRelay
	case "allow_redundant":
		return delivery.AllowRedundant != nil && *delivery.AllowRedundant
	default:
		return false
	}
}

func deliveryHopLimit(delivery *contracts.DeliverySpec) *int {
	if delivery == nil {
		return nil
	}
	return delivery.HopLimit
}

func bearerLabel(lease *contracts.Lease) string {
	if lease == nil || lease.PrimaryBearer == "" {
		return "unplanned"
	}
	switch lease.PrimaryBearer {
	case "lora":
		return "lora_direct"
	case "wifi":
		return "wifi_ip"
	default:
		return lease.PrimaryBearer
	}
}

func applyRadioBudgetToPlan(plan *RoutePlan, bodyBytes int, relayed bool) {
	if plan == nil || bodyBytes <= 0 {
		return
	}
	decision := map[string]any{
		"body_bytes": bodyBytes,
		"relayed":    relayed,
	}
	overheadBytes := jp.DirectUplinkOverheadBytes
	if relayed {
		overheadBytes = jp.RelayedUplinkOverheadBytes
	}
	totalPayloadBytes := bodyBytes + overheadBytes
	decision["overhead_bytes"] = overheadBytes
	decision["total_payload_bytes"] = totalPayloadBytes

	budget, err := radioBudgetForRouteClass(plan.RouteClass)
	if err != nil {
		plan.PayloadFit = false
		plan.Reason = "radio_budget_policy_unavailable"
		decision["decision"] = "block"
		decision["error"] = err.Error()
		plan.Detail["radio_budget"] = decision
		return
	}
	profile := budget.Profile
	if profile == "" {
		profile = "JP125_LONG_SF10"
	}
	decision["profile"] = profile
	decision["max_airtime_ms"] = budget.MaxAirtimeMS
	if budget.OccupancyWindowSeconds > 0 {
		decision["occupancy_window_seconds"] = budget.OccupancyWindowSeconds
	}
	if budget.MaxWindowAirtimeMS > 0 {
		decision["max_window_airtime_ms"] = budget.MaxWindowAirtimeMS
	}
	airtimeMS, err := jp.AirtimeMSForProfile(jpProfilePath(), profile, totalPayloadBytes)
	if err != nil {
		plan.PayloadFit = false
		plan.Reason = "radio_budget_estimate_failed"
		decision["decision"] = "block"
		decision["error"] = err.Error()
		plan.Detail["radio_budget"] = decision
		return
	}
	decision["estimated_airtime_ms"] = airtimeMS
	if budget.MaxAirtimeMS > 0 && airtimeMS > budget.MaxAirtimeMS {
		plan.PayloadFit = false
		plan.Reason = "radio_airtime_budget_exceeded"
		decision["decision"] = "block"
		plan.Detail["radio_budget"] = decision
		return
	}
	decision["decision"] = "allow"
	plan.Detail["radio_budget"] = decision
}

func radioBudgetForRouteClass(routeClass string) (contractpolicy.RadioBudgetPolicy, error) {
	artifact, err := contractpolicy.LoadRadioBudget()
	if err != nil {
		return contractpolicy.RadioBudgetPolicy{}, err
	}
	budget := artifact.Defaults
	if routeBudget, ok := artifact.RouteClasses[routeClass]; ok {
		if routeBudget.Profile != "" {
			budget.Profile = routeBudget.Profile
		}
		if routeBudget.MaxAirtimeMS > 0 {
			budget.MaxAirtimeMS = routeBudget.MaxAirtimeMS
		}
		if routeBudget.OccupancyWindowSeconds > 0 {
			budget.OccupancyWindowSeconds = routeBudget.OccupancyWindowSeconds
		}
		if routeBudget.MaxWindowAirtimeMS > 0 {
			budget.MaxWindowAirtimeMS = routeBudget.MaxWindowAirtimeMS
		}
	}
	return budget, nil
}

func bearerIsLoRa(bearer string) bool {
	return bearer == "lora" || strings.HasPrefix(bearer, "lora_")
}

func bearerIsRelay(bearer string) bool {
	return bearer == "lora_relay" || bearer == "lora_mesh"
}

func bearerUsesHopLimit(bearer string) bool {
	return bearerIsRelay(bearer) || bearer == "wifi_mesh"
}

func applyRouteIntentToPlan(plan *RoutePlan, bearer string) {
	if plan == nil {
		return
	}
	plan.Detail["selected_bearer"] = bearer
	plan.Detail["relay_requested"] = plan.AllowRelay
	plan.Detail["redundant_requested"] = plan.AllowRedundant
	if plan.AllowRedundant {
		plan.Detail["redundant_candidates"] = []string{"primary", "lora_direct", "wifi_ip", "wifi_mesh"}
		plan.Detail["redundant_delivery_mode"] = "route_plan_candidates"
	}
	if plan.HopLimit != nil {
		plan.Detail["hop_limit"] = *plan.HopLimit
		if *plan.HopLimit == 0 && bearerUsesHopLimit(bearer) {
			plan.PayloadFit = false
			plan.Reason = "relay_forbidden_by_hop_limit"
			return
		}
		if plan.AllowRelay && bearerUsesHopLimit(bearer) && deliveryHopCount(plan) >= *plan.HopLimit {
			plan.PayloadFit = false
			plan.Reason = "relay_ttl_exhausted"
			return
		}
	}
	if bearerIsRelay(bearer) && !plan.AllowRelay {
		plan.PayloadFit = false
		plan.Reason = "relay_forbidden_by_delivery_policy"
	}
}

func deliveryHopCount(plan *RoutePlan) int {
	if plan == nil {
		return 0
	}
	if value, ok := plan.Detail["current_hop_count"].(int); ok {
		return value
	}
	return 0
}

func bearerAllowedByPolicy(bearer string, allowed []string, allowRelay bool) bool {
	for _, candidate := range allowed {
		if bearer == candidate {
			return true
		}
		if allowRelay && bearerIsRelay(bearer) && manifestBearerName(bearer) == manifestBearerName(candidate) {
			return true
		}
	}
	return false
}

func routeClassPolicy(routeClass string) (contractpolicy.RouteClassPolicy, bool) {
	if routeClass == "" {
		return contractpolicy.RouteClassPolicy{}, false
	}
	return contractpolicy.MustRouteClass(routeClass)
}

func routeClassPolicyAllowsLoRa(policy contractpolicy.RouteClassPolicy) bool {
	for _, bearer := range policy.AllowedBearers {
		if bearerIsLoRa(bearer) {
			return true
		}
	}
	return false
}

func bearerForbiddenByPolicy(bearer string, forbidden []string) bool {
	for _, candidate := range forbidden {
		if bearer == candidate {
			return true
		}
		if candidate == "lora" && bearerIsLoRa(bearer) {
			return true
		}
		if candidate == "wifi" && manifestBearerName(bearer) == "wifi" {
			return true
		}
	}
	return false
}

func routeClassAllowsTargetRole(routeClass string, info *NodeRuntimeInfo) bool {
	required := requiredRolesForRouteClass(routeClass)
	if len(required) == 0 {
		return true
	}
	role := leaseRole(info)
	for _, candidate := range required {
		if role == candidate {
			return true
		}
	}
	return false
}

func leaseRole(info *NodeRuntimeInfo) string {
	if info == nil || info.Lease == nil {
		return ""
	}
	return info.Lease.EffectiveRole
}

func requiredRolesForRouteClass(routeClass string) []string {
	if policy, ok := routeClassPolicy(routeClass); ok && len(policy.RequiresTargetRole) > 0 {
		return append([]string(nil), policy.RequiresTargetRole...)
	}
	switch routeClass {
	case "sleepy_tiny_control", "maintenance_sync", "sleepy_heartbeat":
		return []string{"sleepy_leaf"}
	case "lora_relay_1":
		return []string{"lora_relay"}
	case "wifi_mesh_backbone":
		return []string{"mesh_router", "mesh_root", "dual_bearer_bridge"}
	default:
		return nil
	}
}

func routeStatusForPlan(plan *RoutePlan) string {
	if plan == nil || plan.Bearer == "" || plan.Bearer == "unplanned" {
		return "route_pending"
	}
	if !plan.PayloadFit {
		return "route_blocked"
	}
	return "ready_to_send"
}

func commandQueueKey(envelope *contracts.Envelope, queueKey string) string {
	if queueKey != "" {
		return queueKey
	}
	if envelope != nil && envelope.CommandID != "" {
		return "command:" + envelope.CommandID
	}
	if envelope != nil {
		return "message:" + envelope.MessageID
	}
	return "message:"
}

func payloadDeclaredBytes(envelope *contracts.Envelope) (int, string) {
	if envelope == nil || !payloadBool(envelope.Payload, "allow_declared_lora_size_for_alpha") {
		return 0, ""
	}
	mode := runtimeSecurityMode(envelope)
	if policy, ok := securityModePolicy(mode); ok && !policy.AllowDeclaredLoRaSizeForAlpha {
		return 0, "lora_declared_payload_forbidden_in_production"
	}
	if mode == "production" {
		return 0, "lora_declared_payload_forbidden_in_production"
	}
	for _, key := range []string{"payload_bytes", "body_bytes", "summary_bytes", "compact_bytes"} {
		if value := payloadInt64(envelope.Payload, key); value != nil && *value > 0 && *value <= int64(^uint(0)>>1) {
			return int(*value), ""
		}
	}
	return 0, ""
}

func compactPayloadBodyBytes(envelope *contracts.Envelope) (int, string, string) {
	if envelope == nil {
		return 0, "", ""
	}
	switch envelope.Kind {
	case "event":
		if eventBodyRepresentable(envelope.Payload) {
			return len([]byte{0, 0, 0, 0}), "onair_event_body", ""
		}
	case "heartbeat":
		if heartbeatBodyRepresentable(envelope.Payload) {
			return len([]byte{0, 0, 0, 0, 0}), "onair_heartbeat_body", ""
		}
	case "state":
		if stateBodyRepresentable(envelope.Payload) {
			return len([]byte{0, 0, 0}), "onair_state_body", ""
		}
	}
	declared, blockedReason := payloadDeclaredBytes(envelope)
	if blockedReason != "" {
		return 0, "", blockedReason
	}
	if declared > 0 {
		return declared, "declared_alpha_opt_in", ""
	}
	return 0, "", ""
}

func eventBodyRepresentable(payload map[string]any) bool {
	return payloadByteValue(payload, "event_code") > 0 ||
		eventCodeToken(payloadString(payload, "event_type")) > 0 ||
		eventCodeToken(payloadString(payload, "event_name")) > 0
}

func heartbeatBodyRepresentable(payload map[string]any) bool {
	return payloadByteValue(payload, "health") > 0 ||
		payloadByteValue(payload, "health_code") > 0 ||
		payloadByteValue(payload, "battery_bucket") > 0 ||
		payloadByteValue(payload, "link_quality") > 0 ||
		payloadByteValue(payload, "uptime_bucket") > 0 ||
		payloadByteValue(payload, "flags") > 0
}

func stateBodyRepresentable(payload map[string]any) bool {
	key := payloadString(payload, "state_key")
	if key == "" {
		key = payloadString(payload, "key")
	}
	if key == "node.power" {
		return true
	}
	return payloadByteValue(payload, "key_token") > 0 && payloadByteValue(payload, "value_token") > 0
}

func eventCodeToken(value string) int {
	switch strings.ToLower(value) {
	case "battery_low":
		return int(onair.EventCodeBatteryLow)
	case "motion_detected":
		return int(onair.EventCodeMotionDetected)
	case "leak_detected":
		return int(onair.EventCodeLeakDetected)
	case "tamper":
		return int(onair.EventCodeTamper)
	case "threshold_crossed":
		return int(onair.EventCodeThresholdCrossed)
	default:
		return 0
	}
}

func payloadByteValue(payload map[string]any, key string) int {
	value := payloadInt64(payload, key)
	if value == nil || *value < 0 || *value > 0xFF {
		return 0
	}
	return int(*value)
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return value
}

func payloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	value, _ := payload[key].(bool)
	return value
}

func runtimeSecurityMode(envelope *contracts.Envelope) string {
	if envelope == nil {
		return defaultRuntimeSecurityMode()
	}
	if envelope.Delivery != nil {
		if mode, _ := envelope.Delivery.IngressMeta["runtime_mode"].(string); mode != "" {
			return normalizeRuntimeSecurityMode(mode)
		}
	}
	if mode := payloadString(envelope.Payload, "runtime_mode"); mode != "" {
		return normalizeRuntimeSecurityMode(mode)
	}
	if payloadBool(envelope.Payload, "production") {
		return "production"
	}
	return defaultRuntimeSecurityMode()
}

func hasExplicitRuntimeSecurityMode(envelope *contracts.Envelope) bool {
	if envelope == nil {
		return false
	}
	if envelope.Delivery != nil {
		if mode, _ := envelope.Delivery.IngressMeta["runtime_mode"].(string); mode != "" {
			return true
		}
	}
	return payloadString(envelope.Payload, "runtime_mode") != "" || payloadBool(envelope.Payload, "production")
}

func normalizeRuntimeSecurityMode(mode string) string {
	if artifact, err := contractpolicy.LoadSecurityModes(); err == nil {
		if _, ok := artifact.Modes[mode]; ok {
			return mode
		}
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "dev", "development":
		return "dev"
	case "production", "prod":
		return "production"
	default:
		return defaultRuntimeSecurityMode()
	}
}

func defaultRuntimeSecurityMode() string {
	artifact, err := contractpolicy.LoadSecurityModes()
	if err == nil && artifact.DefaultMode != "" {
		return artifact.DefaultMode
	}
	return "field-alpha"
}

func securityModePolicy(mode string) (contractpolicy.SecurityModePolicy, bool) {
	artifact, err := contractpolicy.LoadSecurityModes()
	if err != nil {
		return contractpolicy.SecurityModePolicy{}, false
	}
	policy, ok := artifact.Modes[normalizeRuntimeSecurityMode(mode)]
	return policy, ok
}

func envelopeOnAirKey(envelope *contracts.Envelope) string {
	if envelope == nil {
		return ""
	}
	if originKey := payloadString(envelope.Payload, "origin_onair_key"); originKey != "" {
		return originKey
	}
	if envelope.MeshMeta != nil && envelope.MeshMeta.OnAirKey != "" {
		return envelope.MeshMeta.OnAirKey
	}
	return payloadString(envelope.Payload, "onair_key")
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func metricToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return "unknown"
	}
	return result
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func heartbeatKeyForEnvelope(envelope *contracts.Envelope) string {
	subjectKind := heartbeatSubjectKind(envelope)
	subjectID := payloadString(envelope.Payload, "subject_id")
	if subjectID != "" {
		return subjectKind + ":" + subjectID
	}
	gatewayID := payloadString(envelope.Payload, "gateway_id")
	if gatewayID != "" {
		return "gateway:" + gatewayID
	}
	return subjectKind + ":" + envelope.Source.HardwareID
}

func validateHeartbeatSubject(envelope *contracts.Envelope) error {
	if envelope == nil || envelope.Kind != "heartbeat" {
		return nil
	}
	requireStrict := payloadBool(envelope.Payload, "production") || payloadBool(envelope.Payload, "strict_subject")
	if hasExplicitRuntimeSecurityMode(envelope) {
		if policy, ok := securityModePolicy(runtimeSecurityMode(envelope)); ok && policy.RequireStrictHeartbeatSubject {
			requireStrict = true
		}
	}
	if !requireStrict {
		return nil
	}
	subjectKind := payloadString(envelope.Payload, "subject_kind")
	subjectID := payloadString(envelope.Payload, "subject_id")
	if subjectKind == "" || subjectID == "" {
		return contracts.NewValidationError("production heartbeat requires subject_kind and subject_id")
	}
	switch subjectKind {
	case "gateway", "node", "host_agent", "relay":
	default:
		return contracts.NewValidationError("production heartbeat subject_kind %q is not supported", subjectKind)
	}
	if gatewayID := payloadString(envelope.Payload, "gateway_id"); gatewayID != "" && subjectKind == "gateway" && gatewayID != subjectID {
		return contracts.NewValidationError("gateway heartbeat subject_id must match gateway_id")
	}
	return nil
}

func heartbeatSubjectID(envelope *contracts.Envelope) string {
	if subjectID := payloadString(envelope.Payload, "subject_id"); subjectID != "" {
		return subjectID
	}
	if gatewayID := payloadString(envelope.Payload, "gateway_id"); gatewayID != "" {
		return gatewayID
	}
	if nodeID := payloadString(envelope.Payload, "node_id"); nodeID != "" {
		return nodeID
	}
	return envelope.Source.HardwareID
}

func heartbeatSubjectKind(envelope *contracts.Envelope) string {
	if subjectKind := payloadString(envelope.Payload, "subject_kind"); subjectKind != "" {
		return subjectKind
	}
	if payloadString(envelope.Payload, "gateway_id") != "" {
		return "gateway"
	}
	status := payloadString(envelope.Payload, "status")
	switch status {
	case "lora_ingress", "radio_tx_queued", "hop_buffered", "radio_handoff_accepted":
		return "gateway"
	default:
		return "node"
	}
}

func heartbeatStatusImpliesLive(status string) bool {
	switch status {
	case "live", "lora_ingress", "radio_tx_queued", "hop_buffered", "radio_handoff_accepted", "onair_heartbeat", "sleepy_heartbeat":
		return true
	default:
		return false
	}
}

func fabricSummaryKeyForEnvelope(envelope *contracts.Envelope) string {
	if scope := payloadString(envelope.Payload, "summary_scope"); scope != "" {
		return scope
	}
	if siteID := payloadString(envelope.Payload, "site_id"); siteID != "" {
		return siteID
	}
	if envelope.Target.Value != "" {
		return envelope.Target.Value
	}
	return envelope.Source.HardwareID
}

func fileChunkIDForEnvelope(envelope *contracts.Envelope) string {
	if fileID := payloadString(envelope.Payload, "file_id"); fileID != "" {
		return fileID
	}
	if envelope.CorrelationID != "" {
		return envelope.CorrelationID
	}
	return envelope.Source.HardwareID + ":" + envelope.Target.Value
}

func stateCandidateOccurredAt(candidate stateCandidate) string {
	if candidate.Envelope.OccurredAt != "" {
		return candidate.Envelope.OccurredAt
	}
	return candidate.PersistedAt
}

func deliveryIngressValue(delivery *contracts.DeliverySpec, key string) string {
	if delivery == nil || delivery.IngressMeta == nil {
		return ""
	}
	value, _ := delivery.IngressMeta[key].(string)
	return value
}

func commandTokenScopeForEnvelope(envelope *contracts.Envelope) string {
	if envelope == nil || envelope.Target.Kind != "node" {
		return ""
	}
	return envelope.Target.Value
}

func commandTokenEpochForEnvelope(envelope *contracts.Envelope) string {
	if envelope == nil {
		return ""
	}
	if epoch := payloadString(envelope.Payload, "command_window_id"); epoch != "" {
		return epoch
	}
	if epoch := payloadString(envelope.Payload, "lease_epoch"); epoch != "" {
		return epoch
	}
	if envelope.Delivery != nil && envelope.Delivery.ExpiresAt != "" {
		return "expires:" + envelope.Delivery.ExpiresAt
	}
	return ""
}

func commandTokenValidityForEnvelope(envelope *contracts.Envelope) (string, string) {
	if envelope == nil || envelope.Delivery == nil {
		return "", ""
	}
	return envelope.OccurredAt, envelope.Delivery.ExpiresAt
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
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

func manifestBearerName(bearer string) string {
	switch bearer {
	case "lora_direct", "lora_relay", "lora_mesh":
		return "lora"
	case "wifi_ip", "wifi_mesh", "wifi_lr":
		return "wifi"
	default:
		return bearer
	}
}

func validateOptionalFabricShortID(value *int, field string) error {
	if value == nil {
		return nil
	}
	if *value < 1 || *value > 0xFFFF {
		return contracts.NewValidationError("%s must be between 1 and 65535", field)
	}
	return nil
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
	if policy, ok := contractpolicy.MustRole(role); ok {
		return policy.RequiresAlwaysOn
	}
	switch role {
	case "lora_relay", "mesh_router", "mesh_root", "ack_owner", "powered_leaf", "dual_bearer_bridge", "gateway_head":
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

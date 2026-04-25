#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

#define EF_USB_MAGIC_0 'E'
#define EF_USB_MAGIC_1 'F'
#define EF_USB_VERSION 1

typedef enum {
    EF_USB_FRAME_ENVELOPE_JSON = 1,
    EF_USB_FRAME_HEARTBEAT_JSON = 2,
    EF_USB_FRAME_COMPACT_BINARY = 3,
    EF_USB_FRAME_SUMMARY_BINARY = 4,
} ef_usb_frame_type_t;

typedef struct {
    uint8_t version;
    uint8_t frame_type;
    uint16_t payload_length;
} ef_usb_frame_header_t;

typedef struct {
    bool synced;
    uint16_t expected_length;
    size_t buffered;
    uint8_t buffer[512];
} ef_usb_parser_t;

#define EF_ONAIR_VERSION 1u
#define EF_ONAIR_HEADER_SIZE 8u
#define EF_ONAIR_RELAY_EXTENSION_SIZE 7u
#define EF_ONAIR_FLAG_SUMMARY (1u << 0)
#define EF_ONAIR_FLAG_RELAY_EXTENSION (1u << 1)

typedef enum {
    EF_ONAIR_TYPE_STATE = 1,
    EF_ONAIR_TYPE_EVENT = 2,
    EF_ONAIR_TYPE_COMMAND_RESULT = 3,
    EF_ONAIR_TYPE_PENDING_DIGEST = 4,
    EF_ONAIR_TYPE_TINY_POLL = 5,
    EF_ONAIR_TYPE_COMPACT_COMMAND = 6,
    EF_ONAIR_TYPE_HEARTBEAT = 7,
} ef_onair_type_t;

typedef enum {
    EF_ONAIR_STATE_KEY_NODE_POWER = 1,
} ef_onair_state_key_t;

typedef enum {
    EF_ONAIR_STATE_VALUE_UNKNOWN = 0,
    EF_ONAIR_STATE_VALUE_AWAKE = 1,
    EF_ONAIR_STATE_VALUE_SLEEP = 2,
} ef_onair_state_value_t;

typedef enum {
    EF_ONAIR_EVENT_CODE_BATTERY_LOW = 1,
    EF_ONAIR_EVENT_CODE_MOTION_DETECTED = 2,
    EF_ONAIR_EVENT_CODE_LEAK_DETECTED = 3,
    EF_ONAIR_EVENT_CODE_TAMPER = 4,
    EF_ONAIR_EVENT_CODE_THRESHOLD_CROSSED = 5,
} ef_onair_event_code_t;

typedef enum {
    EF_ONAIR_EVENT_SEVERITY_INFO = 1,
    EF_ONAIR_EVENT_SEVERITY_WARNING = 2,
    EF_ONAIR_EVENT_SEVERITY_CRITICAL = 3,
} ef_onair_event_severity_t;

typedef enum {
    EF_ONAIR_EVENT_FLAG_EVENT_WAKE = 1u << 0,
    EF_ONAIR_EVENT_FLAG_LATCHED = 1u << 1,
} ef_onair_event_flag_t;

typedef enum {
    EF_ONAIR_COMMAND_KIND_MAINTENANCE_ON = 1,
    EF_ONAIR_COMMAND_KIND_MAINTENANCE_OFF = 2,
    EF_ONAIR_COMMAND_KIND_THRESHOLD_SET = 3,
    EF_ONAIR_COMMAND_KIND_QUIET_SET = 4,
    EF_ONAIR_COMMAND_KIND_ALARM_CLEAR = 5,
    EF_ONAIR_COMMAND_KIND_SAMPLING_SET = 6,
} ef_onair_command_kind_t;

typedef enum {
    EF_ONAIR_PHASE_ACCEPTED = 1,
    EF_ONAIR_PHASE_EXECUTING = 2,
    EF_ONAIR_PHASE_SUCCEEDED = 3,
    EF_ONAIR_PHASE_FAILED = 4,
    EF_ONAIR_PHASE_REJECTED = 5,
    EF_ONAIR_PHASE_EXPIRED = 6,
} ef_onair_phase_t;

typedef enum {
    EF_ONAIR_REASON_OK = 1,
    EF_ONAIR_REASON_SERVICE = 2,
    EF_ONAIR_REASON_MAINTENANCE = 3,
    EF_ONAIR_REASON_STALE = 4,
    EF_ONAIR_REASON_BAD_COMMAND = 5,
    EF_ONAIR_REASON_UNSUPPORTED = 6,
} ef_onair_reason_t;

typedef enum {
    EF_ONAIR_SERVICE_LEVEL_EVENTUAL_NEXT_POLL = 1,
} ef_onair_service_level_t;

typedef enum {
    EF_ONAIR_PENDING_FLAG_URGENT = 1u << 0,
    EF_ONAIR_PENDING_FLAG_EXPIRES_SOON = 1u << 1,
} ef_onair_pending_flag_t;

typedef enum {
    EF_ONAIR_HEARTBEAT_HEALTH_OK = 1,
    EF_ONAIR_HEARTBEAT_HEALTH_DEGRADED = 2,
    EF_ONAIR_HEARTBEAT_HEALTH_CRITICAL = 3,
} ef_onair_heartbeat_health_t;

typedef enum {
    EF_ONAIR_HEARTBEAT_FLAG_EVENT_WAKE = 1u << 0,
    EF_ONAIR_HEARTBEAT_FLAG_MAINTENANCE_AWAKE = 1u << 1,
    EF_ONAIR_HEARTBEAT_FLAG_LOW_POWER = 1u << 2,
} ef_onair_heartbeat_flag_t;

typedef struct {
    uint16_t origin_short_id;
    uint16_t previous_hop_short_id;
    uint8_t ttl;
    uint8_t hop_count;
    uint8_t route_hint;
} ef_onair_relay_extension_t;

typedef struct {
    uint8_t version;
    uint8_t logical_type;
    uint8_t flags;
    uint8_t sequence;
    uint16_t source_short_id;
    uint16_t target_short_id;
    bool has_relay;
    ef_onair_relay_extension_t relay;
    const uint8_t *body;
    size_t body_len;
} ef_onair_packet_t;

typedef struct {
    uint8_t key_token;
    uint8_t value_token;
    bool event_wake;
} ef_onair_state_body_t;

typedef struct {
    uint8_t event_code;
    uint8_t severity;
    uint8_t value_bucket;
    uint8_t flags;
} ef_onair_event_body_t;

typedef struct {
    uint16_t command_token;
    uint8_t phase_token;
    uint8_t reason_token;
} ef_onair_command_result_body_t;

typedef struct {
    uint8_t pending_count;
    uint8_t flags;
} ef_onair_pending_digest_body_t;

typedef struct {
    uint8_t service_level;
} ef_onair_tiny_poll_body_t;

typedef struct {
    uint16_t command_token;
    uint8_t command_kind;
    uint8_t argument;
    uint8_t expires_in_sec;
} ef_onair_compact_command_body_t;

typedef struct {
    uint8_t health;
    uint8_t battery_bucket;
    uint8_t link_quality;
    uint8_t uptime_bucket;
    uint8_t flags;
} ef_onair_heartbeat_body_t;

esp_err_t ef_usb_frame_validate(const uint8_t *frame, size_t frame_len);
esp_err_t ef_usb_frame_encode(
    uint8_t frame_type,
    const uint8_t *payload,
    size_t payload_len,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);

esp_err_t ef_usb_parser_reset(ef_usb_parser_t *parser);
esp_err_t ef_usb_parser_push(
    ef_usb_parser_t *parser,
    const uint8_t *data,
    size_t data_len,
    uint8_t *frame_buf,
    size_t frame_buf_cap,
    size_t *frame_len,
    bool *frame_ready);

esp_err_t ef_onair_decode_packet(const uint8_t *frame, size_t frame_len, ef_onair_packet_t *out_packet);
esp_err_t ef_onair_encode_packet(
    const ef_onair_packet_t *packet,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_build_relay_forward(
    const ef_onair_packet_t *packet,
    uint16_t relay_short_id,
    ef_onair_packet_t *out_packet);

esp_err_t ef_onair_encode_state(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_state_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_decode_state(const ef_onair_packet_t *packet, ef_onair_state_body_t *out_body);

esp_err_t ef_onair_encode_event(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_event_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_decode_event(const ef_onair_packet_t *packet, ef_onair_event_body_t *out_body);

esp_err_t ef_onair_encode_command_result(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_command_result_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_decode_command_result(
    const ef_onair_packet_t *packet,
    ef_onair_command_result_body_t *out_body);

esp_err_t ef_onair_encode_pending_digest(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_pending_digest_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_decode_pending_digest(
    const ef_onair_packet_t *packet,
    ef_onair_pending_digest_body_t *out_body);

esp_err_t ef_onair_encode_tiny_poll(
    uint16_t source_short_id,
    uint8_t sequence,
    const ef_onair_tiny_poll_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_decode_tiny_poll(const ef_onair_packet_t *packet, ef_onair_tiny_poll_body_t *out_body);

esp_err_t ef_onair_encode_compact_command(
    uint16_t target_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_compact_command_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_decode_compact_command(
    const ef_onair_packet_t *packet,
    ef_onair_compact_command_body_t *out_body);

esp_err_t ef_onair_encode_heartbeat(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_heartbeat_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);
esp_err_t ef_onair_decode_heartbeat(const ef_onair_packet_t *packet, ef_onair_heartbeat_body_t *out_body);

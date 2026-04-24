#include "sleepy_policy.h"

#include <stdio.h>
#include <string.h>

#include "board_xiao_sx1262.h"
#include "esp_check.h"
#include "esp_log.h"
#include "fabric_proto/fabric_proto.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "radio_hal_sx1262.h"

static const char *TAG = "sleepy_policy";
static const uint32_t POLL_RESPONSE_WINDOW_MS = 350u;
static const uint8_t DEFAULT_MAINTENANCE_MAX_CYCLES = 3u;
static SemaphoreHandle_t s_state_lock;

static sleepy_policy_state_t s_state = {
    .rx_window_ms = 1500u,
    .maintenance_window_ms = 8000u,
    .maintenance_awake = false,
    .saw_pending_digest = false,
    .applied_command = false,
    .maintenance_cycles_remaining = 0u,
    .maintenance_max_cycles = DEFAULT_MAINTENANCE_MAX_CYCLES,
    .threshold_value = 0u,
    .quiet_value = 0u,
    .sampling_value = 0u,
    .alarm_clear_seen = false,
    .short_id = 0u,
    .next_sequence = 0u,
    .last_command_token = 0u,
    .last_command_id = "",
    .node_id = "",
    .recent_command_tokens = {0u, 0u, 0u, 0u},
    .recent_command_cursor = 0u,
};

static esp_err_t sleepy_policy_ensure_lock(void);
static esp_err_t sleepy_policy_configure_default_identity(void);
static uint8_t sleepy_take_next_sequence_locked(void);
static esp_err_t sleepy_send_tiny_poll(void);
static esp_err_t sleepy_send_command_result(uint16_t command_token, uint8_t phase_token, uint8_t reason_token);
static esp_err_t sleepy_send_terminal_command_result_once(
    uint16_t command_token,
    uint8_t phase_token,
    uint8_t reason_token);
static esp_err_t sleepy_open_downlink_window(uint32_t window_ms);
static esp_err_t sleepy_handle_downlink_frame(const radio_hal_frame_t *frame, bool *extend_window);
static bool sleepy_recent_command_seen_locked(uint16_t command_token);
static void sleepy_record_recent_command_locked(uint16_t command_token);
static bool sleepy_is_duplicate_command(uint16_t command_token);
static void sleepy_enable_maintenance_locked(void);
static void sleepy_disable_maintenance_locked(void);
static void sleepy_finish_cycle_locked(void);
static void sleepy_store_last_command_locked(uint16_t command_token);
static const char *sleepy_phase_label(uint8_t phase_token);
static const char *sleepy_reason_label(uint8_t reason_token);
static esp_err_t sleepy_apply_compact_command(const ef_onair_compact_command_body_t *command);

esp_err_t sleepy_policy_apply_defaults(void) {
    static const radio_hal_lora_profile_t profile = {
        .frequency_hz = 922400000u,
        .spreading_factor = 10u,
        .bandwidth_khz = 125u,
        .tx_power_dbm = 10u,
    };
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    ESP_RETURN_ON_ERROR(sleepy_policy_configure_default_identity(), TAG, "identity init failed");
    return radio_hal_apply_jp_safe_profile(&profile);
}

esp_err_t sleepy_policy_run_cycle(void) {
    sleepy_policy_state_t snapshot;
    ESP_LOGI(TAG, "sleepy cycle: uplink -> rx window -> sleep");
    ESP_RETURN_ON_ERROR(sleepy_policy_publish_state("node.power", "awake", false), TAG, "uplink failed");
    ESP_RETURN_ON_ERROR(sleepy_policy_get_state(&snapshot), TAG, "state snapshot failed");
    ESP_RETURN_ON_ERROR(
        sleepy_open_downlink_window(snapshot.maintenance_awake ? snapshot.maintenance_window_ms : snapshot.rx_window_ms),
        TAG,
        "downlink window failed");
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    sleepy_finish_cycle_locked();
    xSemaphoreGive(s_state_lock);
    ESP_RETURN_ON_ERROR(sleepy_policy_get_state(&snapshot), TAG, "state snapshot failed");
    ESP_LOGI(
        TAG,
        "cycle complete: short_id=0x%04X digest=%s applied=%s last_command=%s",
        snapshot.short_id,
        snapshot.saw_pending_digest ? "yes" : "no",
        snapshot.applied_command ? "yes" : "no",
        snapshot.last_command_id);
    return ESP_OK;
}

esp_err_t sleepy_policy_get_state(sleepy_policy_state_t *state) {
    if (state == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    *state = s_state;
    xSemaphoreGive(s_state_lock);
    return ESP_OK;
}

esp_err_t sleepy_policy_publish_state(const char *state_key, const char *value, bool event_wake) {
    ef_onair_state_body_t body;
    uint8_t frame[32];
    size_t frame_len = 0u;
    uint8_t sequence;
    uint16_t short_id;
    if ((state_key == NULL || strcmp(state_key, "node.power") == 0) && value != NULL && strcmp(value, "awake") == 0) {
        body.key_token = EF_ONAIR_STATE_KEY_NODE_POWER;
        body.value_token = EF_ONAIR_STATE_VALUE_AWAKE;
    } else if ((state_key == NULL || strcmp(state_key, "node.power") == 0) &&
               value != NULL && strcmp(value, "sleep") == 0) {
        body.key_token = EF_ONAIR_STATE_KEY_NODE_POWER;
        body.value_token = EF_ONAIR_STATE_VALUE_SLEEP;
    } else {
        ESP_LOGW(TAG, "unsupported compact state key=%s value=%s", state_key, value);
        return ESP_ERR_NOT_SUPPORTED;
    }
    body.event_wake = event_wake;
    ESP_RETURN_ON_ERROR(sleepy_policy_configure_default_identity(), TAG, "identity init failed");
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    short_id = s_state.short_id;
    sequence = sleepy_take_next_sequence_locked();
    xSemaphoreGive(s_state_lock);
    ESP_RETURN_ON_ERROR(
        ef_onair_encode_state(short_id, false, sequence, &body, frame, sizeof(frame), &frame_len),
        TAG,
        "state encode failed");
    return radio_hal_send_frame(frame, frame_len);
}

esp_err_t sleepy_policy_emit_event(const char *event_name, const char *value) {
    (void)event_name;
    (void)value;
    ESP_LOGW(TAG, "binary compact event path is not implemented yet");
    return ESP_ERR_NOT_SUPPORTED;
}

esp_err_t sleepy_policy_set_maintenance_awake(bool enabled) {
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    if (enabled) {
        sleepy_enable_maintenance_locked();
    } else {
        sleepy_disable_maintenance_locked();
    }
    xSemaphoreGive(s_state_lock);
    return ESP_OK;
}

esp_err_t sleepy_policy_set_node_id(const char *node_id) {
    size_t length;
    if (node_id == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    length = strlen(node_id);
    if (length == 0u || length >= sizeof(s_state.node_id)) {
        return ESP_ERR_INVALID_SIZE;
    }
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    memcpy(s_state.node_id, node_id, length + 1u);
    xSemaphoreGive(s_state_lock);
    return ESP_OK;
}

const char *sleepy_policy_get_node_id(void) {
    return s_state.node_id;
}

esp_err_t sleepy_policy_set_short_id(uint16_t short_id) {
    if (short_id == 0u) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    s_state.short_id = short_id;
    xSemaphoreGive(s_state_lock);
    return ESP_OK;
}

uint16_t sleepy_policy_get_short_id(void) {
    return s_state.short_id;
}

static esp_err_t sleepy_policy_ensure_lock(void) {
    if (s_state_lock != NULL) {
        return ESP_OK;
    }
    s_state_lock = xSemaphoreCreateMutex();
    if (s_state_lock == NULL) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

static esp_err_t sleepy_policy_configure_default_identity(void) {
    char derived_id[sizeof(s_state.node_id)];
    uint16_t short_id;
    esp_err_t err;
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    if (s_state.node_id[0] != '\0' && s_state.short_id != 0u) {
        xSemaphoreGive(s_state_lock);
        return ESP_OK;
    }
    xSemaphoreGive(s_state_lock);
    err = board_xiao_sx1262_format_identity("leaf", derived_id, sizeof(derived_id));
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "falling back to local node identity: %s", esp_err_to_name(err));
        snprintf(derived_id, sizeof(derived_id), "leaf-local");
    }
    err = board_xiao_sx1262_get_default_short_id(&short_id);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "falling back to local short id: %s", esp_err_to_name(err));
        short_id = 1u;
    }
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    if (s_state.node_id[0] == '\0') {
        size_t length = strlen(derived_id);
        memcpy(s_state.node_id, derived_id, length + 1u);
    }
    if (s_state.short_id == 0u) {
        s_state.short_id = short_id;
    }
    xSemaphoreGive(s_state_lock);
    return ESP_OK;
}

static uint8_t sleepy_take_next_sequence_locked(void) {
    const uint8_t next = s_state.next_sequence;
    s_state.next_sequence = (uint8_t)(s_state.next_sequence + 1u);
    return next;
}

static esp_err_t sleepy_open_downlink_window(uint32_t window_ms) {
    radio_hal_frame_t frame;
    TickType_t deadline = xTaskGetTickCount() + pdMS_TO_TICKS(window_ms);
    while (xTaskGetTickCount() < deadline) {
        bool extend_window = false;
        {
            const esp_err_t service_err = radio_hal_service();
            if (service_err != ESP_OK && service_err != ESP_ERR_NOT_SUPPORTED) {
                ESP_LOGW(TAG, "radio service failed: %s", esp_err_to_name(service_err));
            }
        }
        if (radio_hal_receive_frame(&frame, 50u) == ESP_OK) {
            if (sleepy_handle_downlink_frame(&frame, &extend_window) != ESP_OK) {
                ESP_LOGW(TAG, "downlink handle failed, continuing window");
            }
            if (extend_window) {
                deadline = xTaskGetTickCount() + pdMS_TO_TICKS(POLL_RESPONSE_WINDOW_MS);
            }
        }
        vTaskDelay(pdMS_TO_TICKS(10u));
    }
    return ESP_OK;
}

static esp_err_t sleepy_handle_downlink_frame(const radio_hal_frame_t *frame, bool *extend_window) {
    ef_onair_packet_t packet;
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (extend_window != NULL) {
        *extend_window = false;
    }
    ESP_RETURN_ON_ERROR(ef_onair_decode_packet(frame->payload, frame->payload_len, &packet), TAG, "invalid on-air frame");
    switch (packet.logical_type) {
        case EF_ONAIR_TYPE_PENDING_DIGEST: {
            ef_onair_pending_digest_body_t digest;
            ESP_RETURN_ON_ERROR(ef_onair_decode_pending_digest(&packet, &digest), TAG, "pending digest decode failed");
            ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
            xSemaphoreTake(s_state_lock, portMAX_DELAY);
            s_state.saw_pending_digest = digest.pending_count > 0u;
            xSemaphoreGive(s_state_lock);
            ESP_LOGI(TAG, "received pending digest: count=%u flags=0x%02X", digest.pending_count, digest.flags);
            if (digest.pending_count > 0u) {
                ESP_RETURN_ON_ERROR(sleepy_send_tiny_poll(), TAG, "tiny poll failed");
                if (extend_window != NULL) {
                    *extend_window = true;
                }
            }
            return ESP_OK;
        }
        case EF_ONAIR_TYPE_COMPACT_COMMAND: {
            ef_onair_compact_command_body_t command;
            ESP_RETURN_ON_ERROR(ef_onair_decode_compact_command(&packet, &command), TAG, "compact command decode failed");
            if (packet.target_short_id != 0u && packet.target_short_id != sleepy_policy_get_short_id()) {
                ESP_LOGI(TAG, "ignoring compact command for another short_id=0x%04X", packet.target_short_id);
                return ESP_OK;
            }
            return sleepy_apply_compact_command(&command);
        }
        default:
            ESP_LOGW(TAG, "ignoring unsupported on-air logical type=%u", packet.logical_type);
            return ESP_OK;
    }
}

static esp_err_t sleepy_send_tiny_poll(void) {
    ef_onair_tiny_poll_body_t body = {
        .service_level = EF_ONAIR_SERVICE_LEVEL_EVENTUAL_NEXT_POLL,
    };
    uint8_t frame[24];
    size_t frame_len = 0u;
    uint8_t sequence;
    uint16_t short_id;
    ESP_LOGI(TAG, "sending explicit tiny poll");
    ESP_RETURN_ON_ERROR(sleepy_policy_configure_default_identity(), TAG, "identity init failed");
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    short_id = s_state.short_id;
    sequence = sleepy_take_next_sequence_locked();
    xSemaphoreGive(s_state_lock);
    ESP_RETURN_ON_ERROR(
        ef_onair_encode_tiny_poll(short_id, sequence, &body, frame, sizeof(frame), &frame_len),
        TAG,
        "tiny poll encode failed");
    return radio_hal_send_frame(frame, frame_len);
}

static esp_err_t sleepy_send_command_result(uint16_t command_token, uint8_t phase_token, uint8_t reason_token) {
    ef_onair_command_result_body_t body = {
        .command_token = command_token,
        .phase_token = phase_token,
        .reason_token = reason_token,
    };
    uint8_t frame[24];
    size_t frame_len = 0u;
    uint8_t sequence;
    uint16_t short_id;
    ESP_RETURN_ON_ERROR(sleepy_policy_configure_default_identity(), TAG, "identity init failed");
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    short_id = s_state.short_id;
    sequence = sleepy_take_next_sequence_locked();
    xSemaphoreGive(s_state_lock);
    ESP_LOGI(
        TAG,
        "sending command result token=0x%04X phase=%s reason=%s",
        command_token,
        sleepy_phase_label(phase_token),
        sleepy_reason_label(reason_token));
    ESP_RETURN_ON_ERROR(
        ef_onair_encode_command_result(short_id, false, sequence, &body, frame, sizeof(frame), &frame_len),
        TAG,
        "command result encode failed");
    return radio_hal_send_frame(frame, frame_len);
}

static esp_err_t sleepy_send_terminal_command_result_once(
    uint16_t command_token,
    uint8_t phase_token,
    uint8_t reason_token) {
    if (command_token == 0u) {
        return sleepy_send_command_result(command_token, phase_token, reason_token);
    }
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    if (sleepy_recent_command_seen_locked(command_token)) {
        xSemaphoreGive(s_state_lock);
        ESP_LOGI(TAG, "duplicate terminal result suppressed: token=0x%04X", command_token);
        return ESP_OK;
    }
    sleepy_record_recent_command_locked(command_token);
    xSemaphoreGive(s_state_lock);
    return sleepy_send_command_result(command_token, phase_token, reason_token);
}

static bool sleepy_recent_command_seen_locked(uint16_t command_token) {
    size_t index;
    if (command_token == 0u) {
        return false;
    }
    for (index = 0; index < sizeof(s_state.recent_command_tokens) / sizeof(s_state.recent_command_tokens[0]); ++index) {
        if (command_token == s_state.recent_command_tokens[index]) {
            return true;
        }
    }
    return false;
}

static void sleepy_record_recent_command_locked(uint16_t command_token) {
    if (command_token == 0u) {
        return;
    }
    s_state.recent_command_tokens[s_state.recent_command_cursor] = command_token;
    s_state.recent_command_cursor =
        (uint8_t)((s_state.recent_command_cursor + 1u) %
                  (sizeof(s_state.recent_command_tokens) / sizeof(s_state.recent_command_tokens[0])));
}

static bool sleepy_is_duplicate_command(uint16_t command_token) {
    bool seen;
    if (command_token == 0u) {
        return false;
    }
    ESP_ERROR_CHECK(sleepy_policy_ensure_lock());
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    seen = sleepy_recent_command_seen_locked(command_token);
    xSemaphoreGive(s_state_lock);
    return seen;
}

static void sleepy_enable_maintenance_locked(void) {
    s_state.maintenance_awake = true;
    s_state.maintenance_cycles_remaining = s_state.maintenance_max_cycles;
}

static void sleepy_disable_maintenance_locked(void) {
    s_state.maintenance_awake = false;
    s_state.maintenance_cycles_remaining = 0u;
}

static void sleepy_finish_cycle_locked(void) {
    if (!s_state.maintenance_awake) {
        return;
    }
    if (s_state.maintenance_cycles_remaining > 0u) {
        s_state.maintenance_cycles_remaining--;
    }
    if (s_state.maintenance_cycles_remaining == 0u) {
        s_state.maintenance_awake = false;
    }
}

static void sleepy_store_last_command_locked(uint16_t command_token) {
    int written;
    s_state.last_command_token = command_token;
    written = snprintf(s_state.last_command_id, sizeof(s_state.last_command_id), "tok-%04X", command_token);
    if (written <= 0 || (size_t)written >= sizeof(s_state.last_command_id)) {
        s_state.last_command_id[0] = '\0';
    }
}

static const char *sleepy_phase_label(uint8_t phase_token) {
    switch (phase_token) {
        case EF_ONAIR_PHASE_ACCEPTED:
            return "accepted";
        case EF_ONAIR_PHASE_EXECUTING:
            return "executing";
        case EF_ONAIR_PHASE_SUCCEEDED:
            return "succeeded";
        case EF_ONAIR_PHASE_FAILED:
            return "failed";
        case EF_ONAIR_PHASE_REJECTED:
            return "rejected";
        case EF_ONAIR_PHASE_EXPIRED:
            return "expired";
        default:
            return "unknown";
    }
}

static const char *sleepy_reason_label(uint8_t reason_token) {
    switch (reason_token) {
        case EF_ONAIR_REASON_OK:
            return "ok";
        case EF_ONAIR_REASON_SERVICE:
            return "service";
        case EF_ONAIR_REASON_MAINTENANCE:
            return "maintenance";
        case EF_ONAIR_REASON_STALE:
            return "stale";
        case EF_ONAIR_REASON_BAD_COMMAND:
            return "badcmd";
        case EF_ONAIR_REASON_UNSUPPORTED:
            return "unsupported";
        default:
            return "unknown";
    }
}

static esp_err_t sleepy_apply_compact_command(const ef_onair_compact_command_body_t *command) {
    if (command == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (command->command_token == 0u) {
        return sleepy_send_command_result(0u, EF_ONAIR_PHASE_REJECTED, EF_ONAIR_REASON_BAD_COMMAND);
    }
    if (sleepy_is_duplicate_command(command->command_token)) {
        ESP_LOGI(TAG, "duplicate compact command ignored before terminal result: token=0x%04X", command->command_token);
        return ESP_OK;
    }
    if (command->expires_in_sec == 0u) {
        return sleepy_send_terminal_command_result_once(
            command->command_token,
            EF_ONAIR_PHASE_EXPIRED,
            EF_ONAIR_REASON_STALE);
    }
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    switch (command->command_kind) {
        case EF_ONAIR_COMMAND_KIND_MAINTENANCE_ON:
            sleepy_enable_maintenance_locked();
            break;
        case EF_ONAIR_COMMAND_KIND_MAINTENANCE_OFF:
            sleepy_disable_maintenance_locked();
            break;
        case EF_ONAIR_COMMAND_KIND_THRESHOLD_SET:
            s_state.threshold_value = command->argument;
            break;
        case EF_ONAIR_COMMAND_KIND_QUIET_SET:
            s_state.quiet_value = command->argument;
            break;
        case EF_ONAIR_COMMAND_KIND_ALARM_CLEAR:
            s_state.alarm_clear_seen = true;
            break;
        case EF_ONAIR_COMMAND_KIND_SAMPLING_SET:
            s_state.sampling_value = command->argument;
            break;
        default:
            xSemaphoreGive(s_state_lock);
            return sleepy_send_terminal_command_result_once(
                command->command_token,
                EF_ONAIR_PHASE_REJECTED,
                EF_ONAIR_REASON_UNSUPPORTED);
    }
    s_state.applied_command = true;
    sleepy_store_last_command_locked(command->command_token);
    sleepy_record_recent_command_locked(command->command_token);
    xSemaphoreGive(s_state_lock);
    return sleepy_send_command_result(command->command_token, EF_ONAIR_PHASE_SUCCEEDED, EF_ONAIR_REASON_OK);
}

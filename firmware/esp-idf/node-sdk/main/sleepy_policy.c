#include "sleepy_policy.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "esp_check.h"
#include "esp_log.h"
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
    .last_command_id = "",
    .node_id = "sleepy-leaf-01",
};

static esp_err_t sleepy_open_downlink_window(uint32_t window_ms);
static esp_err_t sleepy_handle_downlink_frame(const radio_hal_frame_t *frame, bool *extend_window);
static int sleepy_extract_pending_count(const char *json);
static bool sleepy_extract_string(const char *json, const char *key, char *out, size_t out_cap);
static bool sleepy_contains(const char *json, const char *token);
static esp_err_t sleepy_send_tiny_poll(void);
static esp_err_t sleepy_send_command_result(const char *command_id, const char *phase, const char *reason);
static esp_err_t sleepy_send_compact_payload(const char *payload);
static int sleepy_extract_pending_digest_count(const char *payload);
static bool sleepy_extract_token(const char *payload, int index, char *out, size_t out_cap);
static bool sleepy_extract_int_field(const char *json, const char *key, int *out_value);
static esp_err_t sleepy_policy_ensure_lock(void);
static bool sleepy_recent_command_seen_locked(const char *command_id);
static void sleepy_record_recent_command_locked(const char *command_id);
static bool sleepy_json_command_allowed_while_sleepy(const char *command_name);
static bool sleepy_json_command_requires_maintenance(const char *command_name, const char *route_class);
static bool sleepy_is_duplicate_command(const char *command_id);
static esp_err_t sleepy_send_terminal_command_result_once(const char *command_id, const char *phase, const char *reason);
static void sleepy_enable_maintenance_locked(void);
static void sleepy_disable_maintenance_locked(void);
static void sleepy_finish_cycle_locked(void);

esp_err_t sleepy_policy_apply_defaults(void) {
    static const radio_hal_lora_profile_t profile = {
        .frequency_hz = 922400000u,
        .spreading_factor = 10u,
        .bandwidth_khz = 125u,
        .tx_power_dbm = 10u,
    };
    ESP_RETURN_ON_ERROR(radio_hal_init(), TAG, "radio init failed");
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
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
        "cycle complete: digest=%s applied=%s last_command_id=%s",
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
    char compact[64];
    snprintf(
        compact,
        sizeof(compact),
        "S|%s|%s|%s|%c",
        sleepy_policy_get_node_id(),
        state_key != NULL ? state_key : "state",
        value != NULL ? value : "",
        event_wake ? '1' : '0');
    return sleepy_send_compact_payload(compact);
}

esp_err_t sleepy_policy_emit_event(const char *event_name, const char *value) {
    char compact[64];
    snprintf(
        compact,
        sizeof(compact),
        "E|%s|%s|%s",
        sleepy_policy_get_node_id(),
        event_name != NULL ? event_name : "event",
        value != NULL ? value : "");
    return sleepy_send_compact_payload(compact);
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
    char payload[256];
    int pending_count;
    if (frame == NULL || frame->payload_len >= sizeof(payload)) {
        return ESP_ERR_INVALID_ARG;
    }
    if (extend_window != NULL) {
        *extend_window = false;
    }
    memcpy(payload, frame->payload, frame->payload_len);
    payload[frame->payload_len] = '\0';

    pending_count = sleepy_extract_pending_digest_count(payload);
    if (pending_count >= 0 && payload[0] == 'D') {
        ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
        xSemaphoreTake(s_state_lock, portMAX_DELAY);
        s_state.saw_pending_digest = pending_count > 0;
        xSemaphoreGive(s_state_lock);
        ESP_LOGI(TAG, "received pending digest token: count=%d", pending_count);
        if (pending_count > 0) {
            ESP_RETURN_ON_ERROR(sleepy_send_tiny_poll(), TAG, "tiny poll failed");
            if (extend_window != NULL) {
                *extend_window = true;
            }
        }
        return ESP_OK;
    }

    pending_count = sleepy_extract_pending_count(payload);
    if (pending_count >= 0) {
        ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
        xSemaphoreTake(s_state_lock, portMAX_DELAY);
        s_state.saw_pending_digest = pending_count > 0;
        xSemaphoreGive(s_state_lock);
        ESP_LOGI(TAG, "received pending digest: count=%d", pending_count);
        if (pending_count > 0) {
            ESP_RETURN_ON_ERROR(sleepy_send_tiny_poll(), TAG, "tiny poll failed");
            if (extend_window != NULL) {
                *extend_window = true;
            }
        }
        return ESP_OK;
    }

    if (payload[0] == 'C' && sleepy_contains(payload, "|")) {
        char token1[48] = "";
        char token2[48] = "";
        char command_id[48] = "";
        char service_level[32] = "";
        char mode[24] = "";
        char expires_in_buf[16] = "";
        const char *node_id = sleepy_policy_get_node_id();
        bool stale = false;
        sleepy_extract_token(payload, 1, token1, sizeof(token1));
        sleepy_extract_token(payload, 2, token2, sizeof(token2));
        if (strcmp(token1, node_id) == 0 || strcmp(token1, "all") == 0) {
            strncpy(command_id, token2, sizeof(command_id) - 1u);
            command_id[sizeof(command_id) - 1u] = '\0';
            sleepy_extract_token(payload, 3, service_level, sizeof(service_level));
            sleepy_extract_token(payload, 4, mode, sizeof(mode));
            sleepy_extract_token(payload, 5, expires_in_buf, sizeof(expires_in_buf));
        } else if (sleepy_extract_token(payload, 5, expires_in_buf, sizeof(expires_in_buf))) {
            ESP_LOGI(TAG, "ignoring compact command for another node: %s", token1);
            return ESP_OK;
        } else {
            strncpy(command_id, token1, sizeof(command_id) - 1u);
            command_id[sizeof(command_id) - 1u] = '\0';
            sleepy_extract_token(payload, 2, service_level, sizeof(service_level));
            sleepy_extract_token(payload, 3, mode, sizeof(mode));
            sleepy_extract_token(payload, 4, expires_in_buf, sizeof(expires_in_buf));
        }
        stale = strcmp(mode, "STALE") == 0 || (expires_in_buf[0] != '\0' && strtol(expires_in_buf, NULL, 10) <= 0);
        if (command_id[0] == '\0') {
            return sleepy_send_command_result("missing", "rejected", "badcmd");
        }
        if (sleepy_is_duplicate_command(command_id)) {
            ESP_LOGI(TAG, "duplicate compact command ignored before terminal result: %s", command_id);
            return ESP_OK;
        }
        if (stale) {
            return sleepy_send_terminal_command_result_once(command_id, "expired", "stale");
        }
        if (service_level[0] != '\0' && strcmp(service_level, "ENP") != 0) {
            return sleepy_send_terminal_command_result_once(command_id, "rejected", "svc");
        }
        ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
        xSemaphoreTake(s_state_lock, portMAX_DELAY);
        if (strcmp(mode, "MAINT_ON") == 0) {
            sleepy_enable_maintenance_locked();
        } else if (strcmp(mode, "MAINT_OFF") == 0) {
            sleepy_disable_maintenance_locked();
        }
        if (!s_state.maintenance_awake && strcmp(mode, "MAINT") == 0) {
            xSemaphoreGive(s_state_lock);
            return sleepy_send_terminal_command_result_once(command_id, "rejected", "maintenance");
        }
        s_state.applied_command = true;
        strncpy(s_state.last_command_id, command_id, sizeof(s_state.last_command_id) - 1u);
        s_state.last_command_id[sizeof(s_state.last_command_id) - 1u] = '\0';
        sleepy_record_recent_command_locked(command_id);
        xSemaphoreGive(s_state_lock);
        return sleepy_send_command_result(command_id, "succeeded", "ok");
    }

    if (sleepy_contains(payload, "\"command_name\"")) {
        char command_id[48] = "";
        char service_level[32] = "";
        char command_name[48] = "";
        char route_class[32] = "";
        char mode_value[32] = "";
        int expires_in_s = 1;
        bool stale = sleepy_contains(payload, "\"stale\":true");
        sleepy_extract_string(payload, "command_id", command_id, sizeof(command_id));
        sleepy_extract_string(payload, "service_level", service_level, sizeof(service_level));
        sleepy_extract_string(payload, "command_name", command_name, sizeof(command_name));
        sleepy_extract_string(payload, "route_class", route_class, sizeof(route_class));
        sleepy_extract_string(payload, "mode", mode_value, sizeof(mode_value));
        if (sleepy_extract_int_field(payload, "expires_in_s", &expires_in_s) && expires_in_s <= 0) {
            stale = true;
        }
        if (command_id[0] == '\0') {
            return sleepy_send_command_result("missing", "rejected", "badcmd");
        }
        if (sleepy_is_duplicate_command(command_id)) {
            ESP_LOGI(TAG, "duplicate json command ignored before terminal result: %s", command_id);
            return ESP_OK;
        }
        if (stale) {
            return sleepy_send_terminal_command_result_once(command_id, "expired", "stale command");
        }
        if (service_level[0] != '\0' && strcmp(service_level, "eventual_next_poll") != 0) {
            return sleepy_send_terminal_command_result_once(command_id, "rejected", "unsupported service level");
        }
        ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
        xSemaphoreTake(s_state_lock, portMAX_DELAY);
        if (strcmp(command_name, "mode.set") == 0 && strcmp(mode_value, "maintenance_awake") == 0) {
            sleepy_enable_maintenance_locked();
        } else if (strcmp(command_name, "mode.set") == 0 && strcmp(mode_value, "deployed") == 0) {
            sleepy_disable_maintenance_locked();
        }
        if (!s_state.maintenance_awake &&
            sleepy_json_command_requires_maintenance(command_name, route_class)) {
            xSemaphoreGive(s_state_lock);
            return sleepy_send_terminal_command_result_once(command_id, "rejected", "maintenance");
        }
        if (!s_state.maintenance_awake && !sleepy_json_command_allowed_while_sleepy(command_name)) {
            xSemaphoreGive(s_state_lock);
            return sleepy_send_terminal_command_result_once(command_id, "rejected", "unsupported sleepy command");
        }
        s_state.applied_command = true;
        strncpy(s_state.last_command_id, command_id, sizeof(s_state.last_command_id) - 1u);
        s_state.last_command_id[sizeof(s_state.last_command_id) - 1u] = '\0';
        sleepy_record_recent_command_locked(command_id);
        xSemaphoreGive(s_state_lock);
        return sleepy_send_command_result(command_id, "succeeded", "applied tiny command");
    }

    return ESP_OK;
}

static int sleepy_extract_pending_count(const char *json) {
    const char *needle = "\"pending_count\":";
    const char *start = strstr(json, needle);
    if (start == NULL) {
        return -1;
    }
    start += strlen(needle);
    return (int)strtol(start, NULL, 10);
}

static bool sleepy_extract_string(const char *json, const char *key, char *out, size_t out_cap) {
    char needle[48];
    const char *start;
    const char *end;
    if (json == NULL || key == NULL || out == NULL || out_cap == 0u) {
        return false;
    }
    snprintf(needle, sizeof(needle), "\"%s\":\"", key);
    start = strstr(json, needle);
    if (start == NULL) {
        out[0] = '\0';
        return false;
    }
    start += strlen(needle);
    end = strchr(start, '"');
    if (end == NULL) {
        out[0] = '\0';
        return false;
    }
    {
        size_t length = (size_t)(end - start);
        if (length >= out_cap) {
            length = out_cap - 1u;
        }
        memcpy(out, start, length);
        out[length] = '\0';
    }
    return true;
}

static bool sleepy_contains(const char *json, const char *token) {
    return json != NULL && token != NULL && strstr(json, token) != NULL;
}

static esp_err_t sleepy_send_tiny_poll(void) {
    char poll_json[64];
    snprintf(poll_json, sizeof(poll_json), "P|%s|TP|ENP", sleepy_policy_get_node_id());
    ESP_LOGI(TAG, "sending explicit tiny poll");
    return sleepy_send_compact_payload(poll_json);
}

static esp_err_t sleepy_send_command_result(const char *command_id, const char *phase, const char *reason) {
    char json[96];
    snprintf(
        json,
        sizeof(json),
        "R|%s|%s|%s|%s",
        sleepy_policy_get_node_id(),
        command_id != NULL ? command_id : "",
        phase,
        reason);
    return sleepy_send_compact_payload(json);
}

static esp_err_t sleepy_send_compact_payload(const char *payload) {
    if (payload == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    return radio_hal_send_frame((const uint8_t *)payload, strlen(payload));
}

static int sleepy_extract_pending_digest_count(const char *payload) {
    char token1[32];
    char token2[32];
    char *end = NULL;
    long parsed;
    if (payload == NULL || payload[0] != 'D') {
        return -1;
    }
    if (!sleepy_extract_token(payload, 1, token1, sizeof(token1))) {
        return -1;
    }
    parsed = strtol(token1, &end, 10);
    if (end != NULL && *end == '\0') {
        return (int)parsed;
    }
    if (!sleepy_extract_token(payload, 2, token2, sizeof(token2))) {
        return -1;
    }
    parsed = strtol(token2, &end, 10);
    if (end == NULL || *end != '\0') {
        return -1;
    }
    return (int)parsed;
}

static bool sleepy_extract_token(const char *payload, int index, char *out, size_t out_cap) {
    const char *cursor = payload;
    const char *next;
    int current = 0;
    if (payload == NULL || out == NULL || out_cap == 0u || index < 0) {
        return false;
    }
    while (current < index && cursor != NULL) {
        cursor = strchr(cursor, '|');
        if (cursor == NULL) {
            out[0] = '\0';
            return false;
        }
        cursor++;
        current++;
    }
    if (cursor == NULL) {
        out[0] = '\0';
        return false;
    }
    next = strchr(cursor, '|');
    if (next == NULL) {
        next = payload + strlen(payload);
    }
    {
        size_t length = (size_t)(next - cursor);
        if (length >= out_cap) {
            length = out_cap - 1u;
        }
        memcpy(out, cursor, length);
        out[length] = '\0';
    }
    return true;
}

static bool sleepy_extract_int_field(const char *json, const char *key, int *out_value) {
    char needle[48];
    const char *start;
    if (json == NULL || key == NULL || out_value == NULL) {
        return false;
    }
    snprintf(needle, sizeof(needle), "\"%s\":", key);
    start = strstr(json, needle);
    if (start == NULL) {
        return false;
    }
    start += strlen(needle);
    *out_value = (int)strtol(start, NULL, 10);
    return true;
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

static bool sleepy_recent_command_seen_locked(const char *command_id) {
    size_t index;
    if (command_id == NULL || command_id[0] == '\0') {
        return false;
    }
    for (index = 0; index < sizeof(s_state.recent_command_ids) / sizeof(s_state.recent_command_ids[0]); ++index) {
        if (strcmp(command_id, s_state.recent_command_ids[index]) == 0) {
            return true;
        }
    }
    return false;
}

static void sleepy_record_recent_command_locked(const char *command_id) {
    if (command_id == NULL || command_id[0] == '\0') {
        return;
    }
    strncpy(
        s_state.recent_command_ids[s_state.recent_command_cursor],
        command_id,
        sizeof(s_state.recent_command_ids[s_state.recent_command_cursor]) - 1u);
    s_state.recent_command_ids[s_state.recent_command_cursor][sizeof(s_state.recent_command_ids[0]) - 1u] = '\0';
    s_state.recent_command_cursor =
        (uint8_t)((s_state.recent_command_cursor + 1u) %
                  (sizeof(s_state.recent_command_ids) / sizeof(s_state.recent_command_ids[0])));
}

static bool sleepy_json_command_allowed_while_sleepy(const char *command_name) {
    static const char *const allowed[] = {
        "mode.set",
        "threshold.set",
        "quiet.set",
        "alarm.clear",
        "sampling.set",
    };
    size_t index;
    if (command_name == NULL || command_name[0] == '\0') {
        return true;
    }
    for (index = 0; index < sizeof(allowed) / sizeof(allowed[0]); ++index) {
        if (strcmp(command_name, allowed[index]) == 0) {
            return true;
        }
    }
    return false;
}

static bool sleepy_json_command_requires_maintenance(const char *command_name, const char *route_class) {
    if (route_class != NULL && strcmp(route_class, "maintenance_sync") == 0) {
        return true;
    }
    if (command_name == NULL) {
        return false;
    }
    return strcmp(command_name, "firmware.sync") == 0 ||
           strcmp(command_name, "diagnostics.upload") == 0 ||
           strcmp(command_name, "ota.begin") == 0;
}

static bool sleepy_is_duplicate_command(const char *command_id) {
    bool seen;
    if (command_id == NULL || command_id[0] == '\0') {
        return false;
    }
    ESP_ERROR_CHECK(sleepy_policy_ensure_lock());
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    seen = sleepy_recent_command_seen_locked(command_id);
    xSemaphoreGive(s_state_lock);
    return seen;
}

static esp_err_t sleepy_send_terminal_command_result_once(const char *command_id, const char *phase, const char *reason) {
    if (command_id == NULL || command_id[0] == '\0') {
        return sleepy_send_command_result(command_id, phase, reason);
    }
    ESP_RETURN_ON_ERROR(sleepy_policy_ensure_lock(), TAG, "state lock init failed");
    xSemaphoreTake(s_state_lock, portMAX_DELAY);
    if (sleepy_recent_command_seen_locked(command_id)) {
        xSemaphoreGive(s_state_lock);
        ESP_LOGI(TAG, "duplicate terminal result suppressed: %s", command_id);
        return ESP_OK;
    }
    sleepy_record_recent_command_locked(command_id);
    xSemaphoreGive(s_state_lock);
    return sleepy_send_command_result(command_id, phase, reason);
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

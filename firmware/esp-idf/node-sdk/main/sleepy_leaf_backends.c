#include "sleepy_leaf_backends.h"

#include <stdio.h>
#include <string.h>

#include "board_xiao_sx1262.h"
#include "esp_check.h"
#include "esp_log.h"
#include "fabric_proto/fabric_proto.h"
#include "radio_hal_sx1262.h"
#include "sleepy_policy.h"

static const char *TAG = "sleepy_backend";
static bool s_emit_pending_digest;
static bool s_emit_demo_command;
static bool s_smoke_completed;
static bool s_auto_smoke_enabled = true;
static int s_scripted_pending_count = -1;
static char s_scripted_mode[24];
static int s_scripted_expires_in_s;
static uint16_t s_scripted_command_token;

static esp_err_t sleepy_radio_backend_apply_profile(const radio_hal_lora_profile_t *profile, void *context);
static esp_err_t sleepy_radio_backend_tx(const uint8_t *payload, size_t payload_len, void *context);
static esp_err_t sleepy_radio_backend_poll_rx(radio_hal_frame_t *frame, void *context);
static uint16_t sleepy_leaf_hash_command_token(const char *command_id);
static uint8_t sleepy_leaf_mode_to_command_kind(const char *mode, uint8_t *argument);
static void sleepy_leaf_clear_scripted_command(void);

esp_err_t sleepy_leaf_install_default_backends(void) {
    static const radio_hal_backend_t radio_backend = {
        .apply_profile = sleepy_radio_backend_apply_profile,
        .tx = sleepy_radio_backend_tx,
        .poll_rx = sleepy_radio_backend_poll_rx,
        .context = NULL,
        .name = "sleepy-dev-radio",
        .development_only = true,
    };
    ESP_RETURN_ON_ERROR(board_xiao_sx1262_init(), TAG, "board init failed");
    ESP_RETURN_ON_ERROR(board_xiao_sx1262_reset_lora(), TAG, "lora reset failed");
    ESP_RETURN_ON_ERROR(radio_hal_install_backend(&radio_backend), TAG, "radio backend install failed");
    ESP_RETURN_ON_ERROR(sleepy_leaf_backend_reset_smoke(), TAG, "smoke reset failed");
    ESP_LOGI(TAG, "installed sleepy default development backend");
    return ESP_OK;
}

esp_err_t sleepy_leaf_backend_reset_smoke(void) {
    s_emit_pending_digest = false;
    s_emit_demo_command = false;
    s_smoke_completed = false;
    s_auto_smoke_enabled = true;
    s_scripted_pending_count = -1;
    memset(s_scripted_mode, 0, sizeof(s_scripted_mode));
    s_scripted_expires_in_s = 0;
    s_scripted_command_token = 0u;
    return ESP_OK;
}

esp_err_t sleepy_leaf_backend_set_auto_smoke(bool enabled) {
    s_auto_smoke_enabled = enabled;
    return ESP_OK;
}

esp_err_t sleepy_leaf_backend_script_pending_digest(int pending_count) {
    if (pending_count < 0) {
        return ESP_ERR_INVALID_ARG;
    }
    s_scripted_pending_count = pending_count;
    s_emit_pending_digest = true;
    return ESP_OK;
}

esp_err_t sleepy_leaf_backend_script_compact_command(const char *command_id, const char *mode, int expires_in_s) {
    if (command_id == NULL || command_id[0] == '\0' || mode == NULL || mode[0] == '\0') {
        return ESP_ERR_INVALID_ARG;
    }
    if (strlen(mode) >= sizeof(s_scripted_mode)) {
        return ESP_ERR_INVALID_SIZE;
    }
    snprintf(s_scripted_mode, sizeof(s_scripted_mode), "%s", mode);
    s_scripted_expires_in_s = expires_in_s;
    s_scripted_command_token = sleepy_leaf_hash_command_token(command_id);
    s_emit_demo_command = true;
    return ESP_OK;
}

static esp_err_t sleepy_radio_backend_apply_profile(const radio_hal_lora_profile_t *profile, void *context) {
    (void)context;
    if (profile == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_LOGI(
        TAG,
        "sleepy radio profile freq=%lu sf=%u bw=%u tx=%u",
        (unsigned long)profile->frequency_hz,
        (unsigned)profile->spreading_factor,
        (unsigned)profile->bandwidth_khz,
        (unsigned)profile->tx_power_dbm);
    return ESP_OK;
}

static esp_err_t sleepy_radio_backend_tx(const uint8_t *payload, size_t payload_len, void *context) {
    ef_onair_packet_t packet;
    (void)context;
    ESP_LOGD(TAG, "sleepy radio tx handoff: %u bytes", (unsigned)payload_len);
    if (payload != NULL && ef_onair_decode_packet(payload, payload_len, &packet) == ESP_OK) {
        if (s_auto_smoke_enabled && !s_smoke_completed &&
            (packet.logical_type == EF_ONAIR_TYPE_STATE || packet.logical_type == EF_ONAIR_TYPE_EVENT)) {
            s_emit_pending_digest = true;
        } else if (s_auto_smoke_enabled && !s_smoke_completed && packet.logical_type == EF_ONAIR_TYPE_TINY_POLL) {
            s_emit_demo_command = true;
        } else if (packet.logical_type == EF_ONAIR_TYPE_COMMAND_RESULT) {
            ESP_LOGI(TAG, "sleepy demo result observed");
            s_smoke_completed = true;
        }
    }
    return ESP_OK;
}

static esp_err_t sleepy_radio_backend_poll_rx(radio_hal_frame_t *frame, void *context) {
    (void)context;
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    memset(frame, 0, sizeof(*frame));
    if (s_emit_pending_digest) {
        ef_onair_pending_digest_body_t digest = {
            .pending_count = (uint8_t)(s_scripted_pending_count >= 0 ? s_scripted_pending_count : 1),
            .flags = 0u,
        };
        size_t frame_len = 0u;
        const int pending_count = s_scripted_pending_count >= 0 ? s_scripted_pending_count : 1;
        if (pending_count > 0) {
            digest.flags = EF_ONAIR_PENDING_FLAG_URGENT;
        }
        ESP_RETURN_ON_ERROR(
            ef_onair_encode_pending_digest(0x7001u, true, 0u, &digest, frame->payload, sizeof(frame->payload), &frame_len),
            TAG,
            "pending digest encode failed");
        frame->payload_len = frame_len;
        frame->rssi_dbm = -72;
        frame->snr_db = 7;
        s_emit_pending_digest = false;
        s_scripted_pending_count = -1;
        ESP_LOGI(TAG, "emitting synthetic pending digest");
        return ESP_OK;
    }
    if (s_emit_demo_command) {
        ef_onair_compact_command_body_t command = {0};
        const char *mode = s_scripted_mode[0] != '\0' ? s_scripted_mode : "MAINT_ON";
        const int expires_in_s = s_scripted_expires_in_s != 0 ? s_scripted_expires_in_s : 60;
        size_t frame_len = 0u;
        command.command_token = s_scripted_command_token != 0u ? s_scripted_command_token : sleepy_leaf_hash_command_token("cmd-demo-001");
        command.expires_in_sec = expires_in_s > 255 ? 255u : (expires_in_s < 0 ? 0u : (uint8_t)expires_in_s);
        command.command_kind = sleepy_leaf_mode_to_command_kind(mode, &command.argument);
        if (command.command_kind == 0u) {
            sleepy_leaf_clear_scripted_command();
            return ESP_ERR_NOT_SUPPORTED;
        }
        ESP_RETURN_ON_ERROR(
            ef_onair_encode_compact_command(
                sleepy_policy_get_short_id(),
                false,
                0u,
                &command,
                frame->payload,
                sizeof(frame->payload),
                &frame_len),
            TAG,
            "compact command encode failed");
        frame->payload_len = frame_len;
        frame->rssi_dbm = -70;
        frame->snr_db = 8;
        sleepy_leaf_clear_scripted_command();
        ESP_LOGI(TAG, "emitting synthetic sleepy command");
        return ESP_OK;
    }
    return ESP_ERR_NOT_FOUND;
}

static uint16_t sleepy_leaf_hash_command_token(const char *command_id) {
    uint32_t hash = 2166136261u;
    const unsigned char *cursor = (const unsigned char *)command_id;
    if (command_id == NULL || command_id[0] == '\0') {
        return 1u;
    }
    while (*cursor != '\0') {
        hash ^= (uint32_t)(*cursor);
        hash *= 16777619u;
        cursor++;
    }
    hash = (hash ^ (hash >> 16u)) & 0xffffu;
    return (uint16_t)(hash == 0u ? 1u : hash);
}

static uint8_t sleepy_leaf_mode_to_command_kind(const char *mode, uint8_t *argument) {
    if (argument != NULL) {
        *argument = 0u;
    }
    if (mode == NULL || mode[0] == '\0') {
        return 0u;
    }
    if (strcmp(mode, "MAINT_ON") == 0 || strcmp(mode, "maintenance_awake") == 0) {
        return EF_ONAIR_COMMAND_KIND_MAINTENANCE_ON;
    }
    if (strcmp(mode, "MAINT_OFF") == 0 || strcmp(mode, "deployed") == 0) {
        return EF_ONAIR_COMMAND_KIND_MAINTENANCE_OFF;
    }
    if (strcmp(mode, "ALARM_CLEAR") == 0) {
        return EF_ONAIR_COMMAND_KIND_ALARM_CLEAR;
    }
    if (strcmp(mode, "THRESHOLD_10") == 0) {
        if (argument != NULL) {
            *argument = 10u;
        }
        return EF_ONAIR_COMMAND_KIND_THRESHOLD_SET;
    }
    if (strcmp(mode, "QUIET_1") == 0) {
        if (argument != NULL) {
            *argument = 1u;
        }
        return EF_ONAIR_COMMAND_KIND_QUIET_SET;
    }
    if (strcmp(mode, "SAMPLING_5") == 0) {
        if (argument != NULL) {
            *argument = 5u;
        }
        return EF_ONAIR_COMMAND_KIND_SAMPLING_SET;
    }
    return 0u;
}

static void sleepy_leaf_clear_scripted_command(void) {
    s_emit_demo_command = false;
    memset(s_scripted_mode, 0, sizeof(s_scripted_mode));
    s_scripted_expires_in_s = 0;
    s_scripted_command_token = 0u;
}

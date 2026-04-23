#include "sleepy_leaf_backends.h"

#include <stdio.h>
#include <string.h>

#include "board_xiao_sx1262.h"
#include "esp_check.h"
#include "esp_log.h"
#include "radio_hal_sx1262.h"
#include "sleepy_policy.h"

static const char *TAG = "sleepy_backend";
static bool s_emit_pending_digest;
static bool s_emit_demo_command;
static bool s_smoke_completed;
static bool s_auto_smoke_enabled = true;
static int s_scripted_pending_count = -1;
static char s_scripted_command_id[48];
static char s_scripted_mode[24];
static int s_scripted_expires_in_s;

static esp_err_t sleepy_radio_backend_apply_profile(const radio_hal_lora_profile_t *profile, void *context);
static esp_err_t sleepy_radio_backend_tx(const uint8_t *payload, size_t payload_len, void *context);
static esp_err_t sleepy_radio_backend_poll_rx(radio_hal_frame_t *frame, void *context);

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
    memset(s_scripted_command_id, 0, sizeof(s_scripted_command_id));
    memset(s_scripted_mode, 0, sizeof(s_scripted_mode));
    s_scripted_expires_in_s = 0;
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
    if (strlen(command_id) >= sizeof(s_scripted_command_id) || strlen(mode) >= sizeof(s_scripted_mode)) {
        return ESP_ERR_INVALID_SIZE;
    }
    snprintf(s_scripted_command_id, sizeof(s_scripted_command_id), "%s", command_id);
    snprintf(s_scripted_mode, sizeof(s_scripted_mode), "%s", mode);
    s_scripted_expires_in_s = expires_in_s;
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
    (void)context;
    ESP_LOGD(TAG, "sleepy radio tx handoff: %u bytes", (unsigned)payload_len);
    if (payload != NULL && payload_len >= 2u) {
        if (s_auto_smoke_enabled && !s_smoke_completed && (payload[0] == 'S' || payload[0] == 'E')) {
            s_emit_pending_digest = true;
        } else if (s_auto_smoke_enabled && !s_smoke_completed && payload[0] == 'P' && payload[1] == '|') {
            s_emit_demo_command = true;
        } else if (payload[0] == 'R' && payload[1] == '|') {
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
        char digest[64];
        const char *node_id = sleepy_policy_get_node_id();
        const int pending_count = s_scripted_pending_count >= 0 ? s_scripted_pending_count : 1;
        const int written = snprintf(digest, sizeof(digest), "D|%s|%d", node_id, pending_count);
        if (written <= 0 || (size_t)written >= sizeof(digest)) {
            return ESP_ERR_INVALID_SIZE;
        }
        memcpy(frame->payload, digest, (size_t)written);
        frame->payload_len = (size_t)written;
        frame->rssi_dbm = -72;
        frame->snr_db = 7;
        s_emit_pending_digest = false;
        s_scripted_pending_count = -1;
        ESP_LOGI(TAG, "emitting synthetic pending digest");
        return ESP_OK;
    }
    if (s_emit_demo_command) {
        char command[96];
        const char *node_id = sleepy_policy_get_node_id();
        const char *command_id = s_scripted_command_id[0] != '\0' ? s_scripted_command_id : "cmd-demo-001";
        const char *mode = s_scripted_mode[0] != '\0' ? s_scripted_mode : "OK";
        const int expires_in_s = s_scripted_expires_in_s != 0 ? s_scripted_expires_in_s : 60;
        const int written = snprintf(command, sizeof(command), "C|%s|%s|ENP|%s|%d", node_id, command_id, mode, expires_in_s);
        if (written <= 0 || (size_t)written >= sizeof(command)) {
            return ESP_ERR_INVALID_SIZE;
        }
        memcpy(frame->payload, command, (size_t)written);
        frame->payload_len = (size_t)written;
        frame->rssi_dbm = -70;
        frame->snr_db = 8;
        s_emit_demo_command = false;
        memset(s_scripted_command_id, 0, sizeof(s_scripted_command_id));
        memset(s_scripted_mode, 0, sizeof(s_scripted_mode));
        s_scripted_expires_in_s = 0;
        ESP_LOGI(TAG, "emitting synthetic sleepy command");
        return ESP_OK;
    }
    return ESP_ERR_NOT_FOUND;
}

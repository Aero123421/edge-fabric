#include "fabric_proto/fabric_proto.h"
#include "board_xiao_sx1262.h"
#include "gateway_head_backends.h"
#include "gateway_head_runtime.h"
#include "radio_hal_sx1262.h"
#include "radio_hal_real_sx1262.h"
#include "usb_link.h"
#include "usb_tinyusb_backend.h"

#include <stdbool.h>
#include <stdio.h>
#include <string.h>

#include "esp_check.h"
#include "esp_err.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

static const char *TAG = "gateway_head";
static bool s_use_default_backends;
static bool s_transport_initialized;
static char s_gateway_id[32];

typedef struct {
    uint32_t usb_rx_frames;
    uint32_t usb_tx_ok;
    uint32_t usb_tx_fail;
    uint32_t usb_tx_backpressure;
    uint32_t radio_rx_frames;
    uint32_t radio_tx_ok;
    uint32_t radio_tx_fail;
} gateway_runtime_counters_t;

static gateway_runtime_counters_t s_counters;

static void gateway_head_runtime_task(void *arg);
static esp_err_t gateway_send_heartbeat(const char *status, int extra_value);
static esp_err_t gateway_send_usb_frame(uint8_t frame_type, const uint8_t *payload, size_t payload_len);
static esp_err_t gateway_send_radio_frame(const uint8_t *payload, size_t payload_len);
static esp_err_t gateway_handle_usb_frame(const uint8_t *frame, size_t frame_len);
static esp_err_t gateway_handle_radio_frame(const radio_hal_frame_t *frame);
static bool gateway_payload_is_json_object(const uint8_t *payload, size_t payload_len);
static bool gateway_payload_is_legacy_compact(const uint8_t *payload, size_t payload_len);
static bool gateway_dev_wire_compat_enabled(void);
static bool gateway_usb_tx_error_is_backpressure(esp_err_t err);
static const char *gateway_usb_dtr_state(void);
static esp_err_t gateway_validate_onair_packet(const ef_onair_packet_t *packet);
static uint8_t gateway_classify_radio_frame_type(const radio_hal_frame_t *frame);
static esp_err_t gateway_head_runtime_ensure_identity(void);

esp_err_t gateway_head_runtime_set_default_backends(bool enabled) {
    s_use_default_backends = enabled;
    return ESP_OK;
}

esp_err_t gateway_head_runtime_init_transport(void) {
    const usb_link_config_t usb_config = {
        .max_frame_len = 512u,
        .rx_queue_depth = 4u,
    };
    if (s_transport_initialized) {
        return ESP_OK;
    }
    ESP_RETURN_ON_ERROR(radio_hal_init(), TAG, "radio init failed");
    ESP_RETURN_ON_ERROR(usb_link_init(&usb_config), TAG, "usb init failed");
    s_transport_initialized = true;
    return ESP_OK;
}

esp_err_t gateway_head_runtime_use_default_backends(void) {
#if defined(CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS) && CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS
    ESP_LOGE(TAG, "development backends are disabled by CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS");
    return ESP_ERR_INVALID_STATE;
#else
    return gateway_head_runtime_set_default_backends(true);
#endif
}

esp_err_t gateway_head_runtime_use_real_backends(void) {
    ESP_RETURN_ON_ERROR(gateway_head_runtime_init_transport(), TAG, "transport init failed");
    ESP_RETURN_ON_ERROR(usb_tinyusb_backend_install(), TAG, "tinyusb backend install failed");
    ESP_RETURN_ON_ERROR(radio_hal_install_real_sx1262_backend(), TAG, "sx1262 real backend install failed");
    return gateway_head_runtime_set_default_backends(false);
}

esp_err_t gateway_head_runtime_start(void) {
    static const radio_hal_lora_profile_t gateway_profile = {
        .frequency_hz = 922400000u,
        .spreading_factor = 10u,
        .bandwidth_khz = 125u,
        .tx_power_dbm = 10u,
    };
    ESP_RETURN_ON_ERROR(gateway_head_runtime_init_transport(), TAG, "transport init failed");
    ESP_RETURN_ON_ERROR(gateway_head_runtime_ensure_identity(), TAG, "identity init failed");
    if (s_use_default_backends) {
        ESP_RETURN_ON_ERROR(gateway_head_install_default_backends(), TAG, "backend install failed");
    }
    if (!usb_link_has_delivery_path() || !usb_link_has_poll_path() ||
        !radio_hal_has_delivery_path() || !radio_hal_has_poll_path()) {
        ESP_LOGE(TAG, "gateway backends are not fully configured; install explicit TX and RX backends before start");
        return ESP_ERR_INVALID_STATE;
    }
#if defined(CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS) && CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS
    if (usb_link_backend_is_development_only() || radio_hal_backend_is_development_only()) {
        ESP_LOGE(
            TAG,
            "strict real backend guard rejected usb_backend=%s radio_backend=%s",
            usb_link_backend_name(),
            radio_hal_backend_name());
        return ESP_ERR_INVALID_STATE;
    }
#endif
    ESP_RETURN_ON_ERROR(radio_hal_apply_jp_safe_profile(&gateway_profile), TAG, "profile apply failed");
    ESP_LOGI(
        TAG,
        "gateway startup usb_backend=%s radio_backend=%s usb_dev=%s radio_dev=%s",
        usb_link_backend_name(),
        radio_hal_backend_name(),
        usb_link_backend_is_development_only() ? "yes" : "no",
        radio_hal_backend_is_development_only() ? "yes" : "no");
    ESP_RETURN_ON_ERROR(gateway_send_heartbeat("live", 0), TAG, "startup heartbeat failed");
    if (xTaskCreate(gateway_head_runtime_task, "gateway_runtime", 4096, NULL, 5, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    ESP_LOGI(TAG, "gateway runtime task started");
    return ESP_OK;
}

static void gateway_head_runtime_task(void *arg) {
    (void)arg;
    for (;;) {
        const esp_err_t err = gateway_head_runtime_poll_once();
        if (err != ESP_OK && err != ESP_ERR_NOT_FOUND) {
            ESP_LOGW(TAG, "gateway runtime poll failed: %s", esp_err_to_name(err));
        }
        vTaskDelay(pdMS_TO_TICKS(10u));
    }
}

esp_err_t gateway_head_runtime_poll_once(void) {
    uint8_t usb_frame[512];
    size_t usb_frame_len = 0u;
    radio_hal_frame_t radio_frame;
    bool handled = false;
    {
        const esp_err_t service_err = usb_link_service();
        if (service_err != ESP_OK && service_err != ESP_ERR_NOT_SUPPORTED) {
            return service_err;
        }
    }
    {
        const esp_err_t service_err = radio_hal_service();
        if (service_err != ESP_OK && service_err != ESP_ERR_NOT_SUPPORTED) {
            return service_err;
        }
    }
    esp_err_t err = usb_link_receive_frame(usb_frame, sizeof(usb_frame), &usb_frame_len, 0u);
    if (err == ESP_OK) {
        handled = true;
        ESP_RETURN_ON_ERROR(gateway_handle_usb_frame(usb_frame, usb_frame_len), TAG, "USB frame handle failed");
    } else if (err != ESP_ERR_TIMEOUT) {
        return err;
    }
    err = radio_hal_receive_frame(&radio_frame, 0u);
    if (err == ESP_OK) {
        handled = true;
        ESP_RETURN_ON_ERROR(gateway_handle_radio_frame(&radio_frame), TAG, "LoRa frame handle failed");
    } else if (err != ESP_ERR_TIMEOUT) {
        return err;
    }
    return handled ? ESP_OK : ESP_ERR_NOT_FOUND;
}

static esp_err_t gateway_handle_usb_frame(const uint8_t *frame, size_t frame_len) {
    uint16_t payload_len;
    const uint8_t *payload;
    ef_onair_packet_t packet;
    if (frame == NULL || frame_len < 10u) {
        return ESP_ERR_INVALID_ARG;
    }
    s_counters.usb_rx_frames++;
    ESP_RETURN_ON_ERROR(ef_usb_frame_validate(frame, frame_len), TAG, "invalid USB frame");
    payload_len = (uint16_t)frame[4] | ((uint16_t)frame[5] << 8u);
    payload = &frame[6];
    switch (frame[3]) {
        case EF_USB_FRAME_ENVELOPE_JSON:
            if (!gateway_dev_wire_compat_enabled()) {
                ESP_LOGW(TAG, "raw JSON over LoRa is disabled outside development backends");
                return ESP_ERR_NOT_SUPPORTED;
            }
            if (!gateway_payload_is_json_object(payload, payload_len)) {
                return ESP_ERR_INVALID_RESPONSE;
            }
            ESP_RETURN_ON_ERROR(gateway_send_radio_frame(payload, payload_len), TAG, "LoRa TX failed");
            return gateway_send_heartbeat("hop_buffered", (int)payload_len);
        case EF_USB_FRAME_COMPACT_BINARY:
        case EF_USB_FRAME_SUMMARY_BINARY:
            if (ef_onair_decode_packet(payload, payload_len, &packet) == ESP_OK) {
                ESP_RETURN_ON_ERROR(gateway_validate_onair_packet(&packet), TAG, "invalid on-air body");
            } else {
                if (!(gateway_dev_wire_compat_enabled() && gateway_payload_is_legacy_compact(payload, payload_len))) {
                    return ESP_ERR_INVALID_RESPONSE;
                }
            }
            ESP_RETURN_ON_ERROR(gateway_send_radio_frame(payload, payload_len), TAG, "LoRa TX failed");
            return gateway_send_heartbeat("hop_buffered", (int)payload_len);
        case EF_USB_FRAME_HEARTBEAT_JSON:
            ESP_LOGI(TAG, "heartbeat received from host");
            return ESP_OK;
        default:
            return ESP_ERR_NOT_SUPPORTED;
    }
}

static esp_err_t gateway_handle_radio_frame(const radio_hal_frame_t *frame) {
    char status_json[448];
    uint8_t frame_type;
    int written;
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    s_counters.radio_rx_frames++;
    frame_type = gateway_classify_radio_frame_type(frame);
    if (frame_type == 0u) {
        return ESP_ERR_INVALID_RESPONSE;
    }
    ESP_RETURN_ON_ERROR(
        gateway_send_usb_frame(frame_type, frame->payload, frame->payload_len),
        TAG,
        "USB relay failed");
    written = snprintf(
        status_json,
        sizeof(status_json),
        "{\"gateway_id\":\"%s\",\"subject_kind\":\"gateway\",\"subject_id\":\"%s\",\"live\":true,\"status\":\"lora_ingress\",\"rssi\":%d,\"snr\":%d,\"usb_dtr\":\"%s\",\"usb_rx_frames\":%lu,\"usb_tx_ok\":%lu,\"usb_tx_fail\":%lu,\"usb_tx_backpressure\":%lu,\"radio_rx_frames\":%lu,\"radio_tx_ok\":%lu,\"radio_tx_fail\":%lu}",
        s_gateway_id,
        s_gateway_id,
        (int)frame->rssi_dbm,
        (int)frame->snr_db,
        gateway_usb_dtr_state(),
        (unsigned long)s_counters.usb_rx_frames,
        (unsigned long)s_counters.usb_tx_ok,
        (unsigned long)s_counters.usb_tx_fail,
        (unsigned long)s_counters.usb_tx_backpressure,
        (unsigned long)s_counters.radio_rx_frames,
        (unsigned long)s_counters.radio_tx_ok,
        (unsigned long)s_counters.radio_tx_fail);
    if (written <= 0 || (size_t)written >= sizeof(status_json)) {
        return ESP_ERR_INVALID_SIZE;
    }
    return gateway_send_usb_frame(EF_USB_FRAME_HEARTBEAT_JSON, (const uint8_t *)status_json, strlen(status_json));
}

static esp_err_t gateway_send_heartbeat(const char *status, int extra_value) {
    char json[448];
    int written = snprintf(
        json,
        sizeof(json),
        "{\"gateway_id\":\"%s\",\"subject_kind\":\"gateway\",\"subject_id\":\"%s\",\"live\":true,\"status\":\"%s\",\"value\":%d,\"usb_dtr\":\"%s\",\"usb_rx_frames\":%lu,\"usb_tx_ok\":%lu,\"usb_tx_fail\":%lu,\"usb_tx_backpressure\":%lu,\"radio_rx_frames\":%lu,\"radio_tx_ok\":%lu,\"radio_tx_fail\":%lu}",
        s_gateway_id,
        s_gateway_id,
        status,
        extra_value,
        gateway_usb_dtr_state(),
        (unsigned long)s_counters.usb_rx_frames,
        (unsigned long)s_counters.usb_tx_ok,
        (unsigned long)s_counters.usb_tx_fail,
        (unsigned long)s_counters.usb_tx_backpressure,
        (unsigned long)s_counters.radio_rx_frames,
        (unsigned long)s_counters.radio_tx_ok,
        (unsigned long)s_counters.radio_tx_fail);
    if (written <= 0 || (size_t)written >= sizeof(json)) {
        return ESP_ERR_INVALID_SIZE;
    }
    return gateway_send_usb_frame(EF_USB_FRAME_HEARTBEAT_JSON, (const uint8_t *)json, strlen(json));
}

static esp_err_t gateway_send_usb_frame(uint8_t frame_type, const uint8_t *payload, size_t payload_len) {
    uint8_t frame[512];
    size_t frame_len = 0u;
    esp_err_t err = ef_usb_frame_encode(frame_type, payload, payload_len, frame, sizeof(frame), &frame_len);
    if (err != ESP_OK) {
        s_counters.usb_tx_fail++;
        return err;
    }
    err = usb_link_send_frame(frame, frame_len);
    if (err == ESP_OK) {
        s_counters.usb_tx_ok++;
        return ESP_OK;
    }
    s_counters.usb_tx_fail++;
    if (gateway_usb_tx_error_is_backpressure(err)) {
        s_counters.usb_tx_backpressure++;
    }
    ESP_LOGW(TAG, "USB TX failed err=%s backpressure_count=%lu", esp_err_to_name(err), (unsigned long)s_counters.usb_tx_backpressure);
    return err;
}

static esp_err_t gateway_send_radio_frame(const uint8_t *payload, size_t payload_len) {
    const esp_err_t err = radio_hal_send_frame(payload, payload_len);
    if (err == ESP_OK) {
        s_counters.radio_tx_ok++;
        return ESP_OK;
    }
    s_counters.radio_tx_fail++;
    return err;
}

static bool gateway_payload_is_json_object(const uint8_t *payload, size_t payload_len) {
    size_t index;
    if (payload == NULL || payload_len < 2u) {
        return false;
    }
    if (payload[0] != '{' || payload[payload_len - 1u] != '}') {
        return false;
    }
    for (index = 0; index < payload_len; ++index) {
        if (payload[index] == '\0') {
            return false;
        }
    }
    return true;
}

static bool gateway_payload_is_legacy_compact(const uint8_t *payload, size_t payload_len) {
    size_t index;
    if (payload == NULL || payload_len < 3u) {
        return false;
    }
    if (!(payload[0] >= 'A' && payload[0] <= 'Z') || payload[1] != '|') {
        return false;
    }
    for (index = 0; index < payload_len; ++index) {
        if (payload[index] == '\0') {
            return false;
        }
    }
    return true;
}

static bool gateway_dev_wire_compat_enabled(void) {
    return radio_hal_backend_is_development_only();
}

static bool gateway_usb_tx_error_is_backpressure(esp_err_t err) {
    return err == ESP_ERR_TIMEOUT || err == ESP_ERR_NO_MEM || err == ESP_FAIL;
}

static const char *gateway_usb_dtr_state(void) {
    if (usb_link_backend_is_development_only()) {
        return "n/a";
    }
    return "unknown";
}

static esp_err_t gateway_validate_onair_packet(const ef_onair_packet_t *packet) {
    if (packet == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    switch (packet->logical_type) {
        case EF_ONAIR_TYPE_STATE:
            return packet->body_len == 3u ? ESP_OK : ESP_ERR_INVALID_SIZE;
        case EF_ONAIR_TYPE_EVENT:
            return packet->body_len == 4u ? ESP_OK : ESP_ERR_INVALID_SIZE;
        case EF_ONAIR_TYPE_COMMAND_RESULT:
            return packet->body_len == 4u ? ESP_OK : ESP_ERR_INVALID_SIZE;
        case EF_ONAIR_TYPE_PENDING_DIGEST:
            return packet->body_len == 2u ? ESP_OK : ESP_ERR_INVALID_SIZE;
        case EF_ONAIR_TYPE_TINY_POLL:
            return packet->body_len == 1u ? ESP_OK : ESP_ERR_INVALID_SIZE;
        case EF_ONAIR_TYPE_COMPACT_COMMAND:
            return packet->body_len == 5u ? ESP_OK : ESP_ERR_INVALID_SIZE;
        case EF_ONAIR_TYPE_HEARTBEAT:
            return packet->body_len == 5u ? ESP_OK : ESP_ERR_INVALID_SIZE;
        default:
            return ESP_ERR_NOT_SUPPORTED;
    }
}

static uint8_t gateway_classify_radio_frame_type(const radio_hal_frame_t *frame) {
    ef_onair_packet_t packet;
    if (frame == NULL || frame->payload_len == 0u) {
        return 0u;
    }
    if (ef_onair_decode_packet(frame->payload, frame->payload_len, &packet) == ESP_OK &&
        gateway_validate_onair_packet(&packet) == ESP_OK) {
        return (packet.flags & EF_ONAIR_FLAG_SUMMARY) != 0u ? EF_USB_FRAME_SUMMARY_BINARY : EF_USB_FRAME_COMPACT_BINARY;
    }
    if (gateway_dev_wire_compat_enabled() && gateway_payload_is_json_object(frame->payload, frame->payload_len)) {
        return EF_USB_FRAME_ENVELOPE_JSON;
    }
    if (gateway_dev_wire_compat_enabled() && gateway_payload_is_legacy_compact(frame->payload, frame->payload_len)) {
        return EF_USB_FRAME_COMPACT_BINARY;
    }
    return 0u;
}

static esp_err_t gateway_head_runtime_ensure_identity(void) {
    if (s_gateway_id[0] != '\0') {
        return ESP_OK;
    }
    if (board_xiao_sx1262_format_identity("gw", s_gateway_id, sizeof(s_gateway_id)) != ESP_OK) {
        snprintf(s_gateway_id, sizeof(s_gateway_id), "gw-local");
        ESP_LOGW(TAG, "falling back to local gateway identity");
    }
    return ESP_OK;
}

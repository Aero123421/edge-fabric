#include "fabric_proto/fabric_proto.h"
#include "gateway_head_backends.h"
#include "radio_hal_sx1262.h"
#include "usb_link.h"

#include <stdbool.h>
#include <stdio.h>
#include <string.h>

#include "esp_check.h"
#include "esp_err.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

static const char *TAG = "gateway_head";
static const char *GATEWAY_ID = "gw-01";
static bool s_use_default_backends;

static void gateway_head_runtime_task(void *arg);
static esp_err_t gateway_send_heartbeat(const char *status, int extra_value);
static esp_err_t gateway_send_usb_frame(uint8_t frame_type, const uint8_t *payload, size_t payload_len);
static esp_err_t gateway_handle_usb_frame(const uint8_t *frame, size_t frame_len);
static esp_err_t gateway_handle_radio_frame(const radio_hal_frame_t *frame);
static bool gateway_payload_is_json_object(const uint8_t *payload, size_t payload_len);
static uint8_t gateway_classify_radio_frame_type(const radio_hal_frame_t *frame);

esp_err_t gateway_head_runtime_set_default_backends(bool enabled) {
    s_use_default_backends = enabled;
    return ESP_OK;
}

esp_err_t gateway_head_runtime_init_transport(void) {
    const usb_link_config_t usb_config = {
        .max_frame_len = 512u,
        .rx_queue_depth = 4u,
    };
    ESP_RETURN_ON_ERROR(radio_hal_init(), TAG, "radio init failed");
    return usb_link_init(&usb_config);
}

esp_err_t gateway_head_runtime_use_default_backends(void) {
    return gateway_head_runtime_set_default_backends(true);
}

esp_err_t gateway_head_runtime_start(void) {
    static const radio_hal_lora_profile_t gateway_profile = {
        .frequency_hz = 922400000u,
        .spreading_factor = 9u,
        .bandwidth_khz = 125u,
        .tx_power_dbm = 10u,
    };
    ESP_RETURN_ON_ERROR(gateway_head_runtime_init_transport(), TAG, "transport init failed");
    if (s_use_default_backends) {
        ESP_RETURN_ON_ERROR(gateway_head_install_default_backends(), TAG, "backend install failed");
    }
    if (!usb_link_has_delivery_path() || !usb_link_has_poll_path() ||
        !radio_hal_has_delivery_path() || !radio_hal_has_poll_path()) {
        ESP_LOGE(TAG, "gateway backends are not fully configured; install explicit TX and RX backends before start");
        return ESP_ERR_INVALID_STATE;
    }
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
    if (frame == NULL || frame_len < 10u) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(ef_usb_frame_validate(frame, frame_len), TAG, "invalid USB frame");
    payload_len = (uint16_t)frame[4] | ((uint16_t)frame[5] << 8u);
    payload = &frame[6];
    switch (frame[3]) {
        case EF_USB_FRAME_ENVELOPE_JSON:
        case EF_USB_FRAME_COMPACT_BINARY:
        case EF_USB_FRAME_SUMMARY_BINARY:
            ESP_RETURN_ON_ERROR(radio_hal_send_frame(payload, payload_len), TAG, "LoRa TX failed");
            return gateway_send_heartbeat("hop_buffered", (int)payload_len);
        case EF_USB_FRAME_HEARTBEAT_JSON:
            ESP_LOGI(TAG, "heartbeat received from host");
            return ESP_OK;
        default:
            return ESP_ERR_NOT_SUPPORTED;
    }
}

static esp_err_t gateway_handle_radio_frame(const radio_hal_frame_t *frame) {
    char status_json[160];
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(
        gateway_send_usb_frame(gateway_classify_radio_frame_type(frame), frame->payload, frame->payload_len),
        TAG,
        "USB relay failed");
    snprintf(
        status_json,
        sizeof(status_json),
        "{\"gateway_id\":\"%s\",\"status\":\"lora_ingress\",\"rssi\":%d,\"snr\":%d}",
        GATEWAY_ID,
        (int)frame->rssi_dbm,
        (int)frame->snr_db);
    return gateway_send_usb_frame(EF_USB_FRAME_HEARTBEAT_JSON, (const uint8_t *)status_json, strlen(status_json));
}

static esp_err_t gateway_send_heartbeat(const char *status, int extra_value) {
    char json[160];
    snprintf(
        json,
        sizeof(json),
        "{\"gateway_id\":\"%s\",\"live\":true,\"status\":\"%s\",\"value\":%d}",
        GATEWAY_ID,
        status,
        extra_value);
    return gateway_send_usb_frame(EF_USB_FRAME_HEARTBEAT_JSON, (const uint8_t *)json, strlen(json));
}

static esp_err_t gateway_send_usb_frame(uint8_t frame_type, const uint8_t *payload, size_t payload_len) {
    uint8_t frame[512];
    size_t frame_len = 0u;
    ESP_RETURN_ON_ERROR(
        ef_usb_frame_encode(frame_type, payload, payload_len, frame, sizeof(frame), &frame_len),
        TAG,
        "USB frame encode failed");
    return usb_link_send_frame(frame, frame_len);
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

static uint8_t gateway_classify_radio_frame_type(const radio_hal_frame_t *frame) {
    static const char compact_prefixes[] = {'S', 'E', 'R', 'C', 'D', 'P', 'H'};
    size_t index;
    if (frame == NULL || frame->payload_len == 0u) {
        return EF_USB_FRAME_SUMMARY_BINARY;
    }
    if (gateway_payload_is_json_object(frame->payload, frame->payload_len)) {
        return EF_USB_FRAME_ENVELOPE_JSON;
    }
    if (memchr(frame->payload, '|', frame->payload_len) == NULL) {
        return EF_USB_FRAME_SUMMARY_BINARY;
    }
    for (index = 0; index < sizeof(compact_prefixes) / sizeof(compact_prefixes[0]); ++index) {
        if (frame->payload[0] == (uint8_t)compact_prefixes[index]) {
            return EF_USB_FRAME_COMPACT_BINARY;
        }
    }
    return EF_USB_FRAME_SUMMARY_BINARY;
}

#include "gateway_head_backends.h"

#include <stdbool.h>
#include <string.h>

#include "board_xiao_sx1262.h"
#include "esp_check.h"
#include "esp_log.h"
#include "fabric_proto/fabric_proto.h"
#include "radio_hal_sx1262.h"
#include "usb_link.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static const char *TAG = "gw_backends";

typedef struct {
    size_t length;
    uint8_t data[512];
} gateway_usb_bytes_t;

static gateway_usb_bytes_t s_pending_usb_rx;
static bool s_pending_usb_rx_ready;
static radio_hal_frame_t s_pending_radio_rx;
static bool s_pending_radio_rx_ready;
static gateway_usb_bytes_t s_last_usb_tx;
static radio_hal_frame_t s_last_radio_tx;
static SemaphoreHandle_t s_smoke_lock;

static esp_err_t gateway_usb_backend_tx(const uint8_t *frame, size_t frame_len, void *context);
static esp_err_t gateway_usb_backend_poll_rx(uint8_t *buf, size_t buf_cap, size_t *received_len, void *context);
static esp_err_t gateway_radio_backend_apply_profile(const radio_hal_lora_profile_t *profile, void *context);
static esp_err_t gateway_radio_backend_tx(const uint8_t *payload, size_t payload_len, void *context);
static esp_err_t gateway_radio_backend_poll_rx(radio_hal_frame_t *frame, void *context);
static esp_err_t gateway_head_backend_ensure_lock(void);

esp_err_t gateway_head_install_default_backends(void) {
    static const usb_link_backend_t usb_backend = {
        .tx = gateway_usb_backend_tx,
        .poll_rx = gateway_usb_backend_poll_rx,
        .context = NULL,
        .name = "gateway-dev-usb",
        .development_only = true,
    };
    static const radio_hal_backend_t radio_backend = {
        .apply_profile = gateway_radio_backend_apply_profile,
        .tx = gateway_radio_backend_tx,
        .poll_rx = gateway_radio_backend_poll_rx,
        .context = NULL,
        .name = "gateway-dev-radio",
        .development_only = true,
    };
    ESP_RETURN_ON_ERROR(board_xiao_sx1262_init(), TAG, "board init failed");
    ESP_RETURN_ON_ERROR(board_xiao_sx1262_reset_lora(), TAG, "lora reset failed");
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    ESP_RETURN_ON_ERROR(usb_link_install_backend(&usb_backend), TAG, "usb backend install failed");
    ESP_RETURN_ON_ERROR(radio_hal_install_backend(&radio_backend), TAG, "radio backend install failed");
    ESP_RETURN_ON_ERROR(gateway_head_backend_reset_smoke(), TAG, "smoke reset failed");
    ESP_LOGW(TAG, "installed default development backends; not for production");
    return ESP_OK;
}

esp_err_t gateway_head_backend_reset_smoke(void) {
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    memset(&s_pending_usb_rx, 0, sizeof(s_pending_usb_rx));
    memset(&s_pending_radio_rx, 0, sizeof(s_pending_radio_rx));
    memset(&s_last_usb_tx, 0, sizeof(s_last_usb_tx));
    memset(&s_last_radio_tx, 0, sizeof(s_last_radio_tx));
    s_pending_usb_rx_ready = false;
    s_pending_radio_rx_ready = false;
    xSemaphoreGive(s_smoke_lock);
    return ESP_OK;
}

esp_err_t gateway_head_backend_script_usb_frame(uint8_t frame_type, const uint8_t *payload, size_t payload_len) {
    size_t frame_len = 0u;
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    if (s_pending_usb_rx_ready) {
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_INVALID_STATE;
    }
    {
        const esp_err_t err = ef_usb_frame_encode(
            frame_type,
            payload,
            payload_len,
            s_pending_usb_rx.data,
            sizeof(s_pending_usb_rx.data),
            &frame_len);
        if (err != ESP_OK) {
            xSemaphoreGive(s_smoke_lock);
            return err;
        }
    }
    s_pending_usb_rx.length = frame_len;
    s_pending_usb_rx_ready = true;
    xSemaphoreGive(s_smoke_lock);
    return ESP_OK;
}

esp_err_t gateway_head_backend_script_radio_frame(const uint8_t *payload, size_t payload_len, int16_t rssi_dbm, int8_t snr_db) {
    if (payload == NULL || payload_len == 0u || payload_len > sizeof(s_pending_radio_rx.payload)) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    if (s_pending_radio_rx_ready) {
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_INVALID_STATE;
    }
    memset(&s_pending_radio_rx, 0, sizeof(s_pending_radio_rx));
    memcpy(s_pending_radio_rx.payload, payload, payload_len);
    s_pending_radio_rx.payload_len = payload_len;
    s_pending_radio_rx.rssi_dbm = rssi_dbm;
    s_pending_radio_rx.snr_db = snr_db;
    s_pending_radio_rx_ready = true;
    xSemaphoreGive(s_smoke_lock);
    return ESP_OK;
}

esp_err_t gateway_head_backend_get_last_usb_tx(uint8_t *frame, size_t frame_cap, size_t *frame_len) {
    if (frame == NULL || frame_len == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    if (s_last_usb_tx.length == 0u) {
        *frame_len = 0u;
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_NOT_FOUND;
    }
    if (frame_cap < s_last_usb_tx.length) {
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_INVALID_SIZE;
    }
    memcpy(frame, s_last_usb_tx.data, s_last_usb_tx.length);
    *frame_len = s_last_usb_tx.length;
    xSemaphoreGive(s_smoke_lock);
    return ESP_OK;
}

esp_err_t gateway_head_backend_get_last_radio_tx(uint8_t *payload, size_t payload_cap, size_t *payload_len) {
    if (payload == NULL || payload_len == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    if (s_last_radio_tx.payload_len == 0u) {
        *payload_len = 0u;
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_NOT_FOUND;
    }
    if (payload_cap < s_last_radio_tx.payload_len) {
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_INVALID_SIZE;
    }
    memcpy(payload, s_last_radio_tx.payload, s_last_radio_tx.payload_len);
    *payload_len = s_last_radio_tx.payload_len;
    xSemaphoreGive(s_smoke_lock);
    return ESP_OK;
}

static esp_err_t gateway_usb_backend_tx(const uint8_t *frame, size_t frame_len, void *context) {
    (void)context;
    if (frame == NULL || frame_len == 0u || frame_len > sizeof(s_last_usb_tx.data)) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    memcpy(s_last_usb_tx.data, frame, frame_len);
    s_last_usb_tx.length = frame_len;
    xSemaphoreGive(s_smoke_lock);
    ESP_LOGD(TAG, "usb backend tx handoff: %u bytes", (unsigned)frame_len);
    return ESP_OK;
}

static esp_err_t gateway_usb_backend_poll_rx(uint8_t *buf, size_t buf_cap, size_t *received_len, void *context) {
    (void)context;
    if (buf == NULL || received_len == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    if (!s_pending_usb_rx_ready) {
        *received_len = 0u;
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_NOT_FOUND;
    }
    if (buf_cap < s_pending_usb_rx.length) {
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_INVALID_SIZE;
    }
    memcpy(buf, s_pending_usb_rx.data, s_pending_usb_rx.length);
    *received_len = s_pending_usb_rx.length;
    s_pending_usb_rx_ready = false;
    s_pending_usb_rx.length = 0u;
    memset(s_pending_usb_rx.data, 0, sizeof(s_pending_usb_rx.data));
    xSemaphoreGive(s_smoke_lock);
    ESP_LOGI(TAG, "emitting scripted USB frame: %u bytes", (unsigned)*received_len);
    return ESP_OK;
}

static esp_err_t gateway_radio_backend_apply_profile(const radio_hal_lora_profile_t *profile, void *context) {
    (void)context;
    if (profile == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_LOGI(
        TAG,
        "radio backend apply profile freq=%lu sf=%u bw=%u tx=%u",
        (unsigned long)profile->frequency_hz,
        (unsigned)profile->spreading_factor,
        (unsigned)profile->bandwidth_khz,
        (unsigned)profile->tx_power_dbm);
    return ESP_OK;
}

static esp_err_t gateway_radio_backend_tx(const uint8_t *payload, size_t payload_len, void *context) {
    (void)context;
    if (payload == NULL || payload_len == 0u || payload_len > sizeof(s_last_radio_tx.payload)) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    memset(&s_last_radio_tx, 0, sizeof(s_last_radio_tx));
    memcpy(s_last_radio_tx.payload, payload, payload_len);
    s_last_radio_tx.payload_len = payload_len;
    xSemaphoreGive(s_smoke_lock);
    ESP_LOGD(TAG, "radio backend tx handoff: %u bytes", (unsigned)payload_len);
    return ESP_OK;
}

static esp_err_t gateway_radio_backend_poll_rx(radio_hal_frame_t *frame, void *context) {
    (void)context;
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(gateway_head_backend_ensure_lock(), TAG, "smoke lock init failed");
    xSemaphoreTake(s_smoke_lock, portMAX_DELAY);
    if (!s_pending_radio_rx_ready) {
        xSemaphoreGive(s_smoke_lock);
        return ESP_ERR_NOT_FOUND;
    }
    *frame = s_pending_radio_rx;
    s_pending_radio_rx_ready = false;
    memset(&s_pending_radio_rx, 0, sizeof(s_pending_radio_rx));
    xSemaphoreGive(s_smoke_lock);
    ESP_LOGI(TAG, "emitting scripted radio frame: %u bytes", (unsigned)frame->payload_len);
    return ESP_OK;
}

static esp_err_t gateway_head_backend_ensure_lock(void) {
    if (s_smoke_lock != NULL) {
        return ESP_OK;
    }
    s_smoke_lock = xSemaphoreCreateMutex();
    if (s_smoke_lock == NULL) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

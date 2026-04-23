#include "radio_hal_sx1262.h"

#include <string.h>

#include "board_xiao_sx1262.h"
#include "esp_check.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "esp_log.h"

typedef struct {
    radio_hal_frame_t frame;
} radio_hal_queue_item_t;

static const char *TAG = "radio_hal";
static const radio_hal_profile_entry_t s_jp_profiles[] = {
    {
        .name = "JP125_LONG_SF10",
        .params = {.frequency_hz = 922400000u, .spreading_factor = 10u, .bandwidth_khz = 125u, .tx_power_dbm = 10u},
        .total_payload_cap = 24u,
    },
    {
        .name = "JP125_BAL_SF9",
        .params = {.frequency_hz = 922400000u, .spreading_factor = 9u, .bandwidth_khz = 125u, .tx_power_dbm = 10u},
        .total_payload_cap = 48u,
    },
    {
        .name = "JP250_FAST_SF8",
        .params = {.frequency_hz = 922400000u, .spreading_factor = 8u, .bandwidth_khz = 250u, .tx_power_dbm = 10u},
        .total_payload_cap = 80u,
    },
    {
        .name = "JP250_CTRL_SF9",
        .params = {.frequency_hz = 922400000u, .spreading_factor = 9u, .bandwidth_khz = 250u, .tx_power_dbm = 10u},
        .total_payload_cap = 64u,
    },
};

static QueueHandle_t s_rx_queue;
static SemaphoreHandle_t s_lock;
static radio_hal_lora_profile_t s_current_profile;
static radio_hal_frame_t s_last_tx_frame;
static const radio_hal_profile_entry_t *s_active_profile;
static radio_hal_backend_t s_backend;
static bool s_initialized;

static bool radio_hal_profile_is_jp_safe(const radio_hal_lora_profile_t *profile) {
    size_t index;
    if (profile == NULL) {
        return false;
    }
    for (index = 0; index < sizeof(s_jp_profiles) / sizeof(s_jp_profiles[0]); ++index) {
        const radio_hal_profile_entry_t *entry = &s_jp_profiles[index];
        if (entry->params.frequency_hz == profile->frequency_hz &&
            entry->params.spreading_factor == profile->spreading_factor &&
            entry->params.bandwidth_khz == profile->bandwidth_khz &&
            entry->params.tx_power_dbm == profile->tx_power_dbm) {
            return true;
        }
    }
    return false;
}

esp_err_t radio_hal_init(void) {
    board_xiao_sx1262_lora_pins_t pins;
    board_xiao_sx1262_get_lora_pins(&pins);
    if (pins.spi_sck == GPIO_NUM_NC || pins.spi_nss == GPIO_NUM_NC) {
        return ESP_ERR_INVALID_STATE;
    }
    if (s_initialized) {
        return ESP_OK;
    }
    s_rx_queue = xQueueCreate(4, sizeof(radio_hal_queue_item_t));
    s_lock = xSemaphoreCreateMutex();
    if (s_rx_queue == NULL || s_lock == NULL) {
        return ESP_ERR_NO_MEM;
    }
    memset(&s_current_profile, 0, sizeof(s_current_profile));
    memset(&s_last_tx_frame, 0, sizeof(s_last_tx_frame));
    s_active_profile = NULL;
    memset(&s_backend, 0, sizeof(s_backend));
    s_initialized = true;
    ESP_LOGI(TAG, "radio HAL initialized for XIAO ESP32-S3 + SX1262");
    return ESP_OK;
}

esp_err_t radio_hal_install_backend(const radio_hal_backend_t *backend) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (backend == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    s_backend = *backend;
    xSemaphoreGive(s_lock);
    ESP_LOGI(TAG, "radio backend installed");
    return ESP_OK;
}

esp_err_t radio_hal_apply_jp_safe_profile(const radio_hal_lora_profile_t *profile) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (!radio_hal_profile_is_jp_safe(profile)) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    s_current_profile = *profile;
    s_active_profile = NULL;
    {
        const radio_hal_backend_t backend = s_backend;
        xSemaphoreGive(s_lock);
        if (backend.apply_profile != NULL) {
            ESP_RETURN_ON_ERROR(backend.apply_profile(profile, backend.context), TAG, "backend apply profile failed");
        }
        xSemaphoreTake(s_lock, portMAX_DELAY);
    }
    {
        size_t index;
        for (index = 0; index < sizeof(s_jp_profiles) / sizeof(s_jp_profiles[0]); ++index) {
            const radio_hal_profile_entry_t *entry = &s_jp_profiles[index];
            if (entry->params.frequency_hz == profile->frequency_hz &&
                entry->params.spreading_factor == profile->spreading_factor &&
                entry->params.bandwidth_khz == profile->bandwidth_khz &&
                entry->params.tx_power_dbm == profile->tx_power_dbm) {
                s_active_profile = entry;
                break;
            }
        }
    }
    ESP_LOGI(
        TAG,
        "applied JP-safe profile: %s freq=%lu sf=%u bw=%u tx=%u",
        s_active_profile != NULL ? s_active_profile->name : "unknown",
        (unsigned long)profile->frequency_hz,
        (unsigned)profile->spreading_factor,
        (unsigned)profile->bandwidth_khz,
        (unsigned)profile->tx_power_dbm);
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

esp_err_t radio_hal_send_frame(const uint8_t *payload, size_t payload_len) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (payload == NULL || payload_len == 0u || payload_len > sizeof(s_last_tx_frame.payload)) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (s_active_profile != NULL && payload_len > s_active_profile->total_payload_cap) {
        xSemaphoreGive(s_lock);
        return ESP_ERR_INVALID_SIZE;
    }
    {
        const radio_hal_backend_t backend = s_backend;
        xSemaphoreGive(s_lock);
        if (backend.tx == NULL) {
            ESP_LOGE(TAG, "radio delivery path is not configured");
            return ESP_ERR_INVALID_STATE;
        }
        ESP_RETURN_ON_ERROR(backend.tx(payload, payload_len, backend.context), TAG, "backend tx failed");
        xSemaphoreTake(s_lock, portMAX_DELAY);
    }
    memcpy(s_last_tx_frame.payload, payload, payload_len);
    s_last_tx_frame.payload_len = payload_len;
    s_last_tx_frame.rssi_dbm = -60;
    s_last_tx_frame.snr_db = 8;
    xSemaphoreGive(s_lock);
    ESP_LOGI(TAG, "radio TX handoff accepted: %u bytes", (unsigned)payload_len);
    return ESP_OK;
}

esp_err_t radio_hal_receive_frame(radio_hal_frame_t *frame, uint32_t timeout_ms) {
    radio_hal_queue_item_t item;
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (xQueueReceive(s_rx_queue, &item, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    *frame = item.frame;
    return ESP_OK;
}

esp_err_t radio_hal_inject_rx_frame(const uint8_t *payload, size_t payload_len, int16_t rssi_dbm, int8_t snr_db) {
    radio_hal_queue_item_t item = {0};
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (payload == NULL || payload_len == 0u || payload_len > sizeof(item.frame.payload)) {
        return ESP_ERR_INVALID_ARG;
    }
    memcpy(item.frame.payload, payload, payload_len);
    item.frame.payload_len = payload_len;
    item.frame.rssi_dbm = rssi_dbm;
    item.frame.snr_db = snr_db;
    if (xQueueSend(s_rx_queue, &item, 0) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    return ESP_OK;
}

esp_err_t radio_hal_get_last_tx_frame(radio_hal_frame_t *frame) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (s_last_tx_frame.payload_len == 0u) {
        xSemaphoreGive(s_lock);
        return ESP_ERR_NOT_FOUND;
    }
    *frame = s_last_tx_frame;
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

esp_err_t radio_hal_get_active_profile(radio_hal_profile_entry_t *profile) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (profile == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (s_active_profile == NULL) {
        xSemaphoreGive(s_lock);
        return ESP_ERR_NOT_FOUND;
    }
    *profile = *s_active_profile;
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

esp_err_t radio_hal_service(void) {
    radio_hal_backend_t backend;
    radio_hal_queue_item_t item = {0};
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    backend = s_backend;
    xSemaphoreGive(s_lock);
    if (backend.poll_rx == NULL) {
        return ESP_ERR_NOT_SUPPORTED;
    }
    for (;;) {
        const esp_err_t err = backend.poll_rx(&item.frame, backend.context);
        if (err == ESP_ERR_NOT_FOUND || err == ESP_ERR_TIMEOUT) {
            return ESP_OK;
        }
        ESP_RETURN_ON_ERROR(err, TAG, "backend poll rx failed");
        if (item.frame.payload_len == 0u) {
            return ESP_OK;
        }
        if (xQueueSend(s_rx_queue, &item, 0) != pdTRUE) {
            return ESP_ERR_TIMEOUT;
        }
    }
}

bool radio_hal_has_delivery_path(void) {
    bool ready;
    if (!s_initialized) {
        return false;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    ready = s_backend.tx != NULL;
    xSemaphoreGive(s_lock);
    return ready;
}

bool radio_hal_has_poll_path(void) {
    bool ready;
    if (!s_initialized) {
        return false;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    ready = s_backend.poll_rx != NULL;
    xSemaphoreGive(s_lock);
    return ready;
}

const char *radio_hal_backend_name(void) {
    const char *name;
    if (!s_initialized) {
        return "uninitialized";
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (s_backend.tx != NULL || s_backend.poll_rx != NULL || s_backend.apply_profile != NULL) {
        name = s_backend.name != NULL ? s_backend.name : "custom";
    } else {
        name = "unconfigured";
    }
    xSemaphoreGive(s_lock);
    return name;
}

bool radio_hal_backend_is_development_only(void) {
    bool development_only;
    if (!s_initialized) {
        return false;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    development_only = s_backend.development_only;
    xSemaphoreGive(s_lock);
    return development_only;
}

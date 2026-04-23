#include "usb_link.h"

#include <stdbool.h>
#include <string.h>

#include "esp_check.h"
#include "fabric_proto/fabric_proto.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "esp_log.h"

typedef struct {
    size_t length;
    uint8_t frame[512];
} usb_link_frame_t;

static const char *TAG = "usb_link";

static QueueHandle_t s_rx_queue;
static SemaphoreHandle_t s_lock;
static ef_usb_parser_t s_parser;
static usb_link_frame_t s_last_tx_frame;
static size_t s_max_frame_len = 512u;
static usb_link_tx_sink_t s_tx_sink;
static void *s_tx_sink_context;
static usb_link_backend_t s_backend;
static bool s_warned_missing_sink;
static bool s_initialized;

static esp_err_t usb_link_queue_received_frame(const uint8_t *frame, size_t frame_len);

esp_err_t usb_link_init(const usb_link_config_t *config) {
    const size_t rx_queue_depth = config != NULL && config->rx_queue_depth > 0u ? config->rx_queue_depth : 4u;
    const size_t max_frame_len = config != NULL && config->max_frame_len > 0u ? config->max_frame_len : sizeof(((usb_link_frame_t *)0)->frame);
    if (s_initialized) {
        return ESP_OK;
    }
    if (max_frame_len > sizeof(((usb_link_frame_t *)0)->frame)) {
        return ESP_ERR_INVALID_SIZE;
    }
    s_rx_queue = xQueueCreate((UBaseType_t)rx_queue_depth, sizeof(usb_link_frame_t));
    s_lock = xSemaphoreCreateMutex();
    if (s_rx_queue == NULL || s_lock == NULL) {
        return ESP_ERR_NO_MEM;
    }
    ESP_ERROR_CHECK(ef_usb_parser_reset(&s_parser));
    memset(&s_last_tx_frame, 0, sizeof(s_last_tx_frame));
    s_max_frame_len = max_frame_len;
    s_tx_sink = NULL;
    s_tx_sink_context = NULL;
    memset(&s_backend, 0, sizeof(s_backend));
    s_warned_missing_sink = false;
    s_initialized = true;
    ESP_LOGI(TAG, "USB link initialized with in-memory RX queue");
    return ESP_OK;
}

esp_err_t usb_link_install_backend(const usb_link_backend_t *backend) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (backend == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    s_backend = *backend;
    xSemaphoreGive(s_lock);
    ESP_LOGI(TAG, "USB backend installed");
    return ESP_OK;
}

esp_err_t usb_link_set_tx_sink(usb_link_tx_sink_t sink, void *context) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    s_tx_sink = sink;
    s_tx_sink_context = context;
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

esp_err_t usb_link_send_frame(const uint8_t *frame, size_t frame_len) {
    usb_link_frame_t item = {0};
    usb_link_tx_sink_t sink = NULL;
    void *sink_context = NULL;
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (frame == NULL || frame_len == 0u || frame_len > s_max_frame_len) {
        return ESP_ERR_INVALID_ARG;
    }
    memcpy(item.frame, frame, frame_len);
    item.length = frame_len;
    xSemaphoreTake(s_lock, portMAX_DELAY);
    sink = s_tx_sink;
    sink_context = s_tx_sink_context;
    {
        const usb_link_backend_t backend = s_backend;
        xSemaphoreGive(s_lock);
        if (backend.tx != NULL) {
            const esp_err_t backend_err = backend.tx(frame, frame_len, backend.context);
            if (backend_err != ESP_OK) {
                return backend_err;
            }
            xSemaphoreTake(s_lock, portMAX_DELAY);
            s_last_tx_frame = item;
            xSemaphoreGive(s_lock);
            ESP_LOGI(TAG, "USB TX handoff accepted via backend: %u bytes", (unsigned)frame_len);
            return ESP_OK;
        }
    }
    if (sink != NULL) {
        const esp_err_t sink_err = sink(frame, frame_len, sink_context);
        if (sink_err != ESP_OK) {
            return sink_err;
        }
        xSemaphoreTake(s_lock, portMAX_DELAY);
        s_last_tx_frame = item;
        xSemaphoreGive(s_lock);
        ESP_LOGI(TAG, "USB TX handoff accepted via tx sink: %u bytes", (unsigned)frame_len);
        return ESP_OK;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (!s_warned_missing_sink) {
        s_warned_missing_sink = true;
        xSemaphoreGive(s_lock);
        ESP_LOGE(TAG, "USB delivery path is not configured");
    } else {
        xSemaphoreGive(s_lock);
    }
    return ESP_ERR_INVALID_STATE;
}

esp_err_t usb_link_receive_frame(uint8_t *frame_buf, size_t frame_buf_cap, size_t *frame_len, uint32_t timeout_ms) {
    usb_link_frame_t item = {0};
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (frame_buf == NULL || frame_len == NULL || frame_buf_cap == 0u) {
        return ESP_ERR_INVALID_ARG;
    }
    if (xQueueReceive(s_rx_queue, &item, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if (item.length > frame_buf_cap) {
        return ESP_ERR_INVALID_SIZE;
    }
    memcpy(frame_buf, item.frame, item.length);
    *frame_len = item.length;
    return ESP_OK;
}

esp_err_t usb_link_inject_rx_bytes(const uint8_t *data, size_t data_len) {
    uint8_t frame_buf[sizeof(((usb_link_frame_t *)0)->frame)] = {0};
    size_t frame_len = 0u;
    bool frame_ready = false;
    size_t offset = 0u;
    esp_err_t err = ESP_OK;
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (data == NULL || data_len == 0u) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    while (offset < data_len) {
        err = ef_usb_parser_push(
            &s_parser,
            &data[offset],
            1u,
            frame_buf,
            s_max_frame_len,
            &frame_len,
            &frame_ready);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "parser error, resyncing");
            err = ef_usb_parser_reset(&s_parser);
            if (err != ESP_OK) {
                break;
            }
            offset++;
            continue;
        }
        if (frame_ready) {
            err = usb_link_queue_received_frame(frame_buf, frame_len);
            if (err != ESP_OK) {
                break;
            }
            frame_ready = false;
            frame_len = 0u;
        }
        offset++;
    }
    xSemaphoreGive(s_lock);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "USB RX inject failed: %s", esp_err_to_name(err));
        return err;
    }
    return ESP_OK;
}

esp_err_t usb_link_reset_parser(void) {
    esp_err_t err;
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    err = ef_usb_parser_reset(&s_parser);
    xSemaphoreGive(s_lock);
    return err;
}

esp_err_t usb_link_get_last_tx_frame(uint8_t *frame_buf, size_t frame_buf_cap, size_t *frame_len) {
    if (!s_initialized) {
        return ESP_ERR_INVALID_STATE;
    }
    if (frame_buf == NULL || frame_len == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (s_last_tx_frame.length == 0u) {
        *frame_len = 0u;
        xSemaphoreGive(s_lock);
        return ESP_ERR_NOT_FOUND;
    }
    if (s_last_tx_frame.length > frame_buf_cap) {
        xSemaphoreGive(s_lock);
        return ESP_ERR_INVALID_SIZE;
    }
    memcpy(frame_buf, s_last_tx_frame.frame, s_last_tx_frame.length);
    *frame_len = s_last_tx_frame.length;
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

esp_err_t usb_link_service(void) {
    uint8_t rx_buf[64];
    size_t received_len = 0u;
    usb_link_backend_t backend;
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
        const esp_err_t err = backend.poll_rx(rx_buf, sizeof(rx_buf), &received_len, backend.context);
        if (err == ESP_ERR_NOT_FOUND || err == ESP_ERR_TIMEOUT || received_len == 0u) {
            return ESP_OK;
        }
        ESP_RETURN_ON_ERROR(err, TAG, "backend poll_rx failed");
        ESP_RETURN_ON_ERROR(usb_link_inject_rx_bytes(rx_buf, received_len), TAG, "backend rx inject failed");
    }
}

bool usb_link_has_delivery_path(void) {
    bool ready;
    if (!s_initialized) {
        return false;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    ready = s_backend.tx != NULL || s_tx_sink != NULL;
    xSemaphoreGive(s_lock);
    return ready;
}

bool usb_link_has_poll_path(void) {
    bool ready;
    if (!s_initialized) {
        return false;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    ready = s_backend.poll_rx != NULL;
    xSemaphoreGive(s_lock);
    return ready;
}

const char *usb_link_backend_name(void) {
    const char *name;
    if (!s_initialized) {
        return "uninitialized";
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (s_backend.tx != NULL || s_backend.poll_rx != NULL) {
        name = s_backend.name != NULL ? s_backend.name : "custom";
    } else if (s_tx_sink != NULL) {
        name = "tx-sink";
    } else {
        name = "unconfigured";
    }
    xSemaphoreGive(s_lock);
    return name;
}

bool usb_link_backend_is_development_only(void) {
    bool development_only;
    if (!s_initialized) {
        return false;
    }
    xSemaphoreTake(s_lock, portMAX_DELAY);
    development_only = s_backend.development_only;
    xSemaphoreGive(s_lock);
    return development_only;
}

static esp_err_t usb_link_queue_received_frame(const uint8_t *frame, size_t frame_len) {
    usb_link_frame_t item = {0};
    if (frame == NULL || frame_len == 0u || frame_len > sizeof(item.frame)) {
        return ESP_ERR_INVALID_ARG;
    }
    memcpy(item.frame, frame, frame_len);
    item.length = frame_len;
    if (xQueueSend(s_rx_queue, &item, 0) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    return ESP_OK;
}

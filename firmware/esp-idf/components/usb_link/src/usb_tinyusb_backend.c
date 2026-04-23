#include "usb_tinyusb_backend.h"

#include <stdbool.h>
#include <string.h>

#include "esp_check.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "usb_link.h"

#if defined(__has_include)
#if __has_include("tinyusb.h") && __has_include("tinyusb_cdc_acm.h") && __has_include("tinyusb_default_config.h")
#define EDGE_FABRIC_HAS_TINYUSB 1
#include "tinyusb.h"
#include "tinyusb_cdc_acm.h"
#include "tinyusb_default_config.h"
#else
#define EDGE_FABRIC_HAS_TINYUSB 0
#endif
#else
#define EDGE_FABRIC_HAS_TINYUSB 0
#endif

static const char *TAG = "usb_tinyusb";

#if EDGE_FABRIC_HAS_TINYUSB

#ifndef CONFIG_TINYUSB_CDC_RX_BUFSIZE
#define CONFIG_TINYUSB_CDC_RX_BUFSIZE 64
#endif

typedef struct {
    size_t length;
    uint8_t data[64];
} usb_tinyusb_rx_item_t;

static QueueHandle_t s_rx_queue;
static volatile bool s_rx_queue_overflowed;
static bool s_tinyusb_driver_ready;
static bool s_backend_installed;
static portMUX_TYPE s_rx_overflow_lock = portMUX_INITIALIZER_UNLOCKED;

static void usb_tinyusb_rx_callback(int itf, cdcacm_event_t *event);
static void usb_tinyusb_line_state_callback(int itf, cdcacm_event_t *event);
static esp_err_t usb_tinyusb_backend_tx(const uint8_t *frame, size_t frame_len, void *context);
static esp_err_t usb_tinyusb_backend_poll_rx(uint8_t *buf, size_t buf_cap, size_t *received_len, void *context);
static bool usb_tinyusb_queue_rx_item(const usb_tinyusb_rx_item_t *item);
static void usb_tinyusb_mark_overflow(void);
static bool usb_tinyusb_consume_overflow(void);

static esp_err_t usb_tinyusb_backend_ensure_ready(void) {
    if (s_backend_installed) {
        return ESP_OK;
    }
    if (s_rx_queue == NULL) {
        s_rx_queue = xQueueCreate(16, sizeof(usb_tinyusb_rx_item_t));
        if (s_rx_queue == NULL) {
            return ESP_ERR_NO_MEM;
        }
    }
    if (!s_tinyusb_driver_ready) {
        const tinyusb_config_t tusb_cfg = TINYUSB_DEFAULT_CONFIG();
        ESP_RETURN_ON_ERROR(tinyusb_driver_install(&tusb_cfg), TAG, "tinyusb driver install failed");
        s_tinyusb_driver_ready = true;
    }
    {
        const tinyusb_config_cdcacm_t acm_cfg = {
            .usb_dev = TINYUSB_USBDEV_0,
            .cdc_port = TINYUSB_CDC_ACM_0,
            .rx_unread_buf_sz = CONFIG_TINYUSB_CDC_RX_BUFSIZE,
            .callback_rx = &usb_tinyusb_rx_callback,
            .callback_rx_wanted_char = NULL,
            .callback_line_state_changed = &usb_tinyusb_line_state_callback,
            .callback_line_coding_changed = NULL,
        };
        ESP_RETURN_ON_ERROR(tusb_cdc_acm_init(&acm_cfg), TAG, "tinyusb cdc acm init failed");
    }
    s_backend_installed = true;
    return ESP_OK;
}

esp_err_t usb_tinyusb_backend_install(void) {
    static const usb_link_backend_t backend = {
        .tx = usb_tinyusb_backend_tx,
        .poll_rx = usb_tinyusb_backend_poll_rx,
        .context = NULL,
        .name = "tinyusb-cdc-acm",
        .development_only = false,
    };
    ESP_RETURN_ON_ERROR(usb_tinyusb_backend_ensure_ready(), TAG, "tinyusb backend init failed");
    ESP_RETURN_ON_ERROR(usb_link_install_backend(&backend), TAG, "usb backend install failed");
    ESP_LOGI(TAG, "installed TinyUSB CDC backend");
    return ESP_OK;
}

static void usb_tinyusb_rx_callback(int itf, cdcacm_event_t *event) {
    uint8_t rx_buf[64];
    (void)event;
    while (itf == TINYUSB_CDC_ACM_0) {
        size_t rx_size = 0u;
        usb_tinyusb_rx_item_t item = {0};
        if (tinyusb_cdcacm_read(itf, rx_buf, sizeof(rx_buf), &rx_size) != ESP_OK || rx_size == 0u) {
            break;
        }
        memcpy(item.data, rx_buf, rx_size);
        item.length = rx_size;
        if (!usb_tinyusb_queue_rx_item(&item)) {
            usb_tinyusb_mark_overflow();
            ESP_LOGW(TAG, "dropping USB RX chunk because queue is full; parser will resync");
            break;
        }
        if (rx_size < sizeof(rx_buf)) {
            break;
        }
    }
}

static bool usb_tinyusb_queue_rx_item(const usb_tinyusb_rx_item_t *item) {
    if (item == NULL || s_rx_queue == NULL) {
        return false;
    }
    if (xPortInIsrContext()) {
        BaseType_t higher_priority_task_woken = pdFALSE;
        const BaseType_t ok = xQueueSendFromISR(s_rx_queue, item, &higher_priority_task_woken);
        if (higher_priority_task_woken == pdTRUE) {
            portYIELD_FROM_ISR();
        }
        return ok == pdTRUE;
    }
    return xQueueSend(s_rx_queue, item, 0) == pdTRUE;
}

static void usb_tinyusb_mark_overflow(void) {
    if (xPortInIsrContext()) {
        portENTER_CRITICAL_ISR(&s_rx_overflow_lock);
        s_rx_queue_overflowed = true;
        portEXIT_CRITICAL_ISR(&s_rx_overflow_lock);
        return;
    }
    portENTER_CRITICAL(&s_rx_overflow_lock);
    s_rx_queue_overflowed = true;
    portEXIT_CRITICAL(&s_rx_overflow_lock);
}

static bool usb_tinyusb_consume_overflow(void) {
    bool overflowed;
    portENTER_CRITICAL(&s_rx_overflow_lock);
    overflowed = s_rx_queue_overflowed;
    s_rx_queue_overflowed = false;
    portEXIT_CRITICAL(&s_rx_overflow_lock);
    return overflowed;
}

static void usb_tinyusb_line_state_callback(int itf, cdcacm_event_t *event) {
    ESP_LOGI(
        TAG,
        "line state changed port=%d dtr=%d rts=%d",
        itf,
        event->line_state_changed_data.dtr,
        event->line_state_changed_data.rts);
}

static esp_err_t usb_tinyusb_backend_tx(const uint8_t *frame, size_t frame_len, void *context) {
    (void)context;
    if (frame == NULL || frame_len == 0u) {
        return ESP_ERR_INVALID_ARG;
    }
    if (tinyusb_cdcacm_write_queue(TINYUSB_CDC_ACM_0, frame, frame_len) != frame_len) {
        return ESP_FAIL;
    }
    return tinyusb_cdcacm_write_flush(TINYUSB_CDC_ACM_0, 0);
}

static esp_err_t usb_tinyusb_backend_poll_rx(uint8_t *buf, size_t buf_cap, size_t *received_len, void *context) {
    usb_tinyusb_rx_item_t item = {0};
    (void)context;
    if (buf == NULL || buf_cap == 0u || received_len == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (usb_tinyusb_consume_overflow()) {
        usb_tinyusb_rx_item_t dropped = {0};
        ESP_RETURN_ON_ERROR(usb_link_reset_parser(), TAG, "parser reset after overflow failed");
        while (xQueueReceive(s_rx_queue, &dropped, 0) == pdTRUE) {
        }
        *received_len = 0u;
        return ESP_ERR_NOT_FOUND;
    }
    if (xQueueReceive(s_rx_queue, &item, 0) != pdTRUE) {
        *received_len = 0u;
        return ESP_ERR_NOT_FOUND;
    }
    if (buf_cap < item.length) {
        *received_len = 0u;
        return ESP_ERR_INVALID_SIZE;
    }
    memcpy(buf, item.data, item.length);
    *received_len = item.length;
    return ESP_OK;
}

#else

esp_err_t usb_tinyusb_backend_install(void) {
    ESP_LOGW(TAG, "TinyUSB headers are unavailable in this build environment");
    return ESP_ERR_NOT_SUPPORTED;
}

#endif

#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

typedef esp_err_t (*usb_link_tx_sink_t)(const uint8_t *frame, size_t frame_len, void *context);
typedef esp_err_t (*usb_link_backend_tx_fn)(const uint8_t *frame, size_t frame_len, void *context);
typedef esp_err_t (*usb_link_backend_poll_rx_fn)(uint8_t *buf, size_t buf_cap, size_t *received_len, void *context);

typedef struct {
    usb_link_backend_tx_fn tx;
    usb_link_backend_poll_rx_fn poll_rx;
    void *context;
    const char *name;
    bool development_only;
} usb_link_backend_t;

typedef struct {
    size_t max_frame_len;
    size_t rx_queue_depth;
} usb_link_config_t;

esp_err_t usb_link_init(const usb_link_config_t *config);
esp_err_t usb_link_install_backend(const usb_link_backend_t *backend);
esp_err_t usb_link_set_tx_sink(usb_link_tx_sink_t sink, void *context);
esp_err_t usb_link_send_frame(const uint8_t *frame, size_t frame_len);
esp_err_t usb_link_receive_frame(uint8_t *frame_buf, size_t frame_buf_cap, size_t *frame_len, uint32_t timeout_ms);
esp_err_t usb_link_inject_rx_bytes(const uint8_t *data, size_t data_len);
esp_err_t usb_link_reset_parser(void);
esp_err_t usb_link_get_last_tx_frame(uint8_t *frame_buf, size_t frame_buf_cap, size_t *frame_len);
esp_err_t usb_link_service(void);
bool usb_link_has_delivery_path(void);
bool usb_link_has_poll_path(void);
const char *usb_link_backend_name(void);
bool usb_link_backend_is_development_only(void);

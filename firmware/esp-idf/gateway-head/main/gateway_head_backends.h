#pragma once

#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

esp_err_t gateway_head_install_default_backends(void);
esp_err_t gateway_head_backend_reset_smoke(void);
esp_err_t gateway_head_backend_script_usb_frame(uint8_t frame_type, const uint8_t *payload, size_t payload_len);
esp_err_t gateway_head_backend_script_radio_frame(const uint8_t *payload, size_t payload_len, int16_t rssi_dbm, int8_t snr_db);
esp_err_t gateway_head_backend_get_last_usb_tx(uint8_t *frame, size_t frame_cap, size_t *frame_len);
esp_err_t gateway_head_backend_get_last_radio_tx(uint8_t *payload, size_t payload_cap, size_t *payload_len);

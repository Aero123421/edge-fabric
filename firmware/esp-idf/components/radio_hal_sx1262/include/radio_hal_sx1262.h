#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

typedef struct {
    uint32_t frequency_hz;
    uint8_t spreading_factor;
    uint16_t bandwidth_khz;
    uint8_t tx_power_dbm;
} radio_hal_lora_profile_t;

typedef struct {
    const char *name;
    radio_hal_lora_profile_t params;
    uint8_t total_payload_cap;
} radio_hal_profile_entry_t;

typedef struct {
    size_t payload_len;
    int16_t rssi_dbm;
    int8_t snr_db;
    uint8_t payload[255];
} radio_hal_frame_t;

typedef esp_err_t (*radio_hal_backend_apply_profile_fn)(const radio_hal_lora_profile_t *profile, void *context);
typedef esp_err_t (*radio_hal_backend_tx_fn)(const uint8_t *payload, size_t payload_len, void *context);
typedef esp_err_t (*radio_hal_backend_poll_rx_fn)(radio_hal_frame_t *frame, void *context);

typedef struct {
    radio_hal_backend_apply_profile_fn apply_profile;
    radio_hal_backend_tx_fn tx;
    radio_hal_backend_poll_rx_fn poll_rx;
    void *context;
    const char *name;
    bool development_only;
} radio_hal_backend_t;

esp_err_t radio_hal_init(void);
esp_err_t radio_hal_install_backend(const radio_hal_backend_t *backend);
esp_err_t radio_hal_apply_jp_safe_profile(const radio_hal_lora_profile_t *profile);
esp_err_t radio_hal_send_frame(const uint8_t *payload, size_t payload_len);
esp_err_t radio_hal_receive_frame(radio_hal_frame_t *frame, uint32_t timeout_ms);
esp_err_t radio_hal_inject_rx_frame(const uint8_t *payload, size_t payload_len, int16_t rssi_dbm, int8_t snr_db);
esp_err_t radio_hal_get_last_tx_frame(radio_hal_frame_t *frame);
esp_err_t radio_hal_get_active_profile(radio_hal_profile_entry_t *profile);
esp_err_t radio_hal_service(void);
bool radio_hal_is_cad_only_allowed(void);
bool radio_hal_has_delivery_path(void);
bool radio_hal_has_poll_path(void);
const char *radio_hal_backend_name(void);
bool radio_hal_backend_is_development_only(void);

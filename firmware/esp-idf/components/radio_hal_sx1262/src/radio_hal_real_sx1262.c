#include "radio_hal_real_sx1262.h"

#include <string.h>

#include "board_xiao_sx1262.h"
#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "radio_hal_sx1262.h"
#include "sx126x.h"
#include "sx126x_hal.h"

typedef struct {
    spi_host_device_t host_id;
    spi_device_handle_t spi_handle;
    board_xiao_sx1262_lora_pins_t pins;
    radio_hal_lora_profile_t active_profile;
    bool spi_bus_ready;
    bool profile_ready;
} sx1262_real_backend_context_t;

static const char *TAG = "sx1262_real";
static StaticSemaphore_t s_real_lock_storage;
static StaticSemaphore_t s_prepare_lock_storage;
static SemaphoreHandle_t s_real_lock;
static SemaphoreHandle_t s_prepare_lock;
static portMUX_TYPE s_lock_init_guard = portMUX_INITIALIZER_UNLOCKED;
static sx1262_real_backend_context_t s_context = {
    .host_id = SPI2_HOST,
};

static esp_err_t sx1262_real_backend_prepare(void);
static esp_err_t sx1262_real_backend_apply_profile(const radio_hal_lora_profile_t *profile, void *context);
static esp_err_t sx1262_real_backend_tx(const uint8_t *payload, size_t payload_len, void *context);
static esp_err_t sx1262_real_backend_poll_rx(radio_hal_frame_t *frame, void *context);
static esp_err_t sx1262_real_wait_while_busy(const sx1262_real_backend_context_t *context, uint32_t timeout_ms);
static esp_err_t sx1262_real_set_rx_continuous(sx1262_real_backend_context_t *context);
static esp_err_t sx1262_real_configure_lora_profile(sx1262_real_backend_context_t *context, const radio_hal_lora_profile_t *profile);
static esp_err_t sx1262_status_to_esp_err(sx126x_status_t status);
static sx126x_lora_sf_t sx1262_map_sf(uint8_t spreading_factor);
static sx126x_lora_bw_t sx1262_map_bw(uint16_t bandwidth_khz);
static bool sx1262_should_enable_ldro(const radio_hal_lora_profile_t *profile);
static esp_err_t sx1262_real_ensure_lock(void);
static esp_err_t sx1262_real_validate_pins(const sx1262_real_backend_context_t *context);

esp_err_t radio_hal_install_real_sx1262_backend(void) {
    static radio_hal_backend_t backend = {
        .apply_profile = sx1262_real_backend_apply_profile,
        .tx = sx1262_real_backend_tx,
        .poll_rx = sx1262_real_backend_poll_rx,
        .context = &s_context,
        .name = "sx1262-real-spi",
        .development_only = false,
    };
    ESP_RETURN_ON_ERROR(sx1262_real_backend_prepare(), TAG, "sx1262 real backend prepare failed");
    ESP_RETURN_ON_ERROR(radio_hal_install_backend(&backend), TAG, "radio backend install failed");
    ESP_LOGI(TAG, "installed SX1262 real backend");
    return ESP_OK;
}

static esp_err_t sx1262_real_backend_prepare(void) {
    spi_bus_config_t bus_cfg = {0};
    spi_device_interface_config_t device_cfg = {0};
    esp_err_t err = ESP_OK;
    bool bus_initialized = false;
    bool device_added = false;
    ESP_RETURN_ON_ERROR(sx1262_real_ensure_lock(), TAG, "real backend lock init failed");
    if (xSemaphoreTake(s_prepare_lock, portMAX_DELAY) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if (s_context.spi_bus_ready) {
        xSemaphoreGive(s_prepare_lock);
        return ESP_OK;
    }
    err = board_xiao_sx1262_init();
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "board init failed: %s", esp_err_to_name(err));
        goto fail;
    }
    board_xiao_sx1262_get_lora_pins(&s_context.pins);
    err = sx1262_real_validate_pins(&s_context);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "invalid board pin mapping: %s", esp_err_to_name(err));
        goto fail;
    }
    bus_cfg.sclk_io_num = s_context.pins.spi_sck;
    bus_cfg.mosi_io_num = s_context.pins.spi_mosi;
    bus_cfg.miso_io_num = s_context.pins.spi_miso;
    bus_cfg.quadwp_io_num = -1;
    bus_cfg.quadhd_io_num = -1;
    bus_cfg.max_transfer_sz = sizeof(radio_hal_frame_t) + 32;
    device_cfg.clock_speed_hz = 8000000;
    device_cfg.mode = 0;
    device_cfg.spics_io_num = s_context.pins.spi_nss;
    device_cfg.queue_size = 4;
    err = spi_bus_initialize(s_context.host_id, &bus_cfg, SPI_DMA_CH_AUTO);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "spi bus init failed: %s", esp_err_to_name(err));
        goto fail;
    }
    bus_initialized = true;
    err = spi_bus_add_device(s_context.host_id, &device_cfg, &s_context.spi_handle);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "spi device add failed: %s", esp_err_to_name(err));
        goto fail;
    }
    device_added = true;
    err = sx1262_status_to_esp_err(sx126x_hal_reset(&s_context));
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "sx1262 reset failed: %s", esp_err_to_name(err));
        goto fail;
    }
    s_context.spi_bus_ready = true;
    xSemaphoreGive(s_prepare_lock);
    return ESP_OK;
fail:
    if (device_added) {
        spi_bus_remove_device(s_context.spi_handle);
        s_context.spi_handle = NULL;
    }
    if (bus_initialized) {
        spi_bus_free(s_context.host_id);
    }
    s_context.profile_ready = false;
    s_context.spi_bus_ready = false;
    xSemaphoreGive(s_prepare_lock);
    return err;
}

static esp_err_t sx1262_real_backend_apply_profile(const radio_hal_lora_profile_t *profile, void *context) {
    sx1262_real_backend_context_t *backend = context;
    esp_err_t err = ESP_OK;
    if (backend == NULL || profile == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ESP_RETURN_ON_ERROR(sx1262_real_ensure_lock(), TAG, "real backend lock init failed");
    if (xSemaphoreTake(s_real_lock, portMAX_DELAY) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    err = sx1262_real_backend_prepare();
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "sx1262 prepare failed: %s", esp_err_to_name(err));
        goto unlock;
    }
    err = sx1262_real_configure_lora_profile(backend, profile);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "configure profile failed: %s", esp_err_to_name(err));
        goto unlock;
    }
    backend->active_profile = *profile;
    backend->profile_ready = true;
    err = sx1262_real_set_rx_continuous(backend);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "set rx continuous failed: %s", esp_err_to_name(err));
    }
unlock:
    xSemaphoreGive(s_real_lock);
    return err;
}

static esp_err_t sx1262_real_backend_tx(const uint8_t *payload, size_t payload_len, void *context) {
    sx1262_real_backend_context_t *backend = context;
    sx126x_pkt_params_lora_t pkt_params = {0};
    esp_err_t err = ESP_OK;
    if (backend == NULL || payload == NULL || payload_len == 0u || payload_len > 255u) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!backend->profile_ready) {
        return ESP_ERR_INVALID_STATE;
    }
    ESP_RETURN_ON_ERROR(sx1262_real_ensure_lock(), TAG, "real backend lock init failed");
    if (xSemaphoreTake(s_real_lock, portMAX_DELAY) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    pkt_params.preamble_len_in_symb = 8u;
    pkt_params.header_type = SX126X_LORA_PKT_EXPLICIT;
    pkt_params.pld_len_in_bytes = (uint8_t)payload_len;
    pkt_params.crc_is_on = true;
    pkt_params.invert_iq_is_on = false;
    err = sx1262_status_to_esp_err(sx126x_clear_irq_status(backend, SX126X_IRQ_ALL));
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "clear irq failed: %s", esp_err_to_name(err));
        goto unlock;
    }
    err = sx1262_status_to_esp_err(sx126x_set_lora_pkt_params(backend, &pkt_params));
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "set pkt params failed: %s", esp_err_to_name(err));
        goto unlock;
    }
    err = sx1262_status_to_esp_err(sx126x_write_buffer(backend, 0u, payload, (uint8_t)payload_len));
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "write buffer failed: %s", esp_err_to_name(err));
        goto unlock;
    }
    err = sx1262_status_to_esp_err(sx126x_set_tx(backend, 5000u));
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "set tx failed: %s", esp_err_to_name(err));
    }
unlock:
    xSemaphoreGive(s_real_lock);
    return err;
}

static esp_err_t sx1262_real_backend_poll_rx(radio_hal_frame_t *frame, void *context) {
    sx1262_real_backend_context_t *backend = context;
    sx126x_irq_mask_t irq = SX126X_IRQ_NONE;
    esp_err_t err = ESP_OK;
    if (backend == NULL || frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!backend->profile_ready) {
        return ESP_ERR_INVALID_STATE;
    }
    ESP_RETURN_ON_ERROR(sx1262_real_ensure_lock(), TAG, "real backend lock init failed");
    if (xSemaphoreTake(s_real_lock, portMAX_DELAY) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    err = sx1262_status_to_esp_err(sx126x_get_and_clear_irq_status(backend, &irq));
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "get irq failed: %s", esp_err_to_name(err));
        goto unlock;
    }
    if (irq & (SX126X_IRQ_HEADER_ERROR | SX126X_IRQ_CRC_ERROR | SX126X_IRQ_TIMEOUT)) {
        err = sx1262_real_set_rx_continuous(backend);
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "rearm rx after error failed: %s", esp_err_to_name(err));
            goto unlock;
        }
        err = ESP_ERR_NOT_FOUND;
        goto unlock;
    }
    if (irq & SX126X_IRQ_TX_DONE) {
        err = sx1262_real_set_rx_continuous(backend);
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "rearm rx after tx failed: %s", esp_err_to_name(err));
            goto unlock;
        }
        err = ESP_ERR_NOT_FOUND;
        goto unlock;
    }
    if (irq & SX126X_IRQ_RX_DONE) {
        sx126x_rx_buffer_status_t rx_status = {0};
        sx126x_pkt_status_lora_t pkt_status = {0};
        memset(frame, 0, sizeof(*frame));
        err = sx1262_status_to_esp_err(sx126x_get_rx_buffer_status(backend, &rx_status));
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "get rx buffer status failed: %s", esp_err_to_name(err));
            goto unlock;
        }
        if (rx_status.pld_len_in_bytes > sizeof(frame->payload)) {
            err = sx1262_real_set_rx_continuous(backend);
            if (err != ESP_OK) {
                ESP_LOGE(TAG, "rearm rx after oversize failed: %s", esp_err_to_name(err));
                goto unlock;
            }
            err = ESP_ERR_INVALID_SIZE;
            goto unlock;
        }
        err = sx1262_status_to_esp_err(
            sx126x_read_buffer(backend, rx_status.buffer_start_pointer, frame->payload, rx_status.pld_len_in_bytes));
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "read rx buffer failed: %s", esp_err_to_name(err));
            goto unlock;
        }
        err = sx1262_status_to_esp_err(sx126x_get_lora_pkt_status(backend, &pkt_status));
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "get pkt status failed: %s", esp_err_to_name(err));
            goto unlock;
        }
        frame->payload_len = rx_status.pld_len_in_bytes;
        frame->rssi_dbm = pkt_status.rssi_pkt_in_dbm;
        frame->snr_db = pkt_status.snr_pkt_in_db;
        err = sx1262_real_set_rx_continuous(backend);
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "rearm rx after packet failed: %s", esp_err_to_name(err));
            goto unlock;
        }
        err = ESP_OK;
        goto unlock;
    }
    err = ESP_ERR_NOT_FOUND;
unlock:
    xSemaphoreGive(s_real_lock);
    return err;
}

static esp_err_t sx1262_real_wait_while_busy(const sx1262_real_backend_context_t *context, uint32_t timeout_ms) {
    if (context == NULL || !GPIO_IS_VALID_GPIO(context->pins.lora_busy)) {
        return ESP_ERR_INVALID_STATE;
    }
    const int64_t deadline = esp_timer_get_time() + ((int64_t)timeout_ms * 1000);
    while (gpio_get_level(context->pins.lora_busy) != 0) {
        if (esp_timer_get_time() > deadline) {
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1u));
    }
    return ESP_OK;
}

static esp_err_t sx1262_real_set_rx_continuous(sx1262_real_backend_context_t *context) {
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_clear_irq_status(context, SX126X_IRQ_ALL)), TAG, "clear irq before rx failed");
    return sx1262_status_to_esp_err(sx126x_set_rx_with_timeout_in_rtc_step(context, SX126X_RX_CONTINUOUS));
}

static esp_err_t sx1262_real_configure_lora_profile(sx1262_real_backend_context_t *context, const radio_hal_lora_profile_t *profile) {
    sx126x_mod_params_lora_t mod_params = {0};
    sx126x_pkt_params_lora_t pkt_params = {0};
    const sx126x_pa_cfg_params_t pa_cfg = {
        .pa_duty_cycle = 0x04,
        .hp_max = 0x07,
        .device_sel = 0x00,
        .pa_lut = 0x01,
    };
    const sx126x_irq_mask_t irq_mask =
        SX126X_IRQ_TX_DONE | SX126X_IRQ_RX_DONE | SX126X_IRQ_HEADER_ERROR | SX126X_IRQ_CRC_ERROR | SX126X_IRQ_TIMEOUT;
    mod_params.sf = sx1262_map_sf(profile->spreading_factor);
    mod_params.bw = sx1262_map_bw(profile->bandwidth_khz);
    mod_params.cr = SX126X_LORA_CR_4_5;
    mod_params.ldro = sx1262_should_enable_ldro(profile) ? 1u : 0u;
    pkt_params.preamble_len_in_symb = 8u;
    pkt_params.header_type = SX126X_LORA_PKT_EXPLICIT;
    pkt_params.pld_len_in_bytes = 0xFFu;
    pkt_params.crc_is_on = true;
    pkt_params.invert_iq_is_on = false;
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_standby(context, SX126X_STANDBY_CFG_RC)), TAG, "standby failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_reg_mode(context, SX126X_REG_MODE_DCDC)), TAG, "reg mode failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_pkt_type(context, SX126X_PKT_TYPE_LORA)), TAG, "set pkt type failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_buffer_base_address(context, 0u, 0u)), TAG, "set buffer base failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_rf_freq(context, profile->frequency_hz)), TAG, "set rf freq failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_pa_cfg(context, &pa_cfg)), TAG, "set pa cfg failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_tx_params(context, (int8_t)profile->tx_power_dbm, SX126X_RAMP_200_US)), TAG, "set tx params failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_lora_mod_params(context, &mod_params)), TAG, "set lora mod params failed");
    ESP_RETURN_ON_ERROR(sx1262_status_to_esp_err(sx126x_set_lora_pkt_params(context, &pkt_params)), TAG, "set lora pkt params failed");
    ESP_RETURN_ON_ERROR(
        sx1262_status_to_esp_err(sx126x_set_dio_irq_params(context, irq_mask, irq_mask, SX126X_IRQ_NONE, SX126X_IRQ_NONE)),
        TAG,
        "set dio irq params failed");
    return ESP_OK;
}

static esp_err_t sx1262_status_to_esp_err(sx126x_status_t status) {
    switch (status) {
        case SX126X_STATUS_OK:
            return ESP_OK;
        case SX126X_STATUS_UNSUPPORTED_FEATURE:
            return ESP_ERR_NOT_SUPPORTED;
        case SX126X_STATUS_UNKNOWN_VALUE:
            return ESP_ERR_INVALID_ARG;
        case SX126X_STATUS_ERROR:
        default:
            return ESP_FAIL;
    }
}

static sx126x_lora_sf_t sx1262_map_sf(uint8_t spreading_factor) {
    switch (spreading_factor) {
        case 8u:
            return SX126X_LORA_SF8;
        case 9u:
            return SX126X_LORA_SF9;
        case 10u:
            return SX126X_LORA_SF10;
        case 11u:
            return SX126X_LORA_SF11;
        case 12u:
            return SX126X_LORA_SF12;
        case 7u:
        default:
            return SX126X_LORA_SF7;
    }
}

static sx126x_lora_bw_t sx1262_map_bw(uint16_t bandwidth_khz) {
    switch (bandwidth_khz) {
        case 250u:
            return SX126X_LORA_BW_250;
        case 125u:
        default:
            return SX126X_LORA_BW_125;
    }
}

static bool sx1262_should_enable_ldro(const radio_hal_lora_profile_t *profile) {
    if (profile == NULL) {
        return false;
    }
    if (profile->bandwidth_khz == 125u && profile->spreading_factor >= 11u) {
        return true;
    }
    return profile->bandwidth_khz == 250u && profile->spreading_factor >= 12u;
}

static esp_err_t sx1262_real_ensure_lock(void) {
    portENTER_CRITICAL(&s_lock_init_guard);
    if (s_real_lock == NULL) {
        s_real_lock = xSemaphoreCreateMutexStatic(&s_real_lock_storage);
    }
    if (s_prepare_lock == NULL) {
        s_prepare_lock = xSemaphoreCreateMutexStatic(&s_prepare_lock_storage);
    }
    portEXIT_CRITICAL(&s_lock_init_guard);
    if (s_real_lock == NULL || s_prepare_lock == NULL) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

static esp_err_t sx1262_real_validate_pins(const sx1262_real_backend_context_t *context) {
    if (context == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!GPIO_IS_VALID_GPIO(context->pins.spi_sck) ||
        !GPIO_IS_VALID_GPIO(context->pins.spi_mosi) ||
        !GPIO_IS_VALID_GPIO(context->pins.spi_miso) ||
        !GPIO_IS_VALID_GPIO(context->pins.spi_nss) ||
        !GPIO_IS_VALID_GPIO(context->pins.lora_busy) ||
        !GPIO_IS_VALID_GPIO(context->pins.lora_reset) ||
        !GPIO_IS_VALID_GPIO(context->pins.lora_dio1)) {
        return ESP_ERR_INVALID_ARG;
    }
    return ESP_OK;
}

sx126x_hal_status_t sx126x_hal_write(
    const void *context,
    const uint8_t *command,
    const uint16_t command_length,
    const uint8_t *data,
    const uint16_t data_length) {
    const sx1262_real_backend_context_t *backend = context;
    spi_transaction_t transaction = {0};
    uint8_t buffer[272];
    if (backend == NULL || backend->spi_handle == NULL || command == NULL || command_length == 0u) {
        return SX126X_HAL_STATUS_ERROR;
    }
    if ((size_t)command_length + (size_t)data_length > sizeof(buffer)) {
        return SX126X_HAL_STATUS_ERROR;
    }
    if (sx1262_real_wait_while_busy(backend, 250u) != ESP_OK) {
        return SX126X_HAL_STATUS_ERROR;
    }
    memcpy(buffer, command, command_length);
    if (data != NULL && data_length > 0u) {
        memcpy(buffer + command_length, data, data_length);
    }
    transaction.length = (command_length + data_length) * 8u;
    transaction.tx_buffer = buffer;
    if (spi_device_polling_transmit(backend->spi_handle, &transaction) != ESP_OK) {
        return SX126X_HAL_STATUS_ERROR;
    }
    return sx1262_real_wait_while_busy(backend, 250u) == ESP_OK ? SX126X_HAL_STATUS_OK : SX126X_HAL_STATUS_ERROR;
}

sx126x_hal_status_t sx126x_hal_read(
    const void *context,
    const uint8_t *command,
    const uint16_t command_length,
    uint8_t *data,
    const uint16_t data_length) {
    const sx1262_real_backend_context_t *backend = context;
    spi_transaction_t transaction = {0};
    uint8_t tx_buffer[272];
    uint8_t rx_buffer[272];
    const size_t total_length = (size_t)command_length + (size_t)data_length;
    if (backend == NULL || backend->spi_handle == NULL || command == NULL || command_length == 0u || data == NULL) {
        return SX126X_HAL_STATUS_ERROR;
    }
    if (total_length > sizeof(tx_buffer)) {
        return SX126X_HAL_STATUS_ERROR;
    }
    if (sx1262_real_wait_while_busy(backend, 250u) != ESP_OK) {
        return SX126X_HAL_STATUS_ERROR;
    }
    memset(tx_buffer, SX126X_NOP, sizeof(tx_buffer));
    memset(rx_buffer, 0, sizeof(rx_buffer));
    memcpy(tx_buffer, command, command_length);
    transaction.length = total_length * 8u;
    transaction.tx_buffer = tx_buffer;
    transaction.rx_buffer = rx_buffer;
    if (spi_device_polling_transmit(backend->spi_handle, &transaction) != ESP_OK) {
        return SX126X_HAL_STATUS_ERROR;
    }
    memcpy(data, rx_buffer + command_length, data_length);
    return sx1262_real_wait_while_busy(backend, 250u) == ESP_OK ? SX126X_HAL_STATUS_OK : SX126X_HAL_STATUS_ERROR;
}

sx126x_hal_status_t sx126x_hal_reset(const void *context) {
    (void)context;
    return board_xiao_sx1262_reset_lora() == ESP_OK ? SX126X_HAL_STATUS_OK : SX126X_HAL_STATUS_ERROR;
}

sx126x_hal_status_t sx126x_hal_wakeup(const void *context) {
    static const uint8_t wakeup_command[] = {0xC0, SX126X_NOP};
    return sx126x_hal_write(context, wakeup_command, sizeof(wakeup_command), NULL, 0u);
}

#include "board_xiao_sx1262.h"

#include "driver/gpio.h"
#include "esp_check.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

esp_err_t board_xiao_sx1262_init(void) {
    const gpio_config_t output_config = {
        .pin_bit_mask = (1ULL << BOARD_LORA_RST) | (1ULL << BOARD_LORA_RF_SW1),
        .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    const gpio_config_t input_config = {
        .pin_bit_mask = (1ULL << BOARD_LORA_DIO1) | (1ULL << BOARD_LORA_BUSY) | (1ULL << BOARD_USER_BUTTON),
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    ESP_RETURN_ON_ERROR(gpio_config(&output_config), "board_xiao", "output gpio init failed");
    ESP_RETURN_ON_ERROR(gpio_config(&input_config), "board_xiao", "input gpio init failed");
    ESP_RETURN_ON_ERROR(gpio_set_level(BOARD_LORA_RF_SW1, 1), "board_xiao", "rf switch init failed");
    return gpio_set_level(BOARD_LORA_RST, 1);
}

esp_err_t board_xiao_sx1262_reset_lora(void) {
    ESP_RETURN_ON_ERROR(gpio_set_level(BOARD_LORA_RST, 0), "board_xiao", "lora reset low failed");
    vTaskDelay(pdMS_TO_TICKS(10u));
    ESP_RETURN_ON_ERROR(gpio_set_level(BOARD_LORA_RST, 1), "board_xiao", "lora reset high failed");
    vTaskDelay(pdMS_TO_TICKS(10u));
    return ESP_OK;
}

void board_xiao_sx1262_get_lora_pins(board_xiao_sx1262_lora_pins_t *pins) {
    if (pins == NULL) {
        return;
    }
    pins->spi_sck = BOARD_LORA_SPI_SCK;
    pins->spi_miso = BOARD_LORA_SPI_MISO;
    pins->spi_mosi = BOARD_LORA_SPI_MOSI;
    pins->spi_nss = BOARD_LORA_SPI_NSS;
    pins->lora_reset = BOARD_LORA_RST;
    pins->lora_dio1 = BOARD_LORA_DIO1;
    pins->lora_busy = BOARD_LORA_BUSY;
    pins->lora_rf_sw1 = BOARD_LORA_RF_SW1;
    pins->user_button = BOARD_USER_BUTTON;
}

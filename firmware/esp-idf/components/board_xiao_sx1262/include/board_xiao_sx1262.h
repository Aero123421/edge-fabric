#pragma once

#include "driver/gpio.h"
#include "esp_err.h"

#define BOARD_LORA_SPI_SCK GPIO_NUM_7
#define BOARD_LORA_SPI_MISO GPIO_NUM_8
#define BOARD_LORA_SPI_MOSI GPIO_NUM_9
#define BOARD_LORA_SPI_NSS GPIO_NUM_41
#define BOARD_LORA_RST GPIO_NUM_42
#define BOARD_LORA_DIO1 GPIO_NUM_39
#define BOARD_LORA_BUSY GPIO_NUM_40
#define BOARD_LORA_RF_SW1 GPIO_NUM_38
#define BOARD_USER_BUTTON GPIO_NUM_21

typedef struct {
    gpio_num_t spi_sck;
    gpio_num_t spi_miso;
    gpio_num_t spi_mosi;
    gpio_num_t spi_nss;
    gpio_num_t lora_reset;
    gpio_num_t lora_dio1;
    gpio_num_t lora_busy;
    gpio_num_t lora_rf_sw1;
    gpio_num_t user_button;
} board_xiao_sx1262_lora_pins_t;

esp_err_t board_xiao_sx1262_init(void);
esp_err_t board_xiao_sx1262_reset_lora(void);
void board_xiao_sx1262_get_lora_pins(board_xiao_sx1262_lora_pins_t *pins);

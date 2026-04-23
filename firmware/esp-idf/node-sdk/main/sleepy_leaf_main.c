#include "esp_log.h"
#include "esp_err.h"
#include "esp_sleep.h"

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "radio_hal_sx1262.h"
#include "sleepy_leaf_backends.h"
#include "sleepy_policy.h"

void app_main(void) {
    ESP_LOGI("sleepy_leaf", "starting sleepy leaf scaffold");
    ESP_ERROR_CHECK(radio_hal_init());
    ESP_ERROR_CHECK(sleepy_leaf_install_default_backends());
    ESP_ERROR_CHECK(sleepy_policy_apply_defaults());
    for (;;) {
        ESP_ERROR_CHECK(sleepy_policy_run_cycle());
        ESP_ERROR_CHECK(esp_sleep_enable_timer_wakeup(2000ULL * 1000ULL));
        ESP_LOGI("sleepy_leaf", "entering light sleep for 2000 ms");
        ESP_ERROR_CHECK(esp_light_sleep_start());
        vTaskDelay(pdMS_TO_TICKS(50u));
    }
}

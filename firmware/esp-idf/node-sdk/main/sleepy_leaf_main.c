#include "esp_log.h"
#include "esp_err.h"
#include "esp_sleep.h"
#include "sdkconfig.h"

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "radio_hal_sx1262.h"
#include "sleepy_leaf_backends.h"
#include "sleepy_policy.h"

#ifndef CONFIG_EDGE_FABRIC_SLEEPY_WAKE_INTERVAL_MS
#define CONFIG_EDGE_FABRIC_SLEEPY_WAKE_INTERVAL_MS 2000
#endif

#ifndef CONFIG_EDGE_FABRIC_SLEEPY_USE_DEEP_SLEEP
#define CONFIG_EDGE_FABRIC_SLEEPY_USE_DEEP_SLEEP 0
#endif

#ifndef CONFIG_EDGE_FABRIC_SLEEPY_ENABLE_RTC_PERSISTENCE
#define CONFIG_EDGE_FABRIC_SLEEPY_ENABLE_RTC_PERSISTENCE 0
#endif

static void sleepy_leaf_sleep_between_cycles(void);

void app_main(void) {
    ESP_LOGI("sleepy_leaf", "starting sleepy leaf scaffold");
    ESP_ERROR_CHECK(radio_hal_init());
    ESP_ERROR_CHECK(sleepy_leaf_install_default_backends());
    ESP_ERROR_CHECK(sleepy_policy_apply_defaults());
    for (;;) {
        ESP_ERROR_CHECK(sleepy_policy_run_cycle());
        sleepy_leaf_sleep_between_cycles();
    }
}

static void sleepy_leaf_sleep_between_cycles(void) {
    const uint64_t wake_interval_us = (uint64_t)CONFIG_EDGE_FABRIC_SLEEPY_WAKE_INTERVAL_MS * 1000ULL;
    ESP_ERROR_CHECK(sleepy_policy_publish_state("node.power", "sleep", false));
    ESP_ERROR_CHECK(esp_sleep_enable_timer_wakeup(wake_interval_us));
#if CONFIG_EDGE_FABRIC_SLEEPY_USE_DEEP_SLEEP
    ESP_LOGI(
        "sleepy_leaf",
        "entering deep sleep for %u ms rtc_persistence=%s recent_token_cache=%u",
        (unsigned)CONFIG_EDGE_FABRIC_SLEEPY_WAKE_INTERVAL_MS,
        CONFIG_EDGE_FABRIC_SLEEPY_ENABLE_RTC_PERSISTENCE ? "on" : "off",
        (unsigned)SLEEPY_RECENT_COMMAND_TOKEN_CACHE_SIZE);
    esp_deep_sleep_start();
#else
    ESP_LOGI(
        "sleepy_leaf",
        "entering light sleep for %u ms rtc_persistence=%s recent_token_cache=%u",
        (unsigned)CONFIG_EDGE_FABRIC_SLEEPY_WAKE_INTERVAL_MS,
        CONFIG_EDGE_FABRIC_SLEEPY_ENABLE_RTC_PERSISTENCE ? "on" : "off",
        (unsigned)SLEEPY_RECENT_COMMAND_TOKEN_CACHE_SIZE);
    ESP_ERROR_CHECK(esp_light_sleep_start());
    vTaskDelay(pdMS_TO_TICKS(50u));
#endif
}

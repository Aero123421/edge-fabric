#include "esp_log.h"
#include "esp_err.h"

#include "gateway_head_runtime.h"

void app_main(void) {
    ESP_LOGI("gateway_head", "starting gateway head scaffold");
#if defined(CONFIG_TINYUSB_CDC_ENABLED) && CONFIG_TINYUSB_CDC_ENABLED
    {
        const esp_err_t err = gateway_head_runtime_use_real_backends();
        if (err != ESP_OK) {
#if defined(CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS) && CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS
            ESP_LOGE(
                "gateway_head",
                "real backend path is required but unavailable: %s",
                esp_err_to_name(err));
            ESP_ERROR_CHECK(err);
#else
            ESP_LOGW(
                "gateway_head",
                "real backend path is unavailable (%s); falling back to development backends",
                esp_err_to_name(err));
            ESP_ERROR_CHECK(gateway_head_runtime_use_default_backends());
#endif
        }
    }
#else
#if defined(CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS) && CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS
    ESP_LOGE("gateway_head", "CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS requires CONFIG_TINYUSB_CDC_ENABLED");
    ESP_ERROR_CHECK(ESP_ERR_INVALID_STATE);
#else
    ESP_ERROR_CHECK(gateway_head_runtime_use_default_backends());
#endif
#endif
    ESP_ERROR_CHECK(gateway_head_runtime_start());
}

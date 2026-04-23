#include "esp_log.h"
#include "esp_err.h"

#include "gateway_head_runtime.h"

void app_main(void) {
    ESP_LOGI("gateway_head", "starting gateway head scaffold");
#if defined(CONFIG_TINYUSB_CDC_ENABLED) && CONFIG_TINYUSB_CDC_ENABLED
    {
        const esp_err_t err = gateway_head_runtime_use_real_backends();
        if (err != ESP_OK) {
            ESP_LOGW(
                "gateway_head",
                "real backend path is unavailable (%s); falling back to development backends",
                esp_err_to_name(err));
            ESP_ERROR_CHECK(gateway_head_runtime_use_default_backends());
        }
    }
#else
    ESP_ERROR_CHECK(gateway_head_runtime_use_default_backends());
#endif
    ESP_ERROR_CHECK(gateway_head_runtime_start());
}

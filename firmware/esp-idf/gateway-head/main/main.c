#include "esp_log.h"
#include "esp_err.h"

#include "gateway_head_runtime.h"

void app_main(void) {
    ESP_LOGI("gateway_head", "starting gateway head scaffold");
    ESP_ERROR_CHECK(gateway_head_runtime_use_default_backends());
    ESP_ERROR_CHECK(gateway_head_runtime_start());
}

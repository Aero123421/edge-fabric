#pragma once

#include "esp_err.h"

esp_err_t gateway_head_runtime_set_default_backends(bool enabled);
esp_err_t gateway_head_runtime_init_transport(void);
esp_err_t gateway_head_runtime_use_default_backends(void);
esp_err_t gateway_head_runtime_start(void);
esp_err_t gateway_head_runtime_poll_once(void);

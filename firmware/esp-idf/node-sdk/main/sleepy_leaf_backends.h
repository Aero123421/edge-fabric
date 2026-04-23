#pragma once

#include <stdbool.h>

#include "esp_err.h"

esp_err_t sleepy_leaf_install_default_backends(void);
esp_err_t sleepy_leaf_backend_reset_smoke(void);
esp_err_t sleepy_leaf_backend_set_auto_smoke(bool enabled);
esp_err_t sleepy_leaf_backend_script_pending_digest(int pending_count);
esp_err_t sleepy_leaf_backend_script_compact_command(const char *command_id, const char *mode, int expires_in_s);

#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

typedef struct {
    uint32_t rx_window_ms;
    uint32_t maintenance_window_ms;
    bool maintenance_awake;
    bool saw_pending_digest;
    bool applied_command;
    uint8_t maintenance_cycles_remaining;
    uint8_t maintenance_max_cycles;
    char last_command_id[48];
    char node_id[48];
    char recent_command_ids[4][48];
    uint8_t recent_command_cursor;
} sleepy_policy_state_t;

esp_err_t sleepy_policy_apply_defaults(void);
esp_err_t sleepy_policy_run_cycle(void);
esp_err_t sleepy_policy_publish_state(const char *state_key, const char *value, bool event_wake);
esp_err_t sleepy_policy_emit_event(const char *event_name, const char *value);
esp_err_t sleepy_policy_set_maintenance_awake(bool enabled);
esp_err_t sleepy_policy_get_state(sleepy_policy_state_t *state);
esp_err_t sleepy_policy_set_node_id(const char *node_id);
const char *sleepy_policy_get_node_id(void);

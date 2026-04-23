#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

#define EF_USB_MAGIC_0 'E'
#define EF_USB_MAGIC_1 'F'
#define EF_USB_VERSION 1

typedef enum {
    EF_USB_FRAME_ENVELOPE_JSON = 1,
    EF_USB_FRAME_HEARTBEAT_JSON = 2,
    EF_USB_FRAME_COMPACT_BINARY = 3,
    EF_USB_FRAME_SUMMARY_BINARY = 4,
} ef_usb_frame_type_t;

typedef struct {
    uint8_t version;
    uint8_t frame_type;
    uint16_t payload_length;
} ef_usb_frame_header_t;

typedef struct {
    bool synced;
    uint16_t expected_length;
    size_t buffered;
    uint8_t buffer[512];
} ef_usb_parser_t;

esp_err_t ef_usb_frame_validate(const uint8_t *frame, size_t frame_len);
esp_err_t ef_usb_frame_encode(
    uint8_t frame_type,
    const uint8_t *payload,
    size_t payload_len,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len);

esp_err_t ef_usb_parser_reset(ef_usb_parser_t *parser);
esp_err_t ef_usb_parser_push(
    ef_usb_parser_t *parser,
    const uint8_t *data,
    size_t data_len,
    uint8_t *frame_buf,
    size_t frame_buf_cap,
    size_t *frame_len,
    bool *frame_ready);

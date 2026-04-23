#include "usb_parser.h"

#include <string.h>

static uint16_t ef_expected_length(const ef_usb_parser_t *parser) {
    return (uint16_t)parser->buffer[4] | ((uint16_t)parser->buffer[5] << 8u);
}

esp_err_t ef_usb_parser_reset(ef_usb_parser_t *parser) {
    if (parser == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    memset(parser, 0, sizeof(*parser));
    return ESP_OK;
}

esp_err_t ef_usb_parser_push(
    ef_usb_parser_t *parser,
    const uint8_t *data,
    size_t data_len,
    uint8_t *frame_buf,
    size_t frame_buf_cap,
    size_t *frame_len,
    bool *frame_ready) {
    if (parser == NULL || data == NULL || frame_buf == NULL || frame_len == NULL || frame_ready == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    *frame_ready = false;
    *frame_len = 0;
    for (size_t i = 0; i < data_len; ++i) {
        if (!parser->synced) {
            if (parser->buffered == 0u && data[i] != EF_USB_MAGIC_0) {
                continue;
            }
            if (parser->buffered == 1u && data[i] != EF_USB_MAGIC_1) {
                parser->buffered = 0u;
                continue;
            }
        }
        if (parser->buffered >= sizeof(parser->buffer)) {
            ef_usb_parser_reset(parser);
            return ESP_ERR_INVALID_SIZE;
        }
        parser->buffer[parser->buffered++] = data[i];
        if (parser->buffered == 2u &&
            parser->buffer[0] == EF_USB_MAGIC_0 &&
            parser->buffer[1] == EF_USB_MAGIC_1) {
            parser->synced = true;
        }
        if (parser->synced && parser->buffered >= 6u) {
            parser->expected_length = ef_expected_length(parser);
            const size_t total = 6u + parser->expected_length + 4u;
            if (total > sizeof(parser->buffer) || total > frame_buf_cap) {
                ef_usb_parser_reset(parser);
                return ESP_ERR_INVALID_SIZE;
            }
            if (parser->buffered == total) {
                const esp_err_t validate_err = ef_usb_frame_validate(parser->buffer, total);
                if (validate_err != ESP_OK) {
                    ef_usb_parser_reset(parser);
                    return validate_err;
                }
                memcpy(frame_buf, parser->buffer, total);
                *frame_len = total;
                *frame_ready = true;
                ef_usb_parser_reset(parser);
                return ESP_OK;
            }
        }
    }
    return ESP_OK;
}

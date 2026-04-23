#include "fabric_proto/fabric_proto.h"

#include <string.h>

static uint32_t ef_crc32(const uint8_t *data, size_t len) {
    uint32_t crc = 0xFFFFFFFFu;
    for (size_t i = 0; i < len; ++i) {
        crc ^= data[i];
        for (int bit = 0; bit < 8; ++bit) {
            uint32_t mask = (uint32_t)(-(int32_t)(crc & 1u));
            crc = (crc >> 1u) ^ (0xEDB88320u & mask);
        }
    }
    return ~crc;
}

static bool ef_usb_frame_type_is_valid(uint8_t frame_type) {
    return frame_type == EF_USB_FRAME_ENVELOPE_JSON ||
           frame_type == EF_USB_FRAME_HEARTBEAT_JSON ||
           frame_type == EF_USB_FRAME_COMPACT_BINARY ||
           frame_type == EF_USB_FRAME_SUMMARY_BINARY;
}

esp_err_t ef_usb_frame_validate(const uint8_t *frame, size_t frame_len) {
    if (frame == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (frame_len < 10u) {
        return ESP_ERR_INVALID_SIZE;
    }
    if (frame[0] != EF_USB_MAGIC_0 || frame[1] != EF_USB_MAGIC_1) {
        return ESP_ERR_INVALID_RESPONSE;
    }
    if (frame[2] != EF_USB_VERSION) {
        return ESP_ERR_NOT_SUPPORTED;
    }
    if (!ef_usb_frame_type_is_valid(frame[3])) {
        return ESP_ERR_INVALID_ARG;
    }
    const uint16_t payload_len = (uint16_t)frame[4] | ((uint16_t)frame[5] << 8u);
    const size_t expected_len = 6u + payload_len + 4u;
    if (expected_len != frame_len) {
        return ESP_ERR_INVALID_SIZE;
    }
    const uint32_t actual_crc = ef_crc32(frame, 6u + payload_len);
    const uint32_t expected_crc =
        ((uint32_t)frame[6u + payload_len + 0u]) |
        ((uint32_t)frame[6u + payload_len + 1u] << 8u) |
        ((uint32_t)frame[6u + payload_len + 2u] << 16u) |
        ((uint32_t)frame[6u + payload_len + 3u] << 24u);
    if (actual_crc != expected_crc) {
        return ESP_ERR_INVALID_CRC;
    }
    return ESP_OK;
}

esp_err_t ef_usb_frame_encode(
    uint8_t frame_type,
    const uint8_t *payload,
    size_t payload_len,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    const size_t total_len = 6u + payload_len + 4u;
    if (payload == NULL || out_buf == NULL || out_len == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!ef_usb_frame_type_is_valid(frame_type)) {
        return ESP_ERR_INVALID_ARG;
    }
    if (payload_len > 0xFFFFu || total_len > out_buf_cap) {
        return ESP_ERR_INVALID_SIZE;
    }
    out_buf[0] = EF_USB_MAGIC_0;
    out_buf[1] = EF_USB_MAGIC_1;
    out_buf[2] = EF_USB_VERSION;
    out_buf[3] = frame_type;
    out_buf[4] = (uint8_t)(payload_len & 0xFFu);
    out_buf[5] = (uint8_t)((payload_len >> 8u) & 0xFFu);
    memcpy(&out_buf[6], payload, payload_len);
    const uint32_t crc = ef_crc32(out_buf, 6u + payload_len);
    out_buf[6u + payload_len + 0u] = (uint8_t)(crc & 0xFFu);
    out_buf[6u + payload_len + 1u] = (uint8_t)((crc >> 8u) & 0xFFu);
    out_buf[6u + payload_len + 2u] = (uint8_t)((crc >> 16u) & 0xFFu);
    out_buf[6u + payload_len + 3u] = (uint8_t)((crc >> 24u) & 0xFFu);
    *out_len = total_len;
    return ESP_OK;
}

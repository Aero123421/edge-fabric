#include "fabric_proto/fabric_proto.h"

#include <string.h>

static void ef_write_le16(uint8_t *dst, uint16_t value) {
    dst[0] = (uint8_t)(value & 0xffu);
    dst[1] = (uint8_t)((value >> 8u) & 0xffu);
}

static uint16_t ef_read_le16(const uint8_t *src) {
    return (uint16_t)src[0] | ((uint16_t)src[1] << 8u);
}

esp_err_t ef_onair_decode_packet(const uint8_t *frame, size_t frame_len, ef_onair_packet_t *out_packet) {
    size_t body_offset = EF_ONAIR_HEADER_SIZE;
    if (frame == NULL || out_packet == NULL || frame_len < (EF_ONAIR_HEADER_SIZE + 1u)) {
        return ESP_ERR_INVALID_ARG;
    }
    if (frame[0] != EF_ONAIR_VERSION) {
        return ESP_ERR_INVALID_VERSION;
    }
    out_packet->version = frame[0];
    out_packet->logical_type = frame[1];
    out_packet->flags = frame[2];
    out_packet->sequence = frame[3];
    out_packet->source_short_id = ef_read_le16(&frame[4]);
    out_packet->target_short_id = ef_read_le16(&frame[6]);
    out_packet->has_relay = false;
    if ((out_packet->flags & EF_ONAIR_FLAG_RELAY_EXTENSION) != 0u) {
        if (frame_len < (EF_ONAIR_HEADER_SIZE + EF_ONAIR_RELAY_EXTENSION_SIZE + 1u)) {
            return ESP_ERR_INVALID_ARG;
        }
        out_packet->has_relay = true;
        out_packet->relay.origin_short_id = ef_read_le16(&frame[8]);
        out_packet->relay.previous_hop_short_id = ef_read_le16(&frame[10]);
        out_packet->relay.ttl = frame[12];
        out_packet->relay.hop_count = frame[13];
        out_packet->relay.route_hint = frame[14];
        body_offset += EF_ONAIR_RELAY_EXTENSION_SIZE;
    }
    out_packet->body = &frame[body_offset];
    out_packet->body_len = frame_len - body_offset;
    return ESP_OK;
}

esp_err_t ef_onair_encode_packet(
    const ef_onair_packet_t *packet,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    size_t frame_len;
    if (packet == NULL || packet->body == NULL || packet->body_len == 0u || out_buf == NULL || out_len == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    if (packet->version != 0u && packet->version != EF_ONAIR_VERSION) {
        return ESP_ERR_INVALID_VERSION;
    }
    if (!packet->has_relay && (packet->flags & EF_ONAIR_FLAG_RELAY_EXTENSION) != 0u) {
        return ESP_ERR_INVALID_ARG;
    }
    if (packet->has_relay && packet->relay.ttl == 0u) {
        return ESP_ERR_INVALID_ARG;
    }
    frame_len = EF_ONAIR_HEADER_SIZE + packet->body_len;
    if (packet->has_relay) {
        frame_len += EF_ONAIR_RELAY_EXTENSION_SIZE;
    }
    if (frame_len > out_buf_cap) {
        return ESP_ERR_INVALID_SIZE;
    }
    out_buf[0] = packet->version == 0u ? EF_ONAIR_VERSION : packet->version;
    out_buf[1] = packet->logical_type;
    out_buf[2] = packet->has_relay ? (packet->flags | EF_ONAIR_FLAG_RELAY_EXTENSION) : packet->flags;
    out_buf[3] = packet->sequence;
    ef_write_le16(&out_buf[4], packet->source_short_id);
    ef_write_le16(&out_buf[6], packet->target_short_id);
    size_t body_offset = EF_ONAIR_HEADER_SIZE;
    if (packet->has_relay) {
        ef_write_le16(&out_buf[8], packet->relay.origin_short_id);
        ef_write_le16(&out_buf[10], packet->relay.previous_hop_short_id);
        out_buf[12] = packet->relay.ttl;
        out_buf[13] = packet->relay.hop_count;
        out_buf[14] = packet->relay.route_hint;
        body_offset += EF_ONAIR_RELAY_EXTENSION_SIZE;
    }
    memcpy(&out_buf[body_offset], packet->body, packet->body_len);
    *out_len = frame_len;
    return ESP_OK;
}

esp_err_t ef_onair_build_relay_forward(
    const ef_onair_packet_t *packet,
    uint16_t relay_short_id,
    ef_onair_packet_t *out_packet) {
    uint16_t origin_short_id;
    uint8_t ttl = 2u;
    uint8_t hop_count = 0u;
    uint8_t route_hint = 0u;
    if (packet == NULL || out_packet == NULL || packet->body == NULL || packet->body_len == 0u ||
        relay_short_id == 0u) {
        return ESP_ERR_INVALID_ARG;
    }
    origin_short_id = packet->source_short_id;
    if (packet->has_relay) {
        origin_short_id = packet->relay.origin_short_id;
        ttl = packet->relay.ttl;
        hop_count = packet->relay.hop_count;
        route_hint = packet->relay.route_hint;
    }
    if (origin_short_id == 0u || ttl <= 1u) {
        return ESP_ERR_INVALID_ARG;
    }
    *out_packet = *packet;
    out_packet->flags |= EF_ONAIR_FLAG_RELAY_EXTENSION;
    out_packet->source_short_id = relay_short_id;
    out_packet->has_relay = true;
    out_packet->relay.origin_short_id = origin_short_id;
    out_packet->relay.previous_hop_short_id = relay_short_id;
    out_packet->relay.ttl = (uint8_t)(ttl - 1u);
    out_packet->relay.hop_count = (uint8_t)(hop_count + 1u);
    out_packet->relay.route_hint = route_hint;
    return ESP_OK;
}

esp_err_t ef_onair_encode_state(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_state_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    uint8_t payload[3];
    ef_onair_packet_t packet = {0};
    if (body == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    payload[0] = body->key_token;
    payload[1] = body->value_token;
    payload[2] = body->event_wake ? 1u : 0u;
    packet.version = EF_ONAIR_VERSION;
    packet.logical_type = EF_ONAIR_TYPE_STATE;
    packet.flags = summary ? EF_ONAIR_FLAG_SUMMARY : 0u;
    packet.sequence = sequence;
    packet.source_short_id = source_short_id;
    packet.target_short_id = 0u;
    packet.body = payload;
    packet.body_len = sizeof(payload);
    return ef_onair_encode_packet(&packet, out_buf, out_buf_cap, out_len);
}

esp_err_t ef_onair_decode_state(const ef_onair_packet_t *packet, ef_onair_state_body_t *out_body) {
    if (packet == NULL || out_body == NULL || packet->logical_type != EF_ONAIR_TYPE_STATE || packet->body_len != 3u) {
        return ESP_ERR_INVALID_ARG;
    }
    out_body->key_token = packet->body[0];
    out_body->value_token = packet->body[1];
    out_body->event_wake = packet->body[2] != 0u;
    return ESP_OK;
}

esp_err_t ef_onair_encode_event(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_event_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    uint8_t payload[4];
    ef_onair_packet_t packet = {0};
    if (body == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    payload[0] = body->event_code;
    payload[1] = body->severity;
    payload[2] = body->value_bucket;
    payload[3] = body->flags;
    packet.version = EF_ONAIR_VERSION;
    packet.logical_type = EF_ONAIR_TYPE_EVENT;
    packet.flags = summary ? EF_ONAIR_FLAG_SUMMARY : 0u;
    packet.sequence = sequence;
    packet.source_short_id = source_short_id;
    packet.target_short_id = 0u;
    packet.body = payload;
    packet.body_len = sizeof(payload);
    return ef_onair_encode_packet(&packet, out_buf, out_buf_cap, out_len);
}

esp_err_t ef_onair_decode_event(const ef_onair_packet_t *packet, ef_onair_event_body_t *out_body) {
    if (packet == NULL || out_body == NULL || packet->logical_type != EF_ONAIR_TYPE_EVENT || packet->body_len != 4u) {
        return ESP_ERR_INVALID_ARG;
    }
    out_body->event_code = packet->body[0];
    out_body->severity = packet->body[1];
    out_body->value_bucket = packet->body[2];
    out_body->flags = packet->body[3];
    return ESP_OK;
}

esp_err_t ef_onair_encode_command_result(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_command_result_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    uint8_t payload[4];
    ef_onair_packet_t packet = {0};
    if (body == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ef_write_le16(&payload[0], body->command_token);
    payload[2] = body->phase_token;
    payload[3] = body->reason_token;
    packet.version = EF_ONAIR_VERSION;
    packet.logical_type = EF_ONAIR_TYPE_COMMAND_RESULT;
    packet.flags = summary ? EF_ONAIR_FLAG_SUMMARY : 0u;
    packet.sequence = sequence;
    packet.source_short_id = source_short_id;
    packet.target_short_id = 0u;
    packet.body = payload;
    packet.body_len = sizeof(payload);
    return ef_onair_encode_packet(&packet, out_buf, out_buf_cap, out_len);
}

esp_err_t ef_onair_decode_command_result(
    const ef_onair_packet_t *packet,
    ef_onair_command_result_body_t *out_body) {
    if (packet == NULL || out_body == NULL || packet->logical_type != EF_ONAIR_TYPE_COMMAND_RESULT ||
        packet->body_len != 4u) {
        return ESP_ERR_INVALID_ARG;
    }
    out_body->command_token = ef_read_le16(&packet->body[0]);
    out_body->phase_token = packet->body[2];
    out_body->reason_token = packet->body[3];
    return ESP_OK;
}

esp_err_t ef_onair_encode_pending_digest(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_pending_digest_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    uint8_t payload[2];
    ef_onair_packet_t packet = {0};
    if (body == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    payload[0] = body->pending_count;
    payload[1] = body->flags;
    packet.version = EF_ONAIR_VERSION;
    packet.logical_type = EF_ONAIR_TYPE_PENDING_DIGEST;
    packet.flags = summary ? EF_ONAIR_FLAG_SUMMARY : 0u;
    packet.sequence = sequence;
    packet.source_short_id = source_short_id;
    packet.target_short_id = 0u;
    packet.body = payload;
    packet.body_len = sizeof(payload);
    return ef_onair_encode_packet(&packet, out_buf, out_buf_cap, out_len);
}

esp_err_t ef_onair_decode_pending_digest(
    const ef_onair_packet_t *packet,
    ef_onair_pending_digest_body_t *out_body) {
    if (packet == NULL || out_body == NULL || packet->logical_type != EF_ONAIR_TYPE_PENDING_DIGEST ||
        packet->body_len != 2u) {
        return ESP_ERR_INVALID_ARG;
    }
    out_body->pending_count = packet->body[0];
    out_body->flags = packet->body[1];
    return ESP_OK;
}

esp_err_t ef_onair_encode_tiny_poll(
    uint16_t source_short_id,
    uint8_t sequence,
    const ef_onair_tiny_poll_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    uint8_t payload[1];
    ef_onair_packet_t packet = {0};
    if (body == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    payload[0] = body->service_level;
    packet.version = EF_ONAIR_VERSION;
    packet.logical_type = EF_ONAIR_TYPE_TINY_POLL;
    packet.flags = 0u;
    packet.sequence = sequence;
    packet.source_short_id = source_short_id;
    packet.target_short_id = 0u;
    packet.body = payload;
    packet.body_len = sizeof(payload);
    return ef_onair_encode_packet(&packet, out_buf, out_buf_cap, out_len);
}

esp_err_t ef_onair_decode_tiny_poll(const ef_onair_packet_t *packet, ef_onair_tiny_poll_body_t *out_body) {
    if (packet == NULL || out_body == NULL || packet->logical_type != EF_ONAIR_TYPE_TINY_POLL ||
        packet->body_len != 1u) {
        return ESP_ERR_INVALID_ARG;
    }
    out_body->service_level = packet->body[0];
    return ESP_OK;
}

esp_err_t ef_onair_encode_compact_command(
    uint16_t target_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_compact_command_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    uint8_t payload[5];
    ef_onair_packet_t packet = {0};
    if (body == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    ef_write_le16(&payload[0], body->command_token);
    payload[2] = body->command_kind;
    payload[3] = body->argument;
    payload[4] = body->expires_in_sec;
    packet.version = EF_ONAIR_VERSION;
    packet.logical_type = EF_ONAIR_TYPE_COMPACT_COMMAND;
    packet.flags = summary ? EF_ONAIR_FLAG_SUMMARY : 0u;
    packet.sequence = sequence;
    packet.source_short_id = 0u;
    packet.target_short_id = target_short_id;
    packet.body = payload;
    packet.body_len = sizeof(payload);
    return ef_onair_encode_packet(&packet, out_buf, out_buf_cap, out_len);
}

esp_err_t ef_onair_decode_compact_command(
    const ef_onair_packet_t *packet,
    ef_onair_compact_command_body_t *out_body) {
    if (packet == NULL || out_body == NULL || packet->logical_type != EF_ONAIR_TYPE_COMPACT_COMMAND ||
        packet->body_len != 5u) {
        return ESP_ERR_INVALID_ARG;
    }
    out_body->command_token = ef_read_le16(&packet->body[0]);
    out_body->command_kind = packet->body[2];
    out_body->argument = packet->body[3];
    out_body->expires_in_sec = packet->body[4];
    return ESP_OK;
}

esp_err_t ef_onair_encode_heartbeat(
    uint16_t source_short_id,
    bool summary,
    uint8_t sequence,
    const ef_onair_heartbeat_body_t *body,
    uint8_t *out_buf,
    size_t out_buf_cap,
    size_t *out_len) {
    uint8_t payload[5];
    ef_onair_packet_t packet = {0};
    if (body == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    payload[0] = body->health;
    payload[1] = body->battery_bucket;
    payload[2] = body->link_quality;
    payload[3] = body->uptime_bucket;
    payload[4] = body->flags;
    packet.version = EF_ONAIR_VERSION;
    packet.logical_type = EF_ONAIR_TYPE_HEARTBEAT;
    packet.flags = summary ? EF_ONAIR_FLAG_SUMMARY : 0u;
    packet.sequence = sequence;
    packet.source_short_id = source_short_id;
    packet.target_short_id = 0u;
    packet.body = payload;
    packet.body_len = sizeof(payload);
    return ef_onair_encode_packet(&packet, out_buf, out_buf_cap, out_len);
}

esp_err_t ef_onair_decode_heartbeat(const ef_onair_packet_t *packet, ef_onair_heartbeat_body_t *out_body) {
    if (packet == NULL || out_body == NULL || packet->logical_type != EF_ONAIR_TYPE_HEARTBEAT ||
        packet->body_len != 5u) {
        return ESP_ERR_INVALID_ARG;
    }
    out_body->health = packet->body[0];
    out_body->battery_bucket = packet->body[1];
    out_body->link_quality = packet->body[2];
    out_body->uptime_bucket = packet->body[3];
    out_body->flags = packet->body[4];
    return ESP_OK;
}

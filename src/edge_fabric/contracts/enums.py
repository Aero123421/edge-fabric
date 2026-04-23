from enum import StrEnum


class Priority(StrEnum):
    CRITICAL = "critical"
    CONTROL = "control"
    NORMAL = "normal"
    BULK = "bulk"


class MessageKind(StrEnum):
    STATE = "state"
    EVENT = "event"
    COMMAND = "command"
    COMMAND_RESULT = "command_result"
    HEARTBEAT = "heartbeat"
    MANIFEST = "manifest"
    LEASE = "lease"
    FILE_CHUNK = "file_chunk"
    FABRIC_SUMMARY = "fabric_summary"


class TargetKind(StrEnum):
    NODE = "node"
    GROUP = "group"
    SERVICE = "service"
    HOST = "host"
    CLIENT = "client"
    SITE = "site"
    BROADCAST = "broadcast"


class AckPhase(StrEnum):
    HOP_ACCEPTED = "hop_accepted"
    PERSISTED = "persisted"
    ACCEPTED = "accepted"
    EXECUTING = "executing"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    REJECTED = "rejected"
    EXPIRED = "expired"


class PowerClass(StrEnum):
    USB_POWERED = "usb_powered"
    MAINS_POWERED = "mains_powered"
    RECHARGEABLE_BATTERY = "rechargeable_battery"
    PRIMARY_BATTERY = "primary_battery"
    ENERGY_HARVESTED = "energy_harvested"


class WakeClass(StrEnum):
    ALWAYS_ON = "always_on"
    SLEEPY_PERIODIC = "sleepy_periodic"
    SLEEPY_EVENT = "sleepy_event"
    MAINTENANCE_AWAKE = "maintenance_awake"


class Bearer(StrEnum):
    LORA = "lora"
    WIFI_MESH = "wifi_mesh"
    WIFI_IP = "wifi_ip"
    WIFI_LR = "wifi_lr"
    USB_CDC = "usb_cdc"
    ETHERNET = "ethernet"
    BLE_MAINTENANCE = "ble_maintenance"


class NetworkRole(StrEnum):
    SLEEPY_LEAF = "sleepy_leaf"
    POWERED_LEAF = "powered_leaf"
    MESH_ROUTER = "mesh_router"
    MESH_ROOT = "mesh_root"
    LORA_RELAY = "lora_relay"
    DUAL_BEARER_BRIDGE = "dual_bearer_bridge"
    GATEWAY_HEAD = "gateway_head"
    SITE_ROUTER = "site_router"
    CONTROLLER_CLIENT = "controller_client"

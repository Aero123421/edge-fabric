from __future__ import annotations

import json
import tempfile
from pathlib import Path

from edge_fabric.host.agent import HostAgent
from edge_fabric.host.site_router import SiteRouter


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory() as temp_dir:
        temp_path = Path(temp_dir)
        router = SiteRouter(temp_path / "site-router.db")
        agent = HostAgent(router, spool_path=temp_path / "host-agent-spool.jsonl")

        lora_event = json.loads(
            (root / "contracts" / "fixtures" / "event-battery-alert.json").read_text(encoding="utf-8")
        )
        wifi_state = json.loads(
            (root / "contracts" / "fixtures" / "state-powered-level.json").read_text(encoding="utf-8")
        )

        frame = agent.encode_envelope_frame(lora_event)
        usb_result = agent.relay_usb_frame(
            ingress_id="gateway-usb-01",
            session_id="session-usb-01",
            frame=frame,
            metadata={"rssi": -110, "snr": 7.5, "hop_count": 0},
        )
        wifi_result = agent.relay_direct_ip(
            ingress_id="wifi-direct-01",
            session_id="session-wifi-01",
            envelope_dict=wifi_state,
            metadata={"rssi": -48},
        )

        print("usb relay:", usb_result.status, usb_result.ack)
        print("wifi relay:", wifi_result.status, wifi_result.ack)
        print("latest state:", router.latest_state("powered-leaf-01", "tank.level"))
        print("event count:", router.count_events())
        print("agent diagnostics:", agent.diagnostics())


if __name__ == "__main__":
    main()

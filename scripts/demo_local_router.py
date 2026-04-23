from __future__ import annotations

import tempfile
from pathlib import Path

from edge_fabric.contracts.enums import Priority
from edge_fabric.host.site_router import SiteRouter
from edge_fabric.sdk.client import LocalSiteRouterClient


def main() -> None:
    with tempfile.TemporaryDirectory() as temp_dir:
        db_path = Path(temp_dir) / "site-router.db"
        router = SiteRouter(db_path)
        client = LocalSiteRouterClient(router)

        client.publish_state(
            hardware_id="leaf-powered-01",
            state_key="tank.level",
            payload={"value": 72, "unit": "percent"},
            priority=Priority.NORMAL,
        )
        client.emit_event(
            hardware_id="leaf-battery-01",
            event_id="evt-battery-low-001",
            service="alerts",
            payload={"alarm_code": "battery_low", "severity": 2, "battery_bucket": 1},
            priority=Priority.CRITICAL,
        )
        client.issue_command(
            command_id="cmd-servo-001",
            target_node="servo-node-01",
            payload={"command_name": "servo.set_angle", "angle": 90},
        )

        print("latest state:", router.latest_state("leaf-powered-01", "tank.level"))
        print("event count:", router.count_events())
        print("command state:", router.command_state("cmd-servo-001"))
        print("queue metrics:", router.queue_metrics())


if __name__ == "__main__":
    main()

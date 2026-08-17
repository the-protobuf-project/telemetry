"""
Simple logging example using the new Telemetry.new() builder API.

Configuration is auto-discovered from:
1. telemetry.toml in current directory
2. Environment variables (TELEMETRY_*)
3. Builder method overrides (highest priority)

Run with:
    uv run python -m examples.logging.simple_example
"""

from telemetry import Telemetry


def main():
    # Auto-discovers telemetry.toml config file
    # No builder overrides - uses config file values
    with Telemetry.new().build() as telemetry:
        telemetry.logger.info("Chat service started")
        telemetry.logger.debug("Debug mode enabled")
        telemetry.logger.info("OpenTelemetry logging example")

        active_rooms = 3
        total_users = 42
        telemetry.logger.info(
            f"Service initialized with {active_rooms} active rooms and {total_users} users"
        )

        telemetry.logger.warning(
            "Rate limit approaching",
            {
                "current_percent": 85.5,
                "user_id": "user-123",
            },
        )

        telemetry.logger.error(
            "Failed to process message",
            {
                "error_code": 500,
                "user_id": "user-456",
            },
        )

        telemetry.logger.info("Chat service shutting down")


if __name__ == "__main__":
    main()

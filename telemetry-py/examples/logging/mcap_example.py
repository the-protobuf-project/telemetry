"""
Example demonstrating MCAP logging with Foxglove Studio visualization.

This example shows how to:
- Enable MCAP recording for logs using Telemetry.new() builder
- Generate various log levels
- View the MCAP file in Foxglove Studio

After running, open the MCAP file in Foxglove Studio to visualize:
- Log timeline
- Log levels distribution
- Structured data fields

Run with:
    uv run python -m examples.logging.mcap_example
"""

from telemetry import Telemetry
import time


def main():
    # Uses telemetry.toml config - enable foxglove.enabled=true and set foxglove.file_path
    with Telemetry.new().build() as telemetry:
        telemetry.logger.info(
            "MCAP logging demo started", {"foxglove_url": "https://studio.foxglove.dev"}
        )

        # Simulate application lifecycle with various log events
        telemetry.logger.debug(
            "Initializing application components",
            {"components": ["database", "cache", "api_server"], "startup_time_ms": 150},
        )

        time.sleep(0.1)

        telemetry.logger.info(
            "Database connection established",
            {
                "host": "localhost",
                "port": 5432,
                "database": "production_db",
                "connection_pool_size": 10,
            },
        )

        time.sleep(0.1)

        telemetry.logger.info(
            "Processing user requests",
            {
                "endpoint": "/api/users",
                "method": "GET",
                "user_count": 150,
                "response_time_ms": 45,
            },
        )

        time.sleep(0.1)

        telemetry.logger.warning(
            "High memory usage detected",
            {
                "memory_used_mb": 1800,
                "memory_total_mb": 2048,
                "usage_percent": 87.9,
                "recommendation": "consider scaling",
            },
        )

        time.sleep(0.1)

        telemetry.logger.error(
            "Failed to process payment",
            {
                "transaction_id": "txn-12345",
                "user_id": "user-789",
                "amount": 99.99,
                "currency": "USD",
                "error": "payment_gateway_timeout",
                "retry_count": 3,
            },
        )

        time.sleep(0.1)

        telemetry.logger.critical(
            "Service health check failed",
            {
                "service": "payment-processor",
                "status": "unhealthy",
                "last_success": "2026-01-25T15:00:00Z",
                "consecutive_failures": 5,
                "action": "triggering_failover",
            },
        )

        time.sleep(0.1)

        telemetry.logger.info(
            "Generating sample log entries for visualization",
            {
                "total_logs": 50,
                "log_types": ["info", "warning", "error"],
                "time_span_seconds": 5,
            },
        )

        # Generate multiple log entries for better visualization
        for i in range(10):
            telemetry.logger.info(
                f"Request processed #{i + 1}",
                {
                    "request_id": f"req-{i + 1:03d}",
                    "endpoint": "/api/data",
                    "status_code": 200,
                    "duration_ms": 20 + (i * 5),
                    "user_agent": "Mozilla/5.0",
                },
            )
            time.sleep(0.05)

        for i in range(5):
            telemetry.logger.warning(
                f"Rate limit warning #{i + 1}",
                {
                    "client_id": f"client-{i + 1}",
                    "requests_count": 95 + i,
                    "limit": 100,
                    "window_seconds": 60,
                },
            )
            time.sleep(0.05)

        for i in range(3):
            telemetry.logger.error(
                f"Database query timeout #{i + 1}",
                {
                    "query_id": f"query-{i + 1}",
                    "table": "users",
                    "timeout_ms": 5000,
                    "rows_affected": 0,
                },
            )
            time.sleep(0.05)

        telemetry.logger.info(
            "MCAP logging demo completed",
            {
                "total_duration_seconds": 2,
                "mcap_file": "/tmp/logging-demo.mcap",
                "next_steps": "Open in Foxglove Studio",
            },
        )

    # Telemetry automatically closes when exiting the 'with' block

    print("\n" + "=" * 70)


if __name__ == "__main__":
    main()

"""Module Levels Example — per-module log level control.

Run from this directory:
    cd examples/module_levels
    python example.py
"""

from telemetry import Telemetry, LogLevel


def main():
    # ========================================
    # 1. Main service — uses global logging.level from telemetry.toml (Level 2 = Info)
    # ========================================
    """
    Demonstrate per-module logging-level configuration and its priority order.
    
    Creates telemetry services with different module-level settings and emits messages at multiple severities to show how environment variables, TOML configuration, code-level settings, and global defaults determine log visibility.
    """
    core = Telemetry.new().with_service("robot-core", "1.0.0").build()

    core.logger.info("=== Robot Core Started ===")
    core.logger.info("Global level is 2 (Info) from telemetry.toml")
    core.logger.debug("This debug message is HIDDEN because global level=2 (Info)")
    core.logger.info("Core system initialized")
    core.logger.error("Core error test — always visible")

    # ========================================
    # 2. Module: nats-module — TOML overrides to Level 1 (Error only)
    #    Code says Level3, but telemetry.toml has [logging.modules.nats-module] level=1
    #    TOML wins → only errors show
    # ========================================
    print("\n--- nats-module (TOML override → Level 1 = Error only) ---")

    nats = (
        Telemetry.new()
        .with_service("nats-module", "0.2.0")
        .with_log_level(LogLevel.MODULE_LEVEL_3)  # Code says Level3 (Debug)
        .build()
    )

    nats.logger.debug("NATS debug — HIDDEN (TOML overrode to Level 1)")
    nats.logger.info("NATS info — HIDDEN (TOML overrode to Level 1)")
    nats.logger.warning("NATS warn — HIDDEN (TOML overrode to Level 1)")
    nats.logger.error("NATS error — VISIBLE (Level 1 = Error only)")

    # ========================================
    # 3. Module: vision-module — no TOML override, code sets Level 3 (Debug)
    #    with_log_level(LEVEL3) wins → full debug output
    # ========================================
    print("\n--- vision-module (code default → Level 3 = Debug) ---")

    vision = (
        Telemetry.new()
        .with_service("vision-module", "0.1.0")
        .with_log_level(LogLevel.MODULE_LEVEL_3)  # No TOML override → Level3 wins
        .build()
    )

    vision.logger.debug("Vision debug — VISIBLE (Level 3)")
    vision.logger.info("Vision info — VISIBLE (Level 3)")
    vision.logger.warning("Vision warn — VISIBLE (Level 3)")
    vision.logger.error("Vision error — VISIBLE (Level 3)")

    # ========================================
    # 4. Module: motor-module — no TOML override, no with_log_level
    #    Falls back to global logging.level=2 from telemetry.toml → Info
    # ========================================
    print("\n--- motor-module (no override → global Level 2 = Info) ---")

    motor = Telemetry.new().with_service("motor-module", "1.5.0").build()

    motor.logger.debug("Motor debug — HIDDEN (global Level 2 = Info)")
    motor.logger.info("Motor info — VISIBLE (Level 2)")
    motor.logger.warning("Motor warn — VISIBLE (Level 2)")
    motor.logger.error("Motor error — VISIBLE (Level 2)")

    # ========================================
    # Summary
    # ========================================
    print("\n=== Priority Chain Summary ===")
    print("  env var (TELEMETRY_LOGGING_MODULES_<NAME>_LEVEL)")
    print("    > TOML [logging.modules.<name>] level")
    print("      > with_log_level() in code")
    print("        > global [logging] level")
    print("          > environment-based default")


if __name__ == "__main__":
    main()

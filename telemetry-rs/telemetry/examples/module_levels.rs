use telemetry::{LogLevel, Telemetry, logger};

/// Initializes telemetry for the example services and demonstrates their logging levels.
///
/// # Examples
///
/// ```
/// # fn main() -> anyhow::Result<()> {
/// #     // Run the example binary to initialize telemetry and view its log output.
/// #     Ok(())
/// # }
/// ```
///
/// # Returns
///
/// An error if telemetry initialization fails.
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // ========================================
    // 1. Main service — uses global logging.level from telemetry.toml (Level 2 = Info)
    // ========================================
    let _core = Telemetry::new()
        .with_service("robot-core", "1.0.0")
        .build()?;

    logger::info!("=== Robot Core Started ===");
    logger::info!("Global level is 2 (Info) from telemetry.toml");
    logger::debug!("This debug message is HIDDEN because global level=2 (Info)");
    logger::info!("Core system initialized");
    logger::error!("Core error test — always visible");

    // ========================================
    // 2. Module: nats-module — TOML overrides to Level 1 (Error only)
    //    Code says Level3, but telemetry.toml has [logging.modules.nats-module] level=1
    //    TOML wins → only errors show
    // ========================================
    println!("\n--- nats-module (TOML override → Level 1 = Error only) ---");

    let _nats = Telemetry::new()
        .with_service("nats-module", "0.2.0")
        .with_log_level(LogLevel::ModuleLevel_3) // Code says Level3 (Debug)
        .build()?;

    logger::debug!("NATS debug — HIDDEN (TOML overrode to Level 1)");
    logger::info!("NATS info — HIDDEN (TOML overrode to Level 1)");
    logger::warn!("NATS warn — HIDDEN (TOML overrode to Level 1)");
    logger::error!("NATS error — VISIBLE (Level 1 = Error only)");

    // ========================================
    // 3. Module: vision-module — no TOML override, code sets Level 3 (Debug)
    //    with_log_level(Level3) wins → full debug output
    // ========================================
    println!("\n--- vision-module (code default → Level 3 = Debug) ---");

    let _vision = Telemetry::new()
        .with_service("vision-module", "0.1.0")
        .with_log_level(LogLevel::ModuleLevel_3) // No TOML override → Level3 wins
        .build()?;

    logger::debug!("Vision debug — VISIBLE (Level 3)");
    logger::info!("Vision info — VISIBLE (Level 3)");
    logger::warn!("Vision warn — VISIBLE (Level 3)");
    logger::error!("Vision error — VISIBLE (Level 3)");

    // ========================================
    // 4. Module: motor-module — no TOML override, no with_log_level
    //    Falls back to global logging.level=2 from telemetry.toml → Info
    // ========================================
    println!("\n--- motor-module (no override → global Level 2 = Info) ---");

    let _motor = Telemetry::new()
        .with_service("motor-module", "1.5.0")
        .build()?;

    logger::debug!("Motor debug — HIDDEN (global Level 2 = Info)");
    logger::info!("Motor info — VISIBLE (Level 2)");
    logger::warn!("Motor warn — VISIBLE (Level 2)");
    logger::error!("Motor error — VISIBLE (Level 2)");

    // ========================================
    // Summary
    // ========================================
    println!("\n=== Priority Chain Summary ===");
    println!("  env var (TELEMETRY_LOGGING_MODULES_<NAME>_LEVEL)");
    println!("    > TOML [logging.modules.<name>] level");
    println!("      > with_log_level() in code");
    println!("        > global [logging] level");
    println!("          > environment-based default");

    Ok(())
}

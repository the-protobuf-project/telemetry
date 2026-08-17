//! Per-module log levels from TOML (`config/module_levels.toml`).
use telemetry::{LogLevel, Telemetry, logger};

/// Returns the path to the module log-level configuration file.
///
/// # Examples
///
/// ```
/// let path = cfg();
/// assert!(path.ends_with("config/module_levels.toml"));
/// ```
fn cfg() -> String {
    format!("{}/config/module_levels.toml", env!("CARGO_MANIFEST_DIR"))
}

/// Initializes telemetry for the robot core and its demonstration modules.
///
/// # Returns
///
/// `Ok(())` after all telemetry instances are initialized; otherwise, the first
/// initialization error is returned.
///
/// # Examples
///
/// ```
/// # fn initialize_application() -> anyhow::Result<()> {
/// #     Ok(())
/// # }
/// assert!(initialize_application().is_ok());
/// ```
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let c = cfg();

    let _core = Telemetry::new()
        .with_config(&c)
        .with_service("robot-core", "1.0.0")
        .build()?;

    logger::info!("=== Robot Core Started ===");
    logger::debug!("Hidden (global level 2)");
    logger::info!("Core initialized");

    println!("\n--- nats-module (TOML → level 1) ---");
    let _nats = Telemetry::new()
        .with_config(&c)
        .with_service("nats-module", "0.2.0")
        .with_log_level(LogLevel::ModuleLevel_3)
        .build()?;
    logger::debug!("NATS debug — hidden");
    logger::error!("NATS error — visible");

    println!("\n--- vision-module (code Level 3) ---");
    let _vision = Telemetry::new()
        .with_config(&c)
        .with_service("vision-module", "0.1.0")
        .with_log_level(LogLevel::ModuleLevel_3)
        .build()?;
    logger::debug!("Vision debug — visible");

    println!("\n--- motor-module (global level 2) ---");
    let _motor = Telemetry::new()
        .with_config(&c)
        .with_service("motor-module", "1.5.0")
        .build()?;
    logger::info!("Motor info — visible");

    Ok(())
}

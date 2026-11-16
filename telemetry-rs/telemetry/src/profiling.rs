//! Continuous profiling to Pyroscope.
//!
//! With the `profiling` feature (on by default) and profiling enabled —
//! [`crate::TelemetryBuilder::with_profiling`], `[profiling] enabled = true`
//! in `telemetry.toml`, or `TELEMETRY_PROFILING_ENABLED=true` — the process's
//! CPU profile is sampled and pushed to Pyroscope, tagged with the service's
//! name, version and environment like the Go SDK's, so Grafana links a slow
//! span to the code that was running.

use anyhow::Result;

use crate::options::{ProfilingOptions, ServiceOptions};

/// A running profiler; dropping it stops profiling and flushes the last
/// profile.
pub struct Profiler {
    #[cfg(feature = "profiling")]
    agent: Option<pyroscope::PyroscopeAgent<pyroscope::pyroscope::PyroscopeAgentRunning>>,
}

impl Profiler {
    /// Starts profiling to `opts.server_address`, or returns `None` when
    /// profiling is off.
    #[cfg(feature = "profiling")]
    pub fn start(service: &ServiceOptions, opts: &ProfilingOptions) -> Result<Option<Self>> {
        use pyroscope::backend::{BackendConfig, PprofConfig, pprof_backend};
        use pyroscope::pyroscope::PyroscopeAgentBuilder;

        if !opts.enabled {
            return Ok(None);
        }
        let environment = service.environment.to_string();
        let mut tags = vec![
            ("service", service.name.as_str()),
            ("version", service.version.as_str()),
            ("environment", environment.as_str()),
        ];
        tags.extend(opts.tags.iter().map(|(k, v)| (k.as_str(), v.as_str())));

        let mut builder = PyroscopeAgentBuilder::new(
            &opts.server_address,
            format!("{}.{}", service.name, environment),
            100,
            "telemetry-rs",
            env!("CARGO_PKG_VERSION"),
            pprof_backend(PprofConfig::default(), BackendConfig::default()),
        )
        .tags(tags);
        if !opts.basic_auth_user.is_empty() {
            builder = builder.basic_auth(&opts.basic_auth_user, &opts.basic_auth_password);
        }
        if !opts.tenant_id.is_empty() {
            builder = builder.tenant_id(opts.tenant_id.clone());
        }
        let agent = builder.build()?.start()?;
        Ok(Some(Self { agent: Some(agent) }))
    }

    /// Without the `profiling` feature, enabling profiling is an error rather
    /// than silently doing nothing.
    #[cfg(not(feature = "profiling"))]
    pub fn start(_service: &ServiceOptions, opts: &ProfilingOptions) -> Result<Option<Self>> {
        if opts.enabled {
            anyhow::bail!(
                "profiling is enabled but telemetry was built without the `profiling` feature"
            );
        }
        Ok(None)
    }

    /// Stops profiling and sends what was collected.
    pub fn stop(&mut self) {
        #[cfg(feature = "profiling")]
        if let Some(agent) = self.agent.take()
            && let Ok(ready) = agent.stop()
        {
            ready.shutdown();
        }
    }
}

impl Drop for Profiler {
    fn drop(&mut self) {
        self.stop();
    }
}

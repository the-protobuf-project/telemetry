"""Dynaconf-based configuration management for Telemetry SDK.

Configuration Priority (lowest to highest):
1. Default values (lowest priority)
2. Config file (telemetry.toml / telemetry.yaml / telemetry.json)
3. Environment variables (.env file or TELEMETRY_* env vars)
4. Code-based (builder methods) - highest priority

Environment variables use TELEMETRY_ prefix with double underscores for nesting:
    TELEMETRY_SERVICE__NAME=my-service
    TELEMETRY_TELEMETRY__OTLP__ENDPOINT=otel.example.com
    TELEMETRY_TELEMETRY__OTLP__AUTH_TOKEN=your-token

Single underscores are also accepted and translated automatically:
    TELEMETRY_SERVICE_NAME=my-service
    TELEMETRY_TELEMETRY_OTLP_ENDPOINT=otel.example.com

Auto-discovers config files from:
1. TELEMETRY_CONFIG_PATH environment variable
2. telemetry.toml in current directory
3. .config/telemetry.toml

Example telemetry.toml:
    [service]
    name = "my-service"
    version = "1.0.0"

    [telemetry.otlp]
    endpoint = "otel.example.com"
    auth_token = "your-token"
"""

import os as _os
from pathlib import Path
from typing import Optional, Dict, Any
from dynaconf import Dynaconf, Validator


# Auto-discover config files
def _find_config_files() -> list[str]:
    """
    Find the first available telemetry configuration file in the current directory or its `.config` subdirectory.
    
    Returns:
        list[str]: A list containing the selected configuration file path, or an empty list when no supported file exists.
    """
    config_files = []

    # Check for telemetry.toml, telemetry.yaml, telemetry.json in current directory
    for ext in ["toml", "yaml", "yml", "json"]:
        path = Path.cwd() / f"telemetry.{ext}"
        if path.exists():
            config_files.append(str(path))
            break

    # Check .config directory
    if not config_files:
        for ext in ["toml", "yaml", "yml", "json"]:
            path = Path.cwd() / ".config" / f"telemetry.{ext}"
            if path.exists():
                config_files.append(str(path))
                break

    return config_files


def _translate_single_underscore_env_vars(prefix: str, validators: list) -> None:
    """
    Translate validator-known single-underscore environment variable names into Dynaconf's nested format.
    
    Parameters:
        prefix (str): Environment variable prefix to preserve.
        validators (list): Validators whose dotted names define supported nested settings.
    """
    for v in validators:
        for name in v.names:
            if "." not in name:
                continue
            parts = name.upper().split(".")
            single = f"{prefix}_" + "_".join(parts)
            double = f"{prefix}_" + "__".join(parts)
            if single in _os.environ and double not in _os.environ:
                _os.environ[double] = _os.environ[single]


_VALIDATORS = [
    # Service validators
    Validator("service.name", default="unnamed-service"),
    Validator("service.version", default="1.0.0"),
    Validator("service.environment", default="development"),
    Validator("service.description", default=""),
    # Telemetry validators
    Validator("telemetry.enabled", default=True),
    # OTLP validators
    Validator("telemetry.otlp.endpoint", default="localhost:4317"),
    Validator("telemetry.otlp.auth_token", default=""),
    Validator("telemetry.otlp.secure", default=False),
    Validator("telemetry.otlp.use_http", default=False),
    # Metrics validators
    Validator("telemetry.metrics.export_interval_seconds", default=10),
    # Logging validators
    Validator("logging.log.report_caller", default=True),
    Validator("logging.log.report_timestamp", default=True),
    # Foxglove validators
    Validator("foxglove.enabled", default=False),
    Validator("foxglove.file_path", default=""),
    # Profiling validators
    Validator("profiling.enabled", default=False),
    Validator("profiling.server_address", default="http://localhost:4040"),
    # Tracing validators
    Validator("tracing.enabled", default=True),
]

_translate_single_underscore_env_vars("TELEMETRY", _VALIDATORS)

# Initialize Dynaconf settings
settings = Dynaconf(
    envvar_prefix="TELEMETRY",
    envar_separator="_",
    settings_files=_find_config_files(),
    environments=False,  # Don't use [development], [production] sections
    load_dotenv=True,
    merge_enabled=True,
    validators=_VALIDATORS,
)


def load_config(config_path: Optional[str] = None) -> Dynaconf:
    """
    Load configuration from an explicit file or the auto-discovered configuration.
    
    Parameters:
        config_path (Optional[str]): Path to a configuration file. When omitted,
            uses the global settings instance.
    
    Returns:
        Dynaconf: The loaded configuration settings.
    """
    if config_path:
        # Load from specific path
        return Dynaconf(
            envvar_prefix="TELEMETRY",
            envar_separator="_",
            settings_files=[config_path],
            environments=False,
            load_dotenv=True,
            merge_enabled=True,
        )
    return settings


def get_service_config() -> Dict[str, Any]:
    """Get service configuration as a dictionary."""
    return {
        "name": settings.get("service.name", "unnamed-service"),
        "version": settings.get("service.version", "1.0.0"),
        "environment": settings.get("service.environment", "development"),
        "description": settings.get("service.description", ""),
        "labels": dict(settings.get("service.labels", {})),
    }


def get_telemetry_config() -> Dict[str, Any]:
    """Get telemetry configuration as a dictionary."""
    return {
        "enabled": settings.get("telemetry.enabled", True),
        "otlp": {
            "endpoint": settings.get("telemetry.otlp.endpoint", "localhost:4317"),
            "auth_token": settings.get("telemetry.otlp.auth_token", ""),
            "secure": settings.get("telemetry.otlp.secure", False),
            "use_http": settings.get("telemetry.otlp.use_http", False),
        },
        "metrics": {
            "export_interval_seconds": settings.get(
                "telemetry.metrics.export_interval_seconds", 10
            ),
        },
    }


def get_foxglove_config() -> Dict[str, Any]:
    """Get Foxglove/MCAP configuration as a dictionary."""
    return {
        "enabled": settings.get("foxglove.enabled", False),
        "file_path": settings.get("foxglove.file_path", ""),
    }


def get_logging_config() -> Dict[str, Any]:
    """Get logging configuration as a dictionary."""
    return {
        "report_caller": settings.get("logging.log.report_caller", True),
        "report_timestamp": settings.get("logging.log.report_timestamp", True),
    }


def get_tracing_config() -> Dict[str, Any]:
    """Get tracing configuration as a dictionary."""
    return {
        "enabled": settings.get("tracing.enabled", True),
    }


def get_profiling_config() -> Dict[str, Any]:
    """Get profiling configuration as a dictionary."""
    return {
        "enabled": settings.get("profiling.enabled", False),
        "server_address": settings.get(
            "profiling.server_address", "http://localhost:4040"
        ),
    }

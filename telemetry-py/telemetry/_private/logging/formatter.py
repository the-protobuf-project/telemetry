"""
Logging formatter with colored output and structured data formatting.

This module provides custom formatting functions for the Telemetry logging system,
including ANSI color codes for terminal output, ISO 8601 timestamp formatting,
and caller information detection.

The formatter automatically detects the actual caller's file and line number
by inspecting the call stack and skipping _private Telemetry frames.

Typical usage example:

    formatter = get_custom_formatter()
    handler.formatter = formatter

    # Format structured data
    formatted = format_data({"key": "value", "count": 42})
"""

import functools
import os
import sys
import threading
from typing import Any

# Thread-local storage for caller information
# This allows the formatter to access caller info set by the logger
_caller_info = threading.local()

# ANSI color codes
GRAY = "\x1b[90m"
CYAN = "\x1b[36m"
YELLOW = "\x1b[33m"
RED = "\x1b[31m"
GREEN = "\x1b[32m"
BLUE = "\x1b[34m"
RESET = "\x1b[0m"

LEVEL_COLORS = {
    "DEBUG": BLUE,
    "INFO": GREEN,
    "WARNING": YELLOW,
    "ERROR": RED,
    "CRITICAL": RED,
}


def get_custom_formatter():
    """Create a custom formatter with ANSI color codes for logbook.

    Creates a formatter function that adds colors to log output, formats
    timestamps as ISO 8601 with timezone, and includes file/line information.

    Returns:
        A formatter function compatible with logbook handlers.

    Example:
        handler = StderrHandler()
        handler.formatter = get_custom_formatter()
    """

    def custom_format(record, handler):
        level_color = LEVEL_COLORS.get(record.level_name, RESET)

        # Format file and line info - use thread-local caller info
        file_info = ""
        if hasattr(_caller_info, "file") and hasattr(_caller_info, "line"):
            file_info = f" {GRAY}<{_caller_info.file}:{_caller_info.line}>{RESET}"
        elif record.filename and record.lineno:
            import os

            try:
                rel_path = os.path.relpath(record.filename)
                if not rel_path.startswith(".."):
                    file_info = f" {GRAY}<{rel_path}:{record.lineno}>{RESET}"
                else:
                    file_info = f" {GRAY}<{os.path.basename(record.filename)}:{record.lineno}>{RESET}"
            except (ValueError, TypeError):
                file_info = f" {GRAY}<{os.path.basename(record.filename)}:{record.lineno}>{RESET}"

        # Format timestamp as ISO 8601 with timezone
        timestamp = record.time.strftime("%Y-%m-%dT%H:%M:%S%z")
        # Add colon in timezone offset (e.g., +0530 -> +05:30)
        if len(timestamp) > 2:
            timestamp = timestamp[:-2] + ":" + timestamp[-2:]

        return (
            f"{GRAY}{timestamp}{RESET} "
            f"{level_color}{record.level_name}{RESET}:"
            f"{file_info} "
            f"{CYAN}{record.channel}{RESET}: "
            f"{record.message}"
        )

    return custom_format


def format_data(data: dict[str, Any]) -> str:
    """Format a data dictionary for pretty-printed output.

    Converts a dictionary to indented JSON and adds visual separators
    for multi-line structured data display.

    Args:
        data: Dictionary to format. Can be None or empty.

    Returns:
        Formatted string with 'data=' prefix and visual separators,
        or empty string if data is None/empty.

    Example:
        >>> format_data({"user": "alice", "count": 42})
        '\n  data=\n  │ {\n  │   "user": "alice",\n  │   "count": 42\n  │ }'
    """
    if not data:
        return ""

    import json

    formatted = json.dumps(data, indent=2)
    lines = formatted.split("\n")
    return "\n  data=\n  │ " + "\n  │ ".join(lines)


# The telemetry package's own directory: frames inside it are the SDK, not the
# caller. Resolved from the installed location, so it holds whether the
# package runs from a checkout or from site-packages.
_PACKAGE_DIR = (
    os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    + os.sep
)


@functools.lru_cache(maxsize=1024)
def _display_path(filename: str) -> str:
    """The caller's path relative to the working directory, or its basename
    when it lies outside it. Cached: a program logs from few files."""
    try:
        rel_path = os.path.relpath(filename)
    except (ValueError, TypeError):
        return os.path.basename(filename)
    return os.path.basename(filename) if rel_path.startswith("..") else rel_path


def get_caller_info():
    """
    Identify the external caller's source file and line number.

    Walks frame objects directly: ``inspect.stack()`` built every frame on the
    stack and read source files from disk on each call, which was 70% of the
    cost of a log call.

    Returns:
        tuple[str, int]: The caller's relative path or basename and line number,
        or ``("unknown", 0)`` when no suitable caller is found.
    """
    frame = sys._getframe(2)  # skip this function and the calling log method
    while frame is not None:
        filename = frame.f_code.co_filename
        if not filename.startswith(_PACKAGE_DIR):
            return _display_path(filename), frame.f_lineno
        frame = frame.f_back
    return "unknown", 0

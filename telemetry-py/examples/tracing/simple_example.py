"""
Tracing example using decorators - cleaner API without manual event tracking.

This example uses the new Telemetry.new() builder API and shows how to use
the @traced decorator and TracedOperation context manager for automatic
event tracking.

Run with:
    uv run python -m examples.tracing.simple_example
"""

import telemetry
from telemetry import Telemetry, TracedOperation
from pydantic import BaseModel
import time


class ProcessingRequest(BaseModel):
    """Request for processing"""

    request_id: str
    data: str


class ProcessingResponse(BaseModel):
    """Response from processing"""

    request_id: str
    result: str
    processing_time_ms: float


@telemetry.trace("processing", auto_events=True)
def random_function():
    """
    Simulate a processing operation.
    
    Returns:
    	str: The string `"processed"`.
    """
    time.sleep(0.1)
    return "processed"


def main():
    """
    Run the tracing examples for decorated functions and nested operations.
    """
    # Uses telemetry.toml config for OTLP endpoint and service info
    with Telemetry.new().build() as p:
        p.logger.info("=== Decorator-Based Tracing Example ===")

        # Example 0: Using @telemetry.trace decorator
        p.logger.info("Calling function with @telemetry.trace decorator...")
        result = random_function()
        p.logger.info(f"Function result: {result}")

        # Example 1: Using nested TracedOperation for sub-spans
        with TracedOperation(
            p.tracing, "data_pipeline", {"pipeline.version": "1.0"}
        ) as _:
            with TracedOperation(p.tracing, "loading_data") as _:
                time.sleep(0.05)

            with TracedOperation(p.tracing, "validating_data") as _:
                time.sleep(0.03)

            with TracedOperation(p.tracing, "transforming_data") as _:
                time.sleep(0.08)

            with TracedOperation(p.tracing, "saving_results") as _:
                time.sleep(0.04)

        p.logger.info("✅ Pipeline completed with sub-spans!")

        # Example 2: Nested operations
        with TracedOperation(p.tracing, "ml_inference", {"model": "gpt-4"}) as _:
            with TracedOperation(p.tracing, "loading_model") as _:
                time.sleep(0.02)

            with TracedOperation(p.tracing, "preprocessing_input") as _:
                with TracedOperation(p.tracing, "tokenization") as tokenize_op:
                    tokenize_op.step("splitting_text")
                    time.sleep(0.01)
                    tokenize_op.step("encoding_tokens")
                    time.sleep(0.015)

            with TracedOperation(p.tracing, "running_inference") as _:
                time.sleep(0.15)

            with TracedOperation(p.tracing, "postprocessing_output") as _:
                time.sleep(0.02)

        p.logger.info("✅ ML inference completed with nested tracing!")

        # Give time for spans to export
        time.sleep(2)


if __name__ == "__main__":
    main()

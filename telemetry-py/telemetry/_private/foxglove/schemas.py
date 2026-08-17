"""
Schema loader for MCAP Foxglove schemas.
"""

import json
from pathlib import Path
from typing import Dict


def load_schema(schema_name: str) -> str:
    """Load a JSON schema from the schemas directory"""
    schema_dir = Path(__file__).parent / "schemas"
    schema_path = schema_dir / f"{schema_name}.json"

    with open(schema_path, "r") as f:
        schema_dict = json.load(f)

    return json.dumps(schema_dict)


def get_all_schemas() -> Dict[str, dict]:
    """Load all available schemas"""
    schemas = {}
    schema_dir = Path(__file__).parent / "schemas"

    for schema_file in schema_dir.glob("*.json"):
        schema_name = schema_file.stem
        with open(schema_file, "r") as f:
            schemas[schema_name] = json.load(f)

    return schemas

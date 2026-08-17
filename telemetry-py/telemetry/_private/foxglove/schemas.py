"""
Schema loader for MCAP Foxglove schemas.
"""

import json
from pathlib import Path


def load_schema(schema_name: str) -> str:
    """
    Load a named JSON schema from the package's schemas directory.

    Parameters:
        schema_name (str): Name of the schema file without the `.json` extension.

    Returns:
        str: The schema serialized as a JSON string.
    """
    schema_dir = Path(__file__).parent / "schemas"
    schema_path = schema_dir / f"{schema_name}.json"

    with open(schema_path) as f:
        schema_dict = json.load(f)

    return json.dumps(schema_dict)


def get_all_schemas() -> dict[str, dict]:
    """
    Load all available JSON schemas.

    Returns:
        Dict[str, dict]: A mapping from each schema filename stem to its parsed contents.
    """
    schemas = {}
    schema_dir = Path(__file__).parent / "schemas"

    for schema_file in schema_dir.glob("*.json"):
        schema_name = schema_file.stem
        with open(schema_file) as f:
            schemas[schema_name] = json.load(f)

    return schemas

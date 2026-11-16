"""Counts the messages in an MCAP file by signal: logs, metrics and traces,
by topic prefix (/logs, /metrics, /traces), as every SDK names them.

    uv run --project telemetry-py python bench/mcap_count.py recording.mcap
"""

import json
import sys
from collections import Counter

from mcap.reader import make_reader


def main() -> None:
    counts: Counter[str] = Counter()
    with open(sys.argv[1], "rb") as f:
        for _, channel, _ in make_reader(f).iter_messages():
            counts[channel.topic.strip("/").split("/", 1)[0]] += 1
    print(json.dumps({"logs": counts["logs"], "metrics": counts["metrics"], "traces": counts["traces"]}))


if __name__ == "__main__":
    main()

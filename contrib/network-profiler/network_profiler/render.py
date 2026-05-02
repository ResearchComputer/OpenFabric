from __future__ import annotations

from collections import defaultdict
from pathlib import Path
from typing import Any
import html
import json


def load_records(path: Path) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                records.append(json.loads(line))
    return records


def latest_metric(records: list[dict[str, Any]], kind: str, metric: str) -> dict[tuple[str, str], float]:
    values: dict[tuple[str, str], float] = {}
    for record in records:
        if record.get("kind") != kind or not record.get("ok"):
            continue
        raw_value = record.get("metrics", {}).get(metric)
        if raw_value is None:
            continue
        values[(record["source"], record["target"])] = float(raw_value)
    return values


def machine_order(records: list[dict[str, Any]]) -> list[str]:
    names: set[str] = set()
    for record in records:
        names.add(record["source"])
        names.add(record["target"])
    return sorted(names)


def color(value: float, low: float, high: float, invert: bool) -> str:
    if high <= low:
        ratio = 0.5
    else:
        ratio = (value - low) / (high - low)
    ratio = max(0.0, min(1.0, ratio))
    if invert:
        ratio = 1.0 - ratio
    red = int(230 - 180 * ratio)
    green = int(70 + 140 * ratio)
    blue = int(70 + 40 * ratio)
    return f"rgb({red},{green},{blue})"


def render_heatmap(records_path: Path, output_path: Path, kind: str, metric: str) -> None:
    records = load_records(records_path)
    values = latest_metric(records, kind, metric)
    names = machine_order(records)
    all_values = list(values.values())
    low = min(all_values) if all_values else 0.0
    high = max(all_values) if all_values else 1.0
    invert = kind == "ping"

    failures = defaultdict(int)
    for record in records:
        if record.get("ok"):
            continue
        failures[(record["source"], record["target"], record["kind"])] += 1

    cells = []
    for source in names:
        row = [f"<th>{html.escape(source)}</th>"]
        for target in names:
            if source == target:
                row.append('<td class="self">self</td>')
                continue
            value = values.get((source, target))
            if value is None:
                row.append('<td class="missing">missing</td>')
                continue
            title = html.escape(f"{source} -> {target}: {value:.2f} {metric}")
            row.append(
                f'<td style="background:{color(value, low, high, invert)}" title="{title}">'
                f"{value:.2f}</td>"
            )
        cells.append("<tr>" + "".join(row) + "</tr>")

    header = "".join(f"<th>{html.escape(name)}</th>" for name in names)
    failure_rows = "".join(
        f"<tr><td>{html.escape(src)}</td><td>{html.escape(dst)}</td>"
        f"<td>{html.escape(fail_kind)}</td><td>{count}</td></tr>"
        for (src, dst, fail_kind), count in sorted(failures.items())
    )
    if not failure_rows:
        failure_rows = '<tr><td colspan="4">No failed measurements recorded.</td></tr>'

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Network {html.escape(kind)} heatmap</title>
<style>
body {{ font-family: system-ui, sans-serif; margin: 24px; color: #1f2933; }}
table {{ border-collapse: collapse; margin: 16px 0 28px; }}
th, td {{ border: 1px solid #c9d1d9; padding: 8px 10px; text-align: right; }}
th {{ background: #f6f8fa; text-align: left; }}
td.self {{ background: #eef2f7; color: #677485; text-align: center; }}
td.missing {{ background: #f9fafb; color: #9aa5b1; text-align: center; }}
.meta {{ color: #52606d; }}
</style>
</head>
<body>
<h1>Network {html.escape(kind)} heatmap</h1>
<p class="meta">Metric: {html.escape(metric)}. Lower is better for ping; higher is better for iperf3.</p>
<table>
<thead><tr><th>source \\ target</th>{header}</tr></thead>
<tbody>
{''.join(cells)}
</tbody>
</table>
<h2>Failures</h2>
<table>
<thead><tr><th>source</th><th>target</th><th>kind</th><th>count</th></tr></thead>
<tbody>{failure_rows}</tbody>
</table>
</body>
</html>
""",
        encoding="utf-8",
    )

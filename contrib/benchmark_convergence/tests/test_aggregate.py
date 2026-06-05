from pathlib import Path

import pandas as pd

from benchmark_convergence.aggregate import aggregate_cell, aggregate_run


FIXTURE = Path(__file__).parent / "fixtures" / "results_small.json"


def test_aggregate_cell_schema_and_latency(tmp_path: Path) -> None:
    out = tmp_path / "out.parquet"
    df = aggregate_cell(FIXTURE, output=out)

    expected = {
        "run_id", "N", "rep", "seq",
        "writer_task_id", "observer_task_id",
        "writer_host", "observer_host",
        "t_write_ns", "t_seen_ns",
        "clock_offset_writer_ns", "clock_offset_observer_ns",
        "latency_ns", "status", "flags",
    }
    assert expected.issubset(set(df.columns))

    assert len(df) == 2

    # task 1 (host eu-2): t_seen=1000200000, t_write=1000000000,
    #   O_writer=100 (eu-1), O_observer=200 (eu-2)
    #   latency = 1000200000 - 1000000000 - (200 - 100) = 199900
    row1 = df[df.observer_task_id == 1].iloc[0]
    assert row1.status == "ok"
    assert int(row1.latency_ns) == 199900

    # task 2: TIMEOUT
    row2 = df[df.observer_task_id == 2].iloc[0]
    assert row2.status == "timeout"
    assert pd.isna(row2.latency_ns) or row2.latency_ns is None


def test_aggregate_run_concatenates(tmp_path: Path) -> None:
    run_dir = tmp_path / "run-xyz"
    for rep in (0, 1):
        cell = run_dir / "N=3" / f"rep={rep}"
        cell.mkdir(parents=True)
        (cell / "results.json").write_bytes(FIXTURE.read_bytes())

    df = aggregate_run(run_dir)
    assert len(df) == 4
    parquet_path = run_dir / "results.parquet"
    assert parquet_path.exists()

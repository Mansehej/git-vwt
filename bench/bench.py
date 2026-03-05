#!/usr/bin/env python3

import argparse
import json
import os
import re
import shutil
import statistics
import subprocess
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Tuple


@dataclass
class CmdResult:
    code: int
    stdout: str
    stderr: str
    dur_s: float


def run_cmd(
    argv: List[str],
    *,
    cwd: Optional[Path] = None,
    env: Optional[Dict[str, str]] = None,
    check: bool = False,
    timeout_s: Optional[int] = None,
) -> CmdResult:
    merged_env = os.environ.copy()
    if env:
        merged_env.update(env)

    start = time.monotonic()
    p = subprocess.run(
        argv,
        cwd=str(cwd) if cwd else None,
        env=merged_env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout_s,
    )
    dur = time.monotonic() - start
    res = CmdResult(p.returncode, p.stdout, p.stderr, dur)
    if check and res.code != 0:
        raise RuntimeError(
            "command failed: {}\nexit={}\nstdout:\n{}\nstderr:\n{}".format(
                " ".join(argv), res.code, res.stdout, res.stderr
            )
        )
    return res


def du_bytes(path: Path) -> int:
    # -s summary, -b bytes
    res = run_cmd(["du", "-sb", str(path)], check=True)
    out = res.stdout.strip().split()
    return int(out[0])


def sum_du_bytes(paths: List[Path]) -> int:
    total = 0
    for p in paths:
        total += du_bytes(p)
    return total


def clone_repo(src: Path, dst: Path) -> None:
    # Local clone without hardlinks for accurate disk accounting.
    run_cmd(
        ["git", "clone", "--local", "--no-hardlinks", str(src), str(dst)], check=True
    )


def build_git_vwt(repo: Path) -> None:
    run_cmd(["go", "build", "-o", "git-vwt", "./cmd/git-vwt"], cwd=repo, check=True)


def disk_bench(repo: Path, n: int, vwt_file_kb: int) -> Dict[str, int]:
    base_git_objects = du_bytes(repo / ".git" / "objects")
    base_worktree = du_bytes(repo) - du_bytes(repo / ".git")

    # Worktrees
    wt_root = repo.parent / "worktrees"
    wt_root.mkdir(parents=True, exist_ok=True)
    wt_dirs: List[Path] = []
    for i in range(1, n + 1):
        wt_dir = wt_root / f"wt{i}"
        branch = f"bench-wt-{i}"
        run_cmd(
            ["git", "worktree", "add", str(wt_dir), "-b", branch], cwd=repo, check=True
        )
        wt_dirs.append(wt_dir)
    wt_bytes = sum_du_bytes(wt_dirs)
    wt_meta = (
        du_bytes(repo / ".git" / "worktrees")
        if (repo / ".git" / "worktrees").exists()
        else 0
    )

    # Cleanup worktrees
    for d in wt_dirs:
        run_cmd(["git", "worktree", "remove", "--force", str(d)], cwd=repo, check=True)
    # Leave wt_root; it should be empty after removes.

    # VWT
    payload = ("X" * 1024 + "\n") * max(1, vwt_file_kb)
    payload_file = repo.parent / "vwt_payload.txt"
    payload_file.write_text(payload, encoding="utf-8")
    try:
        for i in range(1, n + 1):
            ws = f"bench-ws-{i}"
            run_cmd(["./git-vwt", "--ws", ws, "open"], cwd=repo, check=True)
            target = f"tmp/bench-disk/ws{i}.txt"
            run_cmd(
                ["./git-vwt", "--ws", ws, "write", target, str(payload_file)],
                cwd=repo,
                check=True,
                timeout_s=120,
            )
    finally:
        try:
            payload_file.unlink()
        except Exception:
            pass

    after_git_objects = du_bytes(repo / ".git" / "objects")
    vwt_objects_delta = max(0, after_git_objects - base_git_objects)

    return {
        "base_worktree_bytes": base_worktree,
        "worktrees_bytes": wt_bytes + wt_meta,
        "worktrees_dirs_bytes": wt_bytes,
        "worktrees_meta_bytes": wt_meta,
        "vwt_git_objects_delta_bytes": vwt_objects_delta,
        "git_objects_before_bytes": base_git_objects,
        "git_objects_after_bytes": after_git_objects,
    }


def parse_session_id(json_lines: str) -> str:
    for line in json_lines.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except Exception:
            continue
        sid = obj.get("sessionID")
        if sid:
            return sid
    raise RuntimeError("could not find sessionID in opencode output")


def opencode_speed_trial(
    repo: Path,
    attach_url: str,
    model: str,
    n_tasks: int,
    trial_idx: int,
) -> Dict[str, float]:
    # Serial: one session, N separate messages.
    serial_dir = f"tmp/bench-speed/serial-{trial_idx}"
    parallel_dir = f"tmp/bench-speed/parallel-{trial_idx}"

    serial_start = time.monotonic()
    init_msg = (
        "Benchmark serial init. "
        f"Create directory {serial_dir} and do nothing else. "
        "Confirm by printing the path."
    )
    res0 = run_cmd(
        [
            "opencode",
            "run",
            "--attach",
            attach_url,
            "--dir",
            str(repo),
            "--format",
            "json",
            "--model",
            model,
            "--agent",
            "build",
            "--title",
            f"bench-serial-{trial_idx}",
            init_msg,
        ],
        check=True,
        timeout_s=600,
    )
    sid = parse_session_id(res0.stdout)
    for i in range(1, n_tasks + 1):
        msg = (
            f"Serial task {i}/{n_tasks}. "
            f"Using apply_patch, add file {serial_dir}/t{i}.txt with exactly two lines: "
            f"SERIAL_TASK_{i} and DONE. "
            "Do not create or modify anything else."
        )
        run_cmd(
            [
                "opencode",
                "run",
                "--attach",
                attach_url,
                "--dir",
                str(repo),
                "--format",
                "json",
                "--model",
                model,
                "--session",
                sid,
                msg,
            ],
            check=True,
            timeout_s=600,
        )
    cleanup_msg = (
        "Serial cleanup. "
        f"Run bash to remove {serial_dir} (rm -rf). "
        "Then run bash `git status --porcelain=v1 -uall` and print it verbatim."
    )
    run_cmd(
        [
            "opencode",
            "run",
            "--attach",
            attach_url,
            "--dir",
            str(repo),
            "--format",
            "json",
            "--model",
            model,
            "--session",
            sid,
            cleanup_msg,
        ],
        check=True,
        timeout_s=600,
    )
    serial_dur = time.monotonic() - serial_start

    # Parallel: one session with N subagents.
    parallel_start = time.monotonic()
    task_specs = " ".join(
        [
            f"Subagent {i}: create {parallel_dir}/t{i}.txt with exactly two lines: PAR_TASK_{i} and DONE."
            for i in range(1, n_tasks + 1)
        ]
    )
    par_msg = (
        "Benchmark parallel run. "
        f"Create directory {parallel_dir}. "
        f"Spawn {n_tasks} general subagents in parallel. {task_specs} "
        "Subagents must not apply. "
        "After all subagents finish, the primary must vwt_apply each subagent session so the files appear in the working tree. "
        "Then cleanup by rm -rf the directory. "
        "Finally print verbatim output of bash `git status --porcelain=v1 -uall`."
    )
    run_cmd(
        [
            "opencode",
            "run",
            "--attach",
            attach_url,
            "--dir",
            str(repo),
            "--format",
            "json",
            "--model",
            model,
            "--agent",
            "build",
            "--title",
            f"bench-parallel-{trial_idx}",
            par_msg,
        ],
        check=True,
        timeout_s=900,
    )
    parallel_dur = time.monotonic() - parallel_start

    return {
        "serial_s": serial_dur,
        "parallel_s": parallel_dur,
        "speedup": (serial_dur / parallel_dur) if parallel_dur > 0 else 0.0,
    }


def start_opencode_server(repo: Path, model: str) -> Tuple[subprocess.Popen, str]:
    # Start headless server and parse port from stderr/stdout.
    env = os.environ.copy()
    env["OPENCODE_VWT"] = "1"

    p = subprocess.Popen(
        ["opencode", "serve", "--port", "0"],
        cwd=str(repo),
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )

    port = None
    start = time.monotonic()
    buf = []
    while time.monotonic() - start < 10:
        line = p.stdout.readline() if p.stdout else ""
        if line:
            buf.append(line)
            m = re.search(r"127\.0\.0\.1:(\d+)", line)
            if m:
                port = int(m.group(1))
                break
    if port is None:
        p.terminate()
        raise RuntimeError("failed to start opencode serve; output:\n" + "".join(buf))

    return p, f"http://127.0.0.1:{port}"


def main() -> int:
    ap = argparse.ArgumentParser(description="Benchmark git-vwt + OpenCode VWT mode")
    ap.add_argument(
        "--repo",
        default=str(Path(__file__).resolve().parents[1]),
        help="source repo path",
    )
    ap.add_argument(
        "--agents", type=int, default=4, help="number of parallel tasks/subagents"
    )
    ap.add_argument("--trials", type=int, default=3, help="number of speed trials")
    ap.add_argument("--model", default="openai/gpt-5.3-codex", help="OpenCode model id")
    ap.add_argument(
        "--vwt-file-kb",
        type=int,
        default=64,
        help="size of per-workspace written file for disk test",
    )
    ap.add_argument(
        "--skip-disk", action="store_true", help="skip disk usage benchmark"
    )
    ap.add_argument("--skip-speed", action="store_true", help="skip speed benchmark")
    args = ap.parse_args()

    src = Path(args.repo).resolve()
    if not (src / ".git").exists():
        raise RuntimeError(f"not a git repo: {src}")

    with tempfile.TemporaryDirectory(prefix="git-vwt-bench-") as td:
        tmp = Path(td)
        bench_repo = tmp / "repo"
        clone_repo(src, bench_repo)
        build_git_vwt(bench_repo)

        report: Dict[str, object] = {
            "timestamp": int(time.time()),
            "agents": args.agents,
            "trials": args.trials,
            "model": args.model,
            "repo": str(src),
            "scratch": str(bench_repo),
        }

        if not args.skip_disk:
            report["disk"] = disk_bench(bench_repo, args.agents, args.vwt_file_kb)

        if not args.skip_speed:
            server, url = start_opencode_server(bench_repo, args.model)
            try:
                trials: List[Dict[str, float]] = []
                for i in range(1, args.trials + 1):
                    trials.append(
                        opencode_speed_trial(
                            bench_repo, url, args.model, args.agents, i
                        )
                    )
                report["speed"] = {
                    "trials": trials,
                    "median_speedup": statistics.median([t["speedup"] for t in trials]),
                    "median_serial_s": statistics.median(
                        [t["serial_s"] for t in trials]
                    ),
                    "median_parallel_s": statistics.median(
                        [t["parallel_s"] for t in trials]
                    ),
                }
            finally:
                server.terminate()
                try:
                    server.wait(timeout=5)
                except Exception:
                    server.kill()

        print(json.dumps(report, indent=2, sort_keys=True))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())

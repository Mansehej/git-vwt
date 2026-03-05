#!/usr/bin/env python3

import argparse
import json
import os
import re
import shutil
import subprocess
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
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
    timeout_s: Optional[int] = None,
    check: bool = False,
) -> CmdResult:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    start = time.monotonic()
    p = subprocess.run(
        argv,
        cwd=str(cwd) if cwd else None,
        env=merged,
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


def du_bytes(path: Path, *, apparent: bool = False) -> int:
    argv = ["du", "-sb"]
    if apparent:
        argv.append("--apparent-size")
    argv.append(str(path))
    res = run_cmd(argv, check=True)
    return int(res.stdout.strip().split()[0])


def clone_repo(src: Path, dst: Path) -> None:
    run_cmd(
        ["git", "clone", "--local", "--no-hardlinks", str(src), str(dst)], check=True
    )


def build_git_vwt(repo: Path) -> None:
    run_cmd(["go", "build", "-o", "git-vwt", "./cmd/git-vwt"], cwd=repo, check=True)


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


def opencode_run_local(
    *,
    repo_dir: Path,
    model: str,
    title: str,
    message: str,
    env: Optional[Dict[str, str]] = None,
    agent: str = "build",
    timeout_s: int = 1800,
) -> Tuple[CmdResult, str]:
    res = run_cmd(
        [
            "opencode",
            "run",
            "--dir",
            str(repo_dir),
            "--format",
            "json",
            "--model",
            model,
            "--agent",
            agent,
            "--title",
            title,
            message,
        ],
        env=env,
        timeout_s=timeout_s,
        check=True,
    )
    return res, parse_session_id(res.stdout)


def validate_webapp(root: Path, bench_dir: str, components: List[str]) -> None:
    base = root / bench_dir
    if not (base / "index.html").exists():
        raise RuntimeError("missing index.html")
    if not (base / "app.js").exists():
        raise RuntimeError("missing app.js")
    if not (base / "styles.css").exists():
        raise RuntimeError("missing styles.css")
    app_txt = (base / "app.js").read_text("utf-8")
    for c in components:
        for suf in [".js", ".css", ".data.json", ".md"]:
            p = base / "components" / f"{c}{suf}"
            if not p.exists():
                raise RuntimeError(f"missing {p}")
        if f"./components/{c}.js" not in app_txt:
            raise RuntimeError(f"app.js missing import for {c}")
        if f"render{c}" not in app_txt:
            raise RuntimeError(f"app.js missing render call for {c}")


def component_prompt(bench_dir: str, component: str) -> str:
    return f"""Create ONLY the following files under `{bench_dir}` (do not touch anything else):
- `{bench_dir}/components/{component}.js` exporting `render{component}(el)`
- `{bench_dir}/components/{component}.css`
- `{bench_dir}/components/{component}.data.json`
- `{bench_dir}/components/{component}.md`

Use apply_patch. Finish by printing exactly: DONE\n"""


def integrate_prompt(bench_dir: str, components: List[str]) -> str:
    comps = ", ".join(components)
    return f"""Integrate the webapp scaffold under `{bench_dir}` using the existing component files.

Rules:
- Do not modify any existing tracked files.
- Only create/update files under `{bench_dir}`.
- Do not spawn subagents.

Do:
1) Create `{bench_dir}/index.html` with `<div id=\"app\"></div>` and `<script type=\"module\" src=\"./app.js\"></script>`.
2) Create `{bench_dir}/styles.css`.
3) Create/update `{bench_dir}/app.js` that imports and calls render for all components in [{comps}] from `./components/<Component>.js`.

Finish by printing exactly: DONE\n"""


def apply_vwt_workspaces(repo: Path, session_ids: List[str]) -> None:
    for sid in session_ids:
        ws = f"opencode-{sid}"
        run_cmd(["./git-vwt", "--ws", ws, "apply"], cwd=repo, check=True, timeout_s=600)


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Benchmark multi-process parallelism: serial vs worktrees vs VWT workspaces"
    )
    ap.add_argument(
        "--repo",
        default=str(Path(__file__).resolve().parents[1]),
        help="source repo path",
    )
    ap.add_argument("--model", default="openai/gpt-5.3-codex")
    ap.add_argument("--components", type=int, default=8)
    ap.add_argument("--timeout", type=int, default=1800)
    ap.add_argument("--workers", type=int, default=8)
    args = ap.parse_args()

    src = Path(args.repo).resolve()
    if not (src / ".git").exists():
        raise RuntimeError(f"not a git repo: {src}")

    names = [
        "Navbar",
        "Hero",
        "Features",
        "Footer",
        "Pricing",
        "FAQ",
        "Gallery",
        "Contact",
        "Team",
        "Blog",
    ]
    components = names[: max(1, min(args.components, len(names)))]

    report: Dict[str, object] = {
        "timestamp": int(time.time()),
        "model": args.model,
        "components": components,
    }

    with tempfile.TemporaryDirectory(prefix="git-vwt-process-bench-") as td:
        tmp = Path(td)
        repo = tmp / "repo"
        clone_repo(src, repo)
        build_git_vwt(repo)

        stress_id = int(time.time())
        bench_dir = f"tmp/bench-webapp-process-{stress_id}"
        bench_path = repo / bench_dir

        disk0 = {
            "git_objects_bytes": du_bytes(repo / ".git" / "objects"),
            "git_objects_apparent_bytes": du_bytes(
                repo / ".git" / "objects", apparent=True
            ),
        }
        report["disk_before"] = disk0

        # Serial (sequential opencode runs, direct filesystem)
        if bench_path.exists():
            shutil.rmtree(bench_path)
        t0 = time.monotonic()
        for c in components:
            opencode_run_local(
                repo_dir=repo,
                model=args.model,
                title=f"bench-serial-{stress_id}-{c}",
                message=component_prompt(bench_dir, c),
                timeout_s=args.timeout,
            )
        opencode_run_local(
            repo_dir=repo,
            model=args.model,
            title=f"bench-serial-{stress_id}-integrate",
            message=integrate_prompt(bench_dir, components),
            timeout_s=args.timeout,
        )
        serial_s = time.monotonic() - t0
        validate_webapp(repo, bench_dir, components)
        shutil.rmtree(bench_path)

        # Worktrees (parallel)
        wt_root = tmp / "worktrees"
        wt_root.mkdir(parents=True, exist_ok=True)
        wt_dirs: List[Path] = []
        for i, _c in enumerate(components, 1):
            wt_dir = wt_root / f"wt{i}"
            branch = f"bench-wt-{stress_id}-{i}"
            run_cmd(
                ["git", "worktree", "add", str(wt_dir), "-b", branch],
                cwd=repo,
                check=True,
            )
            wt_dirs.append(wt_dir)

        wt_disk = {
            "worktrees_dirs_bytes": sum(du_bytes(d) for d in wt_dirs),
            "worktrees_dirs_apparent_bytes": sum(
                du_bytes(d, apparent=True) for d in wt_dirs
            ),
            "worktrees_meta_bytes": du_bytes(repo / ".git" / "worktrees")
            if (repo / ".git" / "worktrees").exists()
            else 0,
        }

        t1 = time.monotonic()
        with ThreadPoolExecutor(max_workers=min(args.workers, len(components))) as ex:
            futs = []
            for wt_dir, c in zip(wt_dirs, components):
                futs.append(
                    ex.submit(
                        opencode_run_local,
                        repo_dir=wt_dir,
                        model=args.model,
                        title=f"bench-wt-{stress_id}-{c}",
                        message=component_prompt(bench_dir, c),
                        timeout_s=args.timeout,
                    )
                )
            for f in as_completed(futs):
                f.result()

        # Copy component outputs into main checkout.
        (repo / bench_dir / "components").mkdir(parents=True, exist_ok=True)
        for wt_dir, c in zip(wt_dirs, components):
            for suf in [".js", ".css", ".data.json", ".md"]:
                srcf = wt_dir / bench_dir / "components" / f"{c}{suf}"
                if not srcf.exists():
                    raise RuntimeError(f"missing {srcf}")
                shutil.copy2(srcf, repo / bench_dir / "components" / srcf.name)

        opencode_run_local(
            repo_dir=repo,
            model=args.model,
            title=f"bench-wt-{stress_id}-integrate",
            message=integrate_prompt(bench_dir, components),
            timeout_s=args.timeout,
        )
        wt_s = time.monotonic() - t1
        validate_webapp(repo, bench_dir, components)
        shutil.rmtree(bench_path)

        for d in wt_dirs:
            run_cmd(
                ["git", "worktree", "remove", "--force", str(d)], cwd=repo, check=True
            )

        # VWT primary isolation (parallel)
        if bench_path.exists():
            shutil.rmtree(bench_path)
        t2 = time.monotonic()
        vwt_env = {"OPENCODE_VWT": "1", "OPENCODE_VWT_PRIMARY": "1"}
        session_ids: List[str] = []
        with ThreadPoolExecutor(max_workers=min(args.workers, len(components))) as ex:
            futs2 = []
            for c in components:
                futs2.append(
                    ex.submit(
                        opencode_run_local,
                        repo_dir=repo,
                        model=args.model,
                        title=f"bench-vwtproc-{stress_id}-{c}",
                        message=component_prompt(bench_dir, c),
                        env=vwt_env,
                        timeout_s=args.timeout,
                    )
                )
            for f in as_completed(futs2):
                res, sid = f.result()
                session_ids.append(sid)

        # Apply all workspaces into working tree.
        apply_vwt_workspaces(repo, session_ids)

        # Integrate normally in working tree.
        opencode_run_local(
            repo_dir=repo,
            model=args.model,
            title=f"bench-vwtproc-{stress_id}-integrate",
            message=integrate_prompt(bench_dir, components),
            timeout_s=args.timeout,
        )
        vwtproc_s = time.monotonic() - t2
        validate_webapp(repo, bench_dir, components)
        shutil.rmtree(bench_path)

        disk1 = {
            "git_objects_bytes": du_bytes(repo / ".git" / "objects"),
            "git_objects_apparent_bytes": du_bytes(
                repo / ".git" / "objects", apparent=True
            ),
        }
        report["disk_after"] = disk1

        report["results"] = {
            "serial_s": serial_s,
            "worktree_parallel_s": wt_s,
            "vwt_process_parallel_s": vwtproc_s,
            "speedup_worktree_vs_serial": (serial_s / wt_s) if wt_s > 0 else 0.0,
            "speedup_vwtproc_vs_serial": (serial_s / vwtproc_s)
            if vwtproc_s > 0
            else 0.0,
            "disk_worktrees": wt_disk,
            "disk_vwt_git_objects_delta_bytes": max(
                0, disk1["git_objects_bytes"] - disk0["git_objects_bytes"]
            ),
            "disk_vwt_git_objects_delta_apparent_bytes": max(
                0,
                disk1["git_objects_apparent_bytes"]
                - disk0["git_objects_apparent_bytes"],
            ),
        }

    print(json.dumps(report, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

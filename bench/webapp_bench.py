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


def start_opencode_server(
    repo: Path, *, env: Dict[str, str]
) -> Tuple[subprocess.Popen, str]:
    merged = os.environ.copy()
    merged.update(env)
    p = subprocess.Popen(
        ["opencode", "serve", "--port", "0"],
        cwd=str(repo),
        env=merged,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )

    port = None
    buf: List[str] = []
    start = time.monotonic()
    while time.monotonic() - start < 15:
        line = p.stdout.readline() if p.stdout else ""
        if not line:
            continue
        buf.append(line)
        m = re.search(r"127\.0\.0\.1:(\d+)", line)
        if m:
            port = int(m.group(1))
            break
    if port is None:
        p.terminate()
        raise RuntimeError("failed to start opencode serve; output:\n" + "".join(buf))
    return p, f"http://127.0.0.1:{port}"


def opencode_run(
    *,
    attach_url: str,
    repo_dir: Path,
    model: str,
    title: str,
    message: str,
    session: Optional[str] = None,
    agent: str = "build",
    timeout_s: int = 1200,
) -> CmdResult:
    argv = [
        "opencode",
        "run",
        "--attach",
        attach_url,
        "--dir",
        str(repo_dir),
        "--format",
        "json",
        "--model",
        model,
    ]
    if session:
        argv += ["--session", session]
    else:
        argv += ["--agent", agent, "--title", title]
    argv.append(message)
    return run_cmd(argv, check=True, timeout_s=timeout_s)


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


def validate_webapp(root: Path, bench_dir: str, components: List[str]) -> None:
    base = root / bench_dir
    index = base / "index.html"
    app = base / "app.js"
    styles = base / "styles.css"
    if not index.exists():
        raise RuntimeError(f"missing {index}")
    if not app.exists():
        raise RuntimeError(f"missing {app}")
    if not styles.exists():
        raise RuntimeError(f"missing {styles}")

    index_txt = index.read_text("utf-8")
    if "app.js" not in index_txt:
        raise RuntimeError("index.html missing app.js reference")
    if 'id="app"' not in index_txt and "id='app'" not in index_txt:
        raise RuntimeError("index.html missing #app")

    app_txt = app.read_text("utf-8")
    for c in components:
        js = base / "components" / f"{c}.js"
        css = base / "components" / f"{c}.css"
        data = base / "components" / f"{c}.data.json"
        doc = base / "components" / f"{c}.md"
        if not js.exists():
            raise RuntimeError(f"missing {js}")
        if not css.exists():
            raise RuntimeError(f"missing {css}")
        if not data.exists():
            raise RuntimeError(f"missing {data}")
        if not doc.exists():
            raise RuntimeError(f"missing {doc}")
        if f"./components/{c}.js" not in app_txt:
            raise RuntimeError(f"app.js missing import for {c}")
        if f"render{c}" not in app_txt:
            raise RuntimeError(f"app.js missing render call for {c}")


def serial_prompt(bench_dir: str, components: List[str]) -> str:
    comps = ", ".join(components)
    return f"""You are generating a tiny static webapp scaffold under `{bench_dir}`.

Rules:
- Do not modify any existing tracked files.
- Only create/update files under `{bench_dir}`.
- Do not spawn subagents.

Requirements:
1) Create `{bench_dir}/index.html` with a `<div id=\"app\"></div>` and a `<script type=\"module\" src=\"./app.js\"></script>`.
2) Create `{bench_dir}/styles.css` with some basic styling.
3) For each component in [{comps}], create:
   - `{bench_dir}/components/<Component>.js` exporting `render<Component>(el)` that fills `el.innerHTML` with some HTML.
   - `{bench_dir}/components/<Component>.css` with component-specific styles.
   - `{bench_dir}/components/<Component>.data.json` with a small JSON object used by the component (dummy data is fine).
   - `{bench_dir}/components/<Component>.md` short docs for the component.
4) Create `{bench_dir}/app.js` that imports all components from `./components/<Component>.js`, creates mount points, and calls each `render<Component>`.

Use apply_patch to create/update files. Finish by printing exactly: DONE\n"""


def serial_init_prompt(bench_dir: str) -> str:
    return f"""Serial benchmark init.

Rules:
- Do not modify any existing tracked files.
- Only create/update files under `{bench_dir}`.
- Do not spawn subagents.

Do:
1) Create `{bench_dir}/index.html` with `<div id=\"app\"></div>` and `<script type=\"module\" src=\"./app.js\"></script>`.
2) Create `{bench_dir}/styles.css`.
3) Create a placeholder `{bench_dir}/app.js` (it can be minimal for now).

Finish by printing exactly: DONE\n"""


def serial_component_prompt(bench_dir: str, component: str) -> str:
    return f"""Serial benchmark component step.

Rules:
- Do not modify any existing tracked files.
- Only create/update files under `{bench_dir}`.
- Do not spawn subagents.

Create ONLY:
- `{bench_dir}/components/{component}.js` exporting `render{component}(el)` that fills `el.innerHTML`.
- `{bench_dir}/components/{component}.css`.
- `{bench_dir}/components/{component}.data.json`.
- `{bench_dir}/components/{component}.md`.

Use apply_patch. Finish by printing exactly: DONE\n"""


def serial_integrate_prompt(bench_dir: str, components: List[str]) -> str:
    comps = ", ".join(components)
    return f"""Serial benchmark integrate step.

Rules:
- Do not modify any existing tracked files.
- Only create/update files under `{bench_dir}`.
- Do not spawn subagents.

Update `{bench_dir}/app.js` to import and call render for all components in [{comps}] from `./components/<Component>.js`.

Finish by printing exactly: DONE\n"""


def vwt_parallel_prompt(bench_dir: str, components: List[str]) -> str:
    comps = ", ".join(components)
    return f"""We are benchmarking OPENCODE_VWT=1 subagent isolation.

Rules:
- Do not modify any existing tracked files.
- Only create/update files under `{bench_dir}`.
- Use subagents to build component files in parallel.
- Do not ask the user to apply; apply subagent workspaces using vwt_apply.

Plan:
1) In the primary session, create `{bench_dir}/index.html`, `{bench_dir}/styles.css`, and `{bench_dir}/app.js` skeleton (imports can be added later).
2) Spawn one general subagent per component in [{comps}]. Each subagent must ONLY create:
   - `{bench_dir}/components/<Component>.js` exporting `render<Component>(el)`.
   - `{bench_dir}/components/<Component>.css`.
   - `{bench_dir}/components/<Component>.data.json`.
   - `{bench_dir}/components/<Component>.md`.
   Subagents must not modify any other files.
3) When subagents finish, apply each subagent workspace from the primary using vwt_apply(sessionID=...).
4) Update `{bench_dir}/app.js` in the primary to import all components and call `render<Component>` for each.
5) Print exactly: DONE\n"""


def worktree_component_prompt(bench_dir: str, component: str) -> str:
    return f"""Create ONLY the following files under `{bench_dir}` (do not touch anything else):
 - `{bench_dir}/components/{component}.js` exporting `render{component}(el)`
 - `{bench_dir}/components/{component}.css`
 - `{bench_dir}/components/{component}.data.json`
 - `{bench_dir}/components/{component}.md`

Use apply_patch. Finish by printing exactly: DONE\n"""


def worktree_integration_prompt(bench_dir: str, components: List[str]) -> str:
    comps = ", ".join(components)
    return f"""In the primary working directory, generate the webapp scaffold under `{bench_dir}` using the existing component files.

Rules:
- Do not modify any existing tracked files.
- Only create/update files under `{bench_dir}`.
- Do not spawn subagents.

Requirements:
1) Create `{bench_dir}/index.html` with `<div id=\"app\"></div>` and `<script type=\"module\" src=\"./app.js\"></script>`.
2) Create `{bench_dir}/styles.css`.
3) Create `{bench_dir}/app.js` that imports and calls render for all components in [{comps}] from `./components/<Component>.js`.

Finish by printing exactly: DONE\n"""


def ensure_clean_dir(p: Path) -> None:
    if p.exists():
        shutil.rmtree(p)
    p.mkdir(parents=True, exist_ok=True)


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Webapp benchmark: serial vs worktrees vs VWT subagents"
    )
    ap.add_argument(
        "--repo",
        default=str(Path(__file__).resolve().parents[1]),
        help="source repo path",
    )
    ap.add_argument("--model", default="openai/gpt-5.3-codex", help="OpenCode model id")
    ap.add_argument("--components", type=int, default=4, help="number of components")
    ap.add_argument(
        "--timeout", type=int, default=1800, help="per opencode run timeout (seconds)"
    )
    args = ap.parse_args()

    src = Path(args.repo).resolve()
    if not (src / ".git").exists():
        raise RuntimeError(f"not a git repo: {src}")

    all_components = [
        "Navbar",
        "Hero",
        "Features",
        "Footer",
        "Pricing",
        "FAQ",
        "Gallery",
        "Contact",
    ]
    components = all_components[: max(1, min(args.components, len(all_components)))]

    report: Dict[str, object] = {
        "timestamp": int(time.time()),
        "model": args.model,
        "components": components,
    }

    with tempfile.TemporaryDirectory(prefix="git-vwt-webapp-bench-") as td:
        tmp = Path(td)
        repo = tmp / "repo"
        clone_repo(src, repo)
        build_git_vwt(repo)

        server, url = start_opencode_server(repo, env={"OPENCODE_VWT": "1"})
        try:
            stress_id = int(time.time())
            bench_dir = f"tmp/bench-webapp-{stress_id}"
            bench_path = repo / bench_dir

            # Baseline disk usage.
            disk0 = {
                "git_objects_bytes": du_bytes(repo / ".git" / "objects"),
                "git_objects_apparent_bytes": du_bytes(
                    repo / ".git" / "objects", apparent=True
                ),
            }
            report["disk_before"] = disk0

            # Serial
            if bench_path.exists():
                shutil.rmtree(bench_path)
            t0 = time.monotonic()
            res_serial0 = opencode_run(
                attach_url=url,
                repo_dir=repo,
                model=args.model,
                title=f"bench-serial-{stress_id}",
                message=serial_init_prompt(bench_dir),
                timeout_s=args.timeout,
            )
            serial_sid = parse_session_id(res_serial0.stdout)
            for c in components:
                opencode_run(
                    attach_url=url,
                    repo_dir=repo,
                    model=args.model,
                    title=f"bench-serial-{stress_id}",
                    session=serial_sid,
                    message=serial_component_prompt(bench_dir, c),
                    timeout_s=args.timeout,
                )
            opencode_run(
                attach_url=url,
                repo_dir=repo,
                model=args.model,
                title=f"bench-serial-{stress_id}",
                session=serial_sid,
                message=serial_integrate_prompt(bench_dir, components),
                timeout_s=args.timeout,
            )
            serial_s = time.monotonic() - t0
            validate_webapp(repo, bench_dir, components)
            shutil.rmtree(bench_path)

            # VWT parallel
            if bench_path.exists():
                shutil.rmtree(bench_path)
            t1 = time.monotonic()
            opencode_run(
                attach_url=url,
                repo_dir=repo,
                model=args.model,
                title=f"bench-vwt-{stress_id}",
                message=vwt_parallel_prompt(bench_dir, components),
                timeout_s=args.timeout,
            )
            vwt_s = time.monotonic() - t1
            validate_webapp(repo, bench_dir, components)
            shutil.rmtree(bench_path)

            # Worktrees parallel
            wt_root = tmp / "worktrees"
            wt_root.mkdir(parents=True, exist_ok=True)
            wt_dirs: List[Path] = []
            for i, c in enumerate(components, 1):
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

            t2 = time.monotonic()
            futures = []
            with ThreadPoolExecutor(max_workers=len(components)) as ex:
                for wt_dir, c in zip(wt_dirs, components):
                    futures.append(
                        ex.submit(
                            opencode_run,
                            attach_url=url,
                            repo_dir=wt_dir,
                            model=args.model,
                            title=f"bench-wt-{c}-{stress_id}",
                            message=worktree_component_prompt(bench_dir, c),
                            timeout_s=args.timeout,
                        )
                    )
                for f in as_completed(futures):
                    f.result()

            # Copy component outputs into main checkout.
            (repo / bench_dir / "components").mkdir(parents=True, exist_ok=True)
            for wt_dir, c in zip(wt_dirs, components):
                src_js = wt_dir / bench_dir / "components" / f"{c}.js"
                src_css = wt_dir / bench_dir / "components" / f"{c}.css"
                src_data = wt_dir / bench_dir / "components" / f"{c}.data.json"
                src_doc = wt_dir / bench_dir / "components" / f"{c}.md"
                if (
                    not src_js.exists()
                    or not src_css.exists()
                    or not src_data.exists()
                    or not src_doc.exists()
                ):
                    raise RuntimeError(f"missing component output in {wt_dir} for {c}")
                shutil.copy2(src_js, repo / bench_dir / "components" / src_js.name)
                shutil.copy2(src_css, repo / bench_dir / "components" / src_css.name)
                shutil.copy2(src_data, repo / bench_dir / "components" / src_data.name)
                shutil.copy2(src_doc, repo / bench_dir / "components" / src_doc.name)

            opencode_run(
                attach_url=url,
                repo_dir=repo,
                model=args.model,
                title=f"bench-wt-integrate-{stress_id}",
                message=worktree_integration_prompt(bench_dir, components),
                timeout_s=args.timeout,
            )
            wt_s = time.monotonic() - t2
            validate_webapp(repo, bench_dir, components)
            shutil.rmtree(bench_path)

            # Cleanup worktrees.
            for d in wt_dirs:
                run_cmd(
                    ["git", "worktree", "remove", "--force", str(d)],
                    cwd=repo,
                    check=True,
                )

            # Disk after
            disk1 = {
                "git_objects_bytes": du_bytes(repo / ".git" / "objects"),
                "git_objects_apparent_bytes": du_bytes(
                    repo / ".git" / "objects", apparent=True
                ),
            }
            report["disk_after"] = disk1

            report["results"] = {
                "serial_s": serial_s,
                "vwt_parallel_s": vwt_s,
                "worktree_parallel_s": wt_s,
                "speedup_vwt_vs_serial": (serial_s / vwt_s) if vwt_s > 0 else 0.0,
                "speedup_worktree_vs_serial": (serial_s / wt_s) if wt_s > 0 else 0.0,
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

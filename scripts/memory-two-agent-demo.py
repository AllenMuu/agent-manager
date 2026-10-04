#!/usr/bin/env python3
"""Opt-in native acceptance; never starts services, installs, logs in, or retries writes.

Requires an already running pinned Mem0 service and two trusted access files.
Each invocation uses a fresh task directory; all native output is private evidence.
A blocked native run stays blocked. This harness does not substitute another model.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys

TOOLS = ["memory_get", "memory_search", "memory_retained_history", "memory_status",
         "memory_preview_weak", "memory_confirm_weak"]
PIN = "94c3fe9f238f3dbf29c9ce98643bd71eb13077cd"
SOURCE = "https://github.com/AllenMuu/agent-manager/issues/23"
EVIDENCE = "https://github.com/AllenMuu/agent-manager/blob/main/docs/adr/0006-agent-manager-separates-resources-adapters-and-providers.md"


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")
    path.chmod(0o600)


def command_output(argv, cwd=None):
    return subprocess.check_output(argv, cwd=cwd, text=True, timeout=30).strip()


def tool_events(path, agent):
    """Pair actual native tool events with native returned content, not final prose."""
    completed = []
    pending = {}
    for line in path.read_text().splitlines():
        event = json.loads(line)
        if agent == "codex":
            item = event.get("item", {})
            if event.get("type") == "item.completed" and item.get("type") == "mcp_tool_call":
                if item.get("server") != "shared_memory":
                    raise RuntimeError("unrelated native MCP tool observed")
                content = (item.get("result") or {}).get("content", [])
                completed.append((item["tool"], content, item.get("status") != "completed"))
            if item.get("type") in ["command_execution", "file_change"]:
                raise RuntimeError("unrelated native builtin action observed")
        else:
            if event.get("type") == "system" and event.get("subtype") == "init":
                if event.get("plugins") or event.get("skills"):
                    raise RuntimeError("unexpected native plugin or Skill inventory")
                if any(not name.startswith("mcp__shared_memory__") for name in event.get("tools", [])):
                    raise RuntimeError("unexpected native builtin inventory")
            if "hook" in event.get("subtype", ""):
                raise RuntimeError("unexpected native hook activity")
            for part in event.get("message", {}).get("content", []):
                if part.get("type") == "tool_use":
                    name = part["name"]
                    if not name.startswith("mcp__shared_memory__"):
                        raise RuntimeError("unrelated native tool observed")
                    pending[part["id"]] = name.removeprefix("mcp__shared_memory__")
                elif part.get("type") == "tool_result":
                    name = pending.pop(part["tool_use_id"])
                    content = part.get("content", [])
                    if isinstance(content, str):
                        content = [{"type": "text", "text": content}]
                    completed.append((name, content, bool(part.get("is_error"))))
    parsed = []
    for name, content, failed in completed:
        if name not in TOOLS:
            raise RuntimeError("unrecognized native tool")
        texts = [part.get("text", "") for part in content if part.get("type") == "text"]
        value = None
        for text in texts:
            try:
                value = json.loads(text)
                break
            except (ValueError, TypeError):
                value = text
        parsed.append((name, value, failed))
    return parsed


def run_stage(options, root, stage, agent, access, prompt, tools=TOOLS):
    event_path = root / (stage + "-server-events.jsonl")
    server_args = ["memory", "serve-mcp", "--access", str(access), "--events", str(event_path)]
    if agent == "codex":
        argv = ["codex", "exec", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check",
                "--json", "--sandbox", "read-only", "-C", str(root / "scratch"),
                "-c", "features.memories=false", "-c", "features.shell_tool=false",
                "-c", "features.skip_host_skill_discovery=true", "-c", 'approval_policy="never"',
                "-c", "mcp_servers.shared_memory.command=" + json.dumps(str(options.binary)),
                "-c", "mcp_servers.shared_memory.args=" + json.dumps(server_args),
                "-c", "mcp_servers.shared_memory.enabled_tools=" + json.dumps(tools)]
        for tool in tools:
            argv += ["-c", "mcp_servers.shared_memory.tools." + tool + '.approval_mode="approve"']
    else:
        config = root / (stage + "-mcp.json")
        write_json(config, {"mcpServers": {"shared_memory": {"command": str(options.binary), "args": server_args}}})
        argv = ["claude", "-p", "--strict-mcp-config", "--mcp-config", str(config),
                "--tools", "", "--allowedTools", ",".join("mcp__shared_memory__" + t for t in tools),
                "--setting-sources", "user", "--settings", str(root / "claude-settings.json"),
                "--include-hook-events", "--no-session-persistence", "--disable-slash-commands",
                "--no-chrome", "--output-format", "stream-json", "--verbose"]
    env = os.environ.copy()
    if agent == "claude-code":
        env["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] = "1"
    write_json(root / (stage + "-settings.json"), {"argv": argv, "taskBackendSecretsExported": False})
    (root / (stage + "-prompt.txt")).write_text(prompt)
    native = root / (stage + "-native.jsonl")
    with native.open("w") as output, (root / (stage + "-stderr.log")).open("w") as error:
        process = subprocess.Popen(argv + [prompt], stdout=output, stderr=error, env=env,
                                   cwd=root / "scratch", start_new_session=True)
        try:
            code = process.wait(timeout=options.timeout)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGTERM)
            process.wait(timeout=10)
            code = 124
    native.chmod(0o600)
    write_json(root / (stage + "-exit.json"), {"exitCode": code})
    if code != 0:
        raise RuntimeError(stage + ": native runtime blocked; inspect private logs (no retry or auth changes performed)")
    tools_called = tool_events(native, agent)
    if not tools_called or not event_path.exists():
        raise RuntimeError(stage + ": no paired native tool/server evidence")
    server_events = [json.loads(line) for line in event_path.read_text().splitlines()]
    if not any(e.get("tool") == "initialized" and e.get("protocol") for e in server_events):
        raise RuntimeError(stage + ": actual negotiated protocol observation missing")
    if any(e.get("agent") != agent for e in server_events):
        raise RuntimeError(stage + ": trusted server identity mismatch")
    counts = {name: sum(e.get("tool") == name for e in server_events) for name in tools}
    for name, _, _ in tools_called:
        if counts.get(name, 0) < 1:
            raise RuntimeError(stage + ": native/server call pairing missing")
        counts[name] -= 1
    return tools_called


def returned(events, tool):
    return [value for name, value, failed in events if name == tool and not failed]


def require_record(events, record_id, version, content, history=False):
    candidates = returned(events, "memory_retained_history" if history else "memory_get")
    records = [r for value in candidates for r in (value if isinstance(value, list) else [value])]
    if not any(isinstance(r, dict) and r.get("id") == record_id and r.get("version") == version
               and r.get("content") == content and r.get("source") == SOURCE
               and r.get("evidence") == [EVIDENCE]
               and r.get("state") == ("SUPERSEDED" if history else "ACTIVE") for r in records):
        raise RuntimeError("required canonical record/provenance missing from actual native tool results")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ["binary", "codex-access", "claude-access", "output", "mem0-source", "mem0-python"]:
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--mem0-pid", type=int, required=True)
    parser.add_argument("--timeout", type=int, default=180)
    options = parser.parse_args()
    if not 30 <= options.timeout <= 300:
        parser.error("timeout must be 30..300 seconds")
    root = options.output.resolve()
    root.mkdir(mode=0o700)  # Existing evidence is never overwritten or replayed.
    (root / "scratch").mkdir(mode=0o700)
    status = {"state": "blocked", "nativeAcceptance": False}
    try:
        configs = [json.loads(path.read_text()) for path in [options.codex_access, options.claude_access]]
        owner = configs[0]["owner"]
        if owner != configs[1]["owner"] or owner.get("kind") != "PROJECT":
            raise RuntimeError("native integrations must share one exact trusted PROJECT owner")
        if [c["agent"] for c in configs] != ["codex", "claude-code"] or not all(c.get("allowWeakWrites") for c in configs):
            raise RuntimeError("two explicit trusted weak writers required")
        if not all(c["provider"]["provider"] == "mem0" for c in configs):
            raise RuntimeError("real Mem0 required; local provider cannot satisfy acceptance")
        commit = command_output(["git", "rev-parse", "HEAD"], options.mem0_source)
        version = command_output([str(options.mem0_python), "-c", "import importlib.metadata;print(importlib.metadata.version('mem0ai'))"])
        process = command_output(["ps", "-p", str(options.mem0_pid), "-o", "command="])
        if commit != PIN or version != "2.2.1" or str(options.mem0_python) not in process or "uvicorn" not in process:
            raise RuntimeError("actual running Mem0 process/source/package pin not established")
        write_json(root / "environment.json", {"mem0Commit": commit, "mem0PythonPackage": version,
                    "mem0Process": process, "codexVersion": command_output(["codex", "--version"]),
                    "claudeVersion": command_output(["claude", "--version"]),
                    "binarySHA256": hashlib.sha256(options.binary.read_bytes()).hexdigest(),
                    "runtimeVersionDetection": "independent process/source/package proof; not OpenAPI version"})
        user_settings = Path.home() / ".claude" / "settings.json"
        plugins = json.loads(user_settings.read_text()).get("enabledPlugins", {}) if user_settings.exists() else {}
        write_json(root / "claude-settings.json", {"disableAllHooks": True, "enabledPlugins": {name: False for name in plugins}})
        first = "M5 acceptance: run standard Go tests before delivery"
        second = "M5 acceptance: run standard Go tests and race checks before delivery"
        record = {"owner": owner, "type": "CONSTRAINT", "content": first, "source": SOURCE, "evidence": [EVIDENCE], "layer": "ATOMIC"}
        common = "Use only shared_memory MCP tools. Explicitly authorized synthetic acceptance. Never retry uncertain writes, run builtin tools, or write private memory. "
        create = run_stage(options, root, "codex-create", "codex", options.codex_access,
                  common + "Call memory_status; preview operation add record " + json.dumps(record)
                  + "; inspect weak semantics, confirm exact returned intentId/owner/source with confirmed=true once, and memory_get the created ID.")
        added = returned(create, "memory_confirm_weak")
        if len(added) != 1 or not isinstance(added[0], dict):
            raise RuntimeError("exactly one actual Codex confirmed creation required")
        record_id = added[0]["id"]
        require_record(create, record_id, 1, first)
        read_prompt = common + "Call memory_get id " + record_id + " owner " + json.dumps(owner) + "; memory_search same owner query M5 acceptance type CONSTRAINT; memory_status."
        read = run_stage(options, root, "claude-read-v1", "claude-code", options.claude_access, read_prompt + " Attempt memory_get same ID with owner {kind:PROJECT,projectId:unauthorized-project}; verify ownership-denied without records.")
        require_record(read, record_id, 1, first)
        if not any(name == "memory_get" and value == "ownership-denied" for name, value, _ in read):
            raise RuntimeError("actual Claude cross-project rejection missing")
        record["content"] = second
        update = run_stage(options, root, "claude-update", "claude-code", options.claude_access,
                 common + "Preview operation replace-observed-weak id " + record_id + " observedVersion 1 record " + json.dumps(record)
                 + "; inspect lack of CAS/receipts, confirm exact intentId/owner/source once; memory_get same ID; memory_retained_history same ID; memory_search same owner query M5 acceptance; memory_status.")
        require_record(update, record_id, 2, second)
        require_record(update, record_id, 1, first, history=True)
        final = run_stage(options, root, "codex-reread", "codex", options.codex_access,
                 read_prompt + " Also memory_retained_history same ID. Attempt memory_get same ID with owner {kind:PROJECT,projectId:unauthorized-project}; verify ownership-denied without records.")
        require_record(final, record_id, 2, second)
        require_record(final, record_id, 1, first, history=True)
        if not any(name == "memory_get" and value == "ownership-denied" for name, value, _ in final):
            raise RuntimeError("actual native cross-project rejection missing")
        for stage in [update, final]:
            if not any(isinstance(value, list) and any(r.get("id") == record_id and r.get("version") == 2 for r in value)
                       and all(r.get("state") == "ACTIVE" for r in value) for value in returned(stage, "memory_search")):
                raise RuntimeError("actual native effective recall missing")
        readonly = root / "claude-readonly-access.json"
        configs[1]["allowWeakWrites"] = False
        write_json(readonly, configs[1])
        ro = run_stage(options, root, "claude-readonly", "claude-code", readonly, common + "Call memory_status only; verify write=false, read=true, search=true.", TOOLS[:4])
        if not any(isinstance(v, dict) and v.get("write") is False and v.get("read") is True for v in returned(ro, "memory_status")):
            raise RuntimeError("native configured read-only status missing")
        status = {"state": "passed", "nativeAcceptance": True, "neutralID": record_id,
                  "effectiveVersion": 2, "canonicalSource": "Mem0 only", "serverEvidence": "metadata-only",
                  "liveEvidence": "actual paired native tool results; no source/config label inference"}
    except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
        status["reason"] = str(error) if isinstance(error, RuntimeError) else "demo setup failed; inspect task-owned files"
    write_json(root / "result.json", status)
    print(json.dumps(status))
    return 0 if status["nativeAcceptance"] else 1


if __name__ == "__main__":
    sys.exit(main())

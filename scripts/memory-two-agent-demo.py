#!/usr/bin/env python3
"""Opt-in native acceptance; never starts services, installs, logs in, or retries writes.

Requires an already running pinned Mem0 service and two trusted access files.
Each invocation uses a fresh task directory; all native output is private evidence.
A blocked native run stays blocked. This harness does not substitute another model.
"""
import argparse
import ctypes
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
from urllib.parse import urlsplit

TOOLS = ["memory_get", "memory_search", "memory_retained_history", "memory_status",
         "memory_preview_weak", "memory_confirm_weak"]
NATIVE_ENVIRONMENT_KEYS = ("PATH", "HOME", "CODEX_HOME", "TMPDIR", "TEMP", "TMP", "LANG",
                           "LC_ALL", "LC_CTYPE", "USER", "LOGNAME", "TERM", "TZ")
PIN = "94c3fe9f238f3dbf29c9ce98643bd71eb13077cd"
MEM0_CONTRACT = "mem0-oss-python-2.2.1-" + PIN
SYNTHETIC_PROJECT_PREFIX = "issue23-"
SOURCE = "https://github.com/AllenMuu/agent-manager/issues/23"
EVIDENCE = "https://github.com/AllenMuu/agent-manager/blob/main/docs/adr/0006-agent-manager-separates-resources-adapters-and-providers.md"


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")
    path.chmod(0o600)


def command_output(argv, cwd=None):
    return subprocess.check_output(argv, cwd=cwd, text=True, timeout=30).strip()


def native_agent_environment():
    """Keep native profile lookup while avoiding accidental API-key export."""
    return {key: os.environ[key] for key in NATIVE_ENVIRONMENT_KEYS if key in os.environ}


def validate_synthetic_project_owner(owner):
    project_id = owner.get("projectId") if isinstance(owner, dict) else None
    if (not isinstance(owner, dict) or owner.get("kind") != "PROJECT"
            or not isinstance(project_id, str)
            or not project_id.startswith(SYNTHETIC_PROJECT_PREFIX)
            or len(project_id) == len(SYNTHETIC_PROJECT_PREFIX)):
        raise RuntimeError("native acceptance requires a synthetic ISSUE23 project ID beginning with 'issue23-'")


def configured_mem0_endpoint(access):
    if not isinstance(access, dict):
        raise RuntimeError("invalid trusted access configuration")
    provider = access.get("provider") or {}
    if not isinstance(provider, dict):
        raise RuntimeError("invalid Memory provider configuration")
    reference = provider.get("configuration") or {}
    if not isinstance(reference, dict):
        raise RuntimeError("invalid Memory configuration reference")
    if provider.get("provider") != "mem0" or reference.get("kind") != "file":
        raise RuntimeError("acceptance requires a file-backed Mem0 configuration")
    name = reference.get("name")
    if not isinstance(name, str) or not Path(name).is_absolute():
        raise RuntimeError("Mem0 configuration reference must be an absolute file path")
    path = Path(name)
    try:
        before = path.lstat()
        if not stat.S_ISREG(before.st_mode):
            raise RuntimeError("Mem0 configuration must be a regular file")
        with path.open("rb") as source:
            after = os.fstat(source.fileno())
            if not stat.S_ISREG(after.st_mode) or (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
                raise RuntimeError("Mem0 configuration changed while being read")
            raw = source.read(65537)
        if len(raw) > 65536:
            raise RuntimeError("Mem0 configuration exceeds the bounded size")
        config = json.loads(raw)
    except (OSError, ValueError, TypeError) as error:
        raise RuntimeError("Mem0 configuration could not be read") from error
    if not isinstance(config, dict) or config.get("contract") != MEM0_CONTRACT or config.get("allowNetwork") is not True:
        raise RuntimeError("Mem0 configuration does not select the pinned network contract")
    endpoint = config.get("endpoint")
    try:
        parsed = urlsplit(endpoint)
        host = str(ipaddress.ip_address(parsed.hostname or ""))
        port = parsed.port
        if (parsed.scheme not in ("http", "https") or not ipaddress.ip_address(host).is_loopback
                or parsed.username is not None or parsed.password is not None
                or parsed.path not in ("", "/") or parsed.query or parsed.fragment or port is None):
            raise ValueError("endpoint must be a numeric loopback URL with an explicit port")
    except (AttributeError, ValueError) as error:
        raise RuntimeError("Mem0 acceptance endpoint must be an explicit numeric loopback URL") from error
    authority = f"[{host}]:{port}" if ":" in host else f"{host}:{port}"
    return f"{parsed.scheme}://{authority}"


def shared_mem0_endpoint(configs):
    if len(configs) != 2:
        raise RuntimeError("exact Codex and Pi Mem0 configurations required")
    endpoints = [configured_mem0_endpoint(config) for config in configs]
    if endpoints[0] != endpoints[1]:
        raise RuntimeError("Codex and Pi must use the same pinned Mem0 endpoint")
    return endpoints[0]


def mem0_runtime_imports(python, app_dir):
    probe = """
import importlib.util, json, pathlib, sys
app_dir = pathlib.Path(sys.argv[1]).resolve(strict=True)
sys.path.insert(0, str(app_dir))
modules = {}
for name in ("main", "mem0", "uvicorn"):
    spec = importlib.util.find_spec(name)
    if spec is None or spec.origin is None:
        raise SystemExit("required Mem0 runtime module could not be resolved")
    modules[name] = str(pathlib.Path(spec.origin).resolve(strict=True))
print(json.dumps({"prefix": str(pathlib.Path(sys.prefix).resolve(strict=True)), **modules}))
"""
    try:
        return json.loads(command_output([str(python), "-I", "-c", probe, str(app_dir)]))
    except (OSError, subprocess.CalledProcessError, ValueError) as error:
        raise RuntimeError("isolated Mem0 application imports could not be verified") from error


def process_argv(pid):
    """Read process arguments without flattening spaces between argv entries."""
    if pid <= 0:
        raise RuntimeError("a positive running Mem0 process ID is required")
    if sys.platform.startswith("linux"):
        raw = Path(f"/proc/{pid}/cmdline").read_bytes()
        if not raw or len(raw) > 4 * 1024 * 1024:
            raise RuntimeError("Mem0 process arguments could not be verified")
        args = [os.fsdecode(argument) for argument in raw.split(b"\0") if argument]
        if not args:
            raise RuntimeError("Mem0 process arguments could not be verified")
        return args
    if sys.platform == "darwin":
        libc = ctypes.CDLL(None, use_errno=True)
        sysctl = libc.sysctl
        sysctl.argtypes = [ctypes.POINTER(ctypes.c_int), ctypes.c_uint, ctypes.c_void_p,
                           ctypes.POINTER(ctypes.c_size_t), ctypes.c_void_p, ctypes.c_size_t]
        sysctl.restype = ctypes.c_int
        mib = (ctypes.c_int * 3)(1, 49, pid)  # CTL_KERN, KERN_PROCARGS2, pid
        size = ctypes.c_size_t()
        if sysctl(mib, 3, None, ctypes.byref(size), None, 0) != 0:
            raise OSError(ctypes.get_errno(), "process arguments could not be read")
        if size.value < ctypes.sizeof(ctypes.c_int) or size.value > 4 * 1024 * 1024:
            raise RuntimeError("Mem0 process arguments exceeded the verification bound")
        buffer = ctypes.create_string_buffer(size.value)
        actual_size = ctypes.c_size_t(size.value)
        if sysctl(mib, 3, buffer, ctypes.byref(actual_size), None, 0) != 0:
            raise OSError(ctypes.get_errno(), "process arguments could not be read")
        raw = buffer.raw[:actual_size.value]
        argc = int.from_bytes(raw[:ctypes.sizeof(ctypes.c_int)], byteorder=sys.byteorder, signed=True)
        if argc <= 0 or argc > 4096:
            raise RuntimeError("Mem0 process argument count could not be verified")
        exec_path_end = raw.find(b"\0", ctypes.sizeof(ctypes.c_int))
        if exec_path_end < 0:
            raise RuntimeError("Mem0 process arguments could not be verified")
        position = exec_path_end + 1
        while position < len(raw) and raw[position] == 0:
            position += 1
        args = []
        for _ in range(argc):
            end = raw.find(b"\0", position)
            if end < 0:
                raise RuntimeError("Mem0 process arguments could not be verified")
            args.append(os.fsdecode(raw[position:end]))
            position = end + 1
        return args
    raise RuntimeError("argument-preserving Mem0 process inspection is unsupported on this platform")


def verify_mem0_process_binding(pid, endpoint, source, python, process, cwd, listeners,
                                source_clean, module_paths):
    if pid <= 0:
        raise RuntimeError("a positive running Mem0 process ID is required")
    source = Path(source).resolve(strict=True)
    app_dir = source / "server"
    requested_python = Path(python).expanduser().absolute()
    resolved_python = requested_python.resolve(strict=True)
    cwd = Path(cwd).resolve(strict=True)
    if cwd != app_dir.resolve(strict=True):
        raise RuntimeError("Mem0 process is not running from the pinned source tree")
    if not isinstance(process, list) or not process:
        raise RuntimeError("Mem0 process arguments could not be verified")
    process_args = process
    expected_command = [str(requested_python), "-B", "-I", "-m", "uvicorn", "main:app"]
    if process_args[:len(expected_command)] != expected_command:
        raise RuntimeError("Mem0 process is not using the verified Python runtime")
    app_dir_args = process_args[len(expected_command):len(expected_command) + 2]
    if app_dir_args != ["--app-dir", str(app_dir)]:
        raise RuntimeError("Mem0 process does not use the exact pinned Mem0 application directory")
    extra_args = process_args[len(expected_command) + 2:]
    if any(arg == "--app-dir" or arg.startswith("--app-dir=") or arg == "--factory" for arg in extra_args):
        raise RuntimeError("Mem0 process overrides the pinned application import location")
    if not source_clean:
        raise RuntimeError("Mem0 source checkout is not clean")
    if not isinstance(module_paths, dict) or not {"prefix", "main", "mem0", "uvicorn"}.issubset(module_paths):
        raise RuntimeError("Mem0 application import provenance is incomplete")
    try:
        prefix = Path(module_paths["prefix"]).resolve(strict=True)
        modules = {name: Path(module_paths[name]).resolve(strict=True) for name in ("main", "mem0", "uvicorn")}
        venv = requested_python.parent.parent.resolve(strict=True)
        app_file = (app_dir / "main.py").resolve(strict=True)
    except (OSError, TypeError, ValueError) as error:
        raise RuntimeError("Mem0 application import provenance is incomplete") from error
    if prefix != venv or modules["main"] != app_file:
        raise RuntimeError("Mem0 application is not imported from the pinned source and selected virtual environment")
    if modules["uvicorn"] != venv and venv not in modules["uvicorn"].parents:
        raise RuntimeError("Uvicorn is not imported from the selected virtual environment")
    mem0_from_venv = modules["mem0"] == venv or venv in modules["mem0"].parents
    mem0_from_source = modules["mem0"] == source or source in modules["mem0"].parents
    if not mem0_from_venv and not mem0_from_source:
        raise RuntimeError("Mem0 package is not imported from the pinned source or selected virtual environment")
    parsed = urlsplit(endpoint)
    host = str(ipaddress.ip_address(parsed.hostname or ""))
    port = parsed.port
    authority = f"[{host}]:{port}" if ":" in host else f"{host}:{port}"
    expected = "TCP " + authority + " (LISTEN)"
    if not any(line.rstrip().endswith(expected) for line in listeners.splitlines()):
        raise RuntimeError("configured Mem0 endpoint is not served by the verified process")
    return {"pid": pid, "python": str(requested_python), "pythonExecutable": str(resolved_python),
            "runtimePrefix": str(prefix), "applicationDirectory": str(app_dir),
            "applicationModule": str(modules["main"]), "mem0Module": str(modules["mem0"]),
            "uvicornModule": str(modules["uvicorn"]), "sourceCWD": str(cwd), "sourceClean": source_clean,
            "listener": authority}


def process_cwd(pid):
    try:
        result = command_output(["lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"])
    except subprocess.CalledProcessError as error:
        raise RuntimeError("actual Mem0 process working directory could not be verified") from error
    paths = [line[1:] for line in result.splitlines() if line.startswith("n")]
    if len(paths) != 1:
        raise RuntimeError("actual Mem0 process working directory could not be verified")
    return Path(paths[0])


def process_listeners(pid, endpoint):
    parsed = urlsplit(endpoint)
    try:
        return command_output(["lsof", "-nP", "-a", "-p", str(pid),
                               "-iTCP:" + str(parsed.port), "-sTCP:LISTEN"])
    except subprocess.CalledProcessError:
        return ""


def tool_events(path, agent):
    """Pair actual native tool events with native returned content, not final prose."""
    completed = []
    pending = {}
    if agent == "pi":
        connected = False
        direct_tools = {"mcp__shared_memory_" + tool: tool for tool in TOOLS}
        for line in path.read_text().splitlines():
            event = json.loads(line)
            event_type = event.get("type")
            if event_type == "tool_execution_start":
                call_id = event.get("toolCallId")
                name = event.get("toolName")
                if not call_id or call_id in pending:
                    raise RuntimeError("duplicate or missing Pi tool call ID")
                if name == "mcp":
                    if connected or event.get("args") != {"connect": "shared_memory"}:
                        raise RuntimeError("unexpected Pi MCP proxy operation")
                    pending[call_id] = ("connect", name)
                elif name in direct_tools:
                    if not connected:
                        raise RuntimeError("Pi Memory tool ran before MCP connection")
                    pending[call_id] = (direct_tools[name], name)
                else:
                    raise RuntimeError("unrelated native tool observed")
            elif event_type == "tool_execution_end":
                call_id = event.get("toolCallId")
                item = pending.pop(call_id, None)
                if item is None or event.get("toolName") != item[1]:
                    raise RuntimeError("unpaired Pi tool execution")
                name = item[0]
                result = event.get("result") or {}
                failed = bool(event.get("isError") or result.get("isError"))
                if name == "connect":
                    if failed:
                        raise RuntimeError("Pi failed to connect the configured Memory MCP server")
                    connected = True
                else:
                    completed.append((name, result.get("content", []), failed))
        if pending:
            raise RuntimeError("unpaired Pi tool execution")
        if not connected:
            raise RuntimeError("Pi did not connect the configured Memory MCP server")
    elif agent == "codex":
        for line in path.read_text().splitlines():
            event = json.loads(line)
            item = event.get("item", {})
            if event.get("type") == "item.completed" and item.get("type") == "mcp_tool_call":
                if item.get("server") != "shared_memory":
                    raise RuntimeError("unrelated native MCP tool observed")
                content = (item.get("result") or {}).get("content", [])
                completed.append((item["tool"], content, item.get("status") != "completed"))
            if item.get("type") in ["command_execution", "file_change"]:
                raise RuntimeError("unrelated native builtin action observed")
    else:
        raise RuntimeError("unsupported native acceptance agent")
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
                if failed and value.startswith("Error: "):
                    value = value.removeprefix("Error: ")
        parsed.append((name, value, failed))
    return parsed


def build_pi_invocation(options, root, stage, server_args, prompt, tools=TOOLS):
    """Build a Pi run with one isolated MCP server and an exact tool-call guard."""
    agent_dir = Path(options.pi_agent_dir).resolve()
    adapter = agent_dir / "npm" / "node_modules" / "pi-mcp-adapter" / "index.ts"
    if not adapter.is_file():
        raise RuntimeError("Pi MCP adapter is not installed in the task-owned profile")

    config = {"mcpServers": {"shared_memory": {
        "command": str(Path(options.binary).resolve()),
        "args": server_args,
        "directTools": tools,
        "toolPrefix": "mcp",
        "lifecycle": "keep-alive",
    }}}
    config_path = root / (stage + "-mcp.json")
    write_json(config_path, config)

    # Keep the server in the same project `.mcp.json` path users configure.
    # A task-owned HOME holds only the adapter's project-server policy so this
    # non-interactive run can trust its private scratch project without reading
    # or broadening the real user's MCP configuration.
    pi_home = root / (stage + "-pi-home")
    pi_home.mkdir(mode=0o700)
    global_mcp_config = pi_home / ".config" / "mcp" / "mcp.json"
    global_mcp_config.parent.mkdir(mode=0o700, parents=True)
    write_json(global_mcp_config, {"settings": {"projectServers": "allow"}})
    workspace = root / (stage + "-pi-workspace")
    workspace.mkdir(mode=0o700)
    write_json(workspace / ".mcp.json", config)

    guard = root / (stage + "-pi-tool-guard.ts")
    direct_names = ["mcp__shared_memory_" + tool for tool in tools]
    guard.write_text(
        'const directTools = new Set(' + json.dumps(direct_names) + ");\n"
        "let connectRequested = false;\n"
        "let connected = false;\n"
        "export default function (pi) {\n"
        "  pi.on('tool_result', (event) => {\n"
        "    if (event.toolName === \"mcp\" && connectRequested && !event.isError) connected = true;\n"
        "  });\n"
        "  pi.on('tool_call', (event) => {\n"
        "    if (event.toolName === \"mcp\") {\n"
        "      const input = event.input && typeof event.input === \"object\" ? event.input : {};\n"
        "      const allowedConnect = !!event.input && event.input.connect === \"shared_memory\";\n"
        "      if (connectRequested || Object.keys(input).length !== 1 || !allowedConnect) {\n"
        "        return { block: true, reason: \"Only one shared_memory connection is permitted\" };\n"
        "      }\n"
        "      connectRequested = true;\n"
        "      return;\n"
        "    }\n"
        "    if (!connected || !directTools.has(event.toolName)) {\n"
        "      return { block: true, reason: \"Tool is outside the shared_memory acceptance allowlist\" };\n"
        "    }\n"
        "  });\n"
        "}\n",
        encoding="utf-8",
    )
    guard.chmod(0o600)

    argv = [str(Path(options.pi_binary).resolve()), "--mode", "json", "--print",
            "--no-session", "--no-builtin-tools", "--no-skills", "--no-context-files",
            "--no-prompt-templates", "--no-themes", "--no-extensions", "--approve",
            "--provider", "openai-codex", "--model", options.pi_model,
            "--tools", ",".join(["mcp"] + direct_names),
            "--extension", str(adapter), "--extension", str(guard), "--", prompt]
    env = native_agent_environment()
    env["HOME"] = str(pi_home)
    env["PI_CODING_AGENT_DIR"] = str(agent_dir)
    env["PI_TELEMETRY"] = "0"
    return argv, env


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
        env = native_agent_environment()
    elif agent == "pi":
        prompt = ('First call the mcp tool with exactly {"connect":"shared_memory"}. '
                  'Then call only the requested shared_memory direct tools. ' + prompt)
        argv, env = build_pi_invocation(options, root, stage, server_args, prompt, tools)
        stage_cwd = root / (stage + "-pi-workspace")
    else:
        raise RuntimeError("unsupported native acceptance agent")
    if agent == "codex":
        stage_cwd = root / "scratch"
    write_json(root / (stage + "-settings.json"), {"argv": argv, "taskBackendSecretsExported": False})
    (root / (stage + "-prompt.txt")).write_text(prompt)
    native = root / (stage + "-native.jsonl")
    with native.open("w") as output, (root / (stage + "-stderr.log")).open("w") as error:
        command = argv if agent == "pi" else argv + [prompt]
        process = subprocess.Popen(command, stdout=output, stderr=error, env=env,
                                   cwd=stage_cwd, start_new_session=True)
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
    for name in ["binary", "codex-access", "pi-access", "pi-binary", "pi-agent-dir", "output", "mem0-source", "mem0-python"]:
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--mem0-pid", type=int, required=True)
    parser.add_argument("--pi-model", required=True)
    parser.add_argument("--timeout", type=int, default=180)
    options = parser.parse_args()
    if not 30 <= options.timeout <= 300:
        parser.error("timeout must be 30..300 seconds")
    root = options.output.resolve()
    root.mkdir(mode=0o700)  # Existing evidence is never overwritten or replayed.
    (root / "scratch").mkdir(mode=0o700)
    status = {"state": "blocked", "nativeAcceptance": False}
    try:
        configs = [json.loads(path.read_text()) for path in [options.codex_access, options.pi_access]]
        owner = configs[0]["owner"]
        if (not isinstance(owner, dict) or owner != configs[1]["owner"]
                or owner.get("kind") != "PROJECT"):
            raise RuntimeError("native integrations must share one exact trusted PROJECT owner")
        validate_synthetic_project_owner(owner)
        if [c["agent"] for c in configs] != ["codex", "pi"] or not all(c.get("allowWeakWrites") for c in configs):
            raise RuntimeError("two explicit trusted weak writers required")
        endpoint = shared_mem0_endpoint(configs)
        source = options.mem0_source.resolve(strict=True)
        app_dir = source / "server"
        commit = command_output(["git", "rev-parse", "HEAD"], source)
        tracked_changes = command_output(["git", "status", "--porcelain", "--untracked-files=all"], source)
        ignored_or_untracked = command_output(["git", "clean", "-ndx"], source)
        source_clean = not tracked_changes and not ignored_or_untracked
        mem0_python = options.mem0_python.expanduser().absolute()
        module_paths = mem0_runtime_imports(mem0_python, app_dir)
        version = command_output([str(mem0_python), "-I", "-c",
                                  "import importlib.metadata;print(importlib.metadata.version('mem0ai'))"])
        process = process_argv(options.mem0_pid)
        binding = verify_mem0_process_binding(options.mem0_pid, endpoint, source,
                                              mem0_python, process, process_cwd(options.mem0_pid),
                                              process_listeners(options.mem0_pid, endpoint), source_clean, module_paths)
        if commit != PIN or version != "2.2.1":
            raise RuntimeError("actual running Mem0 source/package pin not established")
        write_json(root / "environment.json", {"mem0Commit": commit, "mem0PythonPackage": version,
                    "mem0Endpoint": endpoint, "mem0ProcessBinding": binding,
                    "codexVersion": command_output(["codex", "--version"]),
                    "piVersion": command_output([str(options.pi_binary), "--version"]),
                    "piMCPAdapterVersion": json.loads((options.pi_agent_dir / "npm" / "node_modules" / "pi-mcp-adapter" / "package.json").read_text())["version"],
                    "piModel": options.pi_model,
                    "binarySHA256": hashlib.sha256(options.binary.read_bytes()).hexdigest(),
                    "runtimeVersionDetection": "clean pinned Git source, isolated Python import resolution, exact uvicorn app-dir and virtualenv, and loopback endpoint listener PID; not OpenAPI version"})
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
        read = run_stage(options, root, "pi-read-v1", "pi", options.pi_access, read_prompt + " Attempt memory_get same ID with owner {kind:PROJECT,projectId:unauthorized-project}; verify ownership-denied without records.")
        require_record(read, record_id, 1, first)
        if not any(name == "memory_get" and value == "ownership-denied" and failed
                   for name, value, failed in read):
            raise RuntimeError("actual Pi cross-project rejection missing")
        record["content"] = second
        update = run_stage(options, root, "pi-update", "pi", options.pi_access,
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
        readonly = root / "pi-readonly-access.json"
        configs[1]["allowWeakWrites"] = False
        write_json(readonly, configs[1])
        ro = run_stage(options, root, "pi-readonly", "pi", readonly, common + "Call memory_status only; verify write=false, read=true, search=true.", TOOLS[:4])
        if not any(isinstance(v, dict) and v.get("write") is False and v.get("read") is True for v in returned(ro, "memory_status")):
            raise RuntimeError("native configured read-only status missing")
        status = {"state": "passed", "nativeAcceptance": True, "neutralID": record_id,
                  "effectiveVersion": 2, "canonicalSource": "Mem0 only", "serverEvidence": "metadata-only",
                  "liveEvidence": "actual paired native tool results; no source/config label inference"}
    except (RuntimeError, OSError, TypeError, ValueError, KeyError, subprocess.SubprocessError) as error:
        status["reason"] = str(error) if isinstance(error, RuntimeError) else "demo setup failed; inspect task-owned files"
    write_json(root / "result.json", status)
    print(json.dumps(status))
    return 0 if status["nativeAcceptance"] else 1


if __name__ == "__main__":
    sys.exit(main())

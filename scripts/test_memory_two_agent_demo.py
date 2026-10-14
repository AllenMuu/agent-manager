import importlib.util
import os
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("memory-two-agent-demo.py")
spec = importlib.util.spec_from_file_location("memory_two_agent_demo", SCRIPT)
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


class PiEventTests(unittest.TestCase):
    def parse(self, events):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "native.jsonl"
            path.write_text("".join(json.dumps(event) + "\n" for event in events))
            return demo.tool_events(path, "pi")

    def test_pairs_pi_direct_mcp_results_and_ignores_only_server_discovery(self):
        result = {"content": [{"type": "text", "text": '{"id":"r-1","version":1}'}], "details": {}}
        events = [
            {"type": "tool_execution_start", "toolCallId": "connect-1", "toolName": "mcp", "args": {"connect": "shared_memory"}},
            {"type": "tool_execution_end", "toolCallId": "connect-1", "toolName": "mcp", "result": {"content": [{"type": "text", "text": "connected"}], "details": {}}, "isError": False},
            {"type": "tool_execution_start", "toolCallId": "get-1", "toolName": "mcp__shared_memory_memory_get", "args": {"id": "r-1"}},
            {"type": "tool_execution_end", "toolCallId": "get-1", "toolName": "mcp__shared_memory_memory_get", "result": result, "isError": False},
        ]
        self.assertEqual(self.parse(events), [("memory_get", {"id": "r-1", "version": 1}, False)])

    def test_rejects_unconfigured_mcp_servers_and_proxy_actions(self):
        events = [{"type": "tool_execution_start", "toolCallId": "bad-1", "toolName": "mcp", "args": {"action": "install", "url": "https://example.invalid"}}]
        with self.assertRaisesRegex(RuntimeError, "unexpected Pi MCP proxy operation"):
            self.parse(events)
        events = [{"type": "tool_execution_start", "toolCallId": "bad-2", "toolName": "mcp__other_memory_get", "args": {}}]
        with self.assertRaisesRegex(RuntimeError, "unrelated native tool"):
            self.parse(events)

    def test_rejects_unmatched_or_failed_pi_tool_execution(self):
        events = [{"type": "tool_execution_end", "toolCallId": "missing", "toolName": "mcp__shared_memory_memory_get", "result": {"content": []}, "isError": False}]
        with self.assertRaisesRegex(RuntimeError, "unpaired Pi tool execution"):
            self.parse(events)

    def test_normalizes_pi_adapter_error_prefix_for_denial_evidence(self):
        events = [
            {"type": "tool_execution_start", "toolCallId": "connect-1", "toolName": "mcp", "args": {"connect": "shared_memory"}},
            {"type": "tool_execution_end", "toolCallId": "connect-1", "toolName": "mcp", "result": {"content": [{"type": "text", "text": "connected"}], "details": {}}, "isError": False},
            {"type": "tool_execution_start", "toolCallId": "get-1", "toolName": "mcp__shared_memory_memory_get", "args": {"id": "r-1"}},
            {"type": "tool_execution_end", "toolCallId": "get-1", "toolName": "mcp__shared_memory_memory_get", "result": {"content": [{"type": "text", "text": "Error: ownership-denied"}], "details": {}}, "isError": True},
        ]
        self.assertEqual(self.parse(events), [("memory_get", "ownership-denied", True)])

    def test_codex_mcp_result_parser_remains_supported(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "codex-native.jsonl"
            path.write_text(json.dumps({"type": "item.completed", "item": {
                "type": "mcp_tool_call", "server": "shared_memory", "tool": "memory_get",
                "status": "completed", "result": {"content": [{"type": "text", "text": '{"id":"r-1"}'}]},
            }}) + "\n")
            self.assertEqual(demo.tool_events(path, "codex"), [("memory_get", {"id": "r-1"}, False)])


    def test_pi_invocation_isolated_to_adapter_memory_tools(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "run"
            root.mkdir()
            profile = Path(directory) / "pi-profile"
            extension = profile / "npm" / "node_modules" / "pi-mcp-adapter" / "index.ts"
            extension.parent.mkdir(parents=True)
            extension.write_text("export default function(pi) {}\n")
            options = SimpleNamespace(binary=Path("/approved/agent-manager"),
                                      pi_binary=Path("/opt/pi"), pi_agent_dir=profile,
                                      pi_model="gpt-5.6-luna")
            server_args = ["memory", "serve-mcp", "--access", "/private/pi-access.json",
                           "--events", "/private/pi-events.jsonl"]
            with patch.dict(os.environ, {"OPENAI_API_KEY": "must-not-export", "PATH": "/usr/bin"}):
                argv, env = demo.build_pi_invocation(options, root, "pi-read", server_args, "read prompt")
            config = json.loads((root / "pi-read-mcp.json").read_text())
            server = config["mcpServers"]["shared_memory"]
            self.assertEqual(server["command"], "/approved/agent-manager")
            self.assertEqual(server["args"], server_args)
            self.assertEqual(server["directTools"], demo.TOOLS)
            self.assertEqual(server["toolPrefix"], "mcp")
            self.assertEqual(server["lifecycle"], "keep-alive")
            self.assertEqual(list(config["mcpServers"]), ["shared_memory"])
            project_config = json.loads((root / "pi-read-pi-workspace" / ".mcp.json").read_text())
            self.assertEqual(project_config, config)
            global_config = json.loads((root / "pi-read-pi-home" / ".config" / "mcp" / "mcp.json").read_text())
            self.assertEqual(global_config, {"settings": {"projectServers": "allow"}})
            self.assertEqual(argv[0], "/opt/pi")
            for flag in ["--mode", "json", "--no-session", "--no-builtin-tools", "--no-skills",
                         "--no-context-files", "--no-extensions", "--approve", "--provider",
                         "openai-codex", "--model", "gpt-5.6-luna"]:
                self.assertIn(flag, argv)
            self.assertEqual(env["PI_CODING_AGENT_DIR"], str(profile.resolve()))
            self.assertNotIn("OPENAI_API_KEY", env)
            guard = root / "pi-read-pi-tool-guard.ts"
            guard_source = guard.read_text()
            self.assertIn('event.toolName === "mcp"', guard_source)
            self.assertIn('event.input.connect === "shared_memory"', guard_source)
            module = guard.with_suffix(".mjs")
            module.write_text(guard_source)
            dispatch = r"""
import { pathToFileURL } from "node:url";
const { default: register } = await import(pathToFileURL(process.argv[1]).href);
const handlers = new Map();
register({ on: (event, handler) => handlers.set(event, handler) });
const call = handlers.get("tool_call");
const result = handlers.get("tool_result");
if (call({toolName: "mcp__shared_memory_memory_get", input: {id: "r-1"}})?.block !== true) {
  throw new Error("Memory tool ran before the configured server connected");
}
call({toolName: "mcp", input: {connect: "shared_memory"}});
result({toolName: "mcp", isError: false});
if (call({toolName: "mcp__shared_memory_memory_get", input: {id: "r-1"}}) !== undefined) {
  throw new Error("Memory tool was blocked after the configured server connected");
}
if (call({toolName: "mcp", input: {connect: "another_server"}})?.block !== true) {
  throw new Error("unconfigured server connection was permitted");
}
"""
            completed = subprocess.run(["node", "--input-type=module", "-e", dispatch, str(module)],
                                       capture_output=True, text=True, check=False)
            self.assertEqual(completed.returncode, 0, completed.stderr)

    def test_codex_environment_keeps_native_profile_but_drops_api_keys(self):
        source = {"PATH": "/usr/bin", "HOME": "/home/operator", "CODEX_HOME": "/home/operator/.codex",
                  "OPENAI_API_KEY": "must-not-export", "ANTHROPIC_API_KEY": "must-not-export",
                  "AWS_SECRET_ACCESS_KEY": "must-not-export", "LANG": "C.UTF-8"}
        with patch.dict(os.environ, source, clear=True):
            env = demo.native_agent_environment()
        self.assertEqual(env, {"PATH": "/usr/bin", "HOME": "/home/operator",
                               "CODEX_HOME": "/home/operator/.codex", "LANG": "C.UTF-8"})


class DemoOwnerTests(unittest.TestCase):
    def test_accepts_only_issue23_synthetic_project_owner(self):
        demo.validate_synthetic_project_owner({"kind": "PROJECT", "projectId": "issue23-demo-20261014"})

    def test_rejects_existing_project_owner_for_synthetic_writes(self):
        with self.assertRaisesRegex(RuntimeError, "synthetic ISSUE23 project ID"):
            demo.validate_synthetic_project_owner({"kind": "PROJECT", "projectId": "production-project"})

    def test_rejects_non_project_owner_for_synthetic_writes(self):
        with self.assertRaisesRegex(RuntimeError, "synthetic ISSUE23 project ID"):
            demo.validate_synthetic_project_owner({"kind": "USER", "userId": "issue23-demo"})

    def test_rejects_malformed_owner_for_synthetic_writes(self):
        with self.assertRaisesRegex(RuntimeError, "synthetic ISSUE23 project ID"):
            demo.validate_synthetic_project_owner(None)

    def test_pi_invocation_requires_task_profile_adapter(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "run"
            root.mkdir()
            options = SimpleNamespace(binary=Path("/approved/agent-manager"),
                                      pi_binary=Path("/opt/pi"), pi_agent_dir=Path(directory) / "empty",
                                      pi_model="gpt-5.6-luna")
            with self.assertRaisesRegex(RuntimeError, "Pi MCP adapter is not installed"):
                demo.build_pi_invocation(options, root, "pi-read", ["memory", "serve-mcp"], "read prompt")


class Mem0RuntimePinTests(unittest.TestCase):
    def test_actual_process_inspection_preserves_paths_with_spaces(self):
        with tempfile.TemporaryDirectory() as directory:
            executable = Path(directory) / "python runtime with spaces" / "python"
            executable.parent.mkdir()
            executable.symlink_to(sys.executable)
            argv = [str(executable), "-c", "import time; time.sleep(10)", "argument with spaces"]
            process = subprocess.Popen(argv, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                self.assertEqual(demo.process_argv(process.pid), argv)
            finally:
                process.terminate()
                process.wait(timeout=5)

    def access_config(self, directory, name, endpoint):
        config_path = Path(directory) / (name + "-mem0.json")
        config_path.write_text(json.dumps({"endpoint": endpoint, "contract": demo.MEM0_CONTRACT,
                                           "allowNetwork": True}))
        return {"provider": {"provider": "mem0", "configuration": {
            "kind": "file", "name": str(config_path)}}}

    def process_binding_inputs(self, root):
        source = root / "mem0-source"
        runtime = source / "server"
        runtime.mkdir(parents=True)
        (runtime / "main.py").write_text("app = object()\n")
        python = root / "venv" / "bin" / "python"
        python.parent.mkdir(parents=True)
        python.write_text("")
        prefix = python.parent.parent
        mem0_module = prefix / "lib" / "python3.13" / "site-packages" / "mem0" / "__init__.py"
        uvicorn_module = prefix / "lib" / "python3.13" / "site-packages" / "uvicorn" / "__init__.py"
        mem0_module.parent.mkdir(parents=True)
        uvicorn_module.parent.mkdir(parents=True)
        mem0_module.write_text("")
        uvicorn_module.write_text("")
        modules = {"prefix": str(prefix), "main": str(runtime / "main.py"),
                   "mem0": str(mem0_module), "uvicorn": str(uvicorn_module)}
        process = [str(python), "-B", "-I", "-m", "uvicorn", "main:app",
                   "--app-dir", str(runtime.resolve()), "--host", "127.0.0.1", "--port", "18768"]
        listener = "python 1234 user 3u IPv4 0x123 0t0 TCP 127.0.0.1:18768 (LISTEN)"
        return source, runtime, python, process, listener, modules

    def test_requires_codex_and_pi_to_use_the_same_pinned_endpoint(self):
        with tempfile.TemporaryDirectory() as directory:
            codex = self.access_config(directory, "codex", "http://127.0.0.1:18768")
            pi = self.access_config(directory, "pi", "http://127.0.0.1:18769")
            with self.assertRaisesRegex(RuntimeError, "same pinned Mem0 endpoint"):
                demo.shared_mem0_endpoint([codex, pi])

    def test_rejects_non_loopback_endpoint_for_process_bound_acceptance(self):
        with tempfile.TemporaryDirectory() as directory:
            codex = self.access_config(directory, "codex", "https://mem0.example.test")
            pi = self.access_config(directory, "pi", "https://mem0.example.test")
            with self.assertRaisesRegex(RuntimeError, "numeric loopback URL"):
                demo.shared_mem0_endpoint([codex, pi])

    def test_rejects_missing_mem0_endpoint_as_a_bounded_configuration_error(self):
        with tempfile.TemporaryDirectory() as directory:
            codex = self.access_config(directory, "codex", "http://127.0.0.1:18768")
            provider_path = Path(codex["provider"]["configuration"]["name"])
            provider_path.write_text(json.dumps({"contract": demo.MEM0_CONTRACT, "allowNetwork": True}))
            with self.assertRaisesRegex(RuntimeError, "numeric loopback URL"):
                demo.configured_mem0_endpoint(codex)

    def test_accepts_same_pinned_endpoint_and_matching_process_listener(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            codex = self.access_config(directory, "codex", "http://127.0.0.1:18768")
            pi = self.access_config(directory, "pi", "http://127.0.0.1:18768")
            endpoint = demo.shared_mem0_endpoint([codex, pi])
            source, runtime, python, process, listener, modules = self.process_binding_inputs(root)
            binding = demo.verify_mem0_process_binding(1234, endpoint, source, python,
                                                       process, runtime, listener, True, modules)
            self.assertEqual(binding["listener"], "127.0.0.1:18768")
            self.assertEqual(binding["applicationModule"], str((runtime / "main.py").resolve()))
            self.assertEqual(binding["mem0Module"], str(Path(modules["mem0"]).resolve()))

    def test_rejects_alternate_uvicorn_application_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, runtime, python, process, listener, modules = self.process_binding_inputs(root)
            alternate = root / "other-server"
            alternate.mkdir()
            arguments = list(process)
            arguments[arguments.index("--app-dir") + 1] = str(alternate)
            with self.assertRaisesRegex(RuntimeError, "exact pinned Mem0 application directory"):
                demo.verify_mem0_process_binding(1234, "http://127.0.0.1:18768", source, python,
                                                 arguments, runtime, listener, True, modules)

    def test_rejects_dirty_pinned_source_tree(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, runtime, python, process, listener, modules = self.process_binding_inputs(root)
            with self.assertRaisesRegex(RuntimeError, "source checkout is not clean"):
                demo.verify_mem0_process_binding(1234, "http://127.0.0.1:18768", source, python,
                                                 process, runtime, listener, False, modules)

    def test_rejects_mem0_package_import_outside_selected_venv(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, runtime, python, process, listener, modules = self.process_binding_inputs(root)
            external = root / "external" / "mem0.py"
            external.parent.mkdir()
            external.write_text("")
            modules["mem0"] = str(external)
            with self.assertRaisesRegex(RuntimeError, "pinned source or selected virtual environment"):
                demo.verify_mem0_process_binding(1234, "http://127.0.0.1:18768", source, python,
                                                 process, runtime, listener, True, modules)

    def test_accepts_mem0_package_import_from_clean_pinned_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, runtime, python, process, listener, modules = self.process_binding_inputs(root)
            mem0_module = source / "mem0" / "__init__.py"
            mem0_module.parent.mkdir(parents=True)
            mem0_module.write_text("")
            modules["mem0"] = str(mem0_module)
            binding = demo.verify_mem0_process_binding(1234, "http://127.0.0.1:18768", source, python,
                                                       process, runtime, listener, True, modules)
            self.assertEqual(binding["mem0Module"], str(mem0_module.resolve()))

    def test_rejects_endpoint_not_listened_to_by_verified_process(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, runtime, python, process, _, modules = self.process_binding_inputs(root)
            listener = "python 1234 user 3u IPv4 0x123 0t0 TCP 127.0.0.1:18769 (LISTEN)"
            with self.assertRaisesRegex(RuntimeError, "configured Mem0 endpoint is not served by the verified process"):
                demo.verify_mem0_process_binding(1234, "http://127.0.0.1:18768", source,
                                                 python, process, runtime, listener, True, modules)

    def test_rejects_process_outside_the_pinned_source_tree(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "mem0-source"
            source, pinned_runtime, python, process, listener, modules = self.process_binding_inputs(root)
            runtime = root / "other-source"
            runtime.mkdir()
            with self.assertRaisesRegex(RuntimeError, "Mem0 process is not running from the pinned source tree"):
                demo.verify_mem0_process_binding(1234, "http://127.0.0.1:18768", source,
                                                 python, process, runtime, listener, True, modules)

    def test_rejects_base_interpreter_command_for_two_venvs_sharing_one_binary(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "mem0-source"
            runtime = source / "server"
            runtime.mkdir(parents=True)
            (runtime / "main.py").write_text("app = object()\n")
            base_python = Path(sys.executable).resolve(strict=True)
            selected_python = root / "selected-venv" / "bin" / "python"
            other_python = root / "other-venv" / "bin" / "python"
            selected_python.parent.mkdir(parents=True)
            other_python.parent.mkdir(parents=True)
            try:
                selected_python.symlink_to(base_python)
                other_python.symlink_to(base_python)
            except OSError as error:
                self.skipTest(f"cannot create Python environment symlinks: {error}")
            process = [str(base_python), "-B", "-I", "-m", "uvicorn", "main:app",
                       "--app-dir", str(runtime.resolve())]
            listener = "python 1234 user 3u IPv4 0x123 0t0 TCP 127.0.0.1:18768 (LISTEN)"
            prefix = selected_python.parent.parent
            mem0_module = prefix / "lib" / "python3.13" / "site-packages" / "mem0" / "__init__.py"
            uvicorn_module = prefix / "lib" / "python3.13" / "site-packages" / "uvicorn" / "__init__.py"
            mem0_module.parent.mkdir(parents=True)
            uvicorn_module.parent.mkdir(parents=True)
            mem0_module.write_text("")
            uvicorn_module.write_text("")
            modules = {"prefix": str(prefix), "main": str(runtime / "main.py"),
                       "mem0": str(mem0_module), "uvicorn": str(uvicorn_module)}
            with self.assertRaisesRegex(RuntimeError, "Mem0 process is not using the verified Python runtime"):
                demo.verify_mem0_process_binding(1234, "http://127.0.0.1:18768", source,
                                                 selected_python, process, runtime, listener, True, modules)


if __name__ == "__main__":
    unittest.main()

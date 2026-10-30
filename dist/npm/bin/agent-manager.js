#!/usr/bin/env node
'use strict';
const { spawn } = require('node:child_process');
const path = require('node:path');
const os = require('node:os');
const { hasBinary, binaryPath, installBinary } = require('../install.js');

async function main() {
  const dir = path.resolve(__dirname, '..');
  // Lazy acquisition also supports npm installations with lifecycle scripts disabled.
  if (!hasBinary(dir)) await installBinary({ dir });
  const child = spawn(binaryPath(dir), process.argv.slice(2), { stdio: 'inherit' });
  let requestedSignal;
  const handlers = new Map();
  for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
    const handler = () => { requestedSignal = signal; child.kill(signal); };
    handlers.set(signal, handler);
    process.on(signal, handler);
  }
  child.on('error', err => {
    console.error(`agent-manager: failed to start CLI: ${err.message}`);
    process.exitCode = 1;
  });
  child.on('close', (code, signal) => {
    for (const [name, handler] of handlers) process.removeListener(name, handler);
    const terminated = requestedSignal || signal;
    process.exitCode = terminated ? 128 + (os.constants.signals[terminated] || 0) : (code == null ? 1 : code);
  });
}
main().catch(err => {
  console.error(`agent-manager: ${err.message}`);
  console.error('Retry installation with network access to GitHub Releases. See https://github.com/AllenMuu/agent-manager#install-the-cli');
  process.exitCode = 1;
});

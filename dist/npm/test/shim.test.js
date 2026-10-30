'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');
const { once } = require('node:events');
const shim = path.resolve(__dirname, '../bin/agent-manager.js');

function fixture(t, body) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-manager-shim-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  assert.ok(fs.existsSync(shim), 'npm CLI shim must exist');
  fs.mkdirSync(path.join(dir, 'bin'));
  fs.copyFileSync(shim, path.join(dir, 'bin/agent-manager.js'));
  fs.copyFileSync(path.resolve(__dirname, '../install.js'), path.join(dir, 'install.js'));
  fs.writeFileSync(path.join(dir, 'package.json'), JSON.stringify({ version: '1.2.3' }));
  fs.writeFileSync(path.join(dir, 'bin/.version'), '1.2.3');
  fs.writeFileSync(path.join(dir, 'bin/agent-manager'), `#!${process.execPath}\n${body}\n`, { mode: 0o755 });
  return dir;
}

test('passes install arguments, cwd, environment, stdin and both output streams unchanged', { skip: process.platform === 'win32' }, (t) => {
  const dir = fixture(t, `let input = ''; process.stdin.on('data', c => input += c); process.stdin.on('end', () => { console.log(JSON.stringify({ args: process.argv.slice(2), cwd: process.cwd(), env: process.env.SHIM_TEST, input })); console.error('stderr from CLI'); process.exit(7); });`);
  const args = ['install', 'go-helper', 'skill with spaces', '--target', 'codex', '--yes', '$(echo unsafe)'];
  const result = spawnSync(process.execPath, [path.join(dir, 'bin/agent-manager.js'), ...args], { cwd: dir, env: { ...process.env, SHIM_TEST: 'preserved' }, input: 'hello\n', encoding: 'utf8' });
  assert.equal(result.status, 7, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), { args, cwd: fs.realpathSync(dir), env: 'preserved', input: 'hello\n' });
  assert.equal(result.stderr, 'stderr from CLI\n');
});

test('missing binary produces an actionable failure rather than pretending installation succeeded', (t) => {
  const dir = fixture(t, '');
  fs.unlinkSync(path.join(dir, 'bin/agent-manager'));
  // 0.0.0 is intentionally unpublished; lazy acquisition must fail without a network request.
  fs.writeFileSync(path.join(dir, 'package.json'), JSON.stringify({ version: '0.0.0' }));
  const result = spawnSync(process.execPath, [path.join(dir, 'bin/agent-manager.js'), '--help'], { encoding: 'utf8' });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /agent-manager:.*version/i);
});

test('forwards termination to the child and preserves the shell signal exit code', { skip: process.platform === 'win32' }, async (t) => {
  const dir = fixture(t, `process.on('SIGTERM', () => { require('node:fs').writeFileSync('terminated', 'yes'); process.exit(0); }); console.log('ready'); setInterval(() => {}, 1000);`);
  const child = spawn(process.execPath, [path.join(dir, 'bin/agent-manager.js')], { cwd: dir, stdio: ['ignore', 'pipe', 'pipe'] });
  t.after(() => child.kill('SIGKILL'));
  await once(child.stdout, 'data');
  const closed = once(child, 'close');
  child.kill('SIGTERM');
  const [code] = await closed;
  assert.equal(code, 143);
  assert.equal(fs.readFileSync(path.join(dir, 'terminated'), 'utf8'), 'yes');
});

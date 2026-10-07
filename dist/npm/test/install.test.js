'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { execFileSync } = require('node:child_process');
const installer = () => require('../install.js');
const temp = (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-manager-npm-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
};
function fixture(t) {
  const root = temp(t);
  const source = path.join(root, 'source');
  fs.mkdirSync(source);
  fs.writeFileSync(path.join(source, 'agent-manager'), '#!/bin/sh\nprintf "fixture"\n');
  const archive = path.join(root, 'fixture.tar.gz');
  execFileSync('tar', ['-czf', archive, '-C', source, 'agent-manager']);
  const bytes = fs.readFileSync(archive);
  const asset = 'agent-manager_darwin_arm64.tar.gz';
  const manifest = `${crypto.createHash('sha256').update(bytes).digest('hex')}  ${asset}\n`;
  return { root, bytes, manifest };
}

test('maps exactly the platforms built by the release workflow', () => {
  for (const platform of ['darwin', 'linux', 'win32']) {
    for (const arch of ['x64', 'arm64']) {
      const mapped = installer().mapAsset(platform, arch);
      assert.equal(mapped.asset, `agent-manager_${platform === 'win32' ? 'windows' : platform}_${arch === 'x64' ? 'amd64' : arch}.${platform === 'win32' ? 'zip' : 'tar.gz'}`);
    }
  }
  assert.throws(() => installer().mapAsset('aix', 'x64'), /unsupported/i);
  assert.throws(() => installer().mapAsset('linux', 'ia32'), /unsupported/i);
});

test('pins download URLs to a stable package version and rejects unpublished versions', () => {
  assert.equal(installer().buildUrl('1.2.3', 'checksums.txt'), 'https://github.com/AllenMuu/agent-manager/releases/download/v1.2.3/checksums.txt');
  for (const version of ['0.0.0', '../main', '1.2.3-beta.1', 'latest']) {
    assert.throws(() => installer().buildUrl(version, 'checksums.txt'), /version/i);
  }
});

test('verified archive installs an executable and version marker', async (t) => {
  const f = fixture(t);
  const dir = path.join(f.root, 'package');
  const urls = [];
  const binary = await installer().installBinary({ dir, version: '1.2.3', platform: 'darwin', arch: 'arm64', download: async (url) => {
    urls.push(url);
    return url.endsWith('checksums.txt') ? Buffer.from(f.manifest) : f.bytes;
  } });
  assert.equal(execFileSync(binary, [], { encoding: 'utf8' }), 'fixture');
  assert.equal(installer().hasBinary(dir, '1.2.3'), true);
  assert.equal(installer().hasBinary(dir, '1.2.4'), false);
  assert.equal(urls.length, 2);
  assert.deepEqual(fs.readdirSync(dir).sort(), ['bin']);
});

test('checksum mismatch prevents extraction and leaves no runnable binary', async (t) => {
  const f = fixture(t);
  const dir = path.join(f.root, 'package');
  let extracted = false;
  await assert.rejects(installer().installBinary({ dir, version: '1.2.3', platform: 'darwin', arch: 'arm64', download: async (url) => url.endsWith('checksums.txt') ? Buffer.from(f.manifest) : Buffer.from('corrupt'), extract: () => { extracted = true; } }), /checksum/i);
  assert.equal(extracted, false);
  assert.equal(installer().hasBinary(dir, '1.2.3'), false);
  assert.deepEqual(fs.readdirSync(dir), []);
});

test('missing checksum entry and extraction failure are fatal and clean temporary files', async (t) => {
  const f = fixture(t);
  for (const failure of ['manifest', 'extract']) {
    const dir = path.join(f.root, failure);
    await assert.rejects(installer().installBinary({ dir, version: '1.2.3', platform: 'darwin', arch: 'arm64', download: async (url) => url.endsWith('checksums.txt') ? Buffer.from(failure === 'manifest' ? '' : f.manifest) : f.bytes, extract: () => { throw new Error('extraction failed'); } }), failure === 'manifest' ? /checksum/i : /extraction failed/);
    assert.deepEqual(fs.readdirSync(dir), []);
  }
});

test('download failure is fatal and does not mark the binary installed', async (t) => {
  const dir = temp(t);
  await assert.rejects(installer().installBinary({ dir, version: '1.2.3', platform: 'linux', arch: 'x64', download: async () => { throw new Error('HTTP 404'); } }), /HTTP 404/);
  assert.equal(installer().hasBinary(dir, '1.2.3'), false);
  assert.deepEqual(fs.readdirSync(dir), []);
});

test('checksum manifest rejects ambiguous duplicate entries', () => {
  const bytes = Buffer.from('binary');
  const hash = crypto.createHash('sha256').update(bytes).digest('hex');
  assert.throws(() => installer().verifyChecksum(bytes, Buffer.from(`${hash}  asset\n${hash}  asset\n`), 'asset'), /exactly one/i);
});

test('unverified failure preserves a previously installed executable and marker', async (t) => {
  const dir = temp(t);
  fs.mkdirSync(path.join(dir, 'bin'));
  fs.writeFileSync(installer().binaryPath(dir), 'old binary');
  fs.writeFileSync(path.join(dir, 'bin/.version'), '1.2.2');
  await assert.rejects(installer().installBinary({ dir, version: '1.2.3', download: async () => { throw new Error('offline'); } }), /offline/);
  assert.equal(fs.readFileSync(installer().binaryPath(dir), 'utf8'), 'old binary');
  assert.equal(installer().hasBinary(dir, '1.2.2'), true);
});

test('HTTPS is required for acquisition, including redirect destinations', async () => {
  await assert.rejects(installer().download('http://localhost/asset'), /HTTPS/);
  await assert.rejects(installer().download('https://example.com/asset', 100, 6), /redirects/);
});

test('an extracted symlink cannot be installed as the executable', { skip: process.platform === 'win32' }, async (t) => {
  const f = fixture(t);
  const dir = path.join(f.root, 'package');
  await assert.rejects(installer().installBinary({ dir, version: '1.2.3', platform: 'darwin', arch: 'arm64', download: async (url) => url.endsWith('checksums.txt') ? Buffer.from(f.manifest) : f.bytes, extract: (_archive, destination, executable) => fs.symlinkSync('/bin/sh', path.join(destination, executable)) }), /regular file/);
  assert.deepEqual(fs.readdirSync(dir), []);
});

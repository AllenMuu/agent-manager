'use strict';
const https = require('node:https');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { execFileSync } = require('node:child_process');
const pkg = require('./package.json');

function mapAsset(platform, arch) {
  const goos = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[platform];
  const goarch = { x64: 'amd64', arm64: 'arm64' }[arch];
  if (!goos || !goarch) throw new Error(`unsupported platform ${platform}/${arch}; see https://github.com/AllenMuu/agent-manager/releases`);
  return { asset: `agent-manager_${goos}_${goarch}.${platform === 'win32' ? 'zip' : 'tar.gz'}`, executable: platform === 'win32' ? 'agent-manager.exe' : 'agent-manager' };
}

function buildUrl(version, asset) {
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version) || version === '0.0.0') {
    throw new Error(`version ${version} is not a published stable version; use a released npm package`);
  }
  return `https://github.com/AllenMuu/agent-manager/releases/download/v${version}/${asset}`;
}

function download(url, limit = 100 * 1024 * 1024, redirects = 0) {
  return new Promise((resolve, reject) => {
    const parsed = new URL(url);
    if (parsed.protocol !== 'https:') return reject(new Error('binary downloads require HTTPS'));
    if (redirects > 5) return reject(new Error('too many download redirects'));
    const req = https.get(parsed, (res) => {
      res.on('error', reject);
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        resolve(download(new URL(res.headers.location, parsed).href, limit, redirects + 1));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`HTTP ${res.statusCode} downloading ${url}`));
        return;
      }
      const chunks = [];
      let size = 0;
      res.on('data', (chunk) => {
        size += chunk.length;
        if (size > limit) res.destroy(new Error('download exceeds size limit'));
        else chunks.push(chunk);
      });
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('aborted', () => reject(new Error('download interrupted')));
    });
    req.setTimeout(60000, () => req.destroy(new Error('download timed out after 60s')));
    req.on('error', reject);
  });
}

function verifyChecksum(bytes, manifest, asset) {
  const matches = manifest.toString('utf8').split(/\r?\n/).map(line => /^([a-fA-F0-9]{64})\s+\*?(.+)$/.exec(line)).filter(match => match && match[2] === asset);
  if (matches.length !== 1) throw new Error(`checksum manifest must contain exactly one entry for ${asset}`);
  const actual = crypto.createHash('sha256').update(bytes).digest('hex');
  if (actual !== matches[0][1].toLowerCase()) throw new Error(`SHA-256 checksum mismatch for ${asset}`);
}

function extractArchive(archive, dir, executable) {
  // Extract only the expected binary; no archive-supplied paths or scripts are executed.
  execFileSync('tar', ['-xf', archive, '-C', dir, executable], { stdio: 'pipe' });
}

function binaryPath(dir, platform = process.platform) {
  return path.join(dir, 'bin', platform === 'win32' ? 'agent-manager.exe' : 'agent-manager');
}

function hasBinary(dir, version = pkg.version) {
  try {
    const stat = fs.lstatSync(binaryPath(dir));
    return stat.isFile() && !stat.isSymbolicLink() && fs.readFileSync(path.join(dir, 'bin/.version'), 'utf8') === version;
  } catch { return false; }
}

async function installBinary({ dir = __dirname, version = pkg.version, platform = process.platform, arch = process.arch, download: fetch = download, extract = extractArchive } = {}) {
  const mapped = mapAsset(platform, arch);
  const archiveUrl = buildUrl(version, mapped.asset);
  const checksumUrl = buildUrl(version, 'checksums.txt');
  fs.mkdirSync(dir, { recursive: true });
  const temporary = fs.mkdtempSync(path.join(dir, '.download-'));
  try {
    const bytes = await fetch(archiveUrl);
    const manifest = await fetch(checksumUrl, 1024 * 1024);
    verifyChecksum(bytes, manifest, mapped.asset);
    const archive = path.join(temporary, mapped.asset);
    fs.writeFileSync(archive, bytes);
    extract(archive, temporary, mapped.executable);
    const source = path.join(temporary, mapped.executable);
    const stat = fs.lstatSync(source);
    if (!stat.isFile() || stat.isSymbolicLink() || stat.nlink !== 1) throw new Error('release binary must be a regular file');
    if (platform !== 'win32') fs.chmodSync(source, 0o755);
    const binDir = path.join(dir, 'bin');
    fs.mkdirSync(binDir, { recursive: true });
    fs.writeFileSync(path.join(temporary, '.version'), version);
    const target = binaryPath(dir, platform);
    fs.renameSync(source, target);
    fs.renameSync(path.join(temporary, '.version'), path.join(binDir, '.version'));
    return target;
  } finally {
    fs.rmSync(temporary, { recursive: true, force: true });
  }
}

if (require.main === module) {
  installBinary().catch(err => {
    console.error(`agent-manager: installation failed: ${err.message}`);
    console.error('Check network access to GitHub Releases and retry. Source installation: https://github.com/AllenMuu/agent-manager#install-the-cli');
    process.exitCode = 1;
  });
}
module.exports = { mapAsset, buildUrl, download, verifyChecksum, extractArchive, binaryPath, hasBinary, installBinary };

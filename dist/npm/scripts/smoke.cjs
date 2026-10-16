'use strict';
// Test the packed package with real snapshot archives, without registry publication.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

async function main() {
  const source = path.resolve(__dirname, '..');
  const assets = path.resolve(__dirname, '../../../.goreleaser-dist');
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-manager-pack-'));
  try {
    const staging = path.join(temp, 'package');
    fs.mkdirSync(path.join(staging, 'bin'), { recursive: true });
    for (const file of ['install.js', 'README.md', 'bin/agent-manager.js']) fs.copyFileSync(path.join(source, file), path.join(staging, file));
    const pkg = JSON.parse(fs.readFileSync(path.join(source, 'package.json')));
    pkg.version = '0.1.0'; // local fixture version, never published
    fs.writeFileSync(path.join(staging, 'package.json'), JSON.stringify(pkg));
    const env = { ...process.env, npm_config_cache: path.join(temp, 'cache') };
    const packed = JSON.parse(execFileSync('npm', ['pack', '--json', '--ignore-scripts'], { cwd: staging, env, encoding: 'utf8' }))[0];
    const files = packed.files.map(f => f.path).sort();
    const expected = ['README.md', 'bin/agent-manager.js', 'install.js', 'package.json'];
    if (JSON.stringify(files) !== JSON.stringify(expected)) throw new Error(`unexpected package contents: ${files}`);
    const tarball = path.join(staging, packed.filename);
    const prefix = path.join(temp, 'global');
    const project = path.join(temp, 'project');
    for (const global of [true, false]) {
      const root = global ? prefix : project;
      execFileSync('npm', ['install', ...(global ? ['--global'] : []), '--prefix', root, '--ignore-scripts', '--no-audit', '--no-fund', tarball], { env, stdio: 'pipe' });
      const installed = path.join(root, ...(global && process.platform !== 'win32' ? ['lib'] : []), 'node_modules/@allenmuu/agent-manager');
      const installer = require(path.join(installed, 'install.js'));
      // Use actual GoReleaser assets through the same verification/extraction path.
      await installer.installBinary({ dir: installed, download: async url => fs.readFileSync(path.join(assets, path.basename(new URL(url).pathname))) });
      const output = global
        ? execFileSync(path.join(root, 'bin/agent-manager'), ['--help'], { cwd: temp, env, encoding: 'utf8' })
        : execFileSync('npx', ['--offline', '--yes=false', '@allenmuu/agent-manager', '--help'], { cwd: root, env, encoding: 'utf8' });
      if (!output.includes('Discover and manage local agent resources')) throw new Error(`CLI help missing: ${output}`);
      console.log(`${global ? 'global npm installation' : 'npx execution'}: passed with verified snapshot archive`);
    }
    console.log(`package contents: ${files.join(', ')}`);
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
}
main().catch(err => { console.error(err.message); process.exitCode = 1; });

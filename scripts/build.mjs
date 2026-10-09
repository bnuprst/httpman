#!/usr/bin/env node
// Builds httpman for the current platform (or a given GOOS/GOARCH).
//
//   node scripts/build.mjs                    # desktop app + CLI for this machine
//   node scripts/build.mjs --nogui            # CLI-only binary (no CGO/WebView needed)
//   node scripts/build.mjs --target windows/amd64 --version 1.2.3
//
// Output: dist/bin/<goos>-<goarch>/httpman[.exe]
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const opt = (name, def) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : def;
};
const flag = (name) => args.includes(name);

const goEnv = (k) => execFileSync('go', ['env', k], { cwd: root }).toString().trim();
const [goos, goarch] = (opt('--target') || `${goEnv('GOOS')}/${goEnv('GOARCH')}`).split('/');
const version = (opt('--version') || process.env.HTTPMAN_VERSION || 'dev').replace(/^v/, '');
const nogui = flag('--nogui');
const isWin = process.platform === 'win32';

function run(cmd, cmdArgs, opts = {}) {
  console.log(`$ ${cmd} ${cmdArgs.join(' ')}`);
  execFileSync(cmd, cmdArgs, { stdio: 'inherit', cwd: root, shell: isWin && cmd === 'npm', ...opts });
}

// 1. Sandbox bundle (committed; rebuild only when asked or missing).
const bundle = path.join(root, 'internal/script/sandbox.bundle.js');
if (flag('--sandbox') || !fs.existsSync(bundle)) {
  run('npm', ['ci'], { cwd: path.join(root, 'sandbox') });
  run('npm', ['run', 'build'], { cwd: path.join(root, 'sandbox') });
}

// 2. Frontend.
if (!nogui && !flag('--skip-frontend')) {
  const fe = path.join(root, 'frontend');
  if (!fs.existsSync(path.join(fe, 'node_modules'))) run('npm', ['ci'], { cwd: fe });
  run('npm', ['run', 'build'], { cwd: fe });
}

// 3. Windows resources (icon, manifest, version info).
const syso = [];
if (goos === 'windows' && !nogui) {
  const v4 = (version.match(/^\d+\.\d+\.\d+/)?.[0] || '0.0.0') + '.0';
  run('go', [
    'run', 'github.com/tc-hib/go-winres@v0.3.3', 'simply',
    '--arch', goarch,
    '--icon', 'build/appicon.png',
    '--manifest', 'gui',
    '--product-name', 'httpman',
    '--file-description', 'httpman',
    '--product-version', v4,
    '--file-version', v4,
    '--out', 'rsrc',
  ]);
  for (const f of fs.readdirSync(root)) if (/^rsrc_windows_.*\.syso$/.test(f)) syso.push(path.join(root, f));
}

// 4. Go build.
const tags = nogui ? ['nogui'] : ['desktop', 'production'];
if (goos === 'linux' && !nogui) tags.push('webkit2_41');
const ldflags = ['-s', '-w', `-X main.version=${version}`];
if (goos === 'windows' && !nogui) ldflags.push('-H', 'windowsgui');
const outDir = path.join(root, 'dist', 'bin', `${goos}-${goarch}`);
fs.mkdirSync(outDir, { recursive: true });
const out = path.join(outDir, goos === 'windows' ? 'httpman.exe' : 'httpman');
const env = { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: goos === 'windows' || nogui ? '0' : '1' };
try {
  run('go', ['build', '-trimpath', '-tags', tags.join(','), '-ldflags', ldflags.join(' '), '-o', out, '.'], { env });
} finally {
  syso.forEach((f) => fs.rmSync(f, { force: true }));
}
console.log(`\nBuilt ${path.relative(root, out)} (version ${version})`);

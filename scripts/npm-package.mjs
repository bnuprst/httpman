#!/usr/bin/env node
// Assembles npm packages from built binaries.
//
//   node scripts/npm-package.mjs 1.2.3 [--require-all]
//
// Reads dist/bin/<goos>-<goarch>/httpman[.exe] and writes publishable
// package directories to dist/npm/:
//   httpman-app                    launcher with optionalDependencies
//   httpman-app-<platform>-<arch>  one per binary (os/cpu restricted)
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const requireAll = args.includes('--require-all');
const version = (args.find((a) => !a.startsWith('--')) || '').replace(/^v/, '');
if (!/^\d+\.\d+\.\d+(-[\w.]+)?$/.test(version)) {
  console.error('usage: node scripts/npm-package.mjs <semver> [--require-all]');
  process.exit(1);
}

const mainSrc = path.join(root, 'npm', 'httpman-app');
const mainPkg = JSON.parse(fs.readFileSync(path.join(mainSrc, 'package.json'), 'utf8'));
const NAME = mainPkg.name;
const OS = { windows: 'win32', darwin: 'darwin', linux: 'linux' };
// Name suffix per GOOS. Windows packages are named "windows", not "win32":
// npm's spam detection rejects new packages named *-win32-*.
const NAME_OS = { windows: 'windows', darwin: 'darwin', linux: 'linux' };
const ARCH = { amd64: 'x64', arm64: 'arm64' };

const binRoot = path.join(root, 'dist', 'bin');
const outRoot = path.join(root, 'dist', 'npm');
fs.rmSync(outRoot, { recursive: true, force: true });
fs.mkdirSync(outRoot, { recursive: true });

const readme = fs.readFileSync(path.join(root, 'README.md'), 'utf8');
const built = [];
for (const dir of fs.existsSync(binRoot) ? fs.readdirSync(binRoot) : []) {
  const [goos, goarch] = dir.split('-');
  if (!OS[goos] || !ARCH[goarch]) continue;
  const exe = goos === 'windows' ? 'httpman.exe' : 'httpman';
  const src = path.join(binRoot, dir, exe);
  if (!fs.existsSync(src)) continue;
  const name = `${NAME}-${NAME_OS[goos]}-${ARCH[goarch]}`;
  const dst = path.join(outRoot, name);
  fs.mkdirSync(path.join(dst, 'bin'), { recursive: true });
  fs.copyFileSync(src, path.join(dst, 'bin', exe));
  fs.chmodSync(path.join(dst, 'bin', exe), 0o755);
  const pkg = {
    name,
    version,
    description: `The ${OS[goos]} ${ARCH[goarch]} binary for ${NAME}.`,
    license: mainPkg.license,
    repository: mainPkg.repository,
    os: [OS[goos]],
    cpu: [ARCH[goarch]],
    files: ['bin'],
    preferUnplugged: true,
  };
  fs.writeFileSync(path.join(dst, 'package.json'), JSON.stringify(pkg, null, 2) + '\n');
  fs.writeFileSync(path.join(dst, 'README.md'), `# ${name}\n\nPlatform binary for [${NAME}](https://www.npmjs.com/package/${NAME}). Install \`${NAME}\` instead.\n`);
  built.push(name);
}

const main = { ...mainPkg, version };
main.optionalDependencies = Object.fromEntries(Object.keys(mainPkg.optionalDependencies).map((k) => [k, version]));
const mainDst = path.join(outRoot, NAME);
fs.mkdirSync(path.join(mainDst, 'bin'), { recursive: true });
fs.copyFileSync(path.join(mainSrc, 'bin', 'httpman.js'), path.join(mainDst, 'bin', 'httpman.js'));
fs.chmodSync(path.join(mainDst, 'bin', 'httpman.js'), 0o755);
fs.writeFileSync(path.join(mainDst, 'package.json'), JSON.stringify(main, null, 2) + '\n');
fs.writeFileSync(path.join(mainDst, 'README.md'), readme);
if (fs.existsSync(path.join(root, 'LICENSE'))) fs.copyFileSync(path.join(root, 'LICENSE'), path.join(mainDst, 'LICENSE'));

const missing = Object.keys(main.optionalDependencies).filter((n) => !built.includes(n));
console.log(`Packages in ${path.relative(root, outRoot)}: ${[...built, NAME].join(', ')}`);
if (missing.length) {
  if (requireAll) {
    console.error(`error: no binaries for ${missing.join(', ')}`);
    process.exit(1);
  }
  console.warn(`warning: no binaries for ${missing.join(', ')}`);
}

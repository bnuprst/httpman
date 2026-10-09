#!/usr/bin/env node
// Launcher for the native httpman binary, which is installed as a
// platform-specific optional dependency (httpman-app-<platform>-<arch>).
'use strict';

const { spawn, spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const PKG = 'httpman-app';
const exe = process.platform === 'win32' ? 'httpman.exe' : 'httpman';

function findBinary() {
  if (process.env.HTTPMAN_BINARY) return process.env.HTTPMAN_BINARY;
  const name = `${PKG}-${process.platform}-${process.arch}`;
  try {
    return require.resolve(`${name}/bin/${exe}`);
  } catch (e) {
    // Fall back to a binary placed next to this script (manual installs).
    const local = path.join(__dirname, exe);
    if (fs.existsSync(local)) return local;
    console.error(
      `httpman: no prebuilt binary for ${process.platform}-${process.arch}.\n` +
        `The optional dependency "${name}" was not installed. Reinstall without --no-optional / --omit=optional,\n` +
        `or build from source: https://github.com/bnuprst/httpman#building-from-source`
    );
    process.exit(1);
  }
}

const bin = findBinary();
const args = process.argv.slice(2);
const CLI_COMMANDS = new Set(['run', 'import', 'list', 'version', '--version', '-v', 'help', '--help', '-h']);
const isCLI = args.length > 0 && CLI_COMMANDS.has(args[0]);

if (isCLI || process.env.HTTPMAN_FOREGROUND) {
  // Command-line use: run in the foreground and forward the exit code.
  const res = spawnSync(bin, args, { stdio: 'inherit', windowsHide: false });
  if (res.error) {
    console.error('httpman:', res.error.message);
    process.exit(1);
  }
  process.exit(res.status === null ? 1 : res.status);
} else {
  // Desktop app: detach so the terminal is released immediately.
  const child = spawn(bin, args, { detached: true, stdio: 'ignore', windowsHide: false });
  child.on('error', (e) => {
    console.error('httpman:', e.message);
    process.exit(1);
  });
  child.unref();
}

// Bundles src/ into ../internal/script/sandbox.bundle.js (embedded by Go).
import * as esbuild from 'esbuild';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const shim = (name) => path.join(here, 'node_modules', name);

await esbuild.build({
  entryPoints: [path.join(here, 'src', 'index.js')],
  outfile: path.join(here, '..', 'internal', 'script', 'sandbox.bundle.js'),
  bundle: true,
  format: 'iife',
  platform: 'browser',
  target: 'es2017',
  minify: true,
  legalComments: 'eof',
  define: { 'process.env.NODE_ENV': '"production"', global: 'globalThis' },
  inject: [path.join(here, 'src', 'process-shim.js')],
  alias: {
    buffer: shim('buffer'),
    events: shim('events'),
    stream: shim('stream-browserify'),
    string_decoder: shim('string_decoder'),
    timers: path.join(here, 'src', 'timers-shim.js'),
    util: shim('util'),
    path: shim('path-browserify'),
    url: shim('url'),
    querystring: shim('querystring-es3'),
    punycode: shim('punycode'),
  },
  logLevel: 'info',
});

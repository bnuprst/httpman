// Browser/Node-like globals the Postman sandbox provides: timers, console,
// atob/btoa, crypto.getRandomValues and lazily loaded library globals.
/* global globalThis, __host */

// ---- timers (driven by the Go event loop) ----
const timers = new Map();
let timerSeq = 0;

function addTimer(fn, delay, args, repeat) {
  if (typeof fn !== 'function') throw new TypeError('callback must be a function');
  const id = ++timerSeq;
  delay = Math.max(0, Number(delay) || 0);
  timers.set(id, { id, fn, args, delay, repeat, due: Date.now() + delay, seq: id });
  return id;
}

globalThis.setTimeout = (fn, delay, ...args) => addTimer(fn, delay, args, false);
globalThis.setInterval = (fn, delay, ...args) => addTimer(fn, Math.max(1, Number(delay) || 0), args, true);
globalThis.setImmediate = (fn, ...args) => addTimer(fn, 0, args, false);
globalThis.clearTimeout = globalThis.clearInterval = globalThis.clearImmediate = (id) => { timers.delete(id); };

function nextTimer() {
  let best = null;
  for (const t of timers.values()) {
    if (!best || t.due < best.due || (t.due === best.due && t.seq < best.seq)) best = t;
  }
  return best;
}

// Milliseconds until the next timer fires, or -1 when none are pending.
globalThis.__timerDelay = () => {
  const t = nextTimer();
  if (!t) return -1;
  return Math.max(0, t.due - Date.now());
};

globalThis.__runTimer = () => {
  const t = nextTimer();
  if (!t) return;
  if (t.repeat) {
    t.due = Date.now() + t.delay;
    t.seq = ++timerSeq;
  } else {
    timers.delete(t.id);
  }
  t.fn.apply(null, t.args);
};

// ---- console ----
function inspect(value, seen, depth) {
  seen = seen || new Set();
  depth = depth || 0;
  if (value === null) return 'null';
  if (value === undefined) return 'undefined';
  const t = typeof value;
  if (t === 'string') return depth ? JSON.stringify(value) : value;
  if (t === 'number' || t === 'boolean' || t === 'bigint') return String(value);
  if (t === 'symbol') return value.toString();
  if (t === 'function') return `[Function${value.name ? ': ' + value.name : ' (anonymous)'}]`;
  if (value instanceof Error) return value.stack || `${value.name}: ${value.message}`;
  if (value instanceof Date) return value.toISOString();
  if (value instanceof RegExp) return String(value);
  if (seen.has(value)) return '[Circular]';
  if (depth > 6) return Array.isArray(value) ? '[Array]' : '[Object]';
  seen.add(value);
  try {
    if (typeof value.toJSON === 'function' && !Array.isArray(value)) {
      const j = value.toJSON();
      if (j !== value) return inspect(j, seen, depth);
    }
    const pad = '  '.repeat(depth + 1);
    const end = '  '.repeat(depth);
    if (Array.isArray(value)) {
      if (!value.length) return '[]';
      return '[\n' + value.map((v) => pad + inspect(v, seen, depth + 1)).join(',\n') + '\n' + end + ']';
    }
    if (value instanceof Map) {
      return 'Map(' + value.size + ') ' + inspect(Object.fromEntries(value), seen, depth);
    }
    if (value instanceof Set) {
      return 'Set(' + value.size + ') ' + inspect(Array.from(value), seen, depth);
    }
    const keys = Object.keys(value);
    if (!keys.length) return '{}';
    return '{\n' + keys.map((k) => pad + (/^[A-Za-z_$][\w$]*$/.test(k) ? k : JSON.stringify(k)) + ': ' + inspect(value[k], seen, depth + 1)).join(',\n') + '\n' + end + '}';
  } finally {
    seen.delete(value);
  }
}

function format(args) {
  if (typeof args[0] === 'string' && /%[sdifoOjc%]/.test(args[0])) {
    let i = 1;
    const first = args[0].replace(/%([sdifoOjc%])/g, (m, f) => {
      if (f === '%') return '%';
      if (i >= args.length) return m;
      const a = args[i++];
      switch (f) {
        case 's': return typeof a === 'string' ? a : inspect(a, null, 1);
        case 'd': case 'i': return String(parseInt(a, 10));
        case 'f': return String(parseFloat(a));
        case 'c': return '';
        default: return inspect(a, null, 1);
      }
    });
    return [first].concat(args.slice(i).map((a) => inspect(a))).join(' ');
  }
  return args.map((a) => inspect(a)).join(' ');
}

const counters = {};
const timersLabel = {};
globalThis.console = {
  log: (...a) => __host.log('log', format(a)),
  info: (...a) => __host.log('info', format(a)),
  warn: (...a) => __host.log('warn', format(a)),
  error: (...a) => __host.log('error', format(a)),
  debug: (...a) => __host.log('debug', format(a)),
  trace: (...a) => __host.log('debug', format(a)),
  dir: (o) => __host.log('log', inspect(o)),
  table: (o) => __host.log('log', inspect(o)),
  assert: (cond, ...a) => { if (!cond) __host.log('error', 'Assertion failed' + (a.length ? ': ' + format(a) : '')); },
  count: (label = 'default') => { counters[label] = (counters[label] || 0) + 1; __host.log('log', `${label}: ${counters[label]}`); },
  time: (label = 'default') => { timersLabel[label] = Date.now(); },
  timeEnd: (label = 'default') => { if (timersLabel[label]) __host.log('log', `${label}: ${Date.now() - timersLabel[label]}ms`); delete timersLabel[label]; },
  clear: () => {},
};
globalThis.__inspect = inspect;

// ---- base64 ----
const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
globalThis.btoa = function (input) {
  const str = String(input);
  let out = '';
  for (let i = 0; i < str.length; i += 3) {
    const a = str.charCodeAt(i), b = str.charCodeAt(i + 1), c = str.charCodeAt(i + 2);
    if (a > 255 || b > 255 || c > 255) throw new Error('InvalidCharacterError: btoa accepts latin1 strings only');
    const n = (a << 16) | ((b || 0) << 8) | (c || 0);
    out += B64[(n >> 18) & 63] + B64[(n >> 12) & 63] + (i + 1 < str.length ? B64[(n >> 6) & 63] : '=') + (i + 2 < str.length ? B64[n & 63] : '=');
  }
  return out;
};
globalThis.atob = function (input) {
  const str = String(input).replace(/[\s=]+/g, '').replace(/-/g, '+').replace(/_/g, '/');
  let out = '';
  let buf = 0, bits = 0;
  for (let i = 0; i < str.length; i++) {
    const v = B64.indexOf(str[i]);
    if (v < 0) throw new Error('InvalidCharacterError: invalid base64 input');
    buf = (buf << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out += String.fromCharCode((buf >> bits) & 255);
    }
  }
  return out;
};

// ---- crypto.getRandomValues for uuid & friends ----
globalThis.crypto = {
  getRandomValues(arr) {
    const bytes = __host.randomBytes(arr.length * (arr.BYTES_PER_ELEMENT || 1));
    const view = new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength);
    for (let i = 0; i < view.length; i++) view[i] = bytes[i];
    return arr;
  },
  randomUUID() {
    const b = __host.randomBytes(16);
    b[6] = (b[6] & 0x0f) | 0x40;
    b[8] = (b[8] & 0x3f) | 0x80;
    const h = Array.from(b, (x) => (x + 0x100).toString(16).slice(1)).join('');
    return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
  },
};

// ---- require + lazy library globals ----
const cache = {};
globalThis.require = function (name) {
  const key = String(name).replace(/^node:/, '');
  if (Object.prototype.hasOwnProperty.call(cache, key)) return cache[key];
  const factory = globalThis.__modules[key];
  if (!factory) {
    const err = new Error(`Cannot find module '${name}'`);
    err.code = 'MODULE_NOT_FOUND';
    throw err;
  }
  cache[key] = factory();
  return cache[key];
};

function lazyGlobal(name, loader) {
  Object.defineProperty(globalThis, name, {
    configurable: true,
    enumerable: false,
    get() {
      const v = loader();
      Object.defineProperty(globalThis, name, { value: v, writable: true, configurable: true });
      return v;
    },
    set(v) {
      Object.defineProperty(globalThis, name, { value: v, writable: true, configurable: true });
    },
  });
}
lazyGlobal('_', () => globalThis.require('lodash'));
lazyGlobal('CryptoJS', () => globalThis.require('crypto-js'));
lazyGlobal('tv4', () => globalThis.require('tv4'));
lazyGlobal('cheerio', () => globalThis.require('cheerio'));
lazyGlobal('Buffer', () => globalThis.require('buffer').Buffer);
lazyGlobal('moment', () => globalThis.require('moment'));

globalThis.xml2Json = function (xml) {
  let result, error;
  globalThis.require('xml2js').parseString(String(xml), { explicitArray: false, async: false, trim: true, mergeAttrs: false }, (err, r) => { error = err; result = r; });
  if (error) throw error;
  return result;
};

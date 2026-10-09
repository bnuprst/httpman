import type { Auth, AuthParam, Collection, Event, Item, KV, QueryParam, Request, Url, Variable } from './types';

export const uuid = (): string =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
        const r = (Math.random() * 16) | 0;
        return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
      });

export const clone = <T>(v: T): T => (v === undefined ? v : JSON.parse(JSON.stringify(v)));

export const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS', 'TRACE', 'CONNECT', 'COPY', 'LINK', 'UNLINK', 'PURGE', 'LOCK', 'UNLOCK', 'PROPFIND', 'VIEW'];

export function methodClass(m: string) {
  return 'm-' + (m || 'GET').toLowerCase();
}

export function descriptionText(d: unknown): string {
  if (!d) return '';
  if (typeof d === 'string') return d;
  if (typeof d === 'object' && d && 'content' in d) return String((d as { content?: string }).content ?? '');
  return '';
}

// ---------------------------------------------------------------------------
// URL <-> params
// ---------------------------------------------------------------------------

export function parseQuery(qs: string): QueryParam[] {
  if (!qs) return [];
  return qs.split('&').filter((p) => p !== '').map((p) => {
    const i = p.indexOf('=');
    return i < 0 ? { key: p, value: null } : { key: p.slice(0, i), value: p.slice(i + 1) };
  });
}

export function buildQuery(params: QueryParam[]): string {
  return params
    .filter((p) => !p.disabled && (p.key || p.value))
    .map((p) => (p.value === null || p.value === undefined ? p.key ?? '' : `${p.key ?? ''}=${p.value}`))
    .join('&');
}

/** Splits a raw URL into parts while keeping {{variables}} intact. */
export function parseUrl(raw: string, prev?: Url): Url {
  let s = raw.trim();
  const out: Url = { raw };
  const hashIdx = s.indexOf('#');
  if (hashIdx >= 0) {
    out.hash = s.slice(hashIdx + 1);
    s = s.slice(0, hashIdx);
  }
  const qIdx = s.indexOf('?');
  let query: QueryParam[] = [];
  if (qIdx >= 0) {
    query = parseQuery(s.slice(qIdx + 1));
    s = s.slice(0, qIdx);
  }
  const proto = /^([^:/{}]+):\/\//.exec(s);
  if (proto) {
    out.protocol = proto[1];
    s = s.slice(proto[0].length);
  }
  let i = 0;
  let depth = 0;
  for (; i < s.length; i++) {
    if (s.startsWith('{{', i)) { depth++; i++; continue; }
    if (s.startsWith('}}', i) && depth) { depth--; i++; continue; }
    if (!depth && s[i] === '/') break;
  }
  let host = s.slice(0, i);
  const path = s.slice(i);
  const port = /:(\d+)$/.exec(host);
  if (port) {
    out.port = port[1];
    host = host.slice(0, port.index);
  }
  out.host = host ? host.split('.') : [];
  if (path) out.path = path.replace(/^\//, '').split('/');

  // Keep disabled params from the previous state (they are not in raw).
  const disabled = (prev?.query || []).filter((q) => q.disabled);
  const prevEnabled = (prev?.query || []).filter((q) => !q.disabled);
  const merged = query.map((q, idx) => {
    const p = prevEnabled[idx];
    return p && p.description ? { ...q, description: p.description } : q;
  });
  if (merged.length || disabled.length) out.query = [...merged, ...disabled];

  // Path variables (:name)
  const names = (out.path || []).filter((seg) => seg.startsWith(':') && seg.length > 1).map((seg) => seg.slice(1));
  if (names.length) {
    const prevVars = prev?.variable || [];
    out.variable = names.map((n) => prevVars.find((v) => v.key === n) || { key: n, value: '' });
  }
  // Carry unknown fields
  if (prev) {
    for (const k of Object.keys(prev)) {
      if (!['raw', 'protocol', 'host', 'path', 'port', 'query', 'hash', 'variable'].includes(k)) out[k] = prev[k];
    }
  }
  return out;
}

/** Rebuilds raw after the params table changed. */
export function withQuery(url: Url, query: QueryParam[]): Url {
  const raw = url.raw || '';
  const hashIdx = raw.indexOf('#');
  const hash = hashIdx >= 0 ? raw.slice(hashIdx) : '';
  const base = (hashIdx >= 0 ? raw.slice(0, hashIdx) : raw).split('?')[0];
  const qs = buildQuery(query);
  return { ...url, raw: base + (qs ? '?' + qs : '') + hash, query };
}

export function ensureUrl(u: unknown): Url {
  if (!u) return { raw: '' };
  if (typeof u === 'string') return parseUrl(u);
  const url = u as Url;
  if (url.raw === undefined) {
    let raw = '';
    if (url.protocol) raw += url.protocol + '://';
    raw += (url.host || []).join('.');
    if (url.port) raw += ':' + url.port;
    if (url.path?.length) raw += '/' + url.path.join('/');
    const q = buildQuery(url.query || []);
    if (q) raw += '?' + q;
    return { ...url, raw };
  }
  return url;
}

export function normalizeRequest(r: Request | undefined): Request {
  const req: Request = r ? clone(r) : { method: 'GET', header: [], url: { raw: '' } };
  req.method = (req.method || 'GET').toUpperCase();
  req.url = ensureUrl(req.url);
  if (!Array.isArray(req.header)) req.header = [];
  return req;
}

// ---------------------------------------------------------------------------
// Tree helpers
// ---------------------------------------------------------------------------

export function findItem(items: Item[], id: string): { item: Item; parent: Item[]; parents: Item[] } | null {
  const walk = (list: Item[], parents: Item[]): { item: Item; parent: Item[]; parents: Item[] } | null => {
    for (const it of list) {
      if (it.id === id) return { item: it, parent: list, parents };
      if (it.item) {
        const r = walk(it.item, [...parents, it]);
        if (r) return r;
      }
    }
    return null;
  };
  return walk(items, []);
}

export const isFolder = (it: Item) => !it.request && Array.isArray(it.item);

export function replaceItem(c: Collection, item: Item): boolean {
  const f = findItem(c.item, item.id);
  if (!f) return false;
  const idx = f.parent.indexOf(f.item);
  f.parent[idx] = item;
  return true;
}

export function removeItem(c: Collection, id: string): Item | null {
  const f = findItem(c.item, id);
  if (!f) return null;
  f.parent.splice(f.parent.indexOf(f.item), 1);
  return f.item;
}

export function reassignIds(it: Item): Item {
  it.id = uuid();
  it.item?.forEach(reassignIds);
  return it;
}

export function allRequests(items: Item[], parents: Item[] = []): { item: Item; parents: Item[] }[] {
  const out: { item: Item; parents: Item[] }[] = [];
  for (const it of items) {
    if (it.request) out.push({ item: it, parents });
    if (it.item) out.push(...allRequests(it.item, [...parents, it]));
  }
  return out;
}

export function newRequestItem(name = 'New Request', req?: Request): Item {
  return { id: uuid(), name, request: req ? normalizeRequest(req) : { method: 'GET', header: [], url: { raw: '' } }, response: [] };
}

export function newFolder(name = 'New Folder'): Item {
  return { id: uuid(), name, item: [] };
}

/** Moves item `id` before/after/into `targetId`. Returns false if invalid. */
export function moveItem(c: Collection, id: string, targetId: string | null, where: 'before' | 'after' | 'into'): boolean {
  if (id === targetId) return false;
  const src = findItem(c.item, id);
  if (!src) return false;
  if (targetId && src.item.item && findItem(src.item.item, targetId)) return false; // into own descendant
  src.parent.splice(src.parent.indexOf(src.item), 1);
  if (!targetId) {
    c.item.push(src.item);
    return true;
  }
  const dst = findItem(c.item, targetId);
  if (!dst) {
    c.item.push(src.item);
    return true;
  }
  if (where === 'into') {
    dst.item.item = dst.item.item || [];
    dst.item.item.push(src.item);
  } else {
    const idx = dst.parent.indexOf(dst.item);
    dst.parent.splice(where === 'before' ? idx : idx + 1, 0, src.item);
  }
  return true;
}

// ---------------------------------------------------------------------------
// Scripts & auth
// ---------------------------------------------------------------------------

export function getScript(events: Event[] | undefined, listen: string): string {
  const ev = (events || []).find((e) => e.listen === listen);
  if (!ev) return '';
  const exec = ev.script?.exec as unknown;
  if (typeof exec === 'string') return exec;
  return (exec as string[] | undefined)?.join('\n') ?? '';
}

export function setScript(events: Event[] | undefined, listen: string, code: string): Event[] {
  const existing = (events || []).find((e) => e.listen === listen);
  if (!code.trim() && !existing) return events || [];
  const ev: Event = existing
    ? { ...existing, script: { ...existing.script, type: existing.script?.type || 'text/javascript', exec: code.split('\n') } }
    : { listen, script: { type: 'text/javascript', exec: code.split('\n') } };
  const out = [...(events || [])];
  const idx = existing ? out.indexOf(existing) : -1;
  if (idx >= 0) out[idx] = ev;
  else out.push(ev);
  return out;
}

export function authParam(auth: Auth | null | undefined, key: string): string {
  if (!auth || !auth.type) return '';
  const list = auth[auth.type];
  if (!Array.isArray(list)) {
    if (list && typeof list === 'object') return String((list as unknown as Record<string, unknown>)[key] ?? '');
    return '';
  }
  const p = list.find((x) => x.key === key);
  if (!p || p.value === undefined || p.value === null) return '';
  return typeof p.value === 'string' ? p.value : String(p.value);
}

export function setAuthParam(auth: Auth, key: string, value: string): Auth {
  const type = auth.type;
  const raw = auth[type];
  const list: AuthParam[] = Array.isArray(raw) ? [...raw] : [];
  const idx = list.findIndex((p) => p.key === key);
  if (idx >= 0) list[idx] = { ...list[idx], value };
  else list.push({ key, value, type: 'string' });
  return { ...auth, [type]: list };
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

export function formatBytes(n: number): string {
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(2) + ' MB';
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(2) + ' KB';
  return n + ' B';
}

export function formatMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(2) + ' s';
  return Math.round(ms) + ' ms';
}

export function headerValue(headers: { key: string; value: string }[] | undefined, key: string): string {
  const h = (headers || []).find((x) => x.key.toLowerCase() === key.toLowerCase());
  return h ? h.value : '';
}

export function prettyJSON(text: string): string | null {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return null;
  }
}

export function prettyXML(xml: string): string {
  let formatted = '';
  let pad = 0;
  xml
    .replace(/>\s*</g, '><')
    .replace(/(>)(<)(\/*)/g, '$1\n$2$3')
    .split('\n')
    .forEach((node) => {
      let indent = 0;
      if (/^<\/\w/.test(node)) pad = Math.max(pad - 1, 0);
      else if (/^<\w[^>]*[^/]>.*$/.test(node) && !/<\/\w/.test(node)) indent = 1;
      formatted += '  '.repeat(pad) + node + '\n';
      pad += indent;
    });
  return formatted.trim();
}

export function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function textToB64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = '';
  bytes.forEach((b) => (bin += String.fromCharCode(b)));
  return btoa(bin);
}

export function varValueString(v: unknown): string {
  if (v === undefined || v === null) return '';
  if (typeof v === 'string') return v;
  if (typeof v === 'object') return JSON.stringify(v);
  return String(v);
}

export function kvList(list: KV[] | undefined): KV[] {
  return Array.isArray(list) ? list : [];
}

export function variablesToVars(list: Variable[] | undefined) {
  return (list || []).map((v) => ({ key: v.key, value: v.value, type: v.type, enabled: !v.disabled }));
}

export function debounce<T extends (...a: any[]) => void>(fn: T, ms: number): T {
  let t: ReturnType<typeof setTimeout> | undefined;
  return ((...args: any[]) => {
    if (t) clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  }) as T;
}

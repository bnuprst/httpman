// Postman scripting API (pm.*, plus the legacy postman.* / tests[] globals).
/* global globalThis, __host */

const hasOwn = (o, k) => Object.prototype.hasOwnProperty.call(o, k);
const VAR_RE = /\{\{([^{}]+)\}\}/g;

function str(v) {
  if (v === undefined || v === null) return '';
  if (typeof v === 'object') {
    try { return JSON.stringify(v); } catch (e) { return String(v); }
  }
  return String(v);
}

// ---------------------------------------------------------------------------
// PropertyList: the collection type used for headers, query params, cookies…
// ---------------------------------------------------------------------------
class PropertyList {
  constructor(items, opts) {
    opts = opts || {};
    this._keyField = opts.keyField || 'key';
    this._ci = !!opts.caseInsensitive;
    this._make = opts.make || ((x) => x);
    this.members = [];
    (items || []).forEach((i) => this.add(i));
  }
  _norm(k) { return this._ci && typeof k === 'string' ? k.toLowerCase() : k; }
  _match(m, key) { return this._norm(m[this._keyField]) === this._norm(key); }
  _coerce(item) {
    if (typeof item === 'string' && this._parseString) return this._parseString(item);
    return this._make(item);
  }
  add(item) {
    if (item === undefined || item === null) return this;
    this.members.push(this._coerce(item));
    return this;
  }
  append(item) { return this.add(item); }
  prepend(item) { this.members.unshift(this._coerce(item)); return this; }
  insert(item, before) {
    const idx = before === undefined ? -1 : this.members.findIndex((m) => m === before || this._match(m, before));
    if (idx < 0) return this.add(item);
    this.members.splice(idx, 0, this._coerce(item));
    return this;
  }
  insertAfter(item, after) {
    const idx = this.members.findIndex((m) => m === after || this._match(m, after));
    if (idx < 0) return this.add(item);
    this.members.splice(idx + 1, 0, this._coerce(item));
    return this;
  }
  upsert(item) {
    if (!item) return null;
    const it = this._coerce(item);
    const existing = this.members.find((m) => this._match(m, it[this._keyField]));
    if (existing) {
      Object.assign(existing, it);
      return false;
    }
    this.members.push(it);
    return true;
  }
  one(key) {
    for (let i = this.members.length - 1; i >= 0; i--) {
      if (this._match(this.members[i], key)) return this.members[i];
    }
    return undefined;
  }
  get(key) {
    for (let i = this.members.length - 1; i >= 0; i--) {
      const m = this.members[i];
      if (this._match(m, key) && !m.disabled) return m.value;
    }
    return undefined;
  }
  has(key, value) {
    if (key && typeof key === 'object') return this.members.indexOf(key) >= 0;
    const checkValue = arguments.length >= 2;
    return this.members.some((m) => this._match(m, key) && !m.disabled && (!checkValue || m.value === value));
  }
  remove(predicate, context) {
    let fn = predicate;
    if (typeof predicate === 'string') fn = (m) => this._match(m, predicate);
    else if (predicate && typeof predicate === 'object') fn = (m) => m === predicate;
    if (typeof fn !== 'function') return this;
    this.members = this.members.filter((m) => !fn.call(context, m));
    return this;
  }
  clear() { this.members = []; return this; }
  populate(items) { (items || []).forEach((i) => this.add(i)); return this; }
  repopulate(items) { this.clear(); return this.populate(items); }
  assimilate(source, prune) {
    const list = source instanceof PropertyList ? source.members : source || [];
    if (prune) this.clear();
    list.forEach((i) => this.upsert(i));
    return this;
  }
  all() { return this.members.slice(); }
  idx(i) { return this.members[i]; }
  indexOf(item) {
    return typeof item === 'string' ? this.members.findIndex((m) => this._match(m, item)) : this.members.indexOf(item);
  }
  count() { return this.members.length; }
  get length() { return this.members.length; }
  each(fn, ctx) { this.members.forEach((m, i) => fn.call(ctx, m, i)); }
  forEach(fn, ctx) { this.each(fn, ctx); }
  map(fn, ctx) { return this.members.map((m, i) => fn.call(ctx, m, i)); }
  filter(fn, ctx) { return this.members.filter((m, i) => fn.call(ctx, m, i)); }
  find(fn, ctx) { return this.members.find((m, i) => fn.call(ctx, m, i)); }
  reduce(fn, acc, ctx) { return this.members.reduce((a, m) => fn.call(ctx, a, m), acc); }
  eachParent() {}
  toObject(excludeDisabled, caseSensitive, multiValue) {
    const out = {};
    this.members.forEach((m) => {
      if (excludeDisabled && m.disabled) return;
      let k = m[this._keyField];
      if (k === undefined || k === null) return;
      if (this._ci && caseSensitive === false) k = String(k).toLowerCase();
      if (multiValue && hasOwn(out, k)) {
        out[k] = [].concat(out[k], m.value);
      } else {
        out[k] = m.value;
      }
    });
    return out;
  }
  toJSON() { return this.members.map((m) => (m && typeof m.toJSON === 'function' ? m.toJSON() : Object.assign({}, m))); }
  toString() { return this.members.map((m) => (m && m.toString !== Object.prototype.toString ? m.toString() : str(m.value))).join(this._sep || ', '); }
  [Symbol.iterator]() { return this.members[Symbol.iterator](); }
  static isPropertyList(o) { return o instanceof PropertyList; }
}

class Header {
  constructor(opts, value) {
    if (typeof opts === 'string' && value === undefined) {
      const i = opts.indexOf(':');
      opts = i < 0 ? { key: opts.trim(), value: '' } : { key: opts.slice(0, i).trim(), value: opts.slice(i + 1).trim() };
    } else if (typeof opts === 'string') {
      opts = { key: opts, value };
    }
    opts = opts || {};
    this.key = opts.key === undefined ? '' : String(opts.key);
    this.value = opts.value === undefined || opts.value === null ? '' : String(opts.value);
    if (opts.disabled) this.disabled = true;
    if (opts.type) this.type = opts.type;
    if (opts.description) this.description = opts.description;
  }
  get name() { return this.key; }
  valueOf() { return this.value; }
  toString() { return `${this.key}: ${this.value}`; }
  update(o) { Object.assign(this, new Header(o)); }
  toJSON() {
    const o = { key: this.key, value: this.value };
    if (this.disabled) o.disabled = true;
    if (this.type) o.type = this.type;
    if (this.description) o.description = this.description;
    return o;
  }
}

class HeaderList extends PropertyList {
  constructor(items) { super(items, { caseInsensitive: true, make: (h) => (h instanceof Header ? h : new Header(h)) }); this._sep = '\n'; }
  _parseString(s) { return new Header(s); }
  add(item) {
    if (typeof item === 'string' && item.indexOf('\n') >= 0) {
      item.split('\n').filter((l) => l.trim()).forEach((l) => super.add(l));
      return this;
    }
    return super.add(item);
  }
  contentSize() { return this.toString().length; }
}

class QueryParam {
  constructor(opts) {
    if (typeof opts === 'string') opts = QueryParam.parseSingle(opts);
    opts = opts || {};
    this.key = opts.key === undefined ? null : opts.key;
    this.value = opts.value === undefined ? null : opts.value;
    if (opts.disabled) this.disabled = true;
    if (opts.description) this.description = opts.description;
  }
  static parseSingle(s) {
    const i = s.indexOf('=');
    return i < 0 ? { key: s, value: null } : { key: s.slice(0, i), value: s.slice(i + 1) };
  }
  static parse(qs) {
    if (!qs) return [];
    return String(qs).replace(/^\?/, '').split('&').filter((p) => p !== '').map(QueryParam.parseSingle);
  }
  static unparse(list) {
    return list
      .filter((p) => !p.disabled)
      .map((p) => (p.value === null || p.value === undefined ? str(p.key) : `${str(p.key)}=${str(p.value)}`))
      .join('&');
  }
  valueOf() { return this.value; }
  toString() { return this.value === null ? str(this.key) : `${str(this.key)}=${str(this.value)}`; }
  toJSON() {
    const o = { key: this.key, value: this.value };
    if (this.disabled) o.disabled = true;
    if (this.description) o.description = this.description;
    return o;
  }
}

// ---------------------------------------------------------------------------
// Url
// ---------------------------------------------------------------------------
class Url {
  constructor(input) {
    this.update(input);
  }
  update(input) {
    if (input instanceof Url) input = input.toJSON();
    if (input === undefined || input === null) input = '';
    if (typeof input === 'string') input = Url.parse(input);
    else if (input.raw !== undefined && !input.host && !input.path) input = Object.assign(Url.parse(input.raw), { variable: input.variable });
    this.protocol = input.protocol || undefined;
    this.auth = input.auth || undefined;
    this.host = Array.isArray(input.host) ? input.host.slice() : input.host ? String(input.host).split('.') : undefined;
    this.port = input.port === undefined || input.port === null || input.port === '' ? undefined : String(input.port);
    this.path = Array.isArray(input.path) ? input.path.slice() : input.path ? String(input.path).replace(/^\//, '').split('/') : undefined;
    this.hash = input.hash || undefined;
    this.query = new PropertyList(input.query || [], { make: (q) => (q instanceof QueryParam ? q : new QueryParam(q)) });
    this.query._parseString = (s) => new QueryParam(s);
    this.query._sep = '&';
    this.query.toString = function () { return QueryParam.unparse(this.members); };
    this.variables = new PropertyList(input.variable || input.variables || [], { make: (v) => Object.assign({}, v) });
  }
  static parse(raw) {
    let s = String(raw).trim();
    const out = { raw: s };
    const hashIdx = s.indexOf('#');
    if (hashIdx >= 0) {
      out.hash = s.slice(hashIdx + 1);
      s = s.slice(0, hashIdx);
    }
    const qIdx = s.indexOf('?');
    if (qIdx >= 0) {
      out.query = QueryParam.parse(s.slice(qIdx + 1));
      s = s.slice(0, qIdx);
    }
    const proto = /^([^:/{}]+):\/\//.exec(s);
    if (proto) {
      out.protocol = proto[1];
      s = s.slice(proto[0].length);
    }
    // host[:port] runs until the first '/', ignoring slashes inside {{vars}}
    let i = 0;
    let depth = 0;
    for (; i < s.length; i++) {
      if (s.startsWith('{{', i)) { depth++; i++; continue; }
      if (s.startsWith('}}', i) && depth) { depth--; i++; continue; }
      if (!depth && s[i] === '/') break;
    }
    let hostPart = s.slice(0, i);
    const pathPart = s.slice(i);
    const at = hostPart.lastIndexOf('@');
    if (at >= 0) {
      const [user, password] = hostPart.slice(0, at).split(':');
      out.auth = { user, password };
      hostPart = hostPart.slice(at + 1);
    }
    const portMatch = /:(\d+|\{\{[^{}]+\}\})$/.exec(hostPart);
    if (portMatch) {
      out.port = portMatch[1];
      hostPart = hostPart.slice(0, portMatch.index);
    }
    out.host = hostPart ? hostPart.split('.') : [];
    if (pathPart) out.path = pathPart.replace(/^\//, '').split('/');
    return out;
  }
  getHost() { return (this.host || []).join('.'); }
  getRemote(forcePort) {
    const host = this.getHost();
    const port = this.port || (forcePort ? (this.protocol === 'https' ? '443' : '80') : '');
    return port ? `${host}:${port}` : host;
  }
  getPath(unresolved) {
    let segs = this.path || [];
    if (!unresolved) {
      segs = segs.map((seg) => {
        if (seg && seg[0] === ':') {
          const v = this.variables.one(seg.slice(1));
          if (v && v.value !== undefined && v.value !== null && v.value !== '') return str(v.value);
        }
        return seg;
      });
    }
    return '/' + segs.join('/');
  }
  getQueryString() { return QueryParam.unparse(this.query.members); }
  getPathWithQuery(unresolved) {
    const q = this.getQueryString();
    return this.getPath(unresolved) + (q ? '?' + q : '');
  }
  getRaw() { return this.toString(); }
  getOAuth1BaseUrl() { return `${this.protocol || 'http'}://${this.getRemote()}${this.getPath()}`; }
  addQueryParams(params) {
    if (typeof params === 'string') params = QueryParam.parse(params);
    [].concat(params).forEach((p) => this.query.add(p));
  }
  removeQueryParams(params) {
    [].concat(params).forEach((p) => {
      const k = p && typeof p === 'object' ? p.key : p;
      this.query.remove((q) => q.key === k);
    });
  }
  toString(forceProtocol) {
    let s = '';
    if (this.protocol) s += this.protocol + '://';
    else if (forceProtocol) s += 'http://';
    if (this.auth && (this.auth.user || this.auth.password)) {
      s += str(this.auth.user) + (this.auth.password ? ':' + this.auth.password : '') + '@';
    }
    s += this.getHost();
    if (this.port) s += ':' + this.port;
    if (this.path && this.path.length) s += '/' + this.path.join('/');
    const q = this.getQueryString();
    if (q) s += '?' + q;
    if (this.hash) s += '#' + this.hash;
    return s;
  }
  toJSON() {
    const o = { raw: this.toString() };
    if (this.protocol) o.protocol = this.protocol;
    o.host = (this.host || []).slice();
    if (this.port) o.port = this.port;
    if (this.path) o.path = this.path.slice();
    if (this.query.count()) o.query = this.query.toJSON();
    if (this.hash) o.hash = this.hash;
    if (this.variables.count()) o.variable = this.variables.toJSON();
    return o;
  }
  static isUrl(o) { return o instanceof Url; }
}

// ---------------------------------------------------------------------------
// Variable scopes
// ---------------------------------------------------------------------------
class VariableScope {
  constructor(name, values, opts) {
    this._name = name;
    this._opts = opts || {};
    this.values = new PropertyList((values || []).map((v) => Object.assign({}, v)), { make: (v) => v });
  }
  get name() { return this._name; }
  set name(n) { this._name = n; }
  get id() { return this._opts.id; }
  _find(key) {
    const m = this.values.members;
    for (let i = m.length - 1; i >= 0; i--) if (m[i].key === key && m[i].enabled !== false) return m[i];
    return undefined;
  }
  has(key) { return this._find(key) !== undefined; }
  get(key) {
    const v = this._find(key);
    return v ? v.value : undefined;
  }
  set(key, value, type) {
    if (this._opts.readOnly) throw new Error(`${this._name} variables are read-only`);
    if (key === undefined || key === null) return;
    key = String(key);
    const existing = this.values.members.find((m) => m.key === key);
    if (existing) {
      existing.value = value;
      existing.enabled = true;
      if (type) existing.type = type;
    } else {
      this.values.add({ key, value, type: type || 'default', enabled: true });
    }
  }
  unset(key) {
    if (this._opts.readOnly) throw new Error(`${this._name} variables are read-only`);
    this.values.remove((m) => m.key === key);
  }
  clear() {
    if (this._opts.readOnly) throw new Error(`${this._name} variables are read-only`);
    this.values.clear();
  }
  toObject() {
    const o = {};
    this.values.members.forEach((m) => { if (m.enabled !== false) o[m.key] = m.value; });
    return o;
  }
  replaceIn(template) { return replaceVariables(template, [this]); }
  syncVariablesTo(target) {
    const o = this.toObject();
    if (target) Object.keys(target).forEach((k) => { if (!hasOwn(o, k)) delete target[k]; });
    return Object.assign(target || {}, o);
  }
  toJSON() { return { name: this._name, values: this.values.members.map((m) => Object.assign({}, m)) }; }
}

let scopes; // { globals, collectionVariables, environment, iterationData, _variables }

function chain() {
  // Highest priority first.
  return [scopes._variables, scopes.iterationData, scopes.environment, scopes.collectionVariables, scopes.globals];
}

function lookup(key, list) {
  for (const s of list) if (s.has(key)) return { found: true, value: s.get(key) };
  if (key[0] === '$') {
    const d = __host.dynamic(key);
    if (d !== undefined && d !== null) return { found: true, value: d };
  }
  return { found: false };
}

function replaceVariables(template, list) {
  if (template === undefined || template === null) return template;
  if (typeof template === 'object') {
    try { return JSON.parse(replaceVariables(JSON.stringify(template), list)); } catch (e) { return template; }
  }
  let s = String(template);
  for (let depth = 0; depth < 10 && s.indexOf('{{') >= 0; depth++) {
    let changed = false;
    s = s.replace(VAR_RE, (m, name) => {
      const r = lookup(name.trim(), list);
      if (!r.found) return m;
      changed = true;
      return str(r.value);
    });
    if (!changed) break;
  }
  return s;
}

const variablesApi = {
  has(key) { return chain().some((s) => s.has(key)); },
  get(key) { return lookup(key, chain()).value; },
  set(key, value, type) { scopes._variables.set(key, value, type); },
  unset(key) { scopes._variables.unset(key); },
  clear() { scopes._variables.clear(); },
  replaceIn(template) { return replaceVariables(template, chain()); },
  toObject() {
    return Object.assign({}, scopes.globals.toObject(), scopes.collectionVariables.toObject(), scopes.environment.toObject(), scopes.iterationData.toObject(), scopes._variables.toObject());
  },
  get values() { return scopes._variables.values; },
};

// ---------------------------------------------------------------------------
// Request
// ---------------------------------------------------------------------------
class RequestBody {
  constructor(b) { this.update(b || {}); }
  update(b) {
    if (typeof b === 'string') b = { mode: 'raw', raw: b };
    b = b || {};
    this.mode = b.mode;
    this.raw = b.raw;
    const kv = (list) => new PropertyList(list || [], { make: (x) => Object.assign({}, x) });
    this.urlencoded = kv(b.urlencoded);
    this.formdata = kv(b.formdata);
    this.file = b.file;
    this.graphql = b.graphql;
    this.options = b.options;
    this.disabled = b.disabled;
  }
  isEmpty() {
    switch (this.mode) {
      case 'raw': return !this.raw;
      case 'urlencoded': return !this.urlencoded.count();
      case 'formdata': return !this.formdata.count();
      case 'file': return !this.file;
      case 'graphql': return !this.graphql;
      default: return true;
    }
  }
  toString() {
    switch (this.mode) {
      case 'raw': return str(this.raw);
      case 'urlencoded':
        return this.urlencoded.members.filter((p) => !p.disabled).map((p) => encodeURIComponent(p.key) + '=' + encodeURIComponent(str(p.value))).join('&');
      case 'graphql': return JSON.stringify(this.graphql || {});
      default: return '';
    }
  }
  toJSON() {
    if (!this.mode) return undefined;
    const o = { mode: this.mode };
    if (this.mode === 'raw') o.raw = this.raw === undefined ? '' : str(this.raw);
    if (this.mode === 'urlencoded') o.urlencoded = this.urlencoded.toJSON();
    if (this.mode === 'formdata') o.formdata = this.formdata.toJSON();
    if (this.mode === 'file') o.file = this.file;
    if (this.mode === 'graphql') o.graphql = this.graphql;
    if (this.options) o.options = this.options;
    if (this.disabled) o.disabled = true;
    return o;
  }
}

class RequestAuth {
  constructor(a) { this.update(a); }
  update(a, type) {
    if (Array.isArray(a)) {
      type = type || this.type;
      this[type] = new PropertyList(a, { make: (x) => Object.assign({}, x) });
      this.type = type;
      return;
    }
    a = a || {};
    if (this.type) delete this[this.type];
    this.type = a.type;
    if (a.type && a.type !== 'noauth') this[a.type] = new PropertyList(a[a.type] || [], { make: (x) => Object.assign({}, x) });
  }
  use(type, params) {
    this.type = type;
    if (params) this.update(params, type);
    else if (type !== 'noauth' && !this[type]) this[type] = new PropertyList([], {});
  }
  current() { return this.type; }
  parameters() { return this[this.type] || new PropertyList([], {}); }
  clear(type) { delete this[type]; if (this.type === type) this.type = undefined; }
  toJSON() {
    if (!this.type) return undefined;
    const o = { type: this.type };
    if (this[this.type] instanceof PropertyList) o[this.type] = this[this.type].toJSON();
    return o;
  }
}

class Request {
  constructor(r, meta) {
    r = typeof r === 'string' ? { url: r } : r || {};
    meta = meta || {};
    this.id = meta.id;
    this.name = meta.name;
    this.description = r.description;
    this.method = (r.method || 'GET').toUpperCase();
    this._url = new Url(r.url);
    this.headers = new HeaderList(Array.isArray(r.header) ? r.header : typeof r.header === 'string' ? [r.header] : objectToHeaders(r.header));
    this.body = r.body ? new RequestBody(r.body) : undefined;
    this.auth = r.auth ? new RequestAuth(r.auth) : undefined;
    this.certificate = r.certificate;
    this.proxy = r.proxy;
  }
  get url() { return this._url; }
  set url(v) { this._url = v instanceof Url ? v : new Url(v); }
  addHeader(h) { this.headers.add(h); }
  removeHeader(h, opts) {
    const key = h && typeof h === 'object' ? h.key : h;
    const ci = !(opts && opts.ignoreCase === false);
    this.headers.remove((m) => (ci ? String(m.key).toLowerCase() === String(key).toLowerCase() : m.key === key));
  }
  upsertHeader(h) { this.headers.upsert(h); }
  getHeaders(opts) {
    opts = opts || {};
    const out = {};
    this.headers.members.forEach((h) => {
      if (opts.enabled && h.disabled) return;
      const k = opts.ignoreCase ? h.key.toLowerCase() : h.key;
      if (opts.multiValue) out[k] = [].concat(out[k] || [], h.value);
      else out[k] = h.value;
    });
    return out;
  }
  forEachHeader(fn) { this.headers.each(fn); }
  addQueryParams(p) { this._url.addQueryParams(p); }
  removeQueryParams(p) { this._url.removeQueryParams(p); }
  authorizeUsing(type, options) {
    if (typeof type === 'object' && type) { options = type[type.type]; type = type.type; }
    if (!this.auth) this.auth = new RequestAuth();
    this.auth.use(type, options);
  }
  clone() { return new Request(this.toJSON(), { id: this.id, name: this.name }); }
  update(r) { Object.assign(this, new Request(r, { id: this.id, name: this.name })); }
  size() {
    const body = this.body ? this.body.toString().length : 0;
    const header = this.headers.toString().length;
    return { body, header, total: body + header };
  }
  toJSON() {
    const o = { method: this.method, header: this.headers.toJSON(), url: this._url.toJSON() };
    if (this.body) {
      const b = this.body.toJSON();
      if (b) o.body = b;
    }
    if (this.auth) {
      const a = this.auth.toJSON();
      if (a) o.auth = a;
    }
    if (this.description) o.description = this.description;
    return o;
  }
  get to() { return getChai().expect(this).to; }
}

function objectToHeaders(o) {
  if (!o || typeof o !== 'object') return [];
  return Object.keys(o).map((k) => ({ key: k, value: str(o[k]) }));
}

// ---------------------------------------------------------------------------
// Response
// ---------------------------------------------------------------------------
class Cookie {
  constructor(c) {
    Object.assign(this, c || {});
  }
  valueOf() { return this.value; }
  toString() { return `${this.name}=${this.value}`; }
}

function cookieList(list) {
  return new PropertyList(list || [], { keyField: 'name', make: (c) => (c instanceof Cookie ? c : new Cookie(c)) });
}

class Response {
  constructor(r) {
    r = r || {};
    this.id = r.id;
    this.code = r.code;
    this.status = r.status;
    this.headers = new HeaderList(r.header || []);
    this._body = r.body === undefined || r.body === null ? '' : String(r.body);
    this.responseTime = r.responseTime;
    this.responseSize = r.responseSize !== undefined ? r.responseSize : this._body.length;
    this.cookies = cookieList(r.cookies);
    this.originalRequest = r.originalRequest;
  }
  text() { return this._body; }
  json(reviver, strict) {
    if (this._json === undefined || reviver) {
      let text = this._body;
      if (text.charCodeAt(0) === 0xfeff) text = text.slice(1);
      try {
        const parsed = JSON.parse(text, reviver);
        if (reviver) return parsed;
        this._json = parsed;
      } catch (e) {
        const err = new SyntaxError(e.message);
        err.name = 'JSONError';
        throw err;
      }
    }
    return this._json;
  }
  reason() { return this.status; }
  get body() { return this._body; }
  get stream() { return globalThis.Buffer.from(this._body); }
  dataURI() {
    const ct = this.headers.get('Content-Type') || 'text/plain';
    return `data:${ct};base64,${globalThis.btoa(unescape(encodeURIComponent(this._body)))}`;
  }
  size() {
    const header = this.headers.toString().length;
    return { body: this.responseSize, header, total: this.responseSize + header };
  }
  contentInfo() {
    const ct = this.headers.get('Content-Type') || '';
    const [mime, ...rest] = ct.split(';');
    const charset = (rest.join(';').match(/charset=([^;]+)/i) || [])[1];
    return { contentType: ct, mimeType: mime.trim().split('/')[0], mimeFormat: (mime.trim().split('/')[1] || ''), charset: charset || 'utf8' };
  }
  toJSON() {
    return { code: this.code, status: this.status, header: this.headers.toJSON(), body: this._body, responseTime: this.responseTime, responseSize: this.responseSize };
  }
  get to() { return getChai().expect(this).to; }
  get be() { return getChai().expect(this).to.be; }
  get have() { return getChai().expect(this).to.have; }
  get not() { return getChai().expect(this).to.not; }
  static isResponse(o) { return o instanceof Response; }
}

// ---------------------------------------------------------------------------
// chai + Postman assertion plugin
// ---------------------------------------------------------------------------
let chaiInstance;
function getChai() {
  if (chaiInstance) return chaiInstance;
  chaiInstance = globalThis.require('chai');
  chaiInstance.use(postmanAssertions);
  return chaiInstance;
}

function isResponseLike(o) { return o instanceof Response || (o && typeof o === 'object' && typeof o.code === 'number' && o.headers instanceof PropertyList); }
function isRequestLike(o) { return o instanceof Request; }

function postmanAssertions(chai, utils) {
  const A = chai.Assertion;
  const statusClasses = {
    info: [1, 'informational'],
    success: [2, 'success'],
    redirection: [3, 'redirection'],
    clientError: [4, 'client error'],
    serverError: [5, 'server error'],
  };
  Object.keys(statusClasses).forEach((name) => {
    const [cls, label] = statusClasses[name];
    A.addProperty(name, function () {
      const r = this._obj;
      const c = Math.floor(r.code / 100);
      this.assert(c === cls, `expected response code to be ${cls}XX but found #{act}`, `expected response code to not be ${cls}XX but found #{act}`, cls * 100, r.code);
      void label;
    });
  });
  const statusCodes = { accepted: 202, withoutContent: 204, badRequest: 400, unauthorized: 401, unauthorised: 401, forbidden: 403, notFound: 404, notAcceptable: 406, rateLimited: 429 };
  Object.keys(statusCodes).forEach((name) => {
    A.addProperty(name, function () {
      const code = statusCodes[name];
      this.assert(this._obj.code === code, `expected response code to be #{exp} but found #{act}`, 'expected response code to not be #{exp}', code, this._obj.code);
    });
  });
  utils.overwriteProperty(A.prototype, 'ok', function (_super) {
    return function () {
      if (isResponseLike(this._obj)) {
        this.assert(this._obj.code === 200, "expected response code to be 200 but found #{act}", 'expected response code to not be 200', 200, this._obj.code);
      } else {
        _super.call(this);
      }
    };
  });
  A.addProperty('error', function () {
    const c = Math.floor(this._obj.code / 100);
    this.assert(c === 4 || c === 5, 'expected response code to be 4XX or 5XX but found #{act}', 'expected response code to not be 4XX or 5XX but found #{act}', null, this._obj.code);
  });
  A.addMethod('status', function (expected) {
    const r = this._obj;
    if (typeof expected === 'number') {
      this.assert(r.code === expected, 'expected response to have status code #{exp} but got #{act}', 'expected response to not have status code #{act}', expected, r.code);
    } else {
      this.assert(r.status === expected, "expected response to have status reason '#{exp}' but got '#{act}'", "expected response to not have status reason '#{act}'", expected, r.status);
    }
  });
  A.addMethod('statusCode', function (code) {
    this.assert(this._obj.code === code, 'expected response to have status code #{exp} but got #{act}', 'expected response to not have status code #{act}', code, this._obj.code);
  });
  A.addMethod('statusCodeClass', function (cls) {
    const c = Math.floor(this._obj.code / 100);
    this.assert(c === cls, 'expected response to have status code class #{exp} but got #{act}', 'expected response to not have status code class #{act}', cls, c);
  });
  A.addMethod('statusReason', function (reason) {
    this.assert(this._obj.status === reason, "expected response to have status reason '#{exp}' but got '#{act}'", "expected response to not have status reason '#{act}'", reason, this._obj.status);
  });
  A.addMethod('header', function (key, value) {
    const headers = this._obj.headers;
    const has = headers.has(key);
    if (arguments.length < 2) {
      this.assert(has, `expected response to have header with key '${key}'`, `expected response to not have header with key '${key}'`, true, has);
      return;
    }
    const actual = headers.get(key);
    this.assert(has && actual === value, `expected '${key}' response header to be '#{exp}' but got '#{act}'`, `expected '${key}' response header to not be '#{act}'`, value, actual);
  });
  A.addProperty('withBody', function () {
    const body = isResponseLike(this._obj) ? this._obj.text() : this._obj.body && this._obj.body.toString();
    this.assert(!!body && body.length > 0, 'expected response to have content in body', 'expected response to not have content in body');
  });
  A.addMethod('body', function (expected) {
    const text = this._obj.text();
    if (arguments.length === 0) {
      this.assert(text.length > 0, 'expected response to have content in body', 'expected response to not have content in body');
    } else if (expected instanceof RegExp) {
      this.assert(expected.test(text), 'expected response body text to match #{exp}', 'expected response body text to not match #{exp}', expected, text);
    } else if (typeof expected === 'string') {
      this.assert(text === expected, 'expected response body to equal #{exp} but got #{act}', 'expected response body to not equal #{exp}', expected, text, true);
    } else {
      let json;
      try { json = this._obj.json(); } catch (e) { json = undefined; }
      this.assert(utils.eql(json, expected), 'expected response body json to equal #{exp} but got #{act}', 'expected response body json to not equal #{exp}', expected, json, true);
    }
  });
  function contentTypeIs(name, re) {
    A.addProperty(name, function () {
      const ct = this._obj.headers.get('Content-Type') || '';
      this.assert(re.test(ct), `expected response to be ${name} but got content type #{act}`, `expected response to not be ${name}`, name, ct);
      if (name === 'json' && isResponseLike(this._obj)) {
        let ok = true;
        try { this._obj.json(); } catch (e) { ok = false; }
        this.assert(ok, 'expected response body to be a valid json', 'expected response body to not be a valid json');
      }
    });
  }
  contentTypeIs('json', /json/i);
  contentTypeIs('html', /html/i);
  contentTypeIs('xml', /xml/i);
  contentTypeIs('text', /text\//i);
  A.addMethod('jsonBody', function (path, value) {
    let json;
    let parsed = true;
    try { json = this._obj.json(); } catch (e) { parsed = false; }
    if (arguments.length === 0) {
      this.assert(parsed, 'expected response body to be a valid json', 'expected response body to not be a valid json');
      return;
    }
    if (typeof path === 'object') {
      this.assert(parsed && utils.eql(json, path), 'expected response body json to equal #{exp} but got #{act}', 'expected response body json to not equal #{exp}', path, json, true);
      return;
    }
    const lodash = globalThis.require('lodash');
    const has = parsed && lodash.has(json, path);
    if (arguments.length === 1) {
      this.assert(has, `expected #{act} to have property '${path}'`, `expected #{act} to not have property '${path}'`, path, json);
      return;
    }
    const actual = lodash.get(json, path);
    this.assert(has && utils.eql(actual, value), `expected response body json at '${path}' to contain #{exp} but got #{act}`, `expected response body json at '${path}' to not contain #{exp}`, value, actual, true);
  });
  A.addMethod('jsonSchema', function (schema, options) {
    const Ajv = globalThis.require('ajv');
    const ajv = new Ajv(Object.assign({ allErrors: true, strict: false, logger: false }, options || {}));
    let json;
    try { json = this._obj.json(); } catch (e) { json = undefined; }
    const valid = ajv.validate(schema, json);
    const msg = valid ? '' : ajv.errorsText(ajv.errors);
    this.assert(valid, `expected data to satisfy schema but found following errors: \n${msg}`, 'expected data to not satisfy schema');
  });
  A.addMethod('cookie', function (name, value) {
    const cookies = this._obj.cookies;
    const has = cookies.has(name);
    if (arguments.length < 2) {
      this.assert(has, `expected response to have cookie '${name}'`, `expected response to not have cookie '${name}'`);
      return;
    }
    const actual = cookies.get(name);
    this.assert(has && actual === value, `expected cookie '${name}' to have value #{exp} but got #{act}`, `expected cookie '${name}' to not have value #{exp}`, value, actual);
  });
  A.addMethod('responseTime', function () {});
  void isRequestLike;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------
let testResults;
let pendingTests;

function errorInfo(e) {
  if (e === undefined || e === null) return { name: 'Error', message: String(e) };
  if (typeof e !== 'object') return { name: 'Error', message: String(e) };
  return { name: e.name || 'Error', message: e.message !== undefined ? String(e.message) : String(e), stack: e.stack };
}

function test(name, fn) {
  name = str(name);
  const result = { name, passed: true, skipped: false };
  testResults.push(result);
  if (typeof fn !== 'function') {
    result.skipped = true;
    return pm;
  }
  const fail = (e) => {
    result.passed = false;
    result.error = errorInfo(e);
  };
  try {
    if (fn.length > 0) {
      pendingTests++;
      result.pending = true;
      let finished = false;
      const done = (err) => {
        if (finished) return;
        finished = true;
        pendingTests--;
        delete result.pending;
        if (err) fail(err);
      };
      const ret = fn(done);
      if (ret && typeof ret.then === 'function') ret.then(() => done(), (e) => done(e || new Error('test rejected')));
    } else {
      const ret = fn();
      if (ret && typeof ret.then === 'function') {
        pendingTests++;
        result.pending = true;
        ret.then(
          () => { pendingTests--; delete result.pending; },
          (e) => { pendingTests--; delete result.pending; fail(e); }
        );
      }
    }
  } catch (e) {
    fail(e);
  }
  return pm;
}
test.skip = function (name) {
  testResults.push({ name: str(name), passed: true, skipped: true });
  return pm;
};

// ---------------------------------------------------------------------------
// pm.sendRequest
// ---------------------------------------------------------------------------
function normalizeOutgoing(req) {
  if (typeof req === 'string' || req instanceof Url) req = { url: req.toString() };
  if (req instanceof Request) req = req.toJSON();
  const r = new Request(req);
  const resolve = (s) => variablesApi.replaceIn(s);
  const json = r.toJSON();
  json.url = resolve(r.url.toString());
  json.header = json.header.map((h) => ({ key: resolve(h.key), value: resolve(h.value), disabled: h.disabled }));
  if (json.body) {
    if (json.body.raw !== undefined) json.body.raw = resolve(json.body.raw);
    ['urlencoded', 'formdata'].forEach((m) => {
      if (json.body[m]) json.body[m] = json.body[m].map((p) => Object.assign({}, p, { key: resolve(p.key), value: resolve(p.value) }));
    });
  }
  if (json.auth) json.auth = JSON.parse(resolve(JSON.stringify(json.auth)));
  return json;
}

function sendRequest(req, callback) {
  let err = null;
  let res;
  try {
    const out = JSON.parse(__host.send(JSON.stringify(normalizeOutgoing(req))));
    if (out.error) {
      err = new Error(out.error);
    } else {
      res = new Response(out);
    }
  } catch (e) {
    err = e instanceof Error ? e : new Error(String(e));
  }
  if (typeof callback === 'function') {
    globalThis.setTimeout(() => {
      try {
        callback(err, res, { cookies: res ? res.cookies : cookieList([]) });
      } catch (e) {
        scriptErrors.push(errorInfo(e));
      }
    }, 0);
    return undefined;
  }
  return new Promise((resolve, reject) => globalThis.setTimeout(() => (err ? reject(err) : resolve(res)), 0));
}

// ---------------------------------------------------------------------------
// Cookie jar
// ---------------------------------------------------------------------------
function jar() {
  const call = (op, args, cb) => {
    let err = null;
    let res;
    try {
      const out = JSON.parse(__host.cookies(op, JSON.stringify(args)));
      if (out.error) err = new Error(out.error);
      else res = out.result;
    } catch (e) {
      err = e;
    }
    if (typeof cb === 'function') globalThis.setTimeout(() => cb(err, res), 0);
    return res;
  };
  return {
    get: (url, name, cb) => call('get', { url: str(url), name }, cb),
    getAll: (url, opts, cb) => {
      if (typeof opts === 'function') { cb = opts; opts = {}; }
      return call('getAll', { url: str(url) }, cb && ((e, r) => cb(e, r ? r.map((c) => new Cookie(c)) : r)));
    },
    set: (url, name, value, cb) => {
      let cookie;
      if (name && typeof name === 'object') { cookie = name; cb = value; } else { cookie = { name, value }; }
      return call('set', { url: str(url), cookie }, cb);
    },
    unset: (url, name, cb) => call('unset', { url: str(url), name }, cb),
    clear: (url, cb) => call('clear', { url: str(url) }, cb),
  };
}

// ---------------------------------------------------------------------------
// Setup / teardown, called from Go
// ---------------------------------------------------------------------------
let state;
let pm;
let scriptErrors;
let nextRequest;
let skipRequest;
let visualizer;
let legacyTests;

function legacyRequest(req) {
  let data = {};
  if (req.body) {
    if (req.body.mode === 'raw') data = req.body.raw || '';
    else if (req.body.mode === 'urlencoded') data = req.body.urlencoded.toObject(true);
    else if (req.body.mode === 'formdata') data = req.body.formdata.toObject(true);
  }
  return { id: req.id, name: req.name, description: req.description, url: req.url.toString(), method: req.method, headers: req.headers.toObject(true), data };
}

globalThis.__setup = function (json) {
  state = JSON.parse(json);
  const s = state.scopes || {};
  scopes = {
    globals: new VariableScope('globals', s.globals),
    collectionVariables: new VariableScope(state.collectionName || 'collection', s.collectionVariables),
    environment: new VariableScope(s.environmentName || '', s.environment),
    iterationData: new VariableScope('iterationData', s.iterationData, { readOnly: true }),
    _variables: new VariableScope('_variables', s._variables),
  };
  testResults = [];
  pendingTests = 0;
  scriptErrors = [];
  nextRequest = undefined;
  skipRequest = false;
  visualizer = undefined;

  const info = Object.assign({ eventName: state.event, iteration: 0, iterationCount: 1, requestName: '', requestId: '' }, state.info || {});
  const request = new Request(state.request || {}, { id: info.requestId, name: info.requestName });
  const response = state.response ? new Response(state.response) : undefined;
  const cookies = cookieList(state.cookies);
  cookies.jar = jar;

  const execution = {
    setNextRequest(name) { nextRequest = { value: name === undefined ? null : name }; },
    skipRequest() { skipRequest = true; },
    location: Object.assign([], info.location || [], { current: info.requestName }),
    runRequest() { throw new Error('pm.execution.runRequest is not supported'); },
  };

  pm = {
    info,
    environment: scopes.environment,
    globals: scopes.globals,
    collectionVariables: scopes.collectionVariables,
    iterationData: scopes.iterationData,
    variables: variablesApi,
    request,
    response,
    cookies,
    test,
    get expect() { return getChai().expect; },
    sendRequest,
    execution,
    visualizer: {
      set(template, data, options) { visualizer = { template: str(template), data: data === undefined ? null : data, options: options || null }; },
      clear() { visualizer = undefined; },
    },
    require(name) { return globalThis.require(name); },
  };
  globalThis.pm = pm;

  // ---- legacy API ----
  legacyTests = {};
  globalThis.tests = legacyTests;
  globalThis.environment = scopes.environment.toObject();
  globalThis.globals = scopes.globals.toObject();
  globalThis.data = scopes.iterationData.toObject();
  globalThis.iteration = info.iteration;
  globalThis.request = legacyRequest(request);
  if (response) {
    globalThis.responseBody = response.text();
    globalThis.responseCode = { code: response.code, name: response.status, detail: response.status };
    globalThis.responseTime = response.responseTime;
    globalThis.responseHeaders = response.headers.toObject();
    globalThis.responseCookies = response.cookies.all().map((c) => Object.assign({}, c));
  } else {
    ['responseBody', 'responseCode', 'responseTime', 'responseHeaders', 'responseCookies'].forEach((k) => { delete globalThis[k]; });
  }
  globalThis.postman = {
    setEnvironmentVariable: (k, v) => scopes.environment.set(k, v),
    getEnvironmentVariable: (k) => scopes.environment.get(k),
    clearEnvironmentVariable: (k) => scopes.environment.unset(k),
    clearEnvironmentVariables: () => scopes.environment.clear(),
    setGlobalVariable: (k, v) => scopes.globals.set(k, v),
    getGlobalVariable: (k) => scopes.globals.get(k),
    clearGlobalVariable: (k) => scopes.globals.unset(k),
    clearGlobalVariables: () => scopes.globals.clear(),
    getVariable: (k) => variablesApi.get(k),
    setNextRequest: (name) => execution.setNextRequest(name),
    getResponseHeader: (k) => (response ? response.headers.get(k) : undefined),
    getResponseCookie: (k) => {
      const c = response ? response.cookies.one(k) : undefined;
      return c ? Object.assign({}, c) : undefined;
    },
  };
};

// Records an error thrown (or rejected) by a user script.
globalThis.__scriptError = function (e) {
  scriptErrors.push(errorInfo(e));
};

globalThis.__pending = function () { return pendingTests; };

globalThis.__finish = function () {
  Object.keys(legacyTests).forEach((k) => {
    testResults.push({ name: k, passed: !!legacyTests[k], skipped: false, error: legacyTests[k] ? undefined : { name: 'AssertionError', message: 'expected ' + JSON.stringify(k) + ' to be truthy' } });
  });
  legacyTests = {};
  globalThis.tests = legacyTests;
  testResults.forEach((t) => {
    if (t.pending) {
      t.passed = false;
      t.error = { name: 'Error', message: 'test did not complete' };
      delete t.pending;
    }
  });
  const toVars = (scope) => scope.values.members.map((m) => ({ key: m.key, value: m.value, type: m.type, enabled: m.enabled !== false }));
  const out = {
    scopes: {
      globals: toVars(scopes.globals),
      collectionVariables: toVars(scopes.collectionVariables),
      environment: toVars(scopes.environment),
      _variables: toVars(scopes._variables),
    },
    request: pm.request.toJSON(),
    tests: testResults,
    errors: scriptErrors,
    skipRequest,
  };
  if (nextRequest) out.nextRequest = nextRequest;
  if (visualizer) out.visualizer = visualizer;
  return JSON.stringify(out, (k, v) => (v === undefined ? undefined : v));
};

// Exposed for tests and power users.
globalThis.__sdk = { PropertyList, Header, HeaderList, QueryParam, Url, VariableScope, Request, Response, RequestBody, RequestAuth, Cookie };

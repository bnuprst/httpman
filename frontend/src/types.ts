// Postman collection v2.1 shapes (loose: unknown fields are preserved).

export interface KV {
  key: string;
  value?: string;
  disabled?: boolean;
  description?: string;
  type?: string; // formdata: text | file
  src?: string | string[] | null;
  contentType?: string;
  [extra: string]: unknown;
}

export interface QueryParam {
  key: string | null;
  value: string | null;
  disabled?: boolean;
  description?: string;
}

export interface Variable {
  key: string;
  value?: unknown;
  type?: string;
  disabled?: boolean;
  description?: string;
  id?: string;
  [extra: string]: unknown;
}

export interface Url {
  raw: string;
  protocol?: string;
  host?: string[];
  path?: string[];
  port?: string;
  query?: QueryParam[];
  hash?: string;
  variable?: Variable[];
  [extra: string]: unknown;
}

export interface AuthParam {
  key: string;
  value: unknown;
  type?: string;
}

export interface Auth {
  type: string;
  [type: string]: AuthParam[] | string;
}

export interface Body {
  mode?: 'raw' | 'urlencoded' | 'formdata' | 'file' | 'graphql' | 'none' | string;
  raw?: string;
  urlencoded?: KV[];
  formdata?: KV[];
  file?: { src?: string | null; content?: string };
  graphql?: { query: string; variables?: string };
  options?: { raw?: { language?: string } } & Record<string, unknown>;
  disabled?: boolean;
  [extra: string]: unknown;
}

export interface Request {
  method: string;
  header: KV[];
  url: Url;
  body?: Body;
  auth?: Auth | null;
  description?: string;
  [extra: string]: unknown;
}

export interface Script {
  type?: string;
  exec: string[];
  [extra: string]: unknown;
}

export interface Event {
  listen: 'prerequest' | 'test' | string;
  script: Script;
  disabled?: boolean;
  [extra: string]: unknown;
}

export interface Item {
  id: string;
  name: string;
  description?: string | { content?: string };
  item?: Item[];
  request?: Request;
  response?: unknown[];
  event?: Event[];
  variable?: Variable[];
  auth?: Auth | null;
  protocolProfileBehavior?: Record<string, unknown>;
  [extra: string]: unknown;
}

export interface Collection {
  info: { _postman_id: string; name: string; description?: string | { content?: string }; schema: string; [extra: string]: unknown };
  item: Item[];
  event?: Event[];
  variable?: Variable[];
  auth?: Auth | null;
  protocolProfileBehavior?: Record<string, unknown>;
  [extra: string]: unknown;
}

export interface Var {
  key: string;
  value: unknown;
  type?: string;
  enabled: boolean;
}

export interface Environment {
  id: string;
  name: string;
  values: Var[];
  _postman_variable_scope?: string;
  [extra: string]: unknown;
}

export interface Settings {
  timeoutMs: number;
  followRedirects: boolean;
  maxRedirects: number;
  sslVerify: boolean;
  proxyMode: 'system' | 'none' | 'custom';
  proxyUrl: string;
  maxResponseMB: number;
  theme: 'system' | 'light' | 'dark';
  saveCookies: boolean;
  historyLimit: number;
  scriptTimeoutSec: number;
  fontSize: number;
  editorTabSize: number;
  persistVariables: boolean;
}

export interface HistoryEntry {
  id: string;
  time: string;
  name?: string;
  method: string;
  url: string;
  code?: number;
  duration?: number;
  request: Request;
}

export interface Header {
  key: string;
  value: string;
}

export interface TestResult {
  name: string;
  passed: boolean;
  skipped: boolean;
  error?: { name: string; message: string };
}

export interface ScriptError {
  name: string;
  message: string;
  source?: string;
}

export interface Cookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  expires?: string;
  secure?: boolean;
  httpOnly?: boolean;
  hostOnly?: boolean;
  sameSite?: string;
}

export interface ResponseView {
  code: number;
  status: string;
  proto: string;
  header: Header[];
  responseTime: number;
  timings: { dns: number; connect: number; tls: number; firstByte: number; download: number; total: number };
  bodySize: number;
  headerSize: number;
  truncated?: boolean;
  request: { method: string; url: string; header: Header[]; body?: string };
  redirects?: string[];
  warnings?: string[];
  bodyBase64: string;
  bodyText: string;
  isText: boolean;
  cookies: Cookie[];
}

export interface Execution {
  itemId: string;
  name: string;
  iteration: number;
  request?: { method: string; url: string; header: Header[] };
  response?: ResponseView;
  error?: string;
  tests: TestResult[];
  scriptErrors?: ScriptError[];
  skipped?: boolean;
  visualizer?: { template: string; data: unknown; options?: unknown };
  path?: string[];
}

export interface SendResult {
  execution: Execution;
  environment?: Environment;
  globals: Environment;
  collectionVariables?: Var[];
  history: HistoryEntry[];
}

export interface ConsoleEntry {
  time: string;
  level: string;
  message: string;
  source?: string;
  data?: any;
}

export interface Summary {
  collection: string;
  iterations: number;
  requests: number;
  failedRequests: number;
  tests: number;
  testsPassed: number;
  testsFailed: number;
  testsSkipped: number;
  scriptErrors: number;
  duration: number;
  avgResponseTime: number;
  stopped?: boolean;
  executions: Execution[];
}

export interface ImportResult {
  collections: Collection[];
  environments: Environment[];
  globals?: Environment;
  request?: Request;
  errors: string[];
}

export interface InitData {
  version: string;
  platform: string;
  workspaceDir: string;
  settings: Settings;
  collections: Collection[];
  environments: Environment[];
  globals: Environment;
  history: HistoryEntry[];
  uiState: any;
  errors: string[];
  languages: { ID: string; Name: string }[];
}

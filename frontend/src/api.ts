// Thin typed wrapper around the Wails bindings (window.go.app.App) and
// runtime (window.runtime). Methods map 1:1 to internal/app/app.go.
import type {
  Collection,
  Cookie,
  Environment,
  HistoryEntry,
  ImportResult,
  InitData,
  Request,
  SendResult,
  Settings,
} from './types';

declare global {
  interface Window {
    go?: { app?: { App?: Record<string, (...args: any[]) => Promise<any>> } };
    runtime?: {
      EventsOn(name: string, cb: (...data: any[]) => void): () => void;
      EventsOff(name: string): void;
      BrowserOpenURL(url: string): void;
      ClipboardSetText(text: string): Promise<boolean>;
      ClipboardGetText(): Promise<string>;
      WindowSetTitle(title: string): void;
    };
  }
}

function call<T>(method: string, ...args: any[]): Promise<T> {
  const fn = window.go?.app?.App?.[method];
  if (!fn) return Promise.reject(new Error(`backend not available (${method})`));
  return fn(...args).catch((e: unknown) => {
    throw e instanceof Error ? e : new Error(String(e));
  });
}

export interface SendInput {
  requestId: string;
  collection?: Collection | null;
  itemId?: string;
  item: unknown;
  environmentId?: string;
}

export interface RunInput {
  runId: string;
  collection: Collection;
  folderId?: string;
  environmentId?: string;
  iterations: number;
  delayMs: number;
  dataFile?: string;
  persistVariables: boolean;
  bail: boolean;
  only?: string[];
}

const toInput = (i: SendInput) => ({ ...i, collection: i.collection ?? null });

export const api = {
  init: () => call<InitData>('Init'),
  saveCollection: (c: Collection) => call<string>('SaveCollection', JSON.stringify(c)),
  deleteCollection: (id: string) => call<void>('DeleteCollection', id),
  newCollection: (name: string) => call<Collection>('NewCollection', name),
  importFiles: () => call<ImportResult>('ImportFiles'),
  importText: (text: string) => call<ImportResult>('ImportText', text),
  exportCollection: (c: Collection) => call<string>('ExportCollection', JSON.stringify(c)),
  saveEnvironment: (e: Environment) => call<Environment>('SaveEnvironment', e),
  deleteEnvironment: (id: string) => call<void>('DeleteEnvironment', id),
  saveGlobals: (g: Environment) => call<Environment>('SaveGlobals', g),
  exportEnvironment: (id: string) => call<string>('ExportEnvironment', id),
  saveSettings: (s: Settings) => call<void>('SaveSettings', s),
  history: () => call<HistoryEntry[]>('History'),
  deleteHistory: (id: string) => call<HistoryEntry[]>('DeleteHistory', id),
  saveUIState: (state: unknown) => call<void>('SaveUIState', JSON.stringify(state)),
  cookies: () => call<Cookie[]>('Cookies'),
  putCookie: (c: Cookie) => call<Cookie[]>('PutCookie', c),
  deleteCookies: (domain: string, path: string, name: string) => call<Cookie[]>('DeleteCookies', domain, path, name),
  pickFiles: (title: string) => call<string[] | null>('PickFiles', title),
  saveResponse: (b64: string, name: string) => call<string>('SaveResponse', b64, name),
  send: (i: SendInput) => call<SendResult>('Send', toInput(i)),
  cancel: (id: string) => call<void>('Cancel', id),
  resolveRequest: (i: SendInput) => call<Request>('ResolveRequest', toInput(i)),
  generateCode: (i: SendInput, lang: string) => call<string>('GenerateCode', toInput(i), lang),
  startRun: (i: RunInput) => call<string>('StartRun', i),
  loadDataFile: (path: string) => call<{ columns: string[]; rows: Record<string, unknown>[] }>('LoadDataFile', path),
  dynamicVariables: () => call<string[]>('DynamicVariables'),
};

export function onEvent(name: string, cb: (data: any) => void): () => void {
  if (!window.runtime) return () => {};
  return window.runtime.EventsOn(name, cb);
}

export function openExternal(url: string) {
  if (window.runtime) window.runtime.BrowserOpenURL(url);
  else window.open(url, '_blank');
}

export async function copyText(text: string) {
  try {
    if (window.runtime) {
      await window.runtime.ClipboardSetText(text);
      return;
    }
  } catch {
    /* fall through */
  }
  await navigator.clipboard.writeText(text);
}

export async function readClipboardText(): Promise<string> {
  try {
    if (window.runtime) return await window.runtime.ClipboardGetText();
  } catch {
    /* fall through */
  }
  return navigator.clipboard.readText();
}

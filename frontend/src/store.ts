import { create } from 'zustand';
import { api } from './api';
import type {
  Collection,
  ConsoleEntry,
  Environment,
  Execution,
  HistoryEntry,
  ImportResult,
  InitData,
  Item,
  Request,
  SendResult,
  Settings,
  Summary,
  Var,
} from './types';
import { clone, debounce, findItem, newRequestItem, normalizeRequest, replaceItem, uuid } from './util';

export interface RequestTab {
  id: string;
  kind: 'request';
  collectionId?: string;
  itemId: string;
  draft: Item;
  dirty?: boolean;
  result?: SendResult;
  sending?: boolean;
  requestId?: string;
  error?: string;
  reqPane?: string;
  resPane?: string;
}

export interface CollectionTab {
  id: string;
  kind: 'collection';
  collectionId: string;
  folderId?: string; // folder settings when set
  draft: Item; // name, description, auth, event, variable (for both)
  dirty?: boolean;
  pane?: string;
}

export interface EnvironmentTab {
  id: string;
  kind: 'environment';
  envId: string; // 'globals' for globals
  draft: Environment;
  dirty?: boolean;
}

export interface RunConfig {
  collectionId: string;
  folderId?: string;
  environmentId?: string;
  iterations: number;
  delayMs: number;
  dataFile?: string;
  persistVariables: boolean;
  bail: boolean;
  selected?: string[];
}

export interface RunnerTab {
  id: string;
  kind: 'runner';
  config: RunConfig;
  runId?: string;
  running?: boolean;
  executions: Execution[];
  summary?: Summary;
  total?: number;
  error?: string;
  dirty?: false;
}

export type Tab = RequestTab | CollectionTab | EnvironmentTab | RunnerTab;

export type Modal =
  | { kind: 'import' }
  | { kind: 'settings' }
  | { kind: 'cookies' }
  | { kind: 'code'; tabId: string }
  | { kind: 'saveAs'; tabId: string }
  | { kind: 'prompt'; title: string; label: string; value: string; onOk: (v: string) => void }
  | { kind: 'confirm'; title: string; message: string; okLabel?: string; danger?: boolean; onOk: () => void; extra?: { label: string; onClick: () => void } }
  | { kind: 'about' };

export interface Toast {
  id: string;
  kind: 'info' | 'error' | 'success';
  message: string;
}

interface State {
  ready: boolean;
  version: string;
  platform: string;
  workspaceDir: string;
  languages: { ID: string; Name: string }[];
  settings: Settings;
  collections: Record<string, Collection>;
  environments: Environment[];
  globals: Environment;
  history: HistoryEntry[];
  selectedEnvId: string;
  tabs: Tab[];
  activeTabId: string | null;
  sidebar: 'collections' | 'environments' | 'history';
  expanded: Record<string, boolean>;
  consoleOpen: boolean;
  console: ConsoleEntry[];
  modal: Modal | null;
  toasts: Toast[];
  sidebarWidth: number;
  splitRatio: number;
  layout: 'vertical' | 'horizontal';

  init(d: InitData): void;
  set(p: Partial<State>): void;
  toast(message: string, kind?: Toast['kind']): void;
  dismissToast(id: string): void;

  // collections
  collectionList(): Collection[];
  saveCollection(c: Collection): Promise<void>;
  mutateCollection(id: string, fn: (c: Collection) => void): Promise<void>;
  createCollection(name: string): Promise<Collection>;
  deleteCollection(id: string): Promise<void>;
  applyImport(r: ImportResult): void;

  // tabs
  openTab(tab: Tab): void;
  openItem(collectionId: string, itemId: string): void;
  openCollectionSettings(collectionId: string, folderId?: string): void;
  openEnvironment(envId: string): void;
  openRunner(cfg: Partial<RunConfig> & { collectionId: string }): void;
  newRequest(req?: Request, name?: string): void;
  updateTab(id: string, patch: Partial<Tab>, dirty?: boolean): void;
  closeTab(id: string): void;
  activate(id: string): void;
  saveTab(id: string): Promise<boolean>;
  send(tabId: string): Promise<void>;
  cancel(tabId: string): void;

  // environments
  saveEnvironment(e: Environment): Promise<Environment>;
  deleteEnvironment(id: string): Promise<void>;
  saveGlobals(g: Environment): Promise<void>;
  activeEnvironment(): Environment | undefined;

  // console
  log(e: ConsoleEntry): void;
}

const defaultSettings: Settings = {
  timeoutMs: 0,
  followRedirects: true,
  maxRedirects: 10,
  sslVerify: true,
  proxyMode: 'system',
  proxyUrl: '',
  maxResponseMB: 100,
  theme: 'system',
  saveCookies: true,
  historyLimit: 200,
  scriptTimeoutSec: 60,
  fontSize: 13,
  editorTabSize: 2,
  persistVariables: true,
};

const persistState = debounce((s: State) => {
  const tabs = s.tabs.map((t) => {
    if (t.kind === 'request') return { ...t, result: undefined, sending: false, requestId: undefined, error: undefined };
    if (t.kind === 'runner') return { ...t, executions: [], summary: undefined, running: false, runId: undefined };
    return t;
  });
  api
    .saveUIState({
      tabs,
      activeTabId: s.activeTabId,
      selectedEnvId: s.selectedEnvId,
      sidebar: s.sidebar,
      expanded: s.expanded,
      consoleOpen: s.consoleOpen,
      sidebarWidth: s.sidebarWidth,
      splitRatio: s.splitRatio,
      layout: s.layout,
    })
    .catch(() => {});
}, 600);

export const useStore = create<State>((set, get) => ({
  ready: false,
  version: '',
  platform: '',
  workspaceDir: '',
  languages: [],
  settings: defaultSettings,
  collections: {},
  environments: [],
  globals: { id: 'globals', name: 'Globals', values: [] },
  history: [],
  selectedEnvId: '',
  tabs: [],
  activeTabId: null,
  sidebar: 'collections',
  expanded: {},
  consoleOpen: false,
  console: [],
  modal: null,
  toasts: [],
  sidebarWidth: 300,
  splitRatio: 0.5,
  layout: 'vertical',

  init(d) {
    const collections: Record<string, Collection> = {};
    for (const c of d.collections) collections[c.info._postman_id] = c;
    const ui = d.uiState || {};
    const tabs: Tab[] = Array.isArray(ui.tabs)
      ? ui.tabs.filter((t: Tab) => {
          if (t.kind === 'request') return !t.collectionId || collections[t.collectionId];
          if (t.kind === 'collection' || t.kind === 'runner') return collections[t.kind === 'runner' ? t.config.collectionId : t.collectionId];
          if (t.kind === 'environment') return t.envId === 'globals' || d.environments.some((e) => e.id === t.envId);
          return false;
        })
      : [];
    set({
      ready: true,
      version: d.version,
      platform: d.platform,
      workspaceDir: d.workspaceDir,
      languages: d.languages || [],
      settings: { ...defaultSettings, ...d.settings },
      collections,
      environments: d.environments,
      globals: d.globals,
      history: d.history,
      tabs,
      activeTabId: tabs.some((t) => t.id === ui.activeTabId) ? ui.activeTabId : tabs[0]?.id ?? null,
      selectedEnvId: d.environments.some((e) => e.id === ui.selectedEnvId) ? ui.selectedEnvId : '',
      sidebar: ui.sidebar || 'collections',
      expanded: ui.expanded || {},
      consoleOpen: !!ui.consoleOpen,
      sidebarWidth: ui.sidebarWidth || 300,
      splitRatio: ui.splitRatio || 0.5,
      layout: ui.layout || 'vertical',
    });
    d.errors?.forEach((e) => get().toast(e, 'error'));
  },

  set(p) {
    set(p);
  },

  toast(message, kind = 'info') {
    const id = uuid();
    set((s) => ({ toasts: [...s.toasts, { id, kind, message }] }));
    setTimeout(() => get().dismissToast(id), kind === 'error' ? 8000 : 3500);
  },
  dismissToast(id) {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
  },

  collectionList() {
    return Object.values(get().collections).sort((a, b) => a.info.name.localeCompare(b.info.name));
  },

  async saveCollection(c) {
    set((s) => ({ collections: { ...s.collections, [c.info._postman_id]: c } }));
    try {
      await api.saveCollection(c);
    } catch (e) {
      get().toast('Could not save collection: ' + (e as Error).message, 'error');
    }
  },

  async mutateCollection(id, fn) {
    const c = get().collections[id];
    if (!c) return;
    const next = clone(c);
    fn(next);
    await get().saveCollection(next);
  },

  async createCollection(name) {
    const c = await api.newCollection(name);
    set((s) => ({ collections: { ...s.collections, [c.info._postman_id]: c }, expanded: { ...s.expanded, [c.info._postman_id]: true }, sidebar: 'collections' }));
    return c;
  },

  async deleteCollection(id) {
    await api.deleteCollection(id);
    set((s) => {
      const collections = { ...s.collections };
      delete collections[id];
      const tabs = s.tabs.filter((t) => !((t.kind === 'request' || t.kind === 'collection') && t.collectionId === id) && !(t.kind === 'runner' && t.config.collectionId === id));
      return { collections, tabs, activeTabId: tabs.some((t) => t.id === s.activeTabId) ? s.activeTabId : tabs[tabs.length - 1]?.id ?? null };
    });
  },

  applyImport(r) {
    set((s) => {
      const collections = { ...s.collections };
      r.collections.forEach((c) => (collections[c.info._postman_id] = c));
      const environments = [...s.environments.filter((e) => !r.environments.some((n) => n.id === e.id)), ...r.environments].sort((a, b) => a.name.localeCompare(b.name));
      return { collections, environments, globals: r.globals || s.globals };
    });
    if (r.request) get().newRequest(r.request, r.request.url?.raw || 'Imported request');
    const parts: string[] = [];
    if (r.collections.length) parts.push(`${r.collections.length} collection(s)`);
    if (r.environments.length) parts.push(`${r.environments.length} environment(s)`);
    if (r.globals) parts.push('globals');
    if (parts.length) get().toast('Imported ' + parts.join(', '), 'success');
    r.errors.forEach((e) => get().toast(e, 'error'));
  },

  openTab(tab) {
    set((s) => ({ tabs: [...s.tabs, tab], activeTabId: tab.id }));
  },

  openItem(collectionId, itemId) {
    const existing = get().tabs.find((t) => t.kind === 'request' && t.collectionId === collectionId && t.itemId === itemId);
    if (existing) return get().activate(existing.id);
    const c = get().collections[collectionId];
    const f = c && findItem(c.item, itemId);
    if (!f) return;
    const draft = clone(f.item);
    draft.request = normalizeRequest(draft.request);
    get().openTab({ id: uuid(), kind: 'request', collectionId, itemId, draft });
  },

  openCollectionSettings(collectionId, folderId) {
    const existing = get().tabs.find((t) => t.kind === 'collection' && t.collectionId === collectionId && t.folderId === folderId);
    if (existing) return get().activate(existing.id);
    const c = get().collections[collectionId];
    if (!c) return;
    let draft: Item;
    if (folderId) {
      const f = findItem(c.item, folderId);
      if (!f) return;
      draft = clone({ ...f.item, item: undefined }) as Item;
    } else {
      draft = clone({ id: c.info._postman_id, name: c.info.name, description: c.info.description, auth: c.auth, event: c.event, variable: c.variable }) as Item;
    }
    get().openTab({ id: uuid(), kind: 'collection', collectionId, folderId, draft });
  },

  openEnvironment(envId) {
    const existing = get().tabs.find((t) => t.kind === 'environment' && t.envId === envId);
    if (existing) return get().activate(existing.id);
    const env = envId === 'globals' ? get().globals : get().environments.find((e) => e.id === envId);
    if (!env) return;
    get().openTab({ id: uuid(), kind: 'environment', envId, draft: clone(env) });
  },

  openRunner(cfg) {
    get().openTab({
      id: uuid(),
      kind: 'runner',
      config: { iterations: 1, delayMs: 0, persistVariables: true, bail: false, environmentId: get().selectedEnvId, ...cfg },
      executions: [],
    });
  },

  newRequest(req, name) {
    const item = newRequestItem(name || 'Untitled Request', req);
    get().openTab({ id: uuid(), kind: 'request', itemId: item.id, draft: item, dirty: !!req });
  },

  updateTab(id, patch, dirty = true) {
    set((s) => ({
      tabs: s.tabs.map((t) => (t.id === id ? ({ ...t, ...patch, ...(dirty ? { dirty: true } : {}) } as Tab) : t)),
    }));
  },

  closeTab(id) {
    set((s) => {
      const idx = s.tabs.findIndex((t) => t.id === id);
      const tabs = s.tabs.filter((t) => t.id !== id);
      let activeTabId = s.activeTabId;
      if (s.activeTabId === id) activeTabId = tabs[Math.min(idx, tabs.length - 1)]?.id ?? null;
      return { tabs, activeTabId };
    });
  },

  activate(id) {
    set({ activeTabId: id });
  },

  async saveTab(id) {
    const tab = get().tabs.find((t) => t.id === id);
    if (!tab) return false;
    if (tab.kind === 'request') {
      if (!tab.collectionId) {
        set({ modal: { kind: 'saveAs', tabId: id } });
        return false;
      }
      let found = true;
      await get().mutateCollection(tab.collectionId, (c) => {
        found = replaceItem(c, clone(tab.draft));
      });
      if (!found) {
        set({ modal: { kind: 'saveAs', tabId: id } });
        return false;
      }
      get().updateTab(id, { dirty: false }, false);
      return true;
    }
    if (tab.kind === 'collection') {
      const d = tab.draft;
      await get().mutateCollection(tab.collectionId, (c) => {
        if (tab.folderId) {
          const f = findItem(c.item, tab.folderId);
          if (f) Object.assign(f.item, { name: d.name, description: d.description, auth: d.auth, event: d.event, variable: d.variable });
        } else {
          c.info.name = d.name;
          c.info.description = d.description as string;
          c.auth = d.auth;
          c.event = d.event;
          c.variable = d.variable;
        }
      });
      get().updateTab(id, { dirty: false }, false);
      return true;
    }
    if (tab.kind === 'environment') {
      if (tab.envId === 'globals') await get().saveGlobals(tab.draft);
      else await get().saveEnvironment(tab.draft);
      get().updateTab(id, { dirty: false }, false);
      return true;
    }
    return true;
  },

  async send(tabId) {
    const tab = get().tabs.find((t) => t.id === tabId);
    if (!tab || tab.kind !== 'request' || tab.sending) return;
    const requestId = uuid();
    get().updateTab(tabId, { sending: true, requestId, error: undefined }, false);
    const coll = tab.collectionId ? get().collections[tab.collectionId] : undefined;
    try {
      const res = await api.send({ requestId, collection: coll, itemId: tab.itemId, item: tab.draft, environmentId: get().selectedEnvId || undefined });
      set((s) => {
        const patch: Partial<State> = { history: res.history || s.history, globals: res.globals || s.globals };
        if (res.environment) patch.environments = s.environments.map((e) => (e.id === res.environment!.id ? res.environment! : e));
        // Refresh open environment editors that are not being edited.
        patch.tabs = s.tabs.map((t) => {
          if (t.id === tabId && t.kind === 'request') return { ...t, result: res, sending: false };
          if (t.kind === 'environment' && !t.dirty) {
            if (t.envId === 'globals' && res.globals) return { ...t, draft: clone(res.globals) };
            if (res.environment && t.envId === res.environment.id) return { ...t, draft: clone(res.environment) };
          }
          return t;
        });
        return patch;
      });
      if (coll && res.collectionVariables) syncCollectionVariables(coll.info._postman_id, res.collectionVariables);
    } catch (e) {
      get().updateTab(tabId, { sending: false, error: (e as Error).message }, false);
    }
  },

  cancel(tabId) {
    const tab = get().tabs.find((t) => t.id === tabId);
    if (tab && tab.kind === 'request' && tab.requestId) api.cancel(tab.requestId);
  },

  async saveEnvironment(e) {
    const saved = await api.saveEnvironment(e);
    set((s) => {
      const exists = s.environments.some((x) => x.id === saved.id);
      const environments = (exists ? s.environments.map((x) => (x.id === saved.id ? saved : x)) : [...s.environments, saved]).sort((a, b) => a.name.localeCompare(b.name));
      return { environments };
    });
    return saved;
  },

  async deleteEnvironment(id) {
    await api.deleteEnvironment(id);
    set((s) => {
      const tabs = s.tabs.filter((t) => !(t.kind === 'environment' && t.envId === id));
      return {
        environments: s.environments.filter((e) => e.id !== id),
        selectedEnvId: s.selectedEnvId === id ? '' : s.selectedEnvId,
        tabs,
        activeTabId: tabs.some((t) => t.id === s.activeTabId) ? s.activeTabId : tabs[tabs.length - 1]?.id ?? null,
      };
    });
  },

  async saveGlobals(g) {
    const saved = await api.saveGlobals(g);
    set({ globals: saved });
  },

  activeEnvironment() {
    return get().environments.find((e) => e.id === get().selectedEnvId);
  },

  log(e) {
    set((s) => ({ console: s.console.length >= 2000 ? [...s.console.slice(-1500), e] : [...s.console, e] }));
  },
}));

/** Applies collection variables changed by scripts and saves the collection. */
export function syncCollectionVariables(collectionId: string, vars: Var[]) {
  const st = useStore.getState();
  const c = st.collections[collectionId];
  if (!c) return;
  const next = vars.map((v) => {
    const prev = (c.variable || []).find((p) => p.key === v.key);
    const out = { ...(prev || {}), key: v.key, value: v.value } as Record<string, unknown>;
    if (v.type && !prev?.type) out.type = v.type;
    if (!v.enabled) out.disabled = true;
    else delete out.disabled;
    return out as unknown as NonNullable<Collection['variable']>[number];
  });
  if (JSON.stringify(next) === JSON.stringify(c.variable || [])) return;
  st.mutateCollection(collectionId, (col) => {
    col.variable = next;
  });
}

// Persist UI state on relevant changes.
useStore.subscribe((s, prev) => {
  if (!s.ready) return;
  if (
    s.tabs !== prev.tabs ||
    s.activeTabId !== prev.activeTabId ||
    s.selectedEnvId !== prev.selectedEnvId ||
    s.sidebar !== prev.sidebar ||
    s.expanded !== prev.expanded ||
    s.consoleOpen !== prev.consoleOpen ||
    s.sidebarWidth !== prev.sidebarWidth ||
    s.splitRatio !== prev.splitRatio ||
    s.layout !== prev.layout
  ) {
    persistState(s);
  }
});

export const activeTab = (s: State) => s.tabs.find((t) => t.id === s.activeTabId) || null;

export function tabTitle(t: Tab, s: State): string {
  switch (t.kind) {
    case 'request':
      return t.draft.name;
    case 'collection':
      return t.draft.name || s.collections[t.collectionId]?.info.name || 'Collection';
    case 'environment':
      return t.draft.name || (t.envId === 'globals' ? 'Globals' : 'Environment');
    case 'runner':
      return 'Runner · ' + (s.collections[t.config.collectionId]?.info.name || '');
  }
}

import { useMemo, useState } from 'react';
import { api } from '../api';
import { useStore } from '../store';
import type { Collection, HistoryEntry, Item } from '../types';
import { clone, findItem, isFolder, methodClass, moveItem, newFolder, newRequestItem, reassignIds, removeItem, uuid } from '../util';
import { Menu, MenuItem } from './Modal';

type MenuState = { x: number; y: number; items: MenuItem[] } | null;

export default function Sidebar() {
  const sidebar = useStore((s) => s.sidebar);
  const set = useStore((s) => s.set);
  const [menu, setMenu] = useState<MenuState>(null);
  return (
    <aside className="sidebar">
      <div className="side-tabs">
        {(['collections', 'environments', 'history'] as const).map((t) => (
          <button key={t} className={'side-tab' + (sidebar === t ? ' active' : '')} onClick={() => set({ sidebar: t })}>
            {t === 'collections' ? 'Collections' : t === 'environments' ? 'Environments' : 'History'}
          </button>
        ))}
      </div>
      {sidebar === 'collections' && <Collections setMenu={setMenu} />}
      {sidebar === 'environments' && <Environments setMenu={setMenu} />}
      {sidebar === 'history' && <History />}
      {menu && <Menu {...menu} onClose={() => setMenu(null)} />}
    </aside>
  );
}

function prompt(title: string, label: string, value: string, onOk: (v: string) => void) {
  useStore.getState().set({ modal: { kind: 'prompt', title, label, value, onOk } });
}

function confirm(title: string, message: string, onOk: () => void, okLabel = 'Delete') {
  useStore.getState().set({ modal: { kind: 'confirm', title, message, onOk, okLabel, danger: true } });
}

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

function matches(it: Item, q: string): boolean {
  if (it.name.toLowerCase().includes(q)) return true;
  if (it.request?.url?.raw?.toLowerCase().includes(q)) return true;
  return (it.item || []).some((c) => matches(c, q));
}

type DragState = { collectionId: string; itemId: string } | null;
let drag: DragState = null;

function Collections({ setMenu }: { setMenu: (m: MenuState) => void }) {
  const st = useStore();
  const [filter, setFilter] = useState('');
  const list = st.collectionList();
  const q = filter.trim().toLowerCase();
  return (
    <div className="side-body">
      <div className="side-toolbar">
        <input className="search" placeholder="Filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <button
          className="icon-btn"
          title="New collection"
          onClick={() => prompt('New Collection', 'Name', 'New Collection', async (name) => void (await st.createCollection(name)))}
        >
          ＋
        </button>
      </div>
      <div className="tree">
        {list.length === 0 && (
          <div className="side-empty">
            <p>No collections yet.</p>
            <button className="btn" onClick={() => st.set({ modal: { kind: 'import' } })}>
              Import a Postman collection
            </button>
            <button className="btn" onClick={() => prompt('New Collection', 'Name', 'New Collection', async (name) => void (await st.createCollection(name)))}>
              Create a collection
            </button>
          </div>
        )}
        {list
          .filter((c) => !q || c.info.name.toLowerCase().includes(q) || c.item.some((i) => matches(i, q)))
          .map((c) => (
            <CollectionNode key={c.info._postman_id} c={c} q={q} setMenu={setMenu} />
          ))}
      </div>
    </div>
  );
}

function collectionMenu(c: Collection): MenuItem[] {
  const st = useStore.getState();
  const id = c.info._postman_id;
  return [
    { label: 'Add Request', onClick: () => addRequest(id, null) },
    { label: 'Add Folder', onClick: () => addFolder(id, null) },
    { label: 'Run collection', onClick: () => st.openRunner({ collectionId: id }) },
    { label: '', onClick: () => {}, separator: true },
    { label: 'Edit (auth, scripts, variables)', onClick: () => st.openCollectionSettings(id) },
    { label: 'Rename', onClick: () => prompt('Rename collection', 'Name', c.info.name, (v) => st.mutateCollection(id, (x) => void (x.info.name = v))) },
    {
      label: 'Duplicate',
      onClick: async () => {
        const copy = clone(c);
        copy.info._postman_id = uuid();
        copy.info.name = c.info.name + ' Copy';
        copy.item.forEach(reassignIds);
        await st.saveCollection(copy);
      },
    },
    {
      label: 'Export',
      onClick: async () => {
        try {
          const p = await api.exportCollection(c);
          if (p) st.toast('Exported to ' + p, 'success');
        } catch (e) {
          st.toast((e as Error).message, 'error');
        }
      },
    },
    { label: '', onClick: () => {}, separator: true },
    { label: 'Delete', danger: true, onClick: () => confirm('Delete collection', `Delete “${c.info.name}”? This cannot be undone.`, () => void st.deleteCollection(id)) },
  ];
}

function addRequest(collectionId: string, folderId: string | null) {
  const st = useStore.getState();
  const it = newRequestItem('New Request');
  st.mutateCollection(collectionId, (c) => {
    if (folderId) {
      const f = findItem(c.item, folderId);
      if (f) (f.item.item = f.item.item || []).push(it);
    } else c.item.push(it);
  }).then(() => {
    st.set({ expanded: { ...useStore.getState().expanded, [collectionId]: true, ...(folderId ? { [folderId]: true } : {}) } });
    st.openItem(collectionId, it.id);
  });
}

function addFolder(collectionId: string, folderId: string | null) {
  prompt('New Folder', 'Name', 'New Folder', (name) => {
    const st = useStore.getState();
    const f = newFolder(name);
    st.mutateCollection(collectionId, (c) => {
      if (folderId) {
        const p = findItem(c.item, folderId);
        if (p) (p.item.item = p.item.item || []).push(f);
      } else c.item.push(f);
    });
    st.set({ expanded: { ...st.expanded, [collectionId]: true, ...(folderId ? { [folderId]: true } : {}) } });
  });
}

function itemMenu(collectionId: string, it: Item): MenuItem[] {
  const st = useStore.getState();
  const folder = isFolder(it);
  const items: MenuItem[] = [];
  if (folder) {
    items.push(
      { label: 'Add Request', onClick: () => addRequest(collectionId, it.id) },
      { label: 'Add Folder', onClick: () => addFolder(collectionId, it.id) },
      { label: 'Run folder', onClick: () => st.openRunner({ collectionId, folderId: it.id }) },
      { label: 'Edit (auth, scripts)', onClick: () => st.openCollectionSettings(collectionId, it.id) },
      { label: '', onClick: () => {}, separator: true },
    );
  } else {
    items.push({ label: 'Open', onClick: () => st.openItem(collectionId, it.id) });
  }
  items.push(
    {
      label: 'Rename',
      onClick: () =>
        prompt('Rename', 'Name', it.name, (v) => {
          st.mutateCollection(collectionId, (c) => {
            const f = findItem(c.item, it.id);
            if (f) f.item.name = v;
          });
          // keep open tabs in sync
          useStore.getState().tabs.forEach((t) => {
            if (t.kind === 'request' && t.collectionId === collectionId && t.itemId === it.id) st.updateTab(t.id, { draft: { ...t.draft, name: v } }, false);
          });
        }),
    },
    {
      label: 'Duplicate',
      onClick: () =>
        st.mutateCollection(collectionId, (c) => {
          const f = findItem(c.item, it.id);
          if (!f) return;
          const copy = reassignIds(clone(f.item));
          copy.name = f.item.name + ' Copy';
          f.parent.splice(f.parent.indexOf(f.item) + 1, 0, copy);
        }),
    },
    { label: '', onClick: () => {}, separator: true },
    {
      label: 'Delete',
      danger: true,
      onClick: () =>
        confirm(folder ? 'Delete folder' : 'Delete request', `Delete “${it.name}”${folder ? ' and everything in it' : ''}?`, () => {
          st.mutateCollection(collectionId, (c) => void removeItem(c, it.id));
          useStore.getState().tabs.forEach((t) => {
            if (t.kind === 'request' && t.collectionId === collectionId && (t.itemId === it.id || (folder && findItem(it.item || [], t.itemId)))) st.closeTab(t.id);
          });
        }),
    },
  );
  return items;
}

function onDrop(targetCollection: string, targetId: string | null, where: 'before' | 'after' | 'into') {
  const st = useStore.getState();
  const d = drag;
  drag = null;
  if (!d) return;
  if (d.collectionId === targetCollection) {
    st.mutateCollection(targetCollection, (c) => void moveItem(c, d.itemId, targetId, where));
    return;
  }
  // Cross-collection move.
  const src = st.collections[d.collectionId];
  const f = src && findItem(src.item, d.itemId);
  if (!f) return;
  const moved = clone(f.item);
  st.mutateCollection(d.collectionId, (c) => void removeItem(c, d.itemId));
  st.mutateCollection(targetCollection, (c) => {
    c.item.push(moved);
    if (targetId) moveItem(c, moved.id, targetId, where);
  });
  useStore.getState().tabs.forEach((t) => {
    if (t.kind === 'request' && t.collectionId === d.collectionId && (t.itemId === moved.id || findItem(moved.item || [], t.itemId))) st.updateTab(t.id, { collectionId: targetCollection }, false);
  });
}

function CollectionNode({ c, q, setMenu }: { c: Collection; q: string; setMenu: (m: MenuState) => void }) {
  const st = useStore();
  const id = c.info._postman_id;
  const open = q ? true : !!st.expanded[id];
  const [over, setOver] = useState(false);
  const count = useMemo(() => {
    let n = 0;
    const walk = (items: Item[]) => items.forEach((i) => (i.request ? n++ : walk(i.item || [])));
    walk(c.item);
    return n;
  }, [c]);
  return (
    <div className="tree-collection">
      <div
        className={'tree-row collection' + (over ? ' drop-into' : '')}
        onClick={() => st.set({ expanded: { ...st.expanded, [id]: !open } })}
        onContextMenu={(e) => {
          e.preventDefault();
          setMenu({ x: e.clientX, y: e.clientY, items: collectionMenu(c) });
        }}
        onDragOver={(e) => {
          if (!drag) return;
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setOver(false);
          onDrop(id, null, 'into');
        }}
      >
        <span className={'caret' + (open ? ' open' : '')}>▸</span>
        <span className="coll-icon">▤</span>
        <span className="tree-name">
          {c.info.name}
          <span className="muted small"> {count} request{count === 1 ? '' : 's'}</span>
        </span>
        <button
          className="icon-btn more"
          onClick={(e) => {
            e.stopPropagation();
            setMenu({ x: e.clientX, y: e.clientY, items: collectionMenu(c) });
          }}
        >
          ⋯
        </button>
      </div>
      {open && (
        <div className="tree-children">
          {c.item.length === 0 && (
            <div className="tree-empty">
              This collection is empty. <button className="link" onClick={() => addRequest(id, null)}>Add a request</button>
            </div>
          )}
          {c.item
            .filter((it) => !q || matches(it, q))
            .map((it) => (
              <ItemNode key={it.id} collectionId={id} it={it} depth={1} q={q} setMenu={setMenu} />
            ))}
        </div>
      )}
    </div>
  );
}

function ItemNode({ collectionId, it, depth, q, setMenu }: { collectionId: string; it: Item; depth: number; q: string; setMenu: (m: MenuState) => void }) {
  const st = useStore();
  const folder = isFolder(it);
  const open = q ? true : !!st.expanded[it.id];
  const [dropPos, setDropPos] = useState<'before' | 'after' | 'into' | null>(null);
  const active = st.tabs.find((t) => t.id === st.activeTabId);
  const isActive = active?.kind === 'request' && active.itemId === it.id;
  const method = it.request?.method || 'GET';
  return (
    <>
      <div
        className={'tree-row' + (isActive ? ' active' : '') + (dropPos ? ' drop-' + dropPos : '')}
        style={{ paddingLeft: depth * 14 + 6 }}
        draggable
        onDragStart={(e) => {
          drag = { collectionId, itemId: it.id };
          e.dataTransfer.effectAllowed = 'move';
          e.dataTransfer.setData('text/plain', it.name);
        }}
        onDragOver={(e) => {
          if (!drag || drag.itemId === it.id) return;
          e.preventDefault();
          e.stopPropagation();
          const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
          const y = (e.clientY - r.top) / r.height;
          setDropPos(folder ? (y < 0.25 ? 'before' : y > 0.75 ? 'after' : 'into') : y < 0.5 ? 'before' : 'after');
        }}
        onDragLeave={() => setDropPos(null)}
        onDrop={(e) => {
          e.preventDefault();
          e.stopPropagation();
          const pos = dropPos;
          setDropPos(null);
          if (pos) onDrop(collectionId, it.id, pos);
        }}
        onClick={() => (folder ? st.set({ expanded: { ...st.expanded, [it.id]: !open } }) : st.openItem(collectionId, it.id))}
        onContextMenu={(e) => {
          e.preventDefault();
          setMenu({ x: e.clientX, y: e.clientY, items: itemMenu(collectionId, it) });
        }}
      >
        {folder ? (
          <>
            <span className={'caret' + (open ? ' open' : '')}>▸</span>
            <span className="folder-icon">▰</span>
          </>
        ) : (
          <span className={'method-badge ' + methodClass(method)}>{method.length > 6 ? method.slice(0, 4) : method}</span>
        )}
        <span className="tree-name">{it.name}</span>
        <button
          className="icon-btn more"
          onClick={(e) => {
            e.stopPropagation();
            setMenu({ x: e.clientX, y: e.clientY, items: itemMenu(collectionId, it) });
          }}
        >
          ⋯
        </button>
      </div>
      {folder &&
        open &&
        (it.item || [])
          .filter((c) => !q || matches(c, q))
          .map((child) => <ItemNode key={child.id} collectionId={collectionId} it={child} depth={depth + 1} q={q} setMenu={setMenu} />)}
    </>
  );
}

// ---------------------------------------------------------------------------
// Environments
// ---------------------------------------------------------------------------

function Environments({ setMenu }: { setMenu: (m: MenuState) => void }) {
  const st = useStore();
  const create = () =>
    prompt('New Environment', 'Name', 'New Environment', async (name) => {
      const e = await st.saveEnvironment({ id: '', name, values: [] });
      st.openEnvironment(e.id);
    });
  return (
    <div className="side-body">
      <div className="side-toolbar">
        <span className="muted small">Active: {st.activeEnvironment()?.name || 'No environment'}</span>
        <button className="icon-btn" title="New environment" onClick={create}>
          ＋
        </button>
      </div>
      <div className="tree">
        <div className="tree-row" onClick={() => st.openEnvironment('globals')}>
          <span className="env-icon">◎</span>
          <span className="tree-name">Globals</span>
          <span className="muted small">{st.globals.values.length}</span>
        </div>
        {st.environments.map((e) => (
          <div
            key={e.id}
            className={'tree-row' + (st.selectedEnvId === e.id ? ' selected-env' : '')}
            onClick={() => st.openEnvironment(e.id)}
            onContextMenu={(ev) => {
              ev.preventDefault();
              setMenu({ x: ev.clientX, y: ev.clientY, items: envMenu(e.id) });
            }}
          >
            <input
              type="radio"
              title="Set active"
              checked={st.selectedEnvId === e.id}
              onClick={(ev) => ev.stopPropagation()}
              onChange={() => st.set({ selectedEnvId: e.id })}
            />
            <span className="tree-name">{e.name}</span>
            <span className="muted small">{e.values.length}</span>
            <button
              className="icon-btn more"
              onClick={(ev) => {
                ev.stopPropagation();
                setMenu({ x: ev.clientX, y: ev.clientY, items: envMenu(e.id) });
              }}
            >
              ⋯
            </button>
          </div>
        ))}
        {st.environments.length === 0 && (
          <div className="side-empty">
            <p>Environments hold variables like {'{{baseUrl}}'} that change between setups.</p>
            <button className="btn" onClick={create}>
              Create an environment
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function envMenu(id: string): MenuItem[] {
  const st = useStore.getState();
  const env = st.environments.find((e) => e.id === id)!;
  return [
    { label: st.selectedEnvId === id ? 'Deactivate' : 'Set active', onClick: () => st.set({ selectedEnvId: st.selectedEnvId === id ? '' : id }) },
    { label: 'Edit', onClick: () => st.openEnvironment(id) },
    { label: 'Rename', onClick: () => prompt('Rename environment', 'Name', env.name, (v) => void st.saveEnvironment({ ...env, name: v })) },
    { label: 'Duplicate', onClick: () => void st.saveEnvironment({ ...clone(env), id: '', name: env.name + ' Copy' }) },
    {
      label: 'Export',
      onClick: async () => {
        try {
          const p = await api.exportEnvironment(id);
          if (p) st.toast('Exported to ' + p, 'success');
        } catch (e) {
          st.toast((e as Error).message, 'error');
        }
      },
    },
    { label: '', onClick: () => {}, separator: true },
    { label: 'Delete', danger: true, onClick: () => confirm('Delete environment', `Delete “${env.name}”?`, () => void st.deleteEnvironment(id)) },
  ];
}

// ---------------------------------------------------------------------------
// History
// ---------------------------------------------------------------------------

function dayLabel(d: Date) {
  const today = new Date();
  const y = new Date();
  y.setDate(today.getDate() - 1);
  if (d.toDateString() === today.toDateString()) return 'Today';
  if (d.toDateString() === y.toDateString()) return 'Yesterday';
  return d.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' });
}

function History() {
  const st = useStore();
  const [filter, setFilter] = useState('');
  const groups = useMemo(() => {
    const out: { label: string; entries: HistoryEntry[] }[] = [];
    const q = filter.toLowerCase();
    for (const h of st.history) {
      if (q && !h.url.toLowerCase().includes(q) && !(h.name || '').toLowerCase().includes(q)) continue;
      const label = dayLabel(new Date(h.time));
      let g = out[out.length - 1];
      if (!g || g.label !== label) out.push((g = { label, entries: [] }));
      g.entries.push(h);
    }
    return out;
  }, [st.history, filter]);
  return (
    <div className="side-body">
      <div className="side-toolbar">
        <input className="search" placeholder="Filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <button
          className="icon-btn"
          title="Clear all"
          onClick={() => confirm('Clear history', 'Delete all history entries?', async () => st.set({ history: await api.deleteHistory('') }), 'Clear')}
        >
          🗑
        </button>
      </div>
      <div className="tree">
        {groups.length === 0 && <div className="side-empty">Requests you send will appear here.</div>}
        {groups.map((g) => (
          <div key={g.label}>
            <div className="history-day">{g.label}</div>
            {g.entries.map((h) => (
              <div key={h.id} className="tree-row history" title={h.url} onClick={() => st.newRequest(h.request, h.name || h.url)}>
                <span className={'method-badge ' + methodClass(h.method)}>{h.method.length > 6 ? h.method.slice(0, 4) : h.method}</span>
                <span className="tree-name">{h.url || h.name}</span>
                {h.code ? <span className={'muted small code-' + Math.floor(h.code / 100)}>{h.code}</span> : null}
                <button
                  className="icon-btn more"
                  title="Delete"
                  onClick={async (e) => {
                    e.stopPropagation();
                    st.set({ history: await api.deleteHistory(h.id) });
                  }}
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

import { useState } from 'react';
import { tabTitle, useStore } from '../store';
import { varValueString } from '../util';
import { Menu } from './Modal';

export function Header() {
  const st = useStore();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [peek, setPeek] = useState(false);
  const env = st.activeEnvironment();
  const firstCollection = st.collectionList()[0];
  return (
    <header className="topbar">
      <div className="brand">
        <span className="logo">⇄</span> httpman
      </div>
      <button
        className="btn primary small"
        onClick={(e) => {
          const r = (e.target as HTMLElement).getBoundingClientRect();
          setMenu({ x: r.left, y: r.bottom + 4 });
        }}
      >
        ＋ New
      </button>
      <button className="btn small" onClick={() => st.set({ modal: { kind: 'import' } })}>
        Import
      </button>
      <button className="btn small" disabled={!firstCollection} onClick={() => firstCollection && st.openRunner({ collectionId: firstCollection.info._postman_id })}>
        Runner
      </button>
      <span className="spacer" />
      <select className="env-select" value={st.selectedEnvId} onChange={(e) => st.set({ selectedEnvId: e.target.value })} title="Active environment">
        <option value="">No Environment</option>
        {st.environments.map((e) => (
          <option key={e.id} value={e.id}>
            {e.name}
          </option>
        ))}
      </select>
      <div className="peek-wrap">
        <button className="icon-btn" title="Environment quick look" onClick={() => setPeek(!peek)}>
          👁
        </button>
        {peek && (
          <div className="peek" onMouseLeave={() => setPeek(false)}>
            <div className="peek-head">
              <b>{env ? env.name : 'No environment selected'}</b>
              {env && (
                <button className="link" onClick={() => (st.openEnvironment(env.id), setPeek(false))}>
                  Edit
                </button>
              )}
            </div>
            {env && <VarList values={env.values} />}
            <div className="peek-head">
              <b>Globals</b>
              <button className="link" onClick={() => (st.openEnvironment('globals'), setPeek(false))}>
                Edit
              </button>
            </div>
            <VarList values={st.globals.values} />
          </div>
        )}
      </div>
      <button className="icon-btn" title="Cookies" onClick={() => st.set({ modal: { kind: 'cookies' } })}>
        🍪
      </button>
      <button className="icon-btn" title="Settings" onClick={() => st.set({ modal: { kind: 'settings' } })}>
        ⚙
      </button>
      {menu && (
        <Menu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: 'Request', onClick: () => st.newRequest(), shortcut: 'Ctrl+T' },
            {
              label: 'Collection',
              onClick: () => st.set({ modal: { kind: 'prompt', title: 'New Collection', label: 'Name', value: 'New Collection', onOk: (n) => void st.createCollection(n) } }),
            },
            {
              label: 'Environment',
              onClick: () =>
                st.set({
                  modal: {
                    kind: 'prompt',
                    title: 'New Environment',
                    label: 'Name',
                    value: 'New Environment',
                    onOk: async (n) => {
                      const e = await st.saveEnvironment({ id: '', name: n, values: [] });
                      st.openEnvironment(e.id);
                    },
                  },
                }),
            },
          ]}
        />
      )}
    </header>
  );
}

function VarList({ values }: { values: { key: string; value: unknown; enabled: boolean; type?: string }[] }) {
  if (!values.length) return <div className="muted small pad">No variables</div>;
  return (
    <table className="data-table compact">
      <tbody>
        {values.map((v, i) => (
          <tr key={i} className={v.enabled ? '' : 'disabled'}>
            <td className="mono">{v.key}</td>
            <td className="mono wrap">{v.type === 'secret' ? '••••••' : varValueString(v.value)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function TabBar() {
  const st = useStore();
  const [menu, setMenu] = useState<{ x: number; y: number; id: string } | null>(null);
  const close = (id: string) => {
    const t = st.tabs.find((x) => x.id === id);
    if (t?.dirty) {
      st.set({
        modal: {
          kind: 'confirm',
          title: 'Unsaved changes',
          message: `“${tabTitle(t, st)}” has unsaved changes. Close anyway?`,
          okLabel: "Don't save",
          danger: true,
          onOk: () => st.closeTab(id),
          extra: {
            label: 'Save',
            onClick: async () => {
              if (await st.saveTab(id)) st.closeTab(id);
            },
          },
        },
      });
    } else st.closeTab(id);
  };
  return (
    <div className="tabbar">
      {st.tabs.map((t) => (
        <div
          key={t.id}
          className={'tab' + (t.id === st.activeTabId ? ' active' : '')}
          onClick={() => st.activate(t.id)}
          onMouseDown={(e) => {
            if (e.button === 1) {
              e.preventDefault();
              close(t.id);
            }
          }}
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu({ x: e.clientX, y: e.clientY, id: t.id });
          }}
          title={tabTitle(t, st)}
        >
          {t.kind === 'request' && <span className={'tab-method m-' + t.draft.request!.method.toLowerCase()}>{t.draft.request!.method}</span>}
          {t.kind === 'environment' && <span className="tab-kind">ENV</span>}
          {t.kind === 'collection' && <span className="tab-kind">{t.folderId ? 'DIR' : 'COL'}</span>}
          {t.kind === 'runner' && <span className="tab-kind">RUN</span>}
          <span className="tab-title">{tabTitle(t, st)}</span>
          {t.dirty ? <span className="tab-dirty">●</span> : null}
          <button
            className="tab-close"
            onClick={(e) => {
              e.stopPropagation();
              close(t.id);
            }}
          >
            ×
          </button>
        </div>
      ))}
      <button className="tab-new" title="New request (Ctrl+T)" onClick={() => st.newRequest()}>
        ＋
      </button>
      {menu && (
        <Menu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: 'Close', onClick: () => close(menu.id) },
            { label: 'Close other tabs', onClick: () => st.set({ tabs: st.tabs.filter((t) => t.id === menu.id || t.dirty), activeTabId: menu.id }) },
            { label: 'Close all saved tabs', onClick: () => st.set({ tabs: st.tabs.filter((t) => t.dirty), activeTabId: st.tabs.find((t) => t.dirty)?.id ?? null }) },
            {
              label: 'Duplicate',
              onClick: () => {
                const t = st.tabs.find((x) => x.id === menu.id);
                if (t?.kind === 'request') st.newRequest(t.draft.request, t.draft.name + ' Copy');
              },
            },
          ]}
        />
      )}
    </div>
  );
}

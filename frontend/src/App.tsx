import { useEffect, useState } from 'react';
import { api, onEvent } from './api';
import CollectionEditor from './components/CollectionEditor';
import ConsolePanel from './components/ConsolePanel';
import Dialogs from './components/Dialogs';
import EnvironmentEditor from './components/EnvironmentEditor';
import { Header, TabBar } from './components/Header';
import RequestEditor from './components/RequestEditor';
import RunnerView from './components/RunnerView';
import Sidebar from './components/Sidebar';
import { activeTab, useStore } from './store';
import { useTheme } from './theme';

export default function App() {
  const st = useStore();
  const theme = useTheme();
  const tab = useStore(activeTab);
  const [fatal, setFatal] = useState('');

  useEffect(() => {
    api.init().then(st.init, (e) => setFatal((e as Error).message));
    const offs = [onEvent('console', (e) => useStore.getState().log(e)), onEvent('import', (r) => useStore.getState().applyImport(r))];
    return () => offs.forEach((f) => f());
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  useEffect(() => {
    const fn = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (!mod) return;
      const s = useStore.getState();
      const t = s.tabs.find((x) => x.id === s.activeTabId);
      const k = e.key.toLowerCase();
      if (k === 's') {
        e.preventDefault();
        if (t) s.saveTab(t.id);
      } else if (k === 'enter') {
        if (t?.kind === 'request') {
          e.preventDefault();
          s.send(t.id);
        }
      } else if (k === 't' || k === 'n') {
        e.preventDefault();
        s.newRequest();
      } else if (k === 'w') {
        e.preventDefault();
        if (t) {
          if (t.dirty) document.querySelector<HTMLButtonElement>('.tab.active .tab-close')?.click();
          else s.closeTab(t.id);
        }
      } else if (k === 'o') {
        e.preventDefault();
        s.set({ modal: { kind: 'import' } });
      } else if (e.altKey && k === 'c') {
        e.preventDefault();
        s.set({ consoleOpen: !s.consoleOpen });
      } else if (k === 'tab') {
        e.preventDefault();
        const idx = s.tabs.findIndex((x) => x.id === s.activeTabId);
        const next = s.tabs[(idx + (e.shiftKey ? -1 : 1) + s.tabs.length) % s.tabs.length];
        if (next) s.activate(next.id);
      }
    };
    window.addEventListener('keydown', fn);
    return () => window.removeEventListener('keydown', fn);
  }, []);

  if (fatal) {
    return (
      <div className="fatal">
        <h2>httpman could not start</h2>
        <p>{fatal}</p>
      </div>
    );
  }
  if (!st.ready) return <div className="fatal muted">Loading…</div>;

  const resizeSidebar = (e: React.MouseEvent) => {
    e.preventDefault();
    const move = (ev: MouseEvent) => st.set({ sidebarWidth: Math.min(600, Math.max(200, ev.clientX)) });
    const up = () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  };

  return (
    <div className="app" style={{ ['--sidebar-w' as string]: st.sidebarWidth + 'px', fontSize: st.settings.fontSize }}>
      <Header />
      <div className="main">
        <Sidebar />
        <div className="resizer" onMouseDown={resizeSidebar} />
        <section className="workspace">
          <TabBar />
          <div className="tab-content">
            {!tab && <Welcome />}
            {tab?.kind === 'request' && <RequestEditor key={tab.id} tab={tab} />}
            {tab?.kind === 'collection' && <CollectionEditor key={tab.id} tab={tab} />}
            {tab?.kind === 'environment' && <EnvironmentEditor key={tab.id} tab={tab} />}
            {tab?.kind === 'runner' && <RunnerView key={tab.id} tab={tab} />}
          </div>
          {st.consoleOpen && <ConsolePanel />}
        </section>
      </div>
      <footer className="statusbar">
        <button className="link" onClick={() => st.set({ consoleOpen: !st.consoleOpen })}>
          {st.consoleOpen ? '▾' : '▸'} Console
          {st.console.some((c) => c.level === 'error') && <span className="fail"> ●</span>}
        </button>
        <button className="link" onClick={() => st.set({ layout: st.layout === 'vertical' ? 'horizontal' : 'vertical' })} title="Toggle layout">
          {st.layout === 'vertical' ? '⬒ Stacked' : '◫ Side by side'}
        </button>
        <span className="spacer" />
        <span className="muted" title="Workspace folder">
          {st.workspaceDir}
        </span>
      </footer>
      <Dialogs />
      <div className="toasts">
        {st.toasts.map((t) => (
          <div key={t.id} className={'toast ' + t.kind} onClick={() => st.dismissToast(t.id)}>
            {t.message}
          </div>
        ))}
      </div>
    </div>
  );
}

function Welcome() {
  const st = useStore();
  return (
    <div className="welcome">
      <h1>httpman</h1>
      <p className="muted">A local HTTP client for Postman collections. Nothing leaves your machine except the requests you send.</p>
      <div className="welcome-actions">
        <button className="btn primary" onClick={() => st.newRequest()}>
          New request <span className="muted">Ctrl+T</span>
        </button>
        <button className="btn" onClick={() => st.set({ modal: { kind: 'import' } })}>
          Import collection <span className="muted">Ctrl+O</span>
        </button>
      </div>
      <table className="shortcuts">
        <tbody>
          {[
            ['Ctrl+Enter', 'Send request'],
            ['Ctrl+S', 'Save'],
            ['Ctrl+T', 'New request tab'],
            ['Ctrl+W', 'Close tab'],
            ['Ctrl+Tab', 'Next tab'],
            ['Ctrl+Alt+C', 'Toggle console'],
          ].map(([k, v]) => (
            <tr key={k}>
              <td>
                <kbd>{k}</kbd>
              </td>
              <td className="muted">{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

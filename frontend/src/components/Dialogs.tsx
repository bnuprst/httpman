import { useEffect, useMemo, useState } from 'react';
import { api, copyText, openExternal } from '../api';
import { useStore } from '../store';
import type { Cookie, Item, Settings } from '../types';
import { clone, findItem, isFolder, uuid } from '../util';
import Code from './Code';
import Modal from './Modal';

export default function Dialogs() {
  const modal = useStore((s) => s.modal);
  const set = useStore((s) => s.set);
  if (!modal) return null;
  const close = () => set({ modal: null });
  switch (modal.kind) {
    case 'import':
      return <ImportDialog onClose={close} />;
    case 'settings':
      return <SettingsDialog onClose={close} />;
    case 'cookies':
      return <CookiesDialog onClose={close} />;
    case 'code':
      return <CodeDialog tabId={modal.tabId} onClose={close} />;
    case 'saveAs':
      return <SaveAsDialog tabId={modal.tabId} onClose={close} />;
    case 'prompt':
      return <PromptDialog {...modal} onClose={close} />;
    case 'confirm':
      return (
        <Modal
          title={modal.title}
          onClose={close}
          footer={
            <>
              <button className="btn" onClick={close}>
                Cancel
              </button>
              {modal.extra && (
                <button
                  className="btn"
                  onClick={() => {
                    close();
                    modal.extra!.onClick();
                  }}
                >
                  {modal.extra.label}
                </button>
              )}
              <button
                className={'btn ' + (modal.danger ? 'danger' : 'primary')}
                autoFocus
                onClick={() => {
                  close();
                  modal.onOk();
                }}
              >
                {modal.okLabel || 'OK'}
              </button>
            </>
          }
        >
          <p>{modal.message}</p>
        </Modal>
      );
    case 'about':
      return <AboutDialog onClose={close} />;
  }
}

function PromptDialog({ title, label, value, onOk, onClose }: { title: string; label: string; value: string; onOk: (v: string) => void; onClose: () => void }) {
  const [v, setV] = useState(value);
  const ok = () => {
    if (!v.trim()) return;
    onClose();
    onOk(v.trim());
  };
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={420}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            Cancel
          </button>
          <button className="btn primary" onClick={ok}>
            OK
          </button>
        </>
      }
    >
      <label className="field-label">{label}</label>
      <input className="full" autoFocus value={v} onFocus={(e) => e.target.select()} onChange={(e) => setV(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && ok()} />
    </Modal>
  );
}

function ImportDialog({ onClose }: { onClose: () => void }) {
  const st = useStore();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const run = async (fn: () => Promise<import('../types').ImportResult>) => {
    setBusy(true);
    try {
      const r = await fn();
      st.applyImport(r);
      if (r.collections.length || r.environments.length || r.globals || r.request) onClose();
    } catch (e) {
      st.toast((e as Error).message, 'error');
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal title="Import" onClose={onClose} width={640}>
      <div className="import-drop">
        <p>
          Import Postman collections (v1, v2.0, v2.1), environments, globals or a Postman data dump.
          <br />
          <span className="muted">You can also drag &amp; drop files onto the window.</span>
        </p>
        <button className="btn primary" disabled={busy} onClick={() => run(api.importFiles)}>
          Choose files…
        </button>
      </div>
      <label className="field-label">Or paste raw text (JSON or a cURL command)</label>
      <textarea className="import-text mono" spellCheck={false} placeholder={"curl https://api.example.com -H 'Accept: application/json'"} value={text} onChange={(e) => setText(e.target.value)} />
      <div className="modal-inline-foot">
        <button className="btn" onClick={onClose}>
          Cancel
        </button>
        <button className="btn primary" disabled={busy || !text.trim()} onClick={() => run(() => api.importText(text))}>
          Import text
        </button>
      </div>
    </Modal>
  );
}

function CodeDialog({ tabId, onClose }: { tabId: string; onClose: () => void }) {
  const st = useStore();
  const tab = st.tabs.find((t) => t.id === tabId);
  const [lang, setLang] = useState(() => localStorage.getItem('codegen-lang') || 'curl');
  const [code, setCode] = useState('');
  const [err, setErr] = useState('');
  useEffect(() => {
    if (!tab || tab.kind !== 'request') return;
    localStorage.setItem('codegen-lang', lang);
    const coll = tab.collectionId ? st.collections[tab.collectionId] : undefined;
    api
      .generateCode({ requestId: uuid(), collection: coll, itemId: tab.itemId, item: tab.draft, environmentId: st.selectedEnvId || undefined }, lang)
      .then((c) => (setCode(c), setErr('')), (e) => setErr((e as Error).message));
  }, [lang]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <Modal title="Code snippet" onClose={onClose} wide>
      <div className="code-dialog">
        <div className="code-langs">
          {st.languages.map((l) => (
            <button key={l.ID} className={'snippet' + (lang === l.ID ? ' active' : '')} onClick={() => setLang(l.ID)}>
              {l.Name}
            </button>
          ))}
        </div>
        <div className="code-out">
          {err ? <div className="fail pad">{err}</div> : <Code value={code} lang={lang === 'fetch' ? 'javascript' : 'text'} readOnly wrap />}
          <div className="modal-inline-foot">
            <span className="muted small">Variables are resolved with the active environment. Scripts are not run.</span>
            <button className="btn primary" onClick={() => copyText(code).then(() => st.toast('Copied to clipboard', 'success'))}>
              Copy
            </button>
          </div>
        </div>
      </div>
    </Modal>
  );
}

function folderOptions(items: Item[], depth = 0): { id: string; label: string }[] {
  const out: { id: string; label: string }[] = [];
  for (const it of items) {
    if (isFolder(it)) {
      out.push({ id: it.id, label: '  '.repeat(depth) + '▰ ' + it.name });
      out.push(...folderOptions(it.item || [], depth + 1));
    }
  }
  return out;
}

function SaveAsDialog({ tabId, onClose }: { tabId: string; onClose: () => void }) {
  const st = useStore();
  const tab = st.tabs.find((t) => t.id === tabId);
  const list = st.collectionList();
  const [name, setName] = useState(tab?.kind === 'request' ? tab.draft.name : '');
  const [collectionId, setCollectionId] = useState(list[0]?.info._postman_id || '__new');
  const [newName, setNewName] = useState('New Collection');
  const [folderId, setFolderId] = useState('');
  const folders = useMemo(() => (st.collections[collectionId] ? folderOptions(st.collections[collectionId].item) : []), [st.collections, collectionId]);
  if (!tab || tab.kind !== 'request') return null;
  const save = async () => {
    let cid = collectionId;
    if (cid === '__new') {
      const c = await st.createCollection(newName.trim() || 'New Collection');
      cid = c.info._postman_id;
    }
    const item = clone(tab.draft);
    item.name = name.trim() || 'Untitled Request';
    // A fresh id avoids clashing when the same draft is saved twice.
    if (st.collections[cid] && findItem(st.collections[cid].item, item.id)) item.id = uuid();
    await st.mutateCollection(cid, (c) => {
      const f = folderId ? findItem(c.item, folderId) : null;
      if (f) (f.item.item = f.item.item || []).push(item);
      else c.item.push(item);
    });
    st.updateTab(tabId, { collectionId: cid, itemId: item.id, draft: item, dirty: false }, false);
    st.set({ expanded: { ...useStore.getState().expanded, [cid]: true, ...(folderId ? { [folderId]: true } : {}) } });
    onClose();
  };
  return (
    <Modal
      title="Save request"
      onClose={onClose}
      width={480}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            Cancel
          </button>
          <button className="btn primary" onClick={save}>
            Save
          </button>
        </>
      }
    >
      <label className="field-label">Request name</label>
      <input className="full" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
      <label className="field-label">Collection</label>
      <select className="full" value={collectionId} onChange={(e) => (setCollectionId(e.target.value), setFolderId(''))}>
        {list.map((c) => (
          <option key={c.info._postman_id} value={c.info._postman_id}>
            {c.info.name}
          </option>
        ))}
        <option value="__new">＋ New collection…</option>
      </select>
      {collectionId === '__new' ? (
        <>
          <label className="field-label">New collection name</label>
          <input className="full" value={newName} onChange={(e) => setNewName(e.target.value)} />
        </>
      ) : (
        folders.length > 0 && (
          <>
            <label className="field-label">Folder</label>
            <select className="full" value={folderId} onChange={(e) => setFolderId(e.target.value)}>
              <option value="">(collection root)</option>
              {folders.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.label}
                </option>
              ))}
            </select>
          </>
        )
      )}
    </Modal>
  );
}

function SettingsDialog({ onClose }: { onClose: () => void }) {
  const st = useStore();
  const [s, setS] = useState<Settings>(st.settings);
  const upd = (p: Partial<Settings>) => setS({ ...s, ...p });
  const save = async () => {
    try {
      await api.saveSettings(s);
      st.set({ settings: s });
      onClose();
    } catch (e) {
      st.toast((e as Error).message, 'error');
    }
  };
  return (
    <Modal
      title="Settings"
      onClose={onClose}
      width={620}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            Cancel
          </button>
          <button className="btn primary" onClick={save}>
            Save
          </button>
        </>
      }
    >
      <div className="settings">
        <h4>General</h4>
        <div className="form-row">
          <label>Theme</label>
          <select value={s.theme} onChange={(e) => upd({ theme: e.target.value as Settings['theme'] })}>
            <option value="system">System</option>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </select>
        </div>
        <div className="form-row">
          <label>Editor font size</label>
          <input type="number" min={9} max={24} value={s.fontSize} onChange={(e) => upd({ fontSize: Number(e.target.value) || 13 })} />
        </div>
        <div className="form-row">
          <label>Indentation (spaces)</label>
          <input type="number" min={1} max={8} value={s.editorTabSize} onChange={(e) => upd({ editorTabSize: Number(e.target.value) || 2 })} />
        </div>
        <div className="form-row">
          <label>Request / response layout</label>
          <select value={st.layout} onChange={(e) => st.set({ layout: e.target.value as 'vertical' | 'horizontal' })}>
            <option value="vertical">Stacked</option>
            <option value="horizontal">Side by side</option>
          </select>
        </div>
        <h4>Requests</h4>
        <div className="form-row">
          <label>Request timeout (ms, 0 = none)</label>
          <input type="number" min={0} value={s.timeoutMs} onChange={(e) => upd({ timeoutMs: Number(e.target.value) || 0 })} />
        </div>
        <div className="form-row">
          <label>SSL certificate verification</label>
          <input type="checkbox" checked={s.sslVerify} onChange={(e) => upd({ sslVerify: e.target.checked })} />
        </div>
        <div className="form-row">
          <label>Automatically follow redirects</label>
          <input type="checkbox" checked={s.followRedirects} onChange={(e) => upd({ followRedirects: e.target.checked })} />
        </div>
        <div className="form-row">
          <label>Maximum redirects</label>
          <input type="number" min={0} value={s.maxRedirects} onChange={(e) => upd({ maxRedirects: Number(e.target.value) || 0 })} />
        </div>
        <div className="form-row">
          <label>Max response size (MB)</label>
          <input type="number" min={1} value={s.maxResponseMB} onChange={(e) => upd({ maxResponseMB: Number(e.target.value) || 100 })} />
        </div>
        <div className="form-row">
          <label>Script timeout (seconds)</label>
          <input type="number" min={1} value={s.scriptTimeoutSec} onChange={(e) => upd({ scriptTimeoutSec: Number(e.target.value) || 60 })} />
        </div>
        <div className="form-row">
          <label>Save variable changes made by scripts</label>
          <input type="checkbox" checked={s.persistVariables} onChange={(e) => upd({ persistVariables: e.target.checked })} />
        </div>
        <div className="form-row">
          <label>Persist cookies between sessions</label>
          <input type="checkbox" checked={s.saveCookies} onChange={(e) => upd({ saveCookies: e.target.checked })} />
        </div>
        <div className="form-row">
          <label>History size</label>
          <input type="number" min={0} value={s.historyLimit} onChange={(e) => upd({ historyLimit: Number(e.target.value) || 0 })} />
        </div>
        <h4>Proxy</h4>
        <div className="form-row">
          <label>Proxy</label>
          <select value={s.proxyMode} onChange={(e) => upd({ proxyMode: e.target.value as Settings['proxyMode'] })}>
            <option value="system">Use system proxy (HTTP_PROXY / HTTPS_PROXY)</option>
            <option value="none">No proxy</option>
            <option value="custom">Custom</option>
          </select>
        </div>
        {s.proxyMode === 'custom' && (
          <div className="form-row">
            <label>Proxy URL</label>
            <input value={s.proxyUrl} placeholder="http://user:pass@proxy:8080" onChange={(e) => upd({ proxyUrl: e.target.value })} />
          </div>
        )}
        <h4>Data</h4>
        <p className="muted small">
          Workspace folder: <span className="mono">{st.workspaceDir}</span>
          <br />
          Collections and environments are stored there as regular Postman JSON files. Set the HTTPMAN_HOME environment variable or start with --workspace to use another folder.
        </p>
        <p className="muted small">
          httpman {st.version} ·{' '}
          <button className="link" onClick={() => openExternal('https://github.com/bnuprst/httpman')}>
            github.com/bnuprst/httpman
          </button>
        </p>
      </div>
    </Modal>
  );
}

function CookiesDialog({ onClose }: { onClose: () => void }) {
  const st = useStore();
  const [cookies, setCookies] = useState<Cookie[]>([]);
  const [domain, setDomain] = useState('');
  const [edit, setEdit] = useState<Cookie | null>(null);
  useEffect(() => {
    api.cookies().then((c) => {
      setCookies(c || []);
      if (c?.length) setDomain(c[0].domain);
    });
  }, []);
  const domains = Array.from(new Set(cookies.map((c) => c.domain)));
  const shown = cookies.filter((c) => c.domain === domain);
  const wrap = (p: Promise<Cookie[]>) => p.then((c) => setCookies(c || []), (e) => st.toast((e as Error).message, 'error'));
  return (
    <Modal title="Manage cookies" onClose={onClose} wide>
      <div className="cookies">
        <div className="cookie-domains">
          {domains.length === 0 && <div className="muted pad">No cookies stored.</div>}
          {domains.map((d) => (
            <div key={d} className={'cookie-domain' + (d === domain ? ' active' : '')} onClick={() => setDomain(d)}>
              <span>{d}</span>
              <span className="muted small">{cookies.filter((c) => c.domain === d).length}</span>
              <button className="icon-btn" title="Delete all for domain" onClick={(e) => (e.stopPropagation(), wrap(api.deleteCookies(d, '', '')))}>
                ×
              </button>
            </div>
          ))}
          <div className="cookie-add">
            <button className="btn small" onClick={() => setEdit({ name: '', value: '', domain: domain || '', path: '/' })}>
              ＋ Add cookie
            </button>
            {cookies.length > 0 && (
              <button className="btn small danger" onClick={() => wrap(api.deleteCookies('', '', ''))}>
                Clear all
              </button>
            )}
          </div>
        </div>
        <div className="cookie-list">
          {edit ? (
            <div className="cookie-edit">
              {(['name', 'value', 'domain', 'path'] as const).map((k) => (
                <div className="form-row" key={k}>
                  <label>{k}</label>
                  <input value={edit[k]} onChange={(e) => setEdit({ ...edit, [k]: e.target.value })} />
                </div>
              ))}
              <div className="form-row">
                <label>Secure / HttpOnly</label>
                <span>
                  <input type="checkbox" checked={!!edit.secure} onChange={(e) => setEdit({ ...edit, secure: e.target.checked })} /> secure{' '}
                  <input type="checkbox" checked={!!edit.httpOnly} onChange={(e) => setEdit({ ...edit, httpOnly: e.target.checked })} /> httpOnly
                </span>
              </div>
              <div className="modal-inline-foot">
                <button className="btn" onClick={() => setEdit(null)}>
                  Cancel
                </button>
                <button
                  className="btn primary"
                  disabled={!edit.name || !edit.domain}
                  onClick={() => {
                    wrap(api.putCookie(edit));
                    setDomain(edit.domain.replace(/^\./, '').toLowerCase());
                    setEdit(null);
                  }}
                >
                  Save
                </button>
              </div>
            </div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Value</th>
                  <th>Path</th>
                  <th>Expires</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {shown.map((c, i) => (
                  <tr key={i} onDoubleClick={() => setEdit(c)}>
                    <td className="mono">{c.name}</td>
                    <td className="mono wrap">{c.value}</td>
                    <td>{c.path}</td>
                    <td>{c.expires ? new Date(c.expires).toLocaleString() : 'Session'}</td>
                    <td>
                      <button className="icon-btn" onClick={() => setEdit(c)} title="Edit">
                        ✎
                      </button>
                      <button className="icon-btn" onClick={() => wrap(api.deleteCookies(c.domain, c.path, c.name))} title="Delete">
                        ×
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </Modal>
  );
}

function AboutDialog({ onClose }: { onClose: () => void }) {
  const v = useStore((s) => s.version);
  return (
    <Modal title="About httpman" onClose={onClose} width={420}>
      <p>httpman {v}</p>
      <p className="muted">A local HTTP client compatible with Postman collections. No accounts, no sync, no cloud.</p>
    </Modal>
  );
}

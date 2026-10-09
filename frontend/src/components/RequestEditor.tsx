import { useCallback, useMemo, useRef } from 'react';
import { RequestTab, useStore } from '../store';
import type { Auth, Item, KV, QueryParam, Request } from '../types';
import { findItem, getScript, METHODS, methodClass, parseUrl, setScript, withQuery } from '../util';
import AuthEditor from './AuthEditor';
import BodyEditor from './BodyEditor';
import KeyValueTable from './KeyValueTable';
import ResponseViewer from './ResponseViewer';
import ScriptEditor from './ScriptEditor';
import VarInput from './VarInput';

const PANES = ['params', 'auth', 'headers', 'body', 'prerequest', 'tests', 'settings'] as const;
const PANE_LABEL: Record<string, string> = {
  params: 'Params',
  auth: 'Authorization',
  headers: 'Headers',
  body: 'Body',
  prerequest: 'Pre-request Script',
  tests: 'Tests',
  settings: 'Settings',
};

export default function RequestEditor({ tab }: { tab: RequestTab }) {
  const st = useStore();
  const coll = tab.collectionId ? st.collections[tab.collectionId] : undefined;
  const item = tab.draft;
  const req = item.request as Request;
  const pane = tab.reqPane || 'params';
  const layout = st.layout;
  const containerRef = useRef<HTMLDivElement>(null);

  const setItem = useCallback((patch: Partial<Item>) => st.updateTab(tab.id, { draft: { ...tab.draft, ...patch } }), [st, tab]);
  const setReq = useCallback((patch: Partial<Request>) => setItem({ request: { ...req, ...patch } }), [setItem, req]);
  const save = useCallback(() => void st.saveTab(tab.id), [st, tab.id]);
  const send = useCallback(() => void st.send(tab.id), [st, tab.id]);

  const parents = useMemo(() => (coll ? findItem(coll.item, item.id)?.parents || [] : []), [coll, item.id]);
  const inherited = useMemo(() => {
    for (let i = parents.length - 1; i >= 0; i--) {
      const a = parents[i].auth;
      if (a && a.type && a.type !== 'inherit') return { name: parents[i].name, type: a.type };
    }
    if (coll?.auth && coll.auth.type && coll.auth.type !== 'inherit') return { name: coll.info.name, type: coll.auth.type };
    return null;
  }, [parents, coll]);

  const query: KV[] = (req.url.query || []).map((q: QueryParam) => ({ key: q.key ?? '', value: q.value ?? '', disabled: q.disabled, description: q.description }));
  const headerCount = (req.header || []).filter((h) => !h.disabled && h.key).length;
  const paramCount = (req.url.query || []).filter((q) => !q.disabled && q.key).length;
  const hasPre = !!getScript(item.event, 'prerequest').trim();
  const hasTests = !!getScript(item.event, 'test').trim();
  const bodyOn = req.body && req.body.mode && req.body.mode !== 'none';
  const ppb = (item.protocolProfileBehavior || {}) as Record<string, unknown>;

  const startDrag = (e: React.MouseEvent) => {
    e.preventDefault();
    const el = containerRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const move = (ev: MouseEvent) => {
      const ratio = layout === 'vertical' ? (ev.clientY - rect.top) / rect.height : (ev.clientX - rect.left) / rect.width;
      st.set({ splitRatio: Math.min(0.85, Math.max(0.15, ratio)) });
    };
    const up = () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  };

  return (
    <div className="request-editor">
      <div className="req-title">
        <div className="breadcrumb">
          {coll ? (
            <>
              <span className="crumb">{coll.info.name}</span>
              {parents.map((p) => (
                <span key={p.id} className="crumb">
                  {p.name}
                </span>
              ))}
            </>
          ) : (
            <span className="crumb muted">Unsaved request</span>
          )}
          <input className="name-input" value={item.name} onChange={(e) => setItem({ name: e.target.value })} />
        </div>
        <div className="req-actions">
          <button className="btn" onClick={save} title="Save (Ctrl+S)">
            Save{tab.dirty ? ' •' : ''}
          </button>
          <button className="btn" onClick={() => st.set({ modal: { kind: 'code', tabId: tab.id } })} title="Generate code">
            {'</>'} Code
          </button>
        </div>
      </div>
      <div className="url-bar">
        <select className={'method-select ' + methodClass(req.method)} value={METHODS.includes(req.method) ? req.method : '__custom'} onChange={(e) => setReq({ method: e.target.value })}>
          {METHODS.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
          {!METHODS.includes(req.method) && <option value="__custom">{req.method}</option>}
        </select>
        <VarInput
          className="url-input"
          value={req.url.raw}
          placeholder="Enter request URL"
          collectionId={tab.collectionId}
          onChange={(v) => setReq({ url: parseUrl(v, req.url) })}
          onEnter={send}
        />
        {tab.sending ? (
          <button className="btn danger send" onClick={() => st.cancel(tab.id)}>
            Cancel
          </button>
        ) : (
          <button className="btn primary send" onClick={send} title="Send (Ctrl+Enter)">
            Send
          </button>
        )}
      </div>
      <div ref={containerRef} className={'req-split ' + layout}>
        <div className="req-pane" style={{ flexBasis: `${st.splitRatio * 100}%` }}>
          <div className="subtabs">
            {PANES.map((p) => (
              <button key={p} className={'subtab' + (pane === p ? ' active' : '')} onClick={() => st.updateTab(tab.id, { reqPane: p }, false)}>
                {PANE_LABEL[p]}
                {p === 'params' && paramCount > 0 && <span className="count">{paramCount}</span>}
                {p === 'headers' && headerCount > 0 && <span className="count">{headerCount}</span>}
                {p === 'body' && bodyOn && <span className="dot" />}
                {p === 'prerequest' && hasPre && <span className="dot" />}
                {p === 'tests' && hasTests && <span className="dot" />}
                {p === 'auth' && req.auth && req.auth.type !== 'noauth' && <span className="dot" />}
              </button>
            ))}
          </div>
          <div className="pane-body">
            {pane === 'params' && (
              <div className="scroll">
                <div className="section-title">Query Params</div>
                <KeyValueTable
                  rows={query}
                  collectionId={tab.collectionId}
                  description
                  onChange={(rows) =>
                    setReq({
                      url: withQuery(
                        req.url,
                        rows.map((r) => ({ key: r.key, value: r.value ?? '', ...(r.disabled ? { disabled: true } : {}), ...(r.description ? { description: r.description } : {}) })),
                      ),
                    })
                  }
                />
                {(req.url.variable || []).length > 0 && (
                  <>
                    <div className="section-title">Path Variables</div>
                    <KeyValueTable
                      fixedKeys
                      rows={(req.url.variable || []).map((v) => ({ key: v.key, value: String(v.value ?? ''), description: typeof v.description === 'string' ? v.description : '' }))}
                      collectionId={tab.collectionId}
                      description
                      onChange={(rows) => setReq({ url: { ...req.url, variable: (req.url.variable || []).map((v, i) => ({ ...v, value: rows[i]?.value ?? '', description: rows[i]?.description })) } })}
                    />
                  </>
                )}
              </div>
            )}
            {pane === 'auth' && (
              <div className="scroll">
                <AuthEditor auth={req.auth} allowInherit inheritedFrom={inherited} collectionId={tab.collectionId} onChange={(a: Auth | null) => setReq({ auth: a ?? undefined })} />
              </div>
            )}
            {pane === 'headers' && (
              <div className="scroll">
                <KeyValueTable rows={req.header || []} onChange={(rows) => setReq({ header: rows })} collectionId={tab.collectionId} description />
                <p className="muted small pad">httpman adds User-Agent, Accept, Accept-Encoding, Connection and Content-Type (from the body) when they are not set.</p>
              </div>
            )}
            {pane === 'body' && <BodyEditor body={req.body} onChange={(b) => setReq({ body: b })} collectionId={tab.collectionId} onSave={save} onSend={send} />}
            {pane === 'prerequest' && <ScriptEditor kind="prerequest" value={getScript(item.event, 'prerequest')} onChange={(v) => setItem({ event: setScript(item.event, 'prerequest', v) })} onSave={save} onSend={send} />}
            {pane === 'tests' && <ScriptEditor kind="test" value={getScript(item.event, 'test')} onChange={(v) => setItem({ event: setScript(item.event, 'test', v) })} onSave={save} onSend={send} />}
            {pane === 'settings' && (
              <div className="scroll settings-pane">
                <Toggle
                  label="Follow redirects"
                  hint="Follow HTTP 3xx responses as redirects. Unset uses the app setting."
                  value={ppb.followRedirects as boolean | undefined}
                  onChange={(v) => setItem({ protocolProfileBehavior: { ...ppb, followRedirects: v } })}
                />
                <Toggle
                  label="Enable SSL certificate verification"
                  hint="Verify SSL certificates when sending this request. Unset uses the app setting."
                  value={ppb.strictSSL as boolean | undefined}
                  onChange={(v) => setItem({ protocolProfileBehavior: { ...ppb, strictSSL: v } })}
                />
                <div className="form-row">
                  <label>Maximum number of redirects</label>
                  <input
                    type="number"
                    min={0}
                    value={(ppb.maxRedirects as number | undefined) ?? ''}
                    placeholder="default"
                    onChange={(e) => setItem({ protocolProfileBehavior: { ...ppb, maxRedirects: e.target.value === '' ? undefined : Number(e.target.value) } })}
                  />
                </div>
              </div>
            )}
          </div>
        </div>
        <div className="splitter" onMouseDown={startDrag} />
        <div className="res-pane">
          <ResponseViewer tab={tab} />
        </div>
      </div>
    </div>
  );
}

function Toggle({ label, hint, value, onChange }: { label: string; hint: string; value: boolean | undefined; onChange: (v: boolean | undefined) => void }) {
  return (
    <div className="toggle-row">
      <div>
        <div>{label}</div>
        <div className="muted small">{hint}</div>
      </div>
      <select value={value === undefined ? '' : value ? 'on' : 'off'} onChange={(e) => onChange(e.target.value === '' ? undefined : e.target.value === 'on')}>
        <option value="">Default</option>
        <option value="on">On</option>
        <option value="off">Off</option>
      </select>
    </div>
  );
}

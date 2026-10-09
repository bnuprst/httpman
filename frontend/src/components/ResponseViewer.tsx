import Handlebars from 'handlebars';
import { useMemo, useState } from 'react';
import { api, copyText } from '../api';
import { RequestTab, useStore } from '../store';
import type { ResponseView, TestResult } from '../types';
import { b64ToBytes, formatBytes, formatMs, headerValue, prettyJSON, prettyXML } from '../util';
import Code, { Lang } from './Code';

function detectLang(ct: string, text: string): Lang {
  const c = ct.toLowerCase();
  if (c.includes('json')) return 'json';
  if (c.includes('html')) return 'html';
  if (c.includes('xml')) return 'xml';
  if (c.includes('javascript')) return 'javascript';
  const t = text.trimStart();
  if (t.startsWith('{') || t.startsWith('[')) return 'json';
  if (t.startsWith('<')) return t.toLowerCase().startsWith('<!doctype html') || t.toLowerCase().startsWith('<html') ? 'html' : 'xml';
  return 'text';
}

function statusClass(code: number) {
  if (code >= 500) return 'st-5';
  if (code >= 400) return 'st-4';
  if (code >= 300) return 'st-3';
  return 'st-2';
}

export default function ResponseViewer({ tab }: { tab: RequestTab }) {
  const st = useStore();
  const pane = tab.resPane || 'body';
  const setPane = (p: string) => st.updateTab(tab.id, { resPane: p }, false);
  const ex = tab.result?.execution;
  const res = ex?.response;

  if (tab.sending) {
    return (
      <div className="res-empty">
        <div className="spinner" />
        <div>Sending request…</div>
        <button className="btn" onClick={() => st.cancel(tab.id)}>
          Cancel
        </button>
      </div>
    );
  }
  if (tab.error || (ex && ex.error && !res)) {
    return (
      <div className="res-empty error">
        <div className="err-title">Could not get response</div>
        <div className="err-msg">{tab.error || ex?.error}</div>
        {ex?.scriptErrors?.map((e, i) => (
          <div className="err-msg" key={i}>
            {e.name}: {e.message} {e.source && <span className="muted">({e.source})</span>}
          </div>
        ))}
        <TestList tests={ex?.tests || []} />
      </div>
    );
  }
  if (ex?.skipped) {
    return <div className="res-empty">The request was skipped by pm.execution.skipRequest().</div>;
  }
  if (!res || !ex) {
    return (
      <div className="res-empty">
        <div className="muted">Enter the URL and click Send to get a response</div>
        <div className="muted small">Ctrl+Enter to send · Ctrl+S to save</div>
      </div>
    );
  }
  const tests = ex.tests || [];
  const passed = tests.filter((t) => t.passed && !t.skipped).length;
  const failed = tests.filter((t) => !t.passed && !t.skipped).length;
  return (
    <div className="response">
      <div className="res-head">
        <div className="subtabs">
          {['body', 'cookies', 'headers', 'tests', ...(ex.visualizer ? ['visualize'] : []), 'timeline'].map((p) => (
            <button key={p} className={'subtab' + (pane === p ? ' active' : '')} onClick={() => setPane(p)}>
              {p === 'body' && 'Body'}
              {p === 'cookies' && (
                <>
                  Cookies <span className="count">{res.cookies.length}</span>
                </>
              )}
              {p === 'headers' && (
                <>
                  Headers <span className="count">{res.header.length}</span>
                </>
              )}
              {p === 'tests' && (
                <>
                  Test Results{' '}
                  {tests.length > 0 && (
                    <span className={'count ' + (failed ? 'fail' : 'pass')}>
                      {passed}/{passed + failed}
                    </span>
                  )}
                </>
              )}
              {p === 'visualize' && 'Visualize'}
              {p === 'timeline' && 'Timeline'}
            </button>
          ))}
        </div>
        <div className="res-meta">
          <span>
            Status: <b className={statusClass(res.code)}>{res.code} {res.status}</b>
          </span>
          <span title={timingTitle(res)}>
            Time: <b className="ok">{formatMs(res.responseTime)}</b>
          </span>
          <span title={`Headers ${formatBytes(res.headerSize)}, body ${formatBytes(res.bodySize)}`}>
            Size: <b className="ok">{formatBytes(res.bodySize + res.headerSize)}</b>
          </span>
        </div>
      </div>
      {(ex.scriptErrors?.length || 0) > 0 && (
        <div className="script-errors">
          {ex.scriptErrors!.map((e, i) => (
            <div key={i}>
              ⚠ {e.name}: {e.message} {e.source && <span className="muted">({e.source})</span>}
            </div>
          ))}
        </div>
      )}
      {res.warnings?.map((w, i) => (
        <div key={i} className="script-errors warn">
          ⚠ {w}
        </div>
      ))}
      <div className="res-body">
        {pane === 'body' && <BodyView res={res} name={tab.draft.name} />}
        {pane === 'headers' && <HeaderTable headers={res.header} />}
        {pane === 'cookies' && <CookieTable res={res} />}
        {pane === 'tests' && <TestList tests={tests} filterable />}
        {pane === 'visualize' && ex.visualizer && <Visualizer v={ex.visualizer} />}
        {pane === 'timeline' && <Timeline res={res} />}
      </div>
    </div>
  );
}

function timingTitle(r: ResponseView) {
  const t = r.timings;
  return [
    `DNS lookup: ${formatMs(t.dns)}`,
    `TCP handshake: ${formatMs(t.connect)}`,
    `SSL handshake: ${formatMs(t.tls)}`,
    `Waiting (TTFB): ${formatMs(t.firstByte)}`,
    `Download: ${formatMs(t.download)}`,
    `Total: ${formatMs(t.total)}`,
  ].join('\n');
}

function BodyView({ res, name }: { res: ResponseView; name: string }) {
  const [mode, setMode] = useState<'pretty' | 'raw' | 'preview'>('pretty');
  const [wrap, setWrap] = useState(true);
  const ct = headerValue(res.header, 'content-type');
  const isImage = ct.startsWith('image/');
  const isPdf = ct.includes('pdf');
  const text = res.bodyText;
  const lang = detectLang(ct, text);
  const pretty = useMemo(() => {
    if (!res.isText || text.length > 8 << 20) return text;
    if (lang === 'json') return prettyJSON(text) ?? text;
    if (lang === 'xml') return prettyXML(text);
    return text;
  }, [text, lang, res.isText]);
  const effective = !res.isText && mode !== 'preview' ? 'binary' : mode;
  const ext = lang === 'json' ? 'json' : lang === 'xml' ? 'xml' : lang === 'html' ? 'html' : isImage ? ct.split('/')[1]?.split(';')[0] || 'bin' : isPdf ? 'pdf' : res.isText ? 'txt' : 'bin';

  return (
    <div className="body-view">
      <div className="body-toolbar">
        <div className="seg">
          {(['pretty', 'raw', 'preview'] as const).map((m) => (
            <button key={m} className={mode === m ? 'active' : ''} onClick={() => setMode(m)}>
              {m[0].toUpperCase() + m.slice(1)}
            </button>
          ))}
        </div>
        {mode === 'pretty' && <span className="lang-tag">{lang.toUpperCase()}</span>}
        <label className="check small">
          <input type="checkbox" checked={wrap} onChange={(e) => setWrap(e.target.checked)} /> Wrap
        </label>
        <span className="spacer" />
        {res.truncated && <span className="warn small">Response truncated (max size reached)</span>}
        <button className="btn small" onClick={() => copyText(text || '')} disabled={!res.isText}>
          Copy
        </button>
        <button
          className="btn small"
          onClick={async () => {
            try {
              const p = await api.saveResponse(res.bodyBase64, `${name.replace(/[^\w.-]+/g, '_') || 'response'}.${ext}`);
              if (p) useStore.getState().toast('Saved to ' + p, 'success');
            } catch (e) {
              useStore.getState().toast((e as Error).message, 'error');
            }
          }}
        >
          Save to file
        </button>
      </div>
      <div className="body-content">
        {effective === 'pretty' && <Code value={pretty} lang={lang} readOnly wrap={wrap} codecs />}
        {effective === 'raw' && <Code value={text} lang="text" readOnly wrap={wrap} codecs />}
        {effective === 'binary' && <div className="empty-note">Binary response ({formatBytes(res.bodySize)}). Use Preview or save it to a file.</div>}
        {effective === 'preview' &&
          (isImage ? (
            <div className="preview-img">
              <img src={`data:${ct};base64,${res.bodyBase64}`} alt="response" />
            </div>
          ) : isPdf ? (
            <iframe className="preview-frame" title="preview" src={URL.createObjectURL(new Blob([b64ToBytes(res.bodyBase64)], { type: 'application/pdf' }))} />
          ) : (
            <iframe className="preview-frame" title="preview" sandbox="" srcDoc={lang === 'html' ? text : `<pre style="white-space:pre-wrap;font:12px monospace">${escapeHtml(pretty)}</pre>`} />
          ))}
      </div>
    </div>
  );
}

function escapeHtml(s: string) {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
}

function HeaderTable({ headers }: { headers: { key: string; value: string }[] }) {
  return (
    <div className="scroll">
      <table className="data-table">
        <thead>
          <tr>
            <th>Key</th>
            <th>Value</th>
          </tr>
        </thead>
        <tbody>
          {headers.map((h, i) => (
            <tr key={i}>
              <td className="mono">{h.key}</td>
              <td className="mono wrap">{h.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CookieTable({ res }: { res: ResponseView }) {
  if (!res.cookies.length) return <div className="empty-note">No cookies for this domain</div>;
  return (
    <div className="scroll">
      <table className="data-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Value</th>
            <th>Domain</th>
            <th>Path</th>
            <th>Expires</th>
            <th>HttpOnly</th>
            <th>Secure</th>
          </tr>
        </thead>
        <tbody>
          {res.cookies.map((c, i) => (
            <tr key={i}>
              <td className="mono">{c.name}</td>
              <td className="mono wrap">{c.value}</td>
              <td>{c.domain}</td>
              <td>{c.path}</td>
              <td>{c.expires ? new Date(c.expires).toLocaleString() : 'Session'}</td>
              <td>{c.httpOnly ? 'true' : 'false'}</td>
              <td>{c.secure ? 'true' : 'false'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function TestList({ tests, filterable }: { tests: TestResult[]; filterable?: boolean }) {
  const [filter, setFilter] = useState<'all' | 'passed' | 'skipped' | 'failed'>('all');
  if (!tests.length) return filterable ? <div className="empty-note">There are no tests for this request</div> : null;
  const shown = tests.filter((t) => filter === 'all' || (filter === 'passed' && t.passed && !t.skipped) || (filter === 'failed' && !t.passed) || (filter === 'skipped' && t.skipped));
  return (
    <div className="scroll tests">
      {filterable && (
        <div className="seg tests-filter">
          {(['all', 'passed', 'skipped', 'failed'] as const).map((f) => (
            <button key={f} className={filter === f ? 'active' : ''} onClick={() => setFilter(f)}>
              {f[0].toUpperCase() + f.slice(1)}
            </button>
          ))}
        </div>
      )}
      {shown.map((t, i) => (
        <div key={i} className="test-row">
          <span className={'badge ' + (t.skipped ? 'skip' : t.passed ? 'pass' : 'fail')}>{t.skipped ? 'SKIPPED' : t.passed ? 'PASS' : 'FAIL'}</span>
          <span className="test-name">{t.name}</span>
          {t.error && (
            <span className="test-err">
              | {t.error.name}: {t.error.message}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

function Visualizer({ v }: { v: { template: string; data: unknown } }) {
  const doc = useMemo(() => {
    let body: string;
    try {
      body = Handlebars.compile(v.template)(v.data ?? {});
    } catch (e) {
      body = `<pre style="color:#c00">Template error: ${escapeHtml(String((e as Error).message))}</pre>`;
    }
    const data = JSON.stringify(v.data ?? null).replace(/</g, '\\u003c');
    return `<!doctype html><html><head><meta charset="utf-8"><script>var __d=${data};window.pm={getData:function(cb){cb(null,__d)}};</script></head><body>${body}</body></html>`;
  }, [v]);
  return <iframe className="preview-frame" title="visualizer" sandbox="allow-scripts" srcDoc={doc} />;
}

function Timeline({ res }: { res: ResponseView }) {
  const t = res.timings;
  const total = Math.max(t.total, 1);
  const bars: [string, number][] = [
    ['DNS lookup', t.dns],
    ['TCP handshake', t.connect],
    ['SSL handshake', t.tls],
    ['Waiting (TTFB)', Math.max(0, t.firstByte - t.dns - t.connect - t.tls)],
    ['Download', t.download],
  ];
  let offset = 0;
  return (
    <div className="scroll timeline">
      <div className="section-title">Timings</div>
      {bars.map(([label, ms]) => {
        const left = (offset / total) * 100;
        offset += ms;
        return (
          <div className="tl-row" key={label}>
            <span className="tl-label">{label}</span>
            <span className="tl-track">
              <span className="tl-bar" style={{ left: left + '%', width: Math.max((ms / total) * 100, 0.5) + '%' }} />
            </span>
            <span className="tl-val">{formatMs(ms)}</span>
          </div>
        );
      })}
      <div className="section-title">Request</div>
      <div className="mono pad">
        <b>
          {res.request.method} {res.request.url}
        </b>{' '}
        <span className="muted">{res.proto}</span>
      </div>
      {res.redirects && res.redirects.length > 0 && <div className="pad muted">Redirected via: {res.redirects.join(' → ')}</div>}
      <HeaderTable headers={res.request.header} />
      {res.request.body && (
        <>
          <div className="section-title">Request body</div>
          <pre className="mono pad wrap">{res.request.body}</pre>
        </>
      )}
    </div>
  );
}

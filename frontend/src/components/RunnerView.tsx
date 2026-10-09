import { useEffect, useMemo, useState } from 'react';
import { api, onEvent } from '../api';
import { RunnerTab, syncCollectionVariables, useStore } from '../store';
import type { Environment, Execution, Summary } from '../types';
import { allRequests, clone, findItem, formatBytes, formatMs, methodClass, textToB64, uuid } from '../util';
import { TestList } from './ResponseViewer';

export default function RunnerView({ tab }: { tab: RunnerTab }) {
  const st = useStore();
  const cfg = tab.config;
  const coll = st.collections[cfg.collectionId];
  const [data, setData] = useState<{ columns: string[]; rows: Record<string, unknown>[] } | null>(null);
  const [expanded, setExpanded] = useState<Record<number, boolean>>({});
  const [filter, setFilter] = useState<'all' | 'passed' | 'failed'>('all');

  const scope = useMemo(() => {
    if (!coll) return [];
    if (cfg.folderId) {
      const f = findItem(coll.item, cfg.folderId);
      if (!f) return [];
      return f.item.request ? [{ item: f.item, parents: f.parents }] : allRequests(f.item.item || [], [...f.parents, f.item]);
    }
    return allRequests(coll.item);
  }, [coll, cfg.folderId]);

  const selected = cfg.selected ?? scope.map((s) => s.item.id);
  const setCfg = (patch: Partial<typeof cfg>) => st.updateTab(tab.id, { config: { ...cfg, ...patch } }, false);

  useEffect(() => {
    if (!cfg.dataFile) return setData(null);
    api.loadDataFile(cfg.dataFile).then(setData, (e) => st.toast((e as Error).message, 'error'));
  }, [cfg.dataFile]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!coll) return <div className="res-empty">Collection not found</div>;

  const start = async () => {
    const runId = uuid();
    st.updateTab(tab.id, { runId, running: true, executions: [], summary: undefined, error: undefined, total: undefined }, false);
    setExpanded({});
    const tabId = tab.id;
    const persist = cfg.persistVariables;
    const collectionId = cfg.collectionId;
    const off = onEvent('run:' + runId, (ev: any) => {
      const cur = useStore.getState().tabs.find((t) => t.id === tabId) as RunnerTab | undefined;
      if (!cur || cur.runId !== runId) return off();
      if (ev.type === 'start') st.updateTab(tabId, { total: ev.total }, false);
      else if (ev.type === 'execution') st.updateTab(tabId, { executions: [...cur.executions, ev.execution as Execution] }, false);
      else if (ev.type === 'error') {
        st.updateTab(tabId, { running: false, error: ev.error }, false);
        off();
      } else if (ev.type === 'done') {
        off();
        st.updateTab(tabId, { running: false, summary: ev.summary as Summary }, false);
        const patch: Record<string, unknown> = {};
        if (ev.globals) patch.globals = ev.globals;
        if (ev.environment) patch.environments = useStore.getState().environments.map((e) => (e.id === (ev.environment as Environment).id ? ev.environment : e));
        st.set(patch);
        if (persist && ev.collectionVariables) syncCollectionVariables(collectionId, ev.collectionVariables);
      }
    });
    try {
      await api.startRun({
        runId,
        collection: clone(coll),
        folderId: cfg.folderId,
        environmentId: cfg.environmentId || undefined,
        iterations: cfg.iterations,
        delayMs: cfg.delayMs,
        dataFile: cfg.dataFile,
        persistVariables: cfg.persistVariables,
        bail: cfg.bail,
        only: selected.length === scope.length ? undefined : scope.filter((s) => selected.includes(s.item.id)).map((s) => s.item.id),
      });
    } catch (e) {
      off();
      st.updateTab(tab.id, { running: false, error: (e as Error).message }, false);
    }
  };

  const ex = tab.executions;
  const failedEx = (e: Execution) => !!e.error || (e.scriptErrors?.length || 0) > 0 || e.tests.some((t) => !t.passed && !t.skipped);
  const failedCount = ex.filter(failedEx).length;
  const passedCount = ex.length - failedCount;
  const shown = ex.map((e, i) => [e, i] as const).filter(([e]) => filter === 'all' || (filter === 'failed' ? failedEx(e) : !failedEx(e)));
  const folderName = cfg.folderId ? findItem(coll.item, cfg.folderId)?.item.name : undefined;

  return (
    <div className="runner">
      <div className="runner-config">
        <h3>
          Runner · {coll.info.name}
          {folderName ? ' / ' + folderName : ''}
        </h3>
        <div className="form-row">
          <label>Environment</label>
          <select value={cfg.environmentId || ''} onChange={(e) => setCfg({ environmentId: e.target.value })}>
            <option value="">No environment</option>
            {st.environments.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name}
              </option>
            ))}
          </select>
        </div>
        <div className="form-row">
          <label>Iterations</label>
          <input type="number" min={1} value={cfg.iterations} onChange={(e) => setCfg({ iterations: Math.max(1, Number(e.target.value) || 1) })} />
        </div>
        <div className="form-row">
          <label>Delay (ms)</label>
          <input type="number" min={0} value={cfg.delayMs} onChange={(e) => setCfg({ delayMs: Math.max(0, Number(e.target.value) || 0) })} />
        </div>
        <div className="form-row">
          <label>Data file</label>
          <div className="file-cell">
            <button
              className="btn small"
              onClick={async () => {
                const p = await api.pickFiles('Select CSV or JSON data file');
                if (p && p[0]) setCfg({ dataFile: p[0], iterations: Math.max(cfg.iterations, 1) });
              }}
            >
              Select file
            </button>
            {cfg.dataFile ? (
              <>
                <span className="file-names" title={cfg.dataFile}>
                  {cfg.dataFile.split(/[\\/]/).pop()}
                </span>
                <button className="icon-btn" onClick={() => setCfg({ dataFile: undefined })}>
                  ×
                </button>
              </>
            ) : (
              <span className="muted small">CSV with header row or JSON array</span>
            )}
          </div>
        </div>
        {data && (
          <div className="muted small pad">
            {data.rows.length} rows · columns: {data.columns.join(', ')}. Iterations default to the number of rows.
          </div>
        )}
        <label className="check">
          <input type="checkbox" checked={cfg.persistVariables} onChange={(e) => setCfg({ persistVariables: e.target.checked })} /> Keep variable values
        </label>
        <label className="check">
          <input type="checkbox" checked={cfg.bail} onChange={(e) => setCfg({ bail: e.target.checked })} /> Stop run on first failure
        </label>
        <div className="runner-order">
          <div className="section-title">
            Run order
            <span className="spacer" />
            <button className="link" onClick={() => setCfg({ selected: scope.map((s) => s.item.id) })}>
              Select all
            </button>
            <button className="link" onClick={() => setCfg({ selected: [] })}>
              Deselect all
            </button>
          </div>
          {scope.map(({ item, parents }) => (
            <label key={item.id} className="check order-row">
              <input
                type="checkbox"
                checked={selected.includes(item.id)}
                onChange={(e) => setCfg({ selected: e.target.checked ? [...selected, item.id] : selected.filter((x) => x !== item.id) })}
              />
              <span className={'method-badge ' + methodClass(item.request!.method)}>{item.request!.method}</span>
              <span>
                {parents.length > 0 && <span className="muted">{parents.map((p) => p.name).join(' / ')} / </span>}
                {item.name}
              </span>
            </label>
          ))}
        </div>
        <div className="runner-actions">
          {tab.running ? (
            <button className="btn danger" onClick={() => tab.runId && api.cancel(tab.runId)}>
              Stop
            </button>
          ) : (
            <button className="btn primary" disabled={!selected.length} onClick={start}>
              Run {coll.info.name}
            </button>
          )}
        </div>
      </div>
      <div className="runner-results">
        <div className="runner-summary">
          <div className="seg">
            {(['all', 'passed', 'failed'] as const).map((f) => (
              <button key={f} className={filter === f ? 'active' : ''} onClick={() => setFilter(f)}>
                {f === 'all' ? `All (${ex.length})` : f === 'passed' ? `Passed (${passedCount})` : `Failed (${failedCount})`}
              </button>
            ))}
          </div>
          <span className="spacer" />
          {tab.running && (
            <span className="muted">
              Running… {ex.length}
              {tab.total ? ` / ${tab.total}` : ''}
            </span>
          )}
          {tab.summary && (
            <span className="muted">
              {tab.summary.requests} requests · {formatMs(tab.summary.duration / 1e6)} · avg {formatMs(tab.summary.avgResponseTime)}
              {tab.summary.stopped ? ' · stopped' : ''}
            </span>
          )}
          {tab.summary && (
            <button
              className="btn small"
              onClick={async () => {
                const p = await api.saveResponse(textToB64(JSON.stringify(tab.summary, null, 2)), `${coll.info.name}.run.json`);
                if (p) st.toast('Saved to ' + p, 'success');
              }}
            >
              Export results
            </button>
          )}
        </div>
        {tab.error && <div className="script-errors">{tab.error}</div>}
        {tab.running && tab.total ? (
          <div className="progress">
            <div style={{ width: `${(ex.length / tab.total) * 100}%` }} />
          </div>
        ) : null}
        <div className="scroll">
          {!ex.length && !tab.running && <div className="empty-note">Configure the run on the left and press Run.</div>}
          {shown.map(([e, i]) => {
            const prevIter = i > 0 ? ex[i - 1].iteration : -1;
            const res = e.response;
            return (
              <div key={i}>
                {e.iteration !== prevIter && (tab.summary?.iterations || cfg.iterations) > 1 && <div className="iter-head">Iteration {e.iteration + 1}</div>}
                <div className={'run-row' + (failedEx(e) ? ' failed' : '')} onClick={() => setExpanded({ ...expanded, [i]: !expanded[i] })}>
                  <span className={'method-badge ' + methodClass(e.request?.method || 'GET')}>{e.request?.method || ''}</span>
                  <span className="run-name">
                    {e.name}
                    <span className="muted small"> {e.request?.url}</span>
                  </span>
                  {e.skipped && <span className="muted small">skipped</span>}
                  {res && (
                    <span className="muted small">
                      <span className={'code-' + Math.floor(res.code / 100)}>{res.code}</span> · {formatMs(res.responseTime)} · {formatBytes(res.bodySize)}
                    </span>
                  )}
                  {e.error && <span className="fail small">{e.error}</span>}
                </div>
                {(expanded[i] || e.tests.some((t) => !t.passed && !t.skipped)) && (
                  <div className="run-detail">
                    {e.scriptErrors?.map((se, j) => (
                      <div key={j} className="fail small">
                        {se.name}: {se.message} {se.source && <span className="muted">({se.source})</span>}
                      </div>
                    ))}
                    <TestList tests={e.tests} />
                    {expanded[i] && res && (
                      <pre className="mono small run-body">{res.bodyText ? res.bodyText.slice(0, 4000) : '(binary body)'}</pre>
                    )}
                  </div>
                )}
                {!expanded[i] && !e.tests.some((t) => !t.passed && !t.skipped) && e.tests.length > 0 && (
                  <div className="run-detail compact">
                    <span className="pass small">✓ {e.tests.filter((t) => t.passed && !t.skipped).length} passed</span>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

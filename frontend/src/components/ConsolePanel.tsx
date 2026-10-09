import { useEffect, useRef, useState } from 'react';
import { useStore } from '../store';
import type { ConsoleEntry } from '../types';

export default function ConsolePanel() {
  const entries = useStore((s) => s.console);
  const set = useStore((s) => s.set);
  const [filter, setFilter] = useState('');
  const [level, setLevel] = useState('all');
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => end.current?.scrollIntoView({ block: 'end' }), [entries.length]);
  const shown = entries.filter((e) => (level === 'all' || e.level === level || (level === 'log' && ['log', 'info', 'debug'].includes(e.level))) && (!filter || e.message.toLowerCase().includes(filter.toLowerCase())));
  return (
    <div className="console">
      <div className="console-bar">
        <b>Console</b>
        <input className="search" placeholder="Search messages" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <select value={level} onChange={(e) => setLevel(e.target.value)}>
          <option value="all">All logs</option>
          <option value="request">Requests</option>
          <option value="log">Logs</option>
          <option value="warn">Warnings</option>
          <option value="error">Errors</option>
        </select>
        <span className="spacer" />
        <button className="btn small" onClick={() => set({ console: [] })}>
          Clear
        </button>
        <button className="icon-btn" onClick={() => set({ consoleOpen: false })} title="Close console">
          ×
        </button>
      </div>
      <div className="console-body">
        {shown.length === 0 && <div className="muted pad">Requests, console.log() output and script errors appear here.</div>}
        {shown.map((e, i) => (
          <Entry key={i} e={e} />
        ))}
        <div ref={end} />
      </div>
    </div>
  );
}

function Entry({ e }: { e: ConsoleEntry }) {
  const [open, setOpen] = useState(false);
  const t = new Date(e.time);
  const time = t.toLocaleTimeString(undefined, { hour12: false }) + '.' + String(t.getMilliseconds()).padStart(3, '0');
  const req = e.level === 'request' && e.data;
  return (
    <div className={'console-entry lvl-' + e.level}>
      <div className="console-line" onClick={() => req && setOpen(!open)}>
        <span className="muted mono">{time}</span>
        {req && <span className={'caret' + (open ? ' open' : '')}>▸</span>}
        <span className="console-msg mono">{e.message}</span>
        {e.source && <span className="muted small">{e.source}</span>}
      </div>
      {open && req && (
        <div className="console-detail mono">
          <ConsoleSection title="Request Headers" headers={e.data.request?.header} />
          {e.data.request?.body && (
            <>
              <div className="cd-title">Request Body</div>
              <pre>{e.data.request.body}</pre>
            </>
          )}
          {e.data.response && (
            <>
              <ConsoleSection title="Response Headers" headers={e.data.response.header} />
              <div className="cd-title">Response Body</div>
              <pre>{e.data.response.body}</pre>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function ConsoleSection({ title, headers }: { title: string; headers?: { key: string; value: string }[] }) {
  if (!headers?.length) return null;
  return (
    <>
      <div className="cd-title">{title}</div>
      {headers.map((h, i) => (
        <div key={i}>
          <span className="muted">{h.key}:</span> {h.value}
        </div>
      ))}
    </>
  );
}

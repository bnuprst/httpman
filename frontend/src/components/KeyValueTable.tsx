import { useState } from 'react';
import { api } from '../api';
import type { KV } from '../types';
import VarInput from './VarInput';

interface Props {
  rows: KV[];
  onChange: (rows: KV[]) => void;
  collectionId?: string;
  description?: boolean;
  files?: boolean; // form-data mode: per-row text/file type
  fixedKeys?: boolean; // path variables: keys come from the URL
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  bulk?: boolean;
  secretValues?: boolean;
  extraColumn?: { title: string; render: (row: KV, idx: number) => React.ReactNode };
}

function toBulk(rows: KV[]): string {
  return rows
    .filter((r) => r.key || r.value)
    .map((r) => `${r.disabled ? '//' : ''}${r.key}:${r.value ?? ''}`)
    .join('\n');
}

function fromBulk(text: string, prev: KV[]): KV[] {
  return text
    .split('\n')
    .filter((l) => l.trim())
    .map((line) => {
      let disabled = false;
      let l = line;
      if (l.trimStart().startsWith('//')) {
        disabled = true;
        l = l.trimStart().slice(2);
      }
      const i = l.indexOf(':');
      const key = (i < 0 ? l : l.slice(0, i)).trim();
      const value = i < 0 ? '' : l.slice(i + 1).trim();
      const old = prev.find((p) => p.key === key);
      const row: KV = { ...(old || {}), key, value };
      if (disabled) row.disabled = true;
      else delete row.disabled;
      return row;
    });
}

export default function KeyValueTable(p: Props) {
  const [bulkMode, setBulkMode] = useState(false);
  const [bulkText, setBulkText] = useState('');
  const rows = p.rows || [];
  const display = p.fixedKeys ? rows : [...rows, { key: '', value: '' } as KV];

  const update = (idx: number, patch: Partial<KV>) => {
    const next = [...rows];
    if (idx >= rows.length) {
      next.push({ key: '', value: '', ...(p.files ? { type: 'text' } : {}), ...patch });
    } else {
      next[idx] = { ...next[idx], ...patch };
      if (patch.disabled === false) delete next[idx].disabled;
    }
    p.onChange(next);
  };
  const remove = (idx: number) => p.onChange(rows.filter((_, i) => i !== idx));

  if (bulkMode) {
    return (
      <div className="kv-bulk">
        <div className="kv-toolbar">
          <span className="muted">One per line as key:value — prefix with // to disable</span>
          <button className="link" onClick={() => setBulkMode(false)}>
            Key-Value Edit
          </button>
        </div>
        <textarea
          className="bulk-text"
          spellCheck={false}
          value={bulkText}
          onChange={(e) => {
            setBulkText(e.target.value);
            p.onChange(fromBulk(e.target.value, rows));
          }}
        />
      </div>
    );
  }

  return (
    <div className="kv">
      {p.bulk !== false && !p.fixedKeys && (
        <div className="kv-toolbar">
          <span />
          <button
            className="link"
            onClick={() => {
              setBulkText(toBulk(rows));
              setBulkMode(true);
            }}
          >
            Bulk Edit
          </button>
        </div>
      )}
      <table className="kv-table">
        <thead>
          <tr>
            <th className="kv-check" />
            <th>Key</th>
            {p.files && <th className="kv-type">Type</th>}
            <th>Value</th>
            {p.description && <th>Description</th>}
            {p.extraColumn && <th>{p.extraColumn.title}</th>}
            <th className="kv-del" />
          </tr>
        </thead>
        <tbody>
          {display.map((r, idx) => {
            const placeholder = idx >= rows.length;
            const isFile = p.files && r.type === 'file';
            const files = Array.isArray(r.src) ? r.src : r.src ? [r.src] : [];
            return (
              <tr key={idx} className={r.disabled ? 'disabled' : ''}>
                <td className="kv-check">
                  {!placeholder && <input type="checkbox" checked={!r.disabled} onChange={(e) => update(idx, { disabled: !e.target.checked })} />}
                </td>
                <td>
                  {p.fixedKeys ? (
                    <span className="kv-fixed">{r.key}</span>
                  ) : (
                    <VarInput value={r.key} placeholder={p.keyPlaceholder || 'Key'} collectionId={p.collectionId} onChange={(v) => update(idx, { key: v })} />
                  )}
                </td>
                {p.files && (
                  <td className="kv-type">
                    <select value={r.type === 'file' ? 'file' : 'text'} onChange={(e) => update(idx, e.target.value === 'file' ? { type: 'file', value: undefined, src: [] } : { type: 'text', src: undefined, value: '' })}>
                      <option value="text">Text</option>
                      <option value="file">File</option>
                    </select>
                  </td>
                )}
                <td>
                  {isFile ? (
                    <div className="file-cell">
                      <button
                        className="btn small"
                        onClick={async () => {
                          const picked = await api.pickFiles('Select files');
                          if (picked && picked.length) update(idx, { src: picked.length === 1 ? picked[0] : picked });
                        }}
                      >
                        Select files
                      </button>
                      <span className="file-names" title={files.join('\n')}>
                        {files.length ? files.map((f) => f.split(/[\\/]/).pop()).join(', ') : <span className="muted">No file selected</span>}
                      </span>
                    </div>
                  ) : (
                    <VarInput
                      value={(r.value as string) ?? ''}
                      type={p.secretValues && r.type === 'secret' ? 'password' : undefined}
                      placeholder={p.valuePlaceholder || 'Value'}
                      collectionId={p.collectionId}
                      onChange={(v) => update(idx, { value: v })}
                    />
                  )}
                </td>
                {p.description && (
                  <td>
                    <input className="plain" value={typeof r.description === 'string' ? r.description : ''} placeholder="Description" onChange={(e) => update(idx, { description: e.target.value })} />
                  </td>
                )}
                {p.extraColumn && <td>{!placeholder && p.extraColumn.render(r, idx)}</td>}
                <td className="kv-del">
                  {!placeholder && !p.fixedKeys && (
                    <button className="icon-btn" title="Remove" onClick={() => remove(idx)}>
                      ×
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

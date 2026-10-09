import { api } from '../api';
import type { Body } from '../types';
import { kvList, prettyJSON, prettyXML } from '../util';
import Code, { Lang } from './Code';
import KeyValueTable from './KeyValueTable';

const MODES: { id: string; label: string }[] = [
  { id: 'none', label: 'none' },
  { id: 'formdata', label: 'form-data' },
  { id: 'urlencoded', label: 'x-www-form-urlencoded' },
  { id: 'raw', label: 'raw' },
  { id: 'file', label: 'binary' },
  { id: 'graphql', label: 'GraphQL' },
];

const LANGS = [
  { id: 'text', label: 'Text' },
  { id: 'javascript', label: 'JavaScript' },
  { id: 'json', label: 'JSON' },
  { id: 'html', label: 'HTML' },
  { id: 'xml', label: 'XML' },
];

interface Props {
  body: Body | undefined;
  onChange: (b: Body | undefined) => void;
  collectionId?: string;
  onSave?: () => void;
  onSend?: () => void;
}

export default function BodyEditor({ body, onChange, collectionId, onSave, onSend }: Props) {
  const mode = body?.mode && body.mode !== 'none' && !body.disabled ? body.mode : 'none';
  const lang = body?.options?.raw?.language || 'text';
  const set = (patch: Partial<Body>) => onChange({ ...(body || {}), ...patch });

  return (
    <div className="body-editor">
      <div className="body-modes">
        {MODES.map((m) => (
          <label key={m.id} className="radio">
            <input
              type="radio"
              name="body-mode"
              checked={mode === m.id}
              onChange={() => {
                if (m.id === 'none') onChange(body ? { ...body, mode: 'none' } : undefined);
                else set({ mode: m.id, disabled: undefined, ...(m.id === 'raw' && body?.raw === undefined ? { raw: '' } : {}) });
              }}
            />
            {m.label}
          </label>
        ))}
        {mode === 'raw' && (
          <>
            <select
              className="lang-select"
              value={lang}
              onChange={(e) => set({ options: { ...(body?.options || {}), raw: { ...(body?.options?.raw || {}), language: e.target.value } } })}
            >
              {LANGS.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.label}
                </option>
              ))}
            </select>
            {(lang === 'json' || lang === 'xml') && (
              <button
                className="link"
                onClick={() => {
                  const raw = body?.raw || '';
                  const out = lang === 'json' ? prettyJSON(raw) : prettyXML(raw);
                  if (out !== null) set({ raw: out });
                }}
              >
                Beautify
              </button>
            )}
          </>
        )}
      </div>
      <div className="body-content">
        {mode === 'none' && <div className="empty-note">This request does not have a body</div>}
        {mode === 'raw' && <Code value={body?.raw || ''} lang={lang as Lang} onChange={(v) => set({ raw: v })} onSave={onSave} onSend={onSend} />}
        {mode === 'urlencoded' && <KeyValueTable rows={kvList(body?.urlencoded)} onChange={(rows) => set({ urlencoded: rows })} collectionId={collectionId} description />}
        {mode === 'formdata' && <KeyValueTable rows={kvList(body?.formdata)} onChange={(rows) => set({ formdata: rows })} collectionId={collectionId} description files />}
        {mode === 'file' && (
          <div className="binary">
            <button
              className="btn"
              onClick={async () => {
                const picked = await api.pickFiles('Select file');
                if (picked && picked[0]) set({ file: { src: picked[0] } });
              }}
            >
              Select file
            </button>
            <span className="file-names">{body?.file?.src || <span className="muted">No file selected</span>}</span>
          </div>
        )}
        {mode === 'graphql' && (
          <div className="graphql">
            <div className="graphql-pane">
              <div className="pane-label">Query</div>
              <Code value={body?.graphql?.query || ''} lang="text" onChange={(v) => set({ graphql: { query: v, variables: body?.graphql?.variables || '' } })} onSave={onSave} onSend={onSend} />
            </div>
            <div className="graphql-pane">
              <div className="pane-label">GraphQL Variables</div>
              <Code value={body?.graphql?.variables || ''} lang="json" onChange={(v) => set({ graphql: { query: body?.graphql?.query || '', variables: v } })} onSave={onSave} onSend={onSend} />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

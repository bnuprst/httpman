import { CollectionTab, useStore } from '../store';
import type { Auth, Item, KV, Variable } from '../types';
import { descriptionText, findItem, getScript, setScript, varValueString } from '../util';
import AuthEditor from './AuthEditor';
import KeyValueTable from './KeyValueTable';
import ScriptEditor from './ScriptEditor';

export default function CollectionEditor({ tab }: { tab: CollectionTab }) {
  const st = useStore();
  const d = tab.draft;
  const coll = st.collections[tab.collectionId];
  const isFolder = !!tab.folderId;
  const pane = tab.pane || 'overview';
  const set = (patch: Partial<Item>) => st.updateTab(tab.id, { draft: { ...d, ...patch } });
  const save = () => void st.saveTab(tab.id);

  // For folders, auth may be inherited from a parent folder or the collection.
  let inherited: { name: string; type: string } | null = null;
  if (isFolder && coll) {
    const parents = findItem(coll.item, tab.folderId!)?.parents || [];
    for (let i = parents.length - 1; i >= 0 && !inherited; i--) {
      const a = parents[i].auth;
      if (a && a.type && a.type !== 'inherit') inherited = { name: parents[i].name, type: a.type };
    }
    if (!inherited && coll.auth && coll.auth.type) inherited = { name: coll.info.name, type: coll.auth.type };
  }

  const panes = isFolder ? ['overview', 'auth', 'prerequest', 'tests'] : ['overview', 'auth', 'prerequest', 'tests', 'variables'];
  const labels: Record<string, string> = { overview: 'Overview', auth: 'Authorization', prerequest: 'Pre-request Script', tests: 'Tests', variables: 'Variables' };
  const vars: KV[] = (d.variable || []).map((v) => ({ key: v.key, value: varValueString(v.value), disabled: v.disabled, description: typeof v.description === 'string' ? v.description : '' }));

  return (
    <div className="editor-page">
      <div className="editor-head">
        <span className="muted">{isFolder ? 'Folder' : 'Collection'}</span>
        <input className="title-input" value={d.name} onChange={(e) => set({ name: e.target.value })} />
        <span className="spacer" />
        {!isFolder && (
          <button className="btn" onClick={() => st.openRunner({ collectionId: tab.collectionId })}>
            Run
          </button>
        )}
        <button className="btn primary" onClick={save}>
          Save{tab.dirty ? ' •' : ''}
        </button>
      </div>
      <div className="subtabs">
        {panes.map((p) => (
          <button key={p} className={'subtab' + (pane === p ? ' active' : '')} onClick={() => st.updateTab(tab.id, { pane: p }, false)}>
            {labels[p]}
          </button>
        ))}
      </div>
      <div className="pane-body">
        {pane === 'overview' && (
          <div className="scroll pad">
            <label className="field-label">Description</label>
            <textarea className="desc" value={descriptionText(d.description)} placeholder="Add a description (Markdown)" onChange={(e) => set({ description: e.target.value })} />
            <p className="muted small">
              {isFolder
                ? 'Folder scripts run before/after every request in this folder, after collection scripts.'
                : 'Collection scripts run before/after every request in the collection. Auth set here is inherited by requests set to “Inherit auth from parent”.'}
            </p>
          </div>
        )}
        {pane === 'auth' && (
          <div className="scroll">
            <AuthEditor auth={d.auth} allowInherit={isFolder} inheritedFrom={inherited} collectionId={tab.collectionId} onChange={(a: Auth | null) => set({ auth: a ?? undefined })} />
          </div>
        )}
        {pane === 'prerequest' && <ScriptEditor kind="prerequest" value={getScript(d.event, 'prerequest')} onChange={(v) => set({ event: setScript(d.event, 'prerequest', v) })} onSave={save} />}
        {pane === 'tests' && <ScriptEditor kind="test" value={getScript(d.event, 'test')} onChange={(v) => set({ event: setScript(d.event, 'test', v) })} onSave={save} />}
        {pane === 'variables' && (
          <div className="scroll">
            <p className="muted small pad">Collection variables are available to every request in this collection. Environment variables take precedence over them.</p>
            <KeyValueTable
              rows={vars}
              keyPlaceholder="Variable"
              collectionId={tab.collectionId}
              description
              onChange={(rows) =>
                set({
                  variable: rows.map((r, i) => {
                    const prev = (d.variable || [])[i];
                    const v: Variable = { ...(prev || {}), key: r.key, value: prev && varValueString(prev.value) === r.value ? prev.value : r.value ?? '' };
                    if (r.disabled) v.disabled = true;
                    else delete v.disabled;
                    if (r.description) v.description = r.description;
                    else delete v.description;
                    return v;
                  }),
                })
              }
            />
          </div>
        )}
      </div>
    </div>
  );
}

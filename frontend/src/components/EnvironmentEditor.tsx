import { EnvironmentTab, useStore } from '../store';
import type { KV, Var } from '../types';
import { varValueString } from '../util';
import KeyValueTable from './KeyValueTable';

export default function EnvironmentEditor({ tab }: { tab: EnvironmentTab }) {
  const st = useStore();
  const env = tab.draft;
  const isGlobals = tab.envId === 'globals';
  const rows: KV[] = env.values.map((v) => ({ key: v.key, value: varValueString(v.value), disabled: !v.enabled, type: v.type || 'default' }));
  const setRows = (next: KV[]) => {
    const values: Var[] = next.map((r, i) => {
      const prev = env.values[i];
      const unchanged = prev && prev.key === r.key && varValueString(prev.value) === r.value;
      return { key: r.key, value: unchanged ? prev.value : r.value ?? '', type: r.type || 'default', enabled: !r.disabled };
    });
    st.updateTab(tab.id, { draft: { ...env, values } });
  };
  const active = st.selectedEnvId === tab.envId;
  return (
    <div className="editor-page">
      <div className="editor-head">
        {isGlobals ? <h2>Globals</h2> : <input className="title-input" value={env.name} onChange={(e) => st.updateTab(tab.id, { draft: { ...env, name: e.target.value } })} />}
        <span className="spacer" />
        {!isGlobals && (
          <button className="btn" onClick={() => st.set({ selectedEnvId: active ? '' : tab.envId })}>
            {active ? 'Deactivate' : 'Set active'}
          </button>
        )}
        <button className="btn primary" onClick={() => st.saveTab(tab.id)}>
          Save{tab.dirty ? ' •' : ''}
        </button>
      </div>
      <p className="muted small pad">
        {isGlobals
          ? 'Global variables are available in every request, regardless of the selected environment.'
          : 'Environment variables are used when this environment is active. Scripts that set variables update these values.'}{' '}
        Reference them as {'{{name}}'}.
      </p>
      <div className="scroll">
        <KeyValueTable
          rows={rows}
          onChange={setRows}
          keyPlaceholder="Variable"
          valuePlaceholder="Value"
          secretValues
          extraColumn={{
            title: 'Type',
            render: (r, idx) => (
              <select
                value={r.type === 'secret' ? 'secret' : 'default'}
                onChange={(e) => {
                  const next = [...rows];
                  next[idx] = { ...next[idx], type: e.target.value };
                  setRows(next);
                }}
              >
                <option value="default">default</option>
                <option value="secret">secret</option>
              </select>
            ),
          }}
        />
      </div>
    </div>
  );
}

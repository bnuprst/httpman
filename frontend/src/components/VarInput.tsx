import { useMemo, useRef, useState } from 'react';
import { useStore } from '../store';
import { varValueString } from '../util';

/** Returns a resolver for {{variables}} based on the active scopes. */
export function useVariableLookup(collectionId?: string) {
  const env = useStore((s) => s.environments.find((e) => e.id === s.selectedEnvId));
  const globals = useStore((s) => s.globals);
  const coll = useStore((s) => (collectionId ? s.collections[collectionId] : undefined));
  return useMemo(() => {
    const scopes: { name: string; vars: Map<string, unknown> }[] = [];
    const add = (name: string, list: { key: string; value?: unknown; enabled?: boolean; disabled?: boolean }[] | undefined) => {
      const m = new Map<string, unknown>();
      (list || []).forEach((v) => {
        if (v.enabled !== false && !v.disabled) m.set(v.key, v.value);
      });
      scopes.push({ name, vars: m });
    };
    add('Environment', env?.values);
    add('Collection', coll?.variable);
    add('Global', globals.values);
    return (name: string): { value: string; scope: string } | null => {
      if (name.startsWith('$')) return { value: '(dynamic)', scope: 'Dynamic' };
      for (const s of scopes) if (s.vars.has(name)) return { value: varValueString(s.vars.get(name)), scope: s.name };
      return null;
    };
  }, [env, globals, coll]);
}

interface Props {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  className?: string;
  collectionId?: string;
  onEnter?: () => void;
  autoFocus?: boolean;
  disabled?: boolean;
  type?: string;
}

/** A single-line input that highlights {{variables}} (resolved in green, unknown in red). */
export default function VarInput({ value, onChange, placeholder, className, collectionId, onEnter, autoFocus, disabled, type }: Props) {
  const lookup = useVariableLookup(collectionId);
  const mirror = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<string | null>(null);
  const v = value ?? '';
  const parts = useMemo(() => {
    const out: { text: string; var?: string }[] = [];
    const re = /\{\{([^{}]+)\}\}/g;
    let last = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(v))) {
      if (m.index > last) out.push({ text: v.slice(last, m.index) });
      out.push({ text: m[0], var: m[1].trim() });
      last = m.index + m[0].length;
    }
    if (last < v.length) out.push({ text: v.slice(last) });
    return out;
  }, [v]);
  const hasVars = parts.some((p) => p.var);
  const vars = parts.filter((p) => p.var).map((p) => p.var!);
  const title = hasVars
    ? vars
        .map((n) => {
          const r = lookup(n);
          return r ? `${n} = ${r.value}  (${r.scope})` : `${n}: unresolved`;
        })
        .join('\n')
    : undefined;
  return (
    <div className={'varinput ' + (className || '')} title={title} onMouseEnter={() => setHover(title || null)} onMouseLeave={() => setHover(null)}>
      {hasVars && type !== 'password' && (
        <div className="varinput-mirror" ref={mirror} aria-hidden>
          {parts.map((p, i) =>
            p.var ? (
              <span key={i} className={lookup(p.var) ? 'var-ok' : 'var-missing'}>
                {p.text}
              </span>
            ) : (
              <span key={i}>{p.text}</span>
            ),
          )}
        </div>
      )}
      <input
        className={hasVars && type !== 'password' ? 'has-mirror' : ''}
        value={v}
        type={type || 'text'}
        spellCheck={false}
        autoFocus={autoFocus}
        disabled={disabled}
        placeholder={placeholder}
        data-hover={hover ? '1' : undefined}
        onChange={(e) => onChange(e.target.value)}
        onScroll={(e) => {
          if (mirror.current) mirror.current.scrollLeft = (e.target as HTMLInputElement).scrollLeft;
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && onEnter) {
            e.preventDefault();
            onEnter();
          }
        }}
      />
    </div>
  );
}

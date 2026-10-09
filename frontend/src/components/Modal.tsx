import { ReactNode, useEffect } from 'react';

interface Props {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
  wide?: boolean;
}

export default function Modal({ title, onClose, children, footer, width, wide }: Props) {
  useEffect(() => {
    const fn = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', fn);
    return () => window.removeEventListener('keydown', fn);
  }, [onClose]);
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={'modal' + (wide ? ' wide' : '')} style={width ? { width } : undefined} role="dialog" aria-label={title}>
        <div className="modal-head">
          <h2>{title}</h2>
          <button className="icon-btn" onClick={onClose} title="Close">
            ×
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </div>
  );
}

export interface MenuItem {
  label: string;
  onClick: () => void;
  danger?: boolean;
  separator?: boolean;
  shortcut?: string;
}

export function Menu({ items, x, y, onClose }: { items: MenuItem[]; x: number; y: number; onClose: () => void }) {
  useEffect(() => {
    const close = () => onClose();
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('mousedown', close);
    window.addEventListener('blur', close);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('mousedown', close);
      window.removeEventListener('blur', close);
      window.removeEventListener('keydown', key);
    };
  }, [onClose]);
  const top = Math.min(y, window.innerHeight - items.length * 30 - 16);
  const left = Math.min(x, window.innerWidth - 220);
  return (
    <div className="menu" style={{ top, left }} onMouseDown={(e) => e.stopPropagation()}>
      {items.map((it, i) =>
        it.separator ? (
          <div key={i} className="menu-sep" />
        ) : (
          <button
            key={i}
            className={'menu-item' + (it.danger ? ' danger' : '')}
            onClick={() => {
              onClose();
              it.onClick();
            }}
          >
            <span>{it.label}</span>
            {it.shortcut && <span className="muted">{it.shortcut}</span>}
          </button>
        ),
      )}
    </div>
  );
}

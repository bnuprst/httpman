import CodeMirror, { EditorView, keymap } from '@uiw/react-codemirror';
import { javascript, javascriptLanguage } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { xml } from '@codemirror/lang-xml';
import { html } from '@codemirror/lang-html';
import { CompletionContext, CompletionResult } from '@codemirror/autocomplete';
import { indentUnit } from '@codemirror/language';
import { useMemo, useRef, useState } from 'react';
import { useTheme } from '../theme';
import { useStore } from '../store';
import { copyText, readClipboardText } from '../api';
import { applyCodec, Codec } from '../util';
import { Menu, MenuItem } from './Modal';

const pmCompletions: { label: string; detail?: string; type?: string }[] = [
  'pm.test("name", function () {\n  \n});',
  'pm.expect()',
  'pm.response.code',
  'pm.response.status',
  'pm.response.json()',
  'pm.response.text()',
  'pm.response.headers.get("")',
  'pm.response.responseTime',
  'pm.response.responseSize',
  'pm.response.to.have.status(200)',
  'pm.response.to.have.header("")',
  'pm.response.to.have.jsonBody("")',
  'pm.response.to.have.jsonSchema(schema)',
  'pm.response.to.be.ok',
  'pm.response.to.be.json',
  'pm.response.to.not.be.error',
  'pm.request.url',
  'pm.request.method',
  'pm.request.headers.add({ key: "", value: "" })',
  'pm.request.headers.upsert({ key: "", value: "" })',
  'pm.request.headers.remove("")',
  'pm.request.body',
  'pm.environment.get("")',
  'pm.environment.set("", "")',
  'pm.environment.unset("")',
  'pm.globals.get("")',
  'pm.globals.set("", "")',
  'pm.collectionVariables.get("")',
  'pm.collectionVariables.set("", "")',
  'pm.variables.get("")',
  'pm.variables.set("", "")',
  'pm.variables.replaceIn("")',
  'pm.iterationData.get("")',
  'pm.cookies.get("")',
  'pm.cookies.jar()',
  'pm.sendRequest("", function (err, res) {\n  \n});',
  'pm.execution.setNextRequest("")',
  'pm.execution.skipRequest()',
  'pm.info.requestName',
  'pm.info.iteration',
  'pm.visualizer.set(template, data)',
  'postman.setNextRequest("")',
  'console.log()',
].map((label) => ({ label, type: 'function' }));

function pmComplete(ctx: CompletionContext): CompletionResult | null {
  const word = ctx.matchBefore(/(pm|postman|console)(\.[\w.]*)?/);
  if (!word || (word.from === word.to && !ctx.explicit)) return null;
  return { from: word.from, options: pmCompletions.filter((c) => c.label.startsWith(word.text)), validFor: /^[\w.]*$/ };
}

export type Lang = 'json' | 'javascript' | 'xml' | 'html' | 'text' | 'graphql';

interface Props {
  value: string;
  onChange?: (v: string) => void;
  lang?: Lang;
  readOnly?: boolean;
  height?: string;
  minHeight?: string;
  wrap?: boolean;
  scripts?: boolean;
  placeholder?: string;
  onSave?: () => void;
  onSend?: () => void;
  // Right-click menu with clipboard actions and base64 / gzip+base64 encode/decode of the selection.
  codecs?: boolean;
}

const codecItems: { label: string; codec: Codec }[] = [
  { label: 'Base64 decode', codec: 'base64-decode' },
  { label: 'Base64 encode', codec: 'base64-encode' },
  { label: 'Gzip + Base64 decode', codec: 'gzip-base64-decode' },
  { label: 'Gzip + Base64 encode', codec: 'gzip-base64-encode' },
];

// Replaces the selection (or the whole document when nothing is selected) with
// its transformed text and selects the result. Read-only editors are changed in
// view only: the dispatch bypasses the readOnly facet, which guards user input.
async function transform(view: EditorView, codec: Codec) {
  const sel = view.state.selection.main;
  const [from, to] = sel.empty ? [0, view.state.doc.length] : [sel.from, sel.to];
  const input = view.state.sliceDoc(from, to);
  try {
    const out = await applyCodec(codec, input);
    if (view.state.sliceDoc(from, to) !== input) return;
    view.dispatch({ changes: { from, to, insert: out }, selection: { anchor: from, head: from + out.length }, userEvent: 'input.codec' });
    view.focus();
  } catch (e) {
    useStore.getState().toast((e as Error).message, 'error');
  }
}

export default function Code({ value, onChange, lang = 'text', readOnly, height = '100%', minHeight, wrap, scripts, placeholder, onSave, onSend, codecs }: Props) {
  const view = useRef<EditorView>();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const dark = useTheme() === 'dark';
  const fontSize = useStore((s) => s.settings.fontSize);
  const tabSize = useStore((s) => s.settings.editorTabSize) || 2;
  const extensions = useMemo(() => {
    const ext = [indentUnit.of(' '.repeat(tabSize)), EditorView.theme({ '&': { fontSize: fontSize + 'px' } })];
    if (lang === 'json') ext.push(json());
    else if (lang === 'javascript') ext.push(javascript());
    else if (lang === 'xml') ext.push(xml());
    else if (lang === 'html') ext.push(html());
    if (scripts) ext.push(javascriptLanguage.data.of({ autocomplete: pmComplete }));
    if (wrap) ext.push(EditorView.lineWrapping);
    const keys = [];
    if (onSave) keys.push({ key: 'Mod-s', run: () => (onSave(), true), preventDefault: true });
    if (onSend) keys.push({ key: 'Mod-Enter', run: () => (onSend(), true), preventDefault: true });
    if (keys.length) ext.push(keymap.of(keys));
    if (codecs)
      ext.push(
        EditorView.domEventHandlers({
          contextmenu: (e) => {
            e.preventDefault();
            setMenu({ x: e.clientX, y: e.clientY });
            return true;
          },
        }),
      );
    return ext;
  }, [lang, wrap, scripts, fontSize, tabSize, onSave, onSend, codecs]);

  const menuItems = (): MenuItem[] => {
    const v = view.current!;
    const sel = v.state.selection.main;
    const selected = v.state.sliceDoc(sel.from, sel.to);
    const scope = sel.empty ? ' (all)' : '';
    const items: MenuItem[] = [];
    if (!readOnly && !sel.empty)
      items.push({
        label: 'Cut',
        onClick: () => copyText(selected).then(() => v.dispatch(v.state.replaceSelection(''), { userEvent: 'delete.cut' })),
      });
    items.push({ label: sel.empty ? 'Copy all' : 'Copy', onClick: () => copyText(sel.empty ? v.state.doc.toString() : selected) });
    if (!readOnly)
      items.push({
        label: 'Paste',
        onClick: () =>
          readClipboardText().then(
            (t) => (v.dispatch(v.state.replaceSelection(t), { userEvent: 'input.paste' }), v.focus()),
            () => useStore.getState().toast('Clipboard is not available', 'error'),
          ),
      });
    items.push({ label: 'Select all', onClick: () => (v.dispatch({ selection: { anchor: 0, head: v.state.doc.length } }), v.focus()) });
    items.push({ label: '', separator: true, onClick: () => {} });
    for (const c of codecItems) items.push({ label: c.label + scope, onClick: () => transform(v, c.codec) });
    return items;
  };

  return (
    <>
      <CodeMirror
        className="code"
        value={value}
        onChange={onChange}
        readOnly={readOnly}
        editable={!readOnly}
        theme={dark ? 'dark' : 'light'}
        height={height}
        minHeight={minHeight}
        extensions={extensions}
        placeholder={placeholder}
        basicSetup={{ foldGutter: true, highlightActiveLine: !readOnly, autocompletion: true, tabSize }}
        onCreateEditor={(v) => (view.current = v)}
      />
      {menu && view.current && <Menu x={menu.x} y={menu.y} items={menuItems()} onClose={() => setMenu(null)} />}
    </>
  );
}

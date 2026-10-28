import { useEffect, useRef, useState } from 'react';
import { api, onEvent, openExternal } from '../api';
import { useStore } from '../store';
import type { Auth, Collection } from '../types';
import { authParam, authParamValue, removeAuthParam, setAuthParam, uuid } from '../util';
import VarInput from './VarInput';

type Field = {
  key: string;
  label: string;
  type?: 'password' | 'select' | 'checkbox';
  options?: string[];
  placeholder?: string;
  def?: string;
  section?: string; // starts a titled group of fields
  showFor?: string[]; // OAuth 2.0: grant types that use this field
};

// OAuth 2.0 grant types, as Postman stores them in grant_type.
const AC = 'authorization_code';
const PKCE = 'authorization_code_with_pkce';
const IMPLICIT = 'implicit';
const PASSWORD = 'password_credentials';
const CLIENT = 'client_credentials';
const TOKEN_GRANTS = [AC, PKCE, PASSWORD, CLIENT];
const BROWSER_GRANTS = [AC, PKCE, IMPLICIT];

const TYPES: { id: string; label: string; fields: Field[] }[] = [
  { id: 'noauth', label: 'No Auth', fields: [] },
  { id: 'basic', label: 'Basic Auth', fields: [{ key: 'username', label: 'Username' }, { key: 'password', label: 'Password', type: 'password' }] },
  { id: 'bearer', label: 'Bearer Token', fields: [{ key: 'token', label: 'Token' }] },
  {
    id: 'apikey',
    label: 'API Key',
    fields: [
      { key: 'key', label: 'Key' },
      { key: 'value', label: 'Value' },
      { key: 'in', label: 'Add to', type: 'select', options: ['header', 'query'], def: 'header' },
    ],
  },
  {
    id: 'digest',
    label: 'Digest Auth',
    fields: [
      { key: 'username', label: 'Username' },
      { key: 'password', label: 'Password', type: 'password' },
      { key: 'realm', label: 'Realm', placeholder: 'optional: taken from the server challenge' },
      { key: 'nonce', label: 'Nonce', placeholder: 'optional' },
      { key: 'algorithm', label: 'Algorithm', type: 'select', options: ['MD5', 'MD5-sess', 'SHA-256', 'SHA-256-sess', 'SHA-512-256'], def: 'MD5' },
      { key: 'qop', label: 'qop', placeholder: 'optional, e.g. auth' },
      { key: 'opaque', label: 'Opaque', placeholder: 'optional' },
    ],
  },
  {
    id: 'oauth1',
    label: 'OAuth 1.0',
    fields: [
      { key: 'signatureMethod', label: 'Signature Method', type: 'select', options: ['HMAC-SHA1', 'HMAC-SHA256', 'HMAC-SHA512', 'PLAINTEXT'], def: 'HMAC-SHA1' },
      { key: 'consumerKey', label: 'Consumer Key' },
      { key: 'consumerSecret', label: 'Consumer Secret', type: 'password' },
      { key: 'token', label: 'Access Token' },
      { key: 'tokenSecret', label: 'Token Secret', type: 'password' },
      { key: 'addParamsToHeader', label: 'Add params to header', type: 'checkbox', def: 'true' },
      { key: 'callback', label: 'Callback URL', placeholder: 'optional' },
      { key: 'verifier', label: 'Verifier', placeholder: 'optional' },
      { key: 'timestamp', label: 'Timestamp', placeholder: 'auto' },
      { key: 'nonce', label: 'Nonce', placeholder: 'auto' },
      { key: 'version', label: 'Version', placeholder: '1.0' },
      { key: 'realm', label: 'Realm', placeholder: 'optional' },
    ],
  },
  {
    id: 'oauth2',
    label: 'OAuth 2.0',
    fields: [
      { key: 'accessToken', label: 'Access Token', section: 'Current Token' },
      { key: 'headerPrefix', label: 'Header Prefix', placeholder: 'Bearer' },
      { key: 'addTokenTo', label: 'Add token to', type: 'select', options: ['header', 'queryParams'], def: 'header' },
      { key: 'tokenName', label: 'Token Name', placeholder: 'optional', section: 'Configure New Token' },
      { key: 'grant_type', label: 'Grant Type', type: 'select', options: [AC, PKCE, IMPLICIT, PASSWORD, CLIENT], def: AC },
      { key: 'redirect_uri', label: 'Callback URL', placeholder: 'e.g. http://localhost:8765/callback', showFor: BROWSER_GRANTS },
      { key: 'useBrowser', label: 'Authorize using browser', type: 'checkbox', def: 'false', showFor: BROWSER_GRANTS },
      { key: 'authUrl', label: 'Auth URL', showFor: BROWSER_GRANTS },
      { key: 'accessTokenUrl', label: 'Access Token URL', showFor: TOKEN_GRANTS },
      { key: 'clientId', label: 'Client ID' },
      { key: 'clientSecret', label: 'Client Secret', type: 'password', showFor: TOKEN_GRANTS },
      { key: 'username', label: 'Username', showFor: [PASSWORD] },
      { key: 'password', label: 'Password', type: 'password', showFor: [PASSWORD] },
      { key: 'challengeAlgorithm', label: 'Code Challenge Method', type: 'select', options: ['S256', 'plain'], def: 'S256', showFor: [PKCE] },
      { key: 'code_verifier', label: 'Code Verifier', placeholder: 'automatically generated if left blank', showFor: [PKCE] },
      { key: 'scope', label: 'Scope', placeholder: 'e.g. read:org' },
      { key: 'state', label: 'State', placeholder: 'optional', showFor: BROWSER_GRANTS },
      { key: 'client_authentication', label: 'Client Authentication', type: 'select', options: ['header', 'body'], def: 'header', showFor: TOKEN_GRANTS },
      { key: 'refreshTokenUrl', label: 'Refresh Token URL', placeholder: 'same as Access Token URL', showFor: TOKEN_GRANTS },
    ],
  },
  {
    id: 'hawk',
    label: 'Hawk Authentication',
    fields: [
      { key: 'authId', label: 'Hawk Auth ID' },
      { key: 'authKey', label: 'Hawk Auth Key', type: 'password' },
      { key: 'algorithm', label: 'Algorithm', type: 'select', options: ['sha256', 'sha1'], def: 'sha256' },
      { key: 'user', label: 'User', placeholder: 'optional' },
      { key: 'nonce', label: 'Nonce', placeholder: 'auto' },
      { key: 'extraData', label: 'ext', placeholder: 'optional' },
      { key: 'app', label: 'app', placeholder: 'optional' },
      { key: 'delegation', label: 'dlg', placeholder: 'optional' },
      { key: 'timestamp', label: 'Timestamp', placeholder: 'auto' },
      { key: 'includePayloadHash', label: 'Include payload hash', type: 'checkbox', def: 'false' },
    ],
  },
  {
    id: 'awsv4',
    label: 'AWS Signature',
    fields: [
      { key: 'accessKey', label: 'Access Key' },
      { key: 'secretKey', label: 'Secret Key', type: 'password' },
      { key: 'region', label: 'AWS Region', placeholder: 'us-east-1' },
      { key: 'service', label: 'Service Name', placeholder: 'execute-api' },
      { key: 'sessionToken', label: 'Session Token', placeholder: 'optional' },
    ],
  },
];

interface Props {
  auth: Auth | null | undefined;
  onChange: (a: Auth | null) => void;
  allowInherit: boolean;
  inheritedFrom?: { name: string; type: string } | null;
  collectionId?: string;
  /** The collection whose variables are used to get an OAuth 2.0 token. */
  tokenCollection?: () => Collection | null;
}

export default function AuthEditor({ auth, onChange, allowInherit, inheritedFrom, collectionId, tokenCollection }: Props) {
  const type = !auth || !auth.type || auth.type === 'inherit' ? (allowInherit ? 'inherit' : 'noauth') : auth.type;
  const def = TYPES.find((t) => t.id === type);
  return (
    <div className="auth">
      <div className="auth-side">
        <label className="field-label">Type</label>
        <select
          value={type}
          onChange={(e) => {
            const t = e.target.value;
            if (t === 'inherit') return onChange(null);
            const keep = auth && Array.isArray(auth[t]) ? (auth[t] as unknown[]) : [];
            onChange({ type: t, [t]: keep } as Auth);
          }}
        >
          {allowInherit && <option value="inherit">Inherit auth from parent</option>}
          {TYPES.map((t) => (
            <option key={t.id} value={t.id}>
              {t.label}
            </option>
          ))}
          {!def && type !== 'inherit' && <option value={type}>{type} (unsupported)</option>}
        </select>
        <p className="muted small">
          {type === 'inherit'
            ? 'The authorization header will be generated from the parent folder or collection.'
            : type === 'noauth'
              ? 'This request does not use any authorization.'
              : 'Authorization data is added to the request when it is sent. Variables like {{token}} are supported.'}
        </p>
      </div>
      <div className="auth-main">
        {type === 'inherit' && (
          <div className="info-box">
            {inheritedFrom ? (
              <>
                This request is using <b>{TYPES.find((t) => t.id === inheritedFrom.type)?.label || inheritedFrom.type}</b> from <b>{inheritedFrom.name}</b>.
                {inheritedFrom.type === 'oauth2' && <> Open {inheritedFrom.name} to get a new access token.</>}
              </>
            ) : (
              'No auth is configured on any parent; the request is sent without authorization.'
            )}
          </div>
        )}
        {!def && type !== 'inherit' && <div className="info-box">Auth type “{type}” is not supported by httpman yet. Its settings are preserved in the collection.</div>}
        {def?.fields.map((f) => {
          if (f.showFor && !f.showFor.includes(authParam(auth, 'grant_type') || AC)) return null;
          const v = authParam(auth, f.key);
          // Postman: a missing or null headerPrefix means "Bearer", an empty
          // one means no prefix. Clearing the field restores the default.
          const set = (val: unknown) =>
            onChange(f.key === 'headerPrefix' && val === '' ? removeAuthParam(auth as Auth, f.key) : setAuthParam(auth as Auth, f.key, val));
          const placeholder = f.key === 'headerPrefix' && v === '' && authParamValue(auth, f.key) === '' ? 'none (token sent without prefix)' : f.placeholder || f.label;
          return (
            <div key={f.key}>
              {f.section && <div className="section-title auth-section">{f.section}</div>}
              <div className="form-row">
                <label>{f.label}</label>
                {f.type === 'select' ? (
                  <select value={v || f.def} onChange={(e) => set(e.target.value)}>
                    {f.options!.map((o) => (
                      <option key={o} value={o}>
                        {o}
                      </option>
                    ))}
                  </select>
                ) : f.type === 'checkbox' ? (
                  <input type="checkbox" checked={(v || f.def) === 'true'} onChange={(e) => set(e.target.checked)} />
                ) : (
                  <VarInput value={v} type={f.type === 'password' ? 'password' : undefined} placeholder={placeholder} collectionId={collectionId} onChange={set} />
                )}
              </div>
            </div>
          );
        })}
        {type === 'oauth2' && auth && (
          <>
            <RequestParams auth={auth} onChange={onChange} collectionId={collectionId} />
            <OAuth2Token auth={auth} onChange={onChange} tokenCollection={tokenCollection} />
          </>
        )}
      </div>
    </div>
  );
}

const SEND_AS = [
  { id: 'request_body', label: 'Request Body' },
  { id: 'request_header', label: 'Request Header' },
  { id: 'request_url', label: 'URL' },
];

type ExtraParam = { key: string; value: string; enabled?: boolean; send_as?: string; [extra: string]: unknown };

/** Postman's OAuth 2.0 advanced options: extra parameters for the auth and token requests (audience, resource, ...). */
function RequestParams({ auth, onChange, collectionId }: { auth: Auth; onChange: (a: Auth) => void; collectionId?: string }) {
  const grant = authParam(auth, 'grant_type') || AC;
  const lists = [
    { key: 'authRequestParams', title: 'Auth Request', show: BROWSER_GRANTS.includes(grant), sendAs: false },
    { key: 'tokenRequestParams', title: 'Token Request', show: grant !== IMPLICIT, sendAs: true },
    { key: 'refreshRequestParams', title: 'Refresh Request', show: grant !== IMPLICIT, sendAs: true },
  ];
  return (
    <>
      <div className="section-title auth-section">Advanced Options</div>
      {lists
        .filter((l) => l.show)
        .map((l) => {
          const raw = authParamValue(auth, l.key);
          const rows: ExtraParam[] = Array.isArray(raw) ? (raw as ExtraParam[]) : [];
          const set = (next: ExtraParam[]) => onChange(setAuthParam(auth, l.key, next));
          const update = (i: number, patch: Partial<ExtraParam>) => set(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
          return (
            <div key={l.key} className="oauth2-params">
              <div className="form-row">
                <label>{l.title}</label>
                <button
                  className="btn small"
                  onClick={() => set([...rows, l.sendAs ? { key: '', value: '', enabled: true, send_as: 'request_body' } : { key: '', value: '', enabled: true, send_as: 'request_url' }])}
                >
                  Add parameter
                </button>
              </div>
              {rows.map((r, i) => (
                <div key={i} className="oauth2-param">
                  <input type="checkbox" checked={r.enabled !== false} onChange={(e) => update(i, { enabled: e.target.checked })} title="Enabled" />
                  <VarInput value={String(r.key ?? '')} placeholder="Key (e.g. audience)" collectionId={collectionId} onChange={(key) => update(i, { key })} />
                  <VarInput value={String(r.value ?? '')} placeholder="Value" collectionId={collectionId} onChange={(value) => update(i, { value })} />
                  {l.sendAs && (
                    <select value={r.send_as || 'request_body'} onChange={(e) => update(i, { send_as: e.target.value })}>
                      {SEND_AS.map((o) => (
                        <option key={o.id} value={o.id}>
                          {o.label}
                        </option>
                      ))}
                    </select>
                  )}
                  <button className="btn small" title="Remove" onClick={() => set(rows.filter((_, j) => j !== i))}>
                    ✕
                  </button>
                </div>
              ))}
            </div>
          );
        })}
    </>
  );
}

// Refresh tokens are kept in memory only (by access token), so collections
// never gain keys Postman doesn't know.
const refreshTokens = new Map<string, string>();

function OAuth2Token({ auth, onChange, tokenCollection }: { auth: Auth; onChange: (a: Auth) => void; tokenCollection?: () => Collection | null }) {
  const envId = useStore((s) => s.selectedEnvId);
  const [busy, setBusy] = useState<string | null>(null); // request id
  const [status, setStatus] = useState<{ error?: boolean; text: string } | null>(null);
  const [waiting, setWaiting] = useState<{ url: string; captured: boolean } | null>(null);
  const [pasted, setPasted] = useState('');
  // The token is written to the auth as it is when the token arrives.
  const latest = useRef(auth);
  latest.current = auth;

  // Subscribed for the component's lifetime and matched by ref, so an event
  // emitted before React re-renders with the new request id is not missed.
  const pendingId = useRef<string | null>(null);
  useEffect(
    () =>
      onEvent('oauth2:authorize', (d: { requestId: string; url: string; captured: boolean }) => {
        if (d.requestId !== pendingId.current) return;
        setWaiting({ url: d.url, captured: d.captured });
        openExternal(d.url);
      }),
    [],
  );

  const current = authParam(auth, 'accessToken');
  const refreshToken = current ? refreshTokens.get(current) : undefined;

  const run = async (refresh: boolean) => {
    const requestId = uuid();
    pendingId.current = requestId;
    setBusy(requestId);
    setStatus({ text: refresh ? 'Refreshing token…' : 'Requesting token…' });
    setWaiting(null);
    setPasted('');
    try {
      const tok = await api.getOAuth2Token({
        requestId,
        collection: tokenCollection?.() ?? null,
        auth,
        environmentId: envId || undefined,
        refreshToken: refresh ? refreshToken : undefined,
      });
      let next = setAuthParam(latest.current, 'accessToken', tok.accessToken);
      if (tok.tokenType) next = setAuthParam(next, 'tokenType', tok.tokenType);
      if (tok.refreshToken) refreshTokens.set(tok.accessToken, tok.refreshToken);
      else if (refreshToken) refreshTokens.set(tok.accessToken, refreshToken);
      onChange(next);
      setStatus({ text: 'Token received' + (tok.expiresIn ? `, expires in ${tok.expiresIn} s` : '') + '. Save to keep it.' });
    } catch (e) {
      setStatus({ error: true, text: e instanceof Error ? e.message : String(e) });
    } finally {
      pendingId.current = null;
      setBusy(null);
      setWaiting(null);
    }
  };

  return (
    <div className="oauth2-actions">
      <div className="form-row">
        <span />
        <div className="row-gap">
          <button className="btn primary" disabled={!!busy} onClick={() => void run(false)}>
            Get New Access Token
          </button>
          {refreshToken && (
            <button className="btn" disabled={!!busy} onClick={() => void run(true)}>
              Refresh Token
            </button>
          )}
          {busy && (
            <button className="btn" onClick={() => void api.cancel(busy)}>
              Cancel
            </button>
          )}
        </div>
      </div>
      {waiting && (
        <div className="info-box">
          <p>
            Complete the authorization in your browser.{' '}
            <a href="#" onClick={(e) => (e.preventDefault(), openExternal(waiting.url))}>
              Open it again
            </a>
          </p>
          <p className="muted small">
            {waiting.captured
              ? 'httpman is listening on the callback URL and picks up the redirect automatically.'
              : 'The callback URL is not on this machine, so httpman cannot receive the redirect. When the browser lands on the callback URL, copy the full address and paste it here.'}
          </p>
          <div className="row-gap">
            <input className="grow" value={pasted} placeholder="Redirected URL (…?code=… or …#access_token=…)" onChange={(e) => setPasted(e.target.value)} />
            <button className="btn" disabled={!pasted.trim()} onClick={() => busy && void api.submitOAuth2Callback(busy, pasted.trim())}>
              Submit
            </button>
          </div>
        </div>
      )}
      {status && <p className={'small ' + (status.error ? 'error-text' : 'muted')}>{status.text}</p>}
    </div>
  );
}

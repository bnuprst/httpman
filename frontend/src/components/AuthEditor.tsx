import type { Auth } from '../types';
import { authParam, setAuthParam } from '../util';
import VarInput from './VarInput';

type Field = { key: string; label: string; type?: 'password' | 'select' | 'checkbox'; options?: string[]; placeholder?: string; def?: string };

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
      { key: 'accessToken', label: 'Access Token' },
      { key: 'headerPrefix', label: 'Header Prefix', placeholder: 'Bearer' },
      { key: 'addTokenTo', label: 'Add token to', type: 'select', options: ['header', 'queryParams'], def: 'header' },
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
}

export default function AuthEditor({ auth, onChange, allowInherit, inheritedFrom, collectionId }: Props) {
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
              </>
            ) : (
              'No auth is configured on any parent; the request is sent without authorization.'
            )}
          </div>
        )}
        {!def && type !== 'inherit' && <div className="info-box">Auth type “{type}” is not supported by httpman yet. Its settings are preserved in the collection.</div>}
        {def?.fields.map((f) => {
          const v = authParam(auth, f.key);
          const set = (val: string) => onChange(setAuthParam(auth as Auth, f.key, val));
          return (
            <div className="form-row" key={f.key}>
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
                <input type="checkbox" checked={(v || f.def) === 'true'} onChange={(e) => set(e.target.checked ? 'true' : 'false')} />
              ) : (
                <VarInput value={v} type={f.type === 'password' ? 'password' : undefined} placeholder={f.placeholder || f.label} collectionId={collectionId} onChange={set} />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

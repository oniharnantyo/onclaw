import { useEffect, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Segmented } from "../../components/ui/Segmented";
import { inputCls, labelCls } from "../../components/ui/constants";
import {
  api,
  ApiError,
  formatApiError,
  type ApiWorkspaceStorageConfig,
  type WorkspaceStoragePayload,
} from "../../lib/api";

export interface StoragePaneProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
}

type StorageDriver = 'local' | 's3';

/** Transient inline banner (K5/K6): test-connection outcomes and a probe-failed
 * save; any field edit clears it — matching the Providers pane's verify result. */
interface StorageBanner {
  kind: 'success' | 'error';
  text: string;
}

interface StorageFieldProps {
  id: string;
  label: string;
  value: string;
  placeholder?: string;
  type?: 'text' | 'password';
  disabled?: boolean;
  onChange: (value: string) => void;
}

function StorageField({ id, label, value, placeholder, type = 'text', disabled, onChange }: StorageFieldProps) {
  return (
    <div>
      <label className={labelCls} htmlFor={id}>
        {label} <span className="text-danger">*</span>
      </label>
      <input
        id={id}
        type={type}
        className={inputCls}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  );
}

export function StoragePane({ tenant, onToast }: StoragePaneProps) {
  const targetWsId = tenant.sub || tenant.id;

  const [loading, setLoading] = useState(true);
  /** 403 on GET (Members): the locked backend behavior renders the pane read-only. */
  const [readOnly, setReadOnly] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  /** The ACTIVE backend — last GET or successful PUT. The header badge always
   * reflects this, never the form's in-edit driver selection (K6). */
  const [config, setConfig] = useState<ApiWorkspaceStorageConfig | null>(null);
  const [driver, setDriver] = useState<StorageDriver>('local');
  const [endpoint, setEndpoint] = useState('');
  const [region, setRegion] = useState('');
  const [bucket, setBucket] = useState('');
  const [accessKey, setAccessKey] = useState('');
  /** Write-only secret: '' until typed; an untouched field saves '' (server
   * keeps the stored secret — K3). */
  const [secret, setSecret] = useState('');
  const [secretTouched, setSecretTouched] = useState(false);
  const [usePathStyle, setUsePathStyle] = useState(false);
  const [probing, setProbing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [banner, setBanner] = useState<StorageBanner | null>(null);

  const applyConfig = (next: ApiWorkspaceStorageConfig) => {
    setConfig(next);
    setDriver(next.driver);
    if (next.driver === 's3') {
      setEndpoint(next.endpoint ?? '');
      setRegion(next.region ?? '');
      setBucket(next.bucket ?? '');
      setAccessKey(next.access_key_id ?? '');
      setUsePathStyle(!!next.use_path_style);
    } else {
      setEndpoint('');
      setRegion('');
      setBucket('');
      setAccessKey('');
      setUsePathStyle(false);
    }
    setSecret('');
    setSecretTouched(false);
  };

  const loadConfig = async () => {
    setLoading(true);
    setLoadError(null);
    setReadOnly(false);
    try {
      applyConfig(await api.storage.get(targetWsId));
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 403) {
        setReadOnly(true);
      } else if (!(err instanceof ApiError && err.status === 0)) {
        setLoadError(formatApiError(err, "Couldn't load the storage configuration"));
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadConfig();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [targetWsId]);

  /** Every field edit clears the transient banner (K5/K6). */
  const edit = <T,>(setter: (value: T) => void) => (value: T) => {
    setBanner(null);
    setter(value);
  };

  const s3Complete =
    [endpoint, region, bucket, accessKey].every((v) => v.trim() !== '') &&
    (secretTouched ? secret.trim() !== '' : !!config?.secret_hint);

  const activeDriver = config?.driver ?? 'local';
  const switchingBackToLocal = driver === 'local' && activeDriver === 's3';
  const showSave = driver === 's3' || switchingBackToLocal;
  const saveDisabled = driver === 's3' && !s3Complete;
  const busy = probing || saving;

  const buildPayload = (): WorkspaceStoragePayload => {
    if (driver === 'local') return { driver: 'local' };
    return {
      driver: 's3',
      endpoint: endpoint.trim(),
      region: region.trim(),
      bucket: bucket.trim(),
      access_key_id: accessKey.trim(),
      secret_access_key: secretTouched ? secret : '',
      use_path_style: usePathStyle,
    };
  };

  const handleTest = async () => {
    setBanner(null);
    setProbing(true);
    try {
      await api.storage.probe(targetWsId, buildPayload());
      setBanner({ kind: 'success', text: 'Bucket reachable — credentials verified' });
    } catch (err: unknown) {
      const message =
        err instanceof ApiError && err.status === 422
          ? err.message
          : formatApiError(err, "Couldn't test the connection");
      setBanner({ kind: 'error', text: message });
    } finally {
      setProbing(false);
    }
  };

  const handleSave = async () => {
    setBanner(null);
    setSaving(true);
    try {
      const next = await api.storage.update(targetWsId, buildPayload());
      applyConfig(next);
      onToast('Storage configuration saved');
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 422) {
        // K6: the probe failure reason verbatim; the active backend is untouched.
        setBanner({ kind: 'error', text: `Couldn't reach the bucket: ${err.message}` });
      } else {
        onToast(formatApiError(err, 'Failed to save the storage configuration'), 'danger');
      }
    } finally {
      setSaving(false);
    }
  };

  const localNote = switchingBackToLocal
    ? 'New attachments will be stored locally. Attachments already in bucket storage stay readable.'
    : "Uploads are stored in this server's data directory. No configuration needed.";

  return (
    <div className="max-w-xl" data-od-id="pane-storage" data-testid="pane-storage">
      <div className="mb-5 flex items-start justify-between gap-3">
        <p className="text-[13px] text-muted">Where workspace uploads are stored.</p>
        {!readOnly && !loading && (
          <span
            data-testid="storage-active-badge"
            className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-line bg-surface px-2.5 py-1 text-[11px] font-medium text-fg2"
          >
            <span
              className={cx(
                'h-1.5 w-1.5 rounded-full',
                activeDriver === 's3'
                  ? 'bg-accent'
                  : 'bg-[color-mix(in_oklab,var(--success),black_15%)]'
              )}
            />
            {activeDriver === 's3' ? 'S3' : 'Local'}
          </span>
        )}
      </div>

      {loading ? (
        <div className="py-8 text-center text-[13px] text-muted">Loading storage configuration…</div>
      ) : readOnly ? (
        // K9: Members get 403 on every storage route — read-only pane.
        <div className="space-y-5 opacity-70">
          <div>
            <span className={labelCls}>Storage driver</span>
            <fieldset disabled data-testid="storage-driver">
              <Segmented
                value={undefined}
                onChange={() => {}}
                options={[
                  { id: 'local', label: 'Local', testid: 'storage-driver-local' },
                  { id: 's3', label: 'S3-compatible', testid: 'storage-driver-s3' },
                ]}
              />
            </fieldset>
          </div>
          <div
            className="flex items-start gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-3 py-2.5 text-[12px] text-muted"
            data-testid="storage-readonly-note"
          >
            <Icon name="lock" size={14} className="mt-0.5 shrink-0" />
            <span>Only Owners and Admins can manage storage.</span>
          </div>
        </div>
      ) : loadError ? (
        <div className="py-8 text-center">
          <p className="text-[14px] font-medium text-fg">Couldn't load storage configuration</p>
          <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">{loadError}</p>
          <button
            type="button"
            data-testid="btn-storage-retry"
            onClick={loadConfig}
            className="mt-3 inline-flex h-8 items-center rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Retry
          </button>
        </div>
      ) : (
        <div className="space-y-5">
          <div>
            <span className={labelCls}>Storage driver</span>
            <div data-testid="storage-driver">
              <Segmented
                value={driver}
                onChange={edit<StorageDriver>(setDriver)}
                options={[
                  { id: 'local', label: 'Local', testid: 'storage-driver-local' },
                  { id: 's3', label: 'S3-compatible', testid: 'storage-driver-s3' },
                ]}
              />
            </div>
          </div>

          {driver === 'local' ? (
            // K1 / K8: Local needs no fields — one explanatory note.
            <div
              className="flex items-start gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-3 py-2.5 text-[12px] text-muted"
              data-testid="storage-local-note"
            >
              <Icon name="help" size={14} className="mt-0.5 shrink-0" />
              <span>{localNote}</span>
            </div>
          ) : (
            <>
              <StorageField
                id="storage-endpoint"
                label="Endpoint URL"
                placeholder="https://"
                value={endpoint}
                disabled={busy}
                onChange={edit(setEndpoint)}
              />
              <StorageField
                id="storage-region"
                label="Region"
                value={region}
                disabled={busy}
                onChange={edit(setRegion)}
              />
              <StorageField
                id="storage-bucket"
                label="Bucket"
                value={bucket}
                disabled={busy}
                onChange={edit(setBucket)}
              />
              <StorageField
                id="storage-access-key-id"
                label="Access key id"
                value={accessKey}
                disabled={busy}
                onChange={edit(setAccessKey)}
              />
              <StorageField
                id="storage-secret"
                label="Secret access key"
                type="password"
                value={secret}
                placeholder={
                  config?.secret_hint
                    ? `Stored ••••${config.secret_hint} — leave as-is to keep`
                    : undefined
                }
                disabled={busy}
                onChange={(value) => {
                  setBanner(null);
                  setSecret(value);
                  setSecretTouched(true);
                }}
              />
              <label
                htmlFor="storage-path-style"
                className="flex cursor-pointer items-center gap-2 text-[13px] text-fg2"
              >
                <input
                  id="storage-path-style"
                  type="checkbox"
                  className="h-4 w-4 rounded border-line accent-[var(--accent)]"
                  checked={usePathStyle}
                  disabled={busy}
                  onChange={(e) => {
                    setBanner(null);
                    setUsePathStyle(e.target.checked);
                  }}
                />
                Path style — for MinIO, R2, or on-prem endpoints
              </label>
            </>
          )}

          {banner && (
            <div
              data-testid="storage-banner"
              role="status"
              className={cx(
                'flex items-start gap-2 rounded-md px-3 py-2 text-[12px] font-medium',
                banner.kind === 'success'
                  ? 'bg-[color-mix(in_oklab,var(--success)_12%,transparent)] text-[color-mix(in_oklab,var(--success),black_20%)]'
                  : 'bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] text-danger'
              )}
            >
              <Icon
                name={banner.kind === 'success' ? 'check' : 'alert'}
                size={14}
                className="mt-0.5 shrink-0"
              />
              <span>{banner.text}</span>
            </div>
          )}

          {showSave && (
            <div className="flex items-center justify-between gap-3">
              {driver === 's3' ? (
                <button
                  type="button"
                  data-od-id="btn-storage-test"
                  data-testid="btn-storage-test"
                  disabled={busy || saveDisabled}
                  onClick={handleTest}
                  className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50 disabled:hover:border-line"
                >
                  {probing ? 'Testing…' : 'Test connection'}
                </button>
              ) : (
                <span />
              )}
              <button
                type="button"
                data-od-id="btn-storage-save"
                data-testid="btn-storage-save"
                disabled={busy || saveDisabled}
                onClick={handleSave}
                className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
              >
                {saving ? 'Saving…' : 'Save configuration'}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

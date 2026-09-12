import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { StoragePane } from './StoragePane';
import { api, ApiError, type ApiWorkspaceStorageConfig } from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const storedS3: ApiWorkspaceStorageConfig = {
  driver: 's3',
  endpoint: 'https://s3.us-east-1.amazonaws.com',
  region: 'us-east-1',
  bucket: 'acme-attachments',
  access_key_id: 'AKIAIOSFODNN7EXAMPLE',
  use_path_style: true,
  secret_hint: 'abcd',
  updated_at: '2026-09-01T00:00:00Z',
};

/** Fills the fresh s3 form (K2) including the secret. */
function fillS3Form(overrides: Record<string, string> = {}) {
  const values = {
    endpoint: 'https://minio.internal:9000',
    region: 'us-east-1',
    bucket: 'acme-attachments',
    'access key id': 'AKIAIOSFODNN7EXAMPLE',
    'secret access key': 'top-secret',
    ...overrides,
  };
  const labelFor: Record<string, string> = {
    endpoint: 'Endpoint URL *',
    region: 'Region *',
    bucket: 'Bucket *',
    'access key id': 'Access key id *',
    'secret access key': 'Secret access key *',
  };
  for (const [key, value] of Object.entries(values)) {
    fireEvent.change(screen.getByLabelText(labelFor[key]), { target: { value } });
  }
}

describe('screens/settings/StoragePane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the local default with no save affordance (K1)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue({ driver: 'local' });

    render(<StoragePane tenant={mockTenant} onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-active-badge').textContent).toBe('Local');
    });
    expect(screen.getByTestId('storage-local-note').textContent).toContain(
      "Uploads are stored in this server's data directory. No configuration needed."
    );
    // K1: Local is the passive default — no buttons in this state.
    expect(screen.queryByTestId('btn-storage-save')).toBeNull();
    expect(screen.queryByTestId('btn-storage-test')).toBeNull();
    expect(screen.getByTestId('storage-driver-local').getAttribute('aria-checked')).toBe('true');
  });

  it('reopens stored s3 with a masked write-only secret sentinel (K3)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue(storedS3);

    render(<StoragePane tenant={mockTenant} onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-active-badge').textContent).toBe('S3');
    });
    // Non-secret fields round-trip; the secret never does.
    expect((screen.getByLabelText('Endpoint URL *') as HTMLInputElement).value).toBe(
      'https://s3.us-east-1.amazonaws.com'
    );
    expect((screen.getByLabelText('Bucket *') as HTMLInputElement).value).toBe('acme-attachments');
    expect((screen.getByLabelText('Path style — for MinIO, R2, or on-prem endpoints') as HTMLInputElement).checked).toBe(true);

    const secretInput = screen.getByLabelText('Secret access key *') as HTMLInputElement;
    expect(secretInput.type).toBe('password');
    expect(secretInput.value).toBe('');
    expect(secretInput.placeholder).toBe('Stored ••••abcd — leave as-is to keep');
  });

  it('saves an edited stored config with an empty secret (server keeps stored)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue(storedS3);
    const update = vi.spyOn(api.storage, 'update').mockResolvedValue({
      ...storedS3,
      endpoint: 'https://s3.eu-west-1.amazonaws.com',
    });

    render(<StoragePane tenant={mockTenant} onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByLabelText('Endpoint URL *')).not.toBeNull();
    });
    fireEvent.change(screen.getByLabelText('Endpoint URL *'), {
      target: { value: 'https://s3.eu-west-1.amazonaws.com' },
    });
    fireEvent.click(screen.getByTestId('btn-storage-save'));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith(
        'acme',
        expect.objectContaining({
          driver: 's3',
          endpoint: 'https://s3.eu-west-1.amazonaws.com',
          secret_access_key: '',
        })
      );
    });
  });

  it('keeps Save disabled until every required s3 field is non-empty (K2)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue({ driver: 'local' });

    render(<StoragePane tenant={mockTenant} onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-active-badge').textContent).toBe('Local');
    });
    fireEvent.click(screen.getByTestId('storage-driver-s3'));

    const save = screen.getByTestId('btn-storage-save') as HTMLButtonElement;
    expect(save.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('Endpoint URL *'), {
      target: { value: 'https://minio.internal:9000' },
    });
    fireEvent.change(screen.getByLabelText('Region *'), { target: { value: 'us-east-1' } });
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Bucket *'), { target: { value: 'acme-attachments' } });
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Access key id *'), {
      target: { value: 'AKIAIOSFODNN7EXAMPLE' },
    });
    // The secret is still empty on a fresh form — the last required field.
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Secret access key *'), {
      target: { value: 'top-secret' },
    });
    expect(save.disabled).toBe(false);
  });

  it('surfaces a probe-failed save inline with the reason verbatim and keeps the active badge (K6)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue({ driver: 'local' });
    const update = vi.spyOn(api.storage, 'update').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'The security token included in the request is invalid')
    );
    const onToast = vi.fn();

    render(<StoragePane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-active-badge').textContent).toBe('Local');
    });
    fireEvent.click(screen.getByTestId('storage-driver-s3'));
    fillS3Form();
    fireEvent.click(screen.getByTestId('btn-storage-save'));

    await waitFor(() => {
      expect(screen.getByTestId('storage-banner').textContent).toContain(
        'The security token included in the request is invalid'
      );
    });
    expect(screen.getByTestId('storage-banner').textContent).toContain("Couldn't reach the bucket:");
    // The failed save changed nothing: the badge still shows the previous driver.
    expect(screen.getByTestId('storage-active-badge').textContent).toBe('Local');
    expect(onToast).not.toHaveBeenCalled();
    // K6: Save stays enabled — fix the field and retry.
    expect((screen.getByTestId('btn-storage-save') as HTMLButtonElement).disabled).toBe(false);
    expect(update).toHaveBeenCalledTimes(1);
  });

  it('saves s3 configuration with a success toast and flips the badge (K7)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue({ driver: 'local' });
    const update = vi.spyOn(api.storage, 'update').mockResolvedValue(storedS3);
    const onToast = vi.fn();

    render(<StoragePane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-driver-s3')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('storage-driver-s3'));
    fillS3Form();
    fireEvent.click(screen.getByTestId('btn-storage-save'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Storage configuration saved');
    });
    expect(update).toHaveBeenCalledWith(
      'acme',
      expect.objectContaining({ driver: 's3', secret_access_key: 'top-secret' })
    );
    await waitFor(() => {
      expect(screen.getByTestId('storage-active-badge').textContent).toBe('S3');
    });
    // The stored secret is re-masked after the save round-trip.
    const secretInput = screen.getByLabelText('Secret access key *') as HTMLInputElement;
    expect(secretInput.value).toBe('');
    expect(secretInput.placeholder).toBe('Stored ••••abcd — leave as-is to keep');
  });

  it('shows a transient test-connection banner that clears on the next field edit (K5)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue({ driver: 'local' });
    const probe = vi.spyOn(api.storage, 'probe').mockResolvedValue({ ok: true });

    render(<StoragePane tenant={mockTenant} onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-driver-s3')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('storage-driver-s3'));
    fillS3Form();
    fireEvent.click(screen.getByTestId('btn-storage-test'));

    await waitFor(() => {
      expect(screen.getByTestId('storage-banner').textContent).toContain(
        'Bucket reachable — credentials verified'
      );
    });
    expect(probe).toHaveBeenCalledWith(
      'acme',
      expect.objectContaining({ driver: 's3', bucket: 'acme-attachments' })
    );

    // The banner is transient: the next field edit clears it.
    fireEvent.change(screen.getByLabelText('Bucket *'), { target: { value: 'acme-attachments-2' } });
    expect(screen.queryByTestId('storage-banner')).toBeNull();
  });

  it('locks both buttons and shows Testing… while a probe is in flight (K4)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue({ driver: 'local' });
    vi.spyOn(api.storage, 'probe').mockImplementation(() => new Promise(() => {}));

    render(<StoragePane tenant={mockTenant} onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-driver-s3')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('storage-driver-s3'));
    fillS3Form();
    fireEvent.click(screen.getByTestId('btn-storage-test'));

    await waitFor(() => {
      expect(screen.getByTestId('btn-storage-test').textContent).toBe('Testing…');
    });
    expect((screen.getByTestId('btn-storage-test') as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId('btn-storage-save') as HTMLButtonElement).disabled).toBe(true);
  });

  it('persists the switch back to Local probe-free with the migration note (K8)', async () => {
    vi.spyOn(api.storage, 'get').mockResolvedValue(storedS3);
    const update = vi.spyOn(api.storage, 'update').mockResolvedValue({ driver: 'local' });
    const probe = vi.spyOn(api.storage, 'probe');
    const onToast = vi.fn();

    render(<StoragePane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-active-badge').textContent).toBe('S3');
    });
    fireEvent.click(screen.getByTestId('storage-driver-local'));

    expect(screen.getByTestId('storage-local-note').textContent).toContain(
      'New attachments will be stored locally. Attachments already in bucket storage stay readable.'
    );
    expect(screen.queryByTestId('btn-storage-test')).toBeNull();

    fireEvent.click(screen.getByTestId('btn-storage-save'));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', { driver: 'local' });
      expect(onToast).toHaveBeenCalledWith('Storage configuration saved');
    });
    expect(probe).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(screen.getByTestId('storage-active-badge').textContent).toBe('Local');
    });
  });

  it('renders read-only when GET is forbidden for Members (K9)', async () => {
    vi.spyOn(api.storage, 'get').mockRejectedValue(
      new ApiError(403, 'forbidden', 'workspace settings management required')
    );
    const update = vi.spyOn(api.storage, 'update');
    const probe = vi.spyOn(api.storage, 'probe');

    render(<StoragePane tenant={mockTenant} onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('storage-readonly-note')).not.toBeNull();
    });
    expect(screen.getByText('Only Owners and Admins can manage storage.')).not.toBeNull();
    // The driver selector is disabled and no write affordance exists.
    const localOption = screen.getByTestId('storage-driver-local');
    expect(localOption.closest('fieldset')?.disabled).toBe(true);
    expect(screen.queryByTestId('btn-storage-save')).toBeNull();
    expect(screen.queryByTestId('btn-storage-test')).toBeNull();
    expect(update).not.toHaveBeenCalled();
    expect(probe).not.toHaveBeenCalled();
  });
});

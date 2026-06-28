# Onclaw Security Model

## Keyfile-Only Architecture

Onclaw uses a simplified **keyfile-only** encryption model optimized for IoT and headless devices.

### Encryption Layers

```
┌─────────────────────────────────────────────────────────────┐
│              Two-Layer Key Wrapping                          │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  master.key (KEK) → Wrapped DEK → Encrypted API Keys       │
│   [filesystem]      [database]        [database]             │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Components

1. **KEK (Key Encryption Key)**: `master.key` file (32 bytes)
   - Location: `~/.local/share/onclaw/master.key`
   - Permissions: 0600 (owner read/write only)
   - Generated: First run if missing
   - Purpose: Encrypts the Data Encryption Key (DEK)

2. **DEK (Data Encryption Key)**: Stored encrypted in database
   - Location: `preferences` table as `wrapped_dek`
   - Algorithm: AES-256-GCM
   - Purpose: Encrypts API keys

3. **API Keys**: Stored encrypted in database
   - Location: `config_secrets` table
   - Format: `base64(nonce || ciphertext || auth_tag)`
   - Key naming: `provider:<provider-name>`

### Security Properties

- ✅ **No plaintext API keys** in database
- ✅ **No plaintext encryption keys** in filesystem
- ✅ **File permissions enforced** (0600)
- ✅ **WAL mode enabled** for database integrity
- ✅ **IoT-optimized** for unattended operation

### Data Storage

```
~/.local/share/onclaw/
├── master.key          # KEK (32 bytes, 0600 permissions)
├── onclaw.db           # SQLite database
├── onclaw.db-wal       # Write-Ahead Log
└── onclaw.db-shm       # Shared memory
```

### Encryption Algorithm

- **Cipher**: AES-256-GCM (Advanced Encryption Standard with Galois/Counter Mode)
- **Key size**: 256 bits (32 bytes)
- **Nonce**: 12 bytes per encryption (cryptographically random)
- **Authentication**: GCM provides both confidentiality and integrity

### First Run Initialization

On first run, onclaw automatically:

1. Generates a new DEK (32 random bytes)
2. Generates a new KEK in `master.key` (32 random bytes)
3. Wraps the DEK using the KEK
4. Stores the wrapped DEK in the database

This process is automatic and requires no user interaction.

### Why Keyfile-Only?

Onclaw targets low-resource IoT devices (Raspberry Pi, Orange Pi) that:
- Run unattended without keyboard/monitor
- Boot automatically and start services
- Are located in secure physical environments (homes, datacenters)
- Prioritize operational simplicity over multi-factor security

For these use cases, keyfile mode provides:
- **Automatic operation** - No password prompts at startup
- **Unattended boot** - Services start automatically
- **Low complexity** - Single security model, simpler code
- **IoT-appropriate** - Designed for headless operation

### Security Best Practices

1. **File Permissions**: Always ensure 0600 permissions on `master.key`
2. **Backups**: Exclude `master.key` from unencrypted backups
3. **Disk Encryption**: Use filesystem encryption (FileVault, LUKS) for additional protection
4. **Physical Security**: Keep devices in secure locations

### Comparison with Other Systems

Unlike cloud encryption systems that support multiple key modes (keyfile, passphrase, cloud KMS), onclaw's keyfile-only approach:

- **Simpler codebase** - No mode switching logic
- **Better IoT fit** - Designed for unattended operation
- **Lower resource usage** - No password prompt handling
- **Clear security model** - Single well-defined approach

This simplification reduces code complexity by ~400 lines while maintaining appropriate security for the target use case.

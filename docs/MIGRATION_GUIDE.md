# Migration Guide: Keyfile-Only Simplification

## Overview

Onclaw has been simplified to use **keyfile-only mode** for encryption, removing the passphrase mode option. This guide helps you understand what changed and how to adapt if you were using passphrase mode.

## What Changed

### Removed Features
- ❌ `onclaw unlock` command (removed entirely)
- ❌ Passphrase mode (`key_mode: "passphrase"`)
- ❌ `SwitchToPassphrase()` method
- ❌ Password prompts at startup
- ❌ `ONCLAW_PASSPHRASE` environment variable

### Kept Features
- ✅ Keyfile mode (`master.key` in `~/.local/share/onclaw/`)
- ✅ Automatic key generation on first run
- ✅ AES-256-GCM encryption
- ✅ All existing provider commands work unchanged
- ✅ API key encryption unchanged

## Impact Assessment

### Most Users: No Action Required ✅
If you were using the default **keyfile mode** (most users), you don't need to do anything. Your setup continues to work exactly as before:

```bash
# Everything still works:
onclaw provider list
onclaw provider add my-provider
onclaw config show
```

### Former Passphrase Mode Users: Action Required ⚠️
If you were using passphrase mode, your existing database will **not be compatible** with the new keyfile-only version.

### How to Check Which Mode You Used

```bash
# Check if passphrase salt exists in database
sqlite3 ~/.local/share/onclaw/onclaw.db "SELECT value FROM preferences WHERE key = 'passphrase_salt';"

# If this returns a value (not NULL), you were using passphrase mode
# If this returns "no value", you were using keyfile mode (no action needed)
```

## Migration Options

### Option 1: Fresh Start (Recommended)

The cleanest approach is to start fresh with the new keyfile-only mode:

```bash
# 1. Export your current provider configurations (for reference)
onclaw config show > my-config-backup.txt

# 2. Note down your current providers
onclaw provider list

# 3. Delete old database and keys
rm ~/.local/share/onclaw/onclaw.db
rm ~/.local/share/onclaw/master.key

# 4. Run any onclaw command to auto-initialize
onclaw provider list

# 5. Re-add your providers
onclaw provider add nvidia --kind openai-compatible --model minimaxai/minimax-m3 --base-url https://integrate.api.nvidia.com/v1
onclaw provider login nvidia  # You'll be prompted for API key
```

### Option 2: Manual Migration (Advanced)

If you need to preserve existing encrypted API keys, you'll need to decrypt them first:

```bash
# 1. Install the OLD version that supports passphrase mode
git checkout <commit-before-simplification>
make build

# 2. Export your API keys in plaintext temporarily
onclaw provider list  # Note down your API keys

# 3. Switch to the NEW version
git checkout main
make build

# 4. Delete old database and start fresh
rm ~/.local/share/onclaw/onclaw.db

# 5. Re-add providers with your API keys
onclaw provider add <provider> ...
```

## Benefits of Keyfile-Only Mode

### Why This Change?
1. **Simplicity**: ~400 lines of complex code removed
2. **IoT-Optimized**: Perfect for headless devices (Raspberry Pi, etc.)
3. **Reliability**: No password prompts = better automation
4. **Maintenance**: Single code path is easier to maintain
5. **Clarity**: No confusion about which mode to use

### Security Considerations
- **Same encryption**: AES-256-GCM unchanged
- **Key wrapping**: Two-tier architecture preserved
- **File permissions**: 0600 enforced on `master.key`
- **Best for IoT**: Keyfile mode is the industry standard for headless devices

## Troubleshooting

### Error: "database security is not initialized"

This means your database is missing the `wrapped_dek` preference. Solution:

```bash
# Delete and re-initialize
rm ~/.local/share/onclaw/onclaw.db
onclaw provider list  # Will auto-initialize
```

### Error: "failed to unwrap DEK"

This suggests you're trying to use a passphrase-encrypted database with the new keyfile-only version. Solution: Follow Option 1 (Fresh Start) above.

## What About `master.key` Security?

The `master.key` file contains your Key Encryption Key (KEK). Security best practices:

1. **File Permissions**: Always ensure 0600 permissions
   ```bash
   ls -la ~/.local/share/onclaw/master.key
   # Should show: -rw-------
   ```

2. **Backups**: Exclude `master.key` from unencrypted backups
   ```bash
   # Backup without master.key
   tar --exclude='master.key' -czf backup.tar.gz ~/.local/share/onclaw/
   ```

3. **Disk Encryption**: Use filesystem encryption (FileVault, LUKS) for additional protection

4. **Physical Security**: Keep devices in secure locations

## Development Impact

If you're developing onclaw:

### Code Changes
- Removed `internal/cli/unlock_cmd.go` (213 lines)
- Simplified `internal/secrets/` interfaces
- Removed passphrase branching logic from `internal/cli/context.go`
- Cleaned up test files (removed passphrase tests)

### Testing
All existing tests updated to use keyfile-only mode. No passphrase mode tests remain.

## Need Help?

If you encounter issues during migration:

1. **Check your current mode**: Use the SQL query above
2. **Backup first**: Always backup `~/.local/share/onclaw/` before making changes
3. **Fresh start**: When in doubt, delete and re-initialize
4. **File permissions**: Ensure `master.key` has 0600 permissions

## Summary

- **Most users**: No action needed ✅
- **Passphrase mode users**: Fresh start required
- **All users**: Benefit from simpler, more maintainable codebase
- **Security**: Same strong AES-256-GCM encryption
- **IoT focus**: Better alignment with headless device use case

The simplification removes complexity while maintaining security and improving the IoT experience.

# ⚠️ Human-ISM appliance secrets — READ BEFORE YOU LOSE SOMETHING IRREPLACEABLE

Some of the secrets this fork generates are **irreplaceable**. If you lose them, a running Fleet
instance can **never decrypt what it already stored** — there is no reset, no recovery. Back them up
**off-machine** (a password manager, at minimum). A second copy on the same PC is not a backup.

## The crown jewel: `fleet_server_private_key`

Fleet uses this 32-byte key to encrypt **MDM enrollment secrets and every integration credential at
rest in the database**. Lose it and every one of those stored secrets is permanently unreadable —
you'd have to re-enroll every device and re-enter every integration. **Never change it on a running
instance that has stored secrets** (the old ciphertext becomes garbage). Set it once, back it up, leave
it alone. Generate a fresh one with `openssl rand -base64 32`.

## Where the secrets live (all git-ignored — see `.gitignore`)

| Secret | File | Notes |
|---|---|---|
All appliance secrets live in the git-ignored **`human/secrets/`** tree (moved there — not buried in
`group_vars` — so their criticality is obvious):

| Secret | File | Notes |
|---|---|---|
| `fleet_server_private_key` | `human/secrets/vault.yml` | irreplaceable — see above |
| `vault_mysql_password` / `vault_mysql_root_password` | `human/secrets/vault.yml` | random per-appliance; reset the DB volume to change on an existing box |
| Provider secrets (ScreenConnect API-Manager secret, etc.) | `human/secrets/vault.yml` | re-obtainable from the vendor console, but treat as secret |
| Appliance SSH keys + admin password | `C:\HyperV\<vm>-ssh\` and `C:\HyperV\<vm>-admin-credentials.txt` | out-of-repo already |

The appliance firstboot **auto-generates `human/secrets/vault.yml`** (private key + random DB passwords)
**only when it's absent**. The moment you create it by hand (e.g. to add provider creds), you own
generating and backing up the private key yourself.

## Backup rules

1. **Off-machine copy of `fleet_server_private_key`** in a password manager the day it is created.
2. A convenience copy of the whole `vault.yml` lives at `C:\HyperV\<vm>-vault-BACKUP.yml` — but that is
   still the same machine; it survives an accidental repo/file deletion, not a disk loss.
3. `human/secrets/vault.yml` is git-ignored on purpose. It lives here (not the repo root, which is the
   single most likely spot to get force-added into a commit, and not buried in `group_vars`) —
   `human/secrets/` is obvious and safe. Only `vault.yml.example` and this dir's README are tracked.

## Direction: get the key out of a flat file entirely

The plaintext-vault-per-appliance model is an interim. The durable answer (and the fork's stated
direction) is a managed secret store — **KMS / envelope encryption**, never plaintext — so the key is
replicated, access-controlled, and not one deleted file away from disaster. Until then: **back it up
off-machine.**

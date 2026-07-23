# human/secrets/ — appliance secret material

**This directory holds irreplaceable secrets. Read [`../SECRETS.md`](../SECRETS.md) before touching it.**

- `vault.yml` — the real Ansible vault (private key, DB passwords, provider API keys). **Git-ignored.**
  The appliance firstboot auto-generates it when absent; create it by hand for manual/push-mode runs
  (copy `vault.yml.example`). **Back it up off-machine** — losing `fleet_server_private_key` permanently
  locks every secret Fleet encrypts at rest.
- `vault.yml.example` — template (tracked). `cp vault.yml.example vault.yml` and fill in.

The Ansible playbook (`human/appliance/ansible/site.yml`) loads `../../secrets/vault.yml` via `vars_files`.
Only `vault.yml.example` and this README are committed; `vault.yml` and `*-BACKUP.yml` are ignored.

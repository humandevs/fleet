# Fleet instance VM — spin-up & Ansible runbook

Stand up a **self-contained** Fleet VM (Hyper-V) with Ansible. MySQL 8 + Redis run via the repo's **own
`docker-compose`** (Docker Engine is native on the Linux VM — the thing that was broken is Docker *Desktop
on Windows*, which needed WSL2); the **Fleet server runs native** (systemd binary from our fork). This is
Fleet's own dev model — compose provides the deps, you run the server against them — and it means the DB
config (GTID flags, `max_allowed_packet`, users) can't drift from what the code expects. The same compose
services drive CI (§7). To move the DB to a separate box later, drop the `deps` role and point
`fleet_mysql_address` elsewhere. Strategy/why: [../../INFRA-vm-management.md](../../INFRA-vm-management.md).

## 0. Prereqs

- **Control machine** (your workstation or a small Linux box) with Ansible: `pipx install ansible` or
  `dnf/apt install ansible`. Ansible doesn't run natively on Windows — if this host is Windows, run Ansible
  from WSL, a small Linux VM, or the DR box. (The *targets* are Linux; only the controller needs Ansible.)
- Required collections: `ansible-galaxy collection install -r requirements.yml`.
- SSH key access to the VM as a sudo user.

## 1. Create the VM (elevated PowerShell on the Hyper-V host)

```powershell
New-VM -Name fleet-prod -Generation 2 -MemoryStartupBytes 4GB `
  -NewVHDPath C:\HyperV\fleet-prod.vhdx -NewVHDSizeBytes 40GB -SwitchName "Default Switch"
Set-VMProcessor fleet-prod -Count 2
Add-VMDvdDrive -VMName fleet-prod -Path C:\isos\Rocky-9-latest-x86_64-minimal.iso
Set-VMFirmware fleet-prod -EnableSecureBoot Off
Start-VM fleet-prod
```

Install Rocky 9 (minimal), create your sudo user, enable SSH, note the IP (`ip -4 addr`). 60 GB VHDX gives
the MySQL container's volume room to grow.

## 2. Dependencies (nothing to do — the `deps` role runs the repo's compose)

Fleet is **MySQL 8.0.36+ only** (not MariaDB/Postgres). The `common` role installs Docker Engine + the
compose plugin and checks out the fork; the `deps` role runs `docker compose up -d mysql redis` from the
repo. The compose `mysql` service publishes on `127.0.0.1:3306` with a persistent volume and auto-creates
db `fleet` + user `fleet`/`insecure` + `root`/`toor` — so there's nothing to configure. (To move the DB to
a separate box later: drop the `deps` role and set `fleet_mysql_address` to the external host.)

## 3. Configure Ansible

From this directory (`human/infra/ansible/`):

```bash
# 1. Inventory: set the VM IP + SSH user
$EDITOR inventory/hosts.ini

# 2. Non-secret vars: fork URL/branch (DB creds come from docker-compose)
$EDITOR group_vars/all.yml           # fleet_repo_url, fleet_repo_branch, versions

# 3. Secrets: copy the example, fill in, encrypt
cp group_vars/vault.yml.example group_vars/vault.yml
$EDITOR group_vars/vault.yml         # fleet_server_private_key (openssl rand -base64 32)
ansible-vault encrypt group_vars/vault.yml
```

## 4. Run it

```bash
ansible -m ping fleet_prod                              # connectivity check
ansible-playbook site.yml --limit fleet_prod --ask-vault-pass
```

This installs Docker + the Go/Node toolchain, clones the fork, brings up **MySQL + Redis via
`docker compose`**, builds the fork, generates a self-signed TLS cert, writes `/etc/fleet/fleet.env`, runs
DB migrations against the compose MySQL, and starts the native `fleet` systemd service on `0.0.0.0:8080`.

Create the first admin, then verify from your workstation with the Puppeteer harness:

```bash
# on the VM (once), create the admin user:
sudo -u fleet fleet --config /dev/null fleetctl ... # or use: fleetctl setup --address https://<vm-ip>:8080

# from the host (human/verify/):
FLEET_URL=https://<vm-ip>:8080 FLEET_USER=admin@example.com FLEET_PASS='...' npm run smoke
```

Updates later: `git`-side change → `ansible-playbook site.yml --limit fleet_prod` (the git role pulls, the
build + restart handlers fire on change). Pin `go_version`/`fleet_repo_branch` in `all.yml` so prod and DR
stay identical.

## 5. Run our datastore integration tests (on the VM)

The integration harness connects as **root/toor** to the compose **`mysql_test`** service (port 3307, the
harness default — hardcoded in `server/platform/mysql/testing_utils/testing_utils.go`) and **creates its own
throwaway DBs** per package. So just bring that service up and run the tests — identical to CI:

```bash
cd /opt/fleet-src
export PATH=$PATH:/usr/local/go/bin
docker compose up -d mysql_test redis        # 3307 = harness default; no port override needed
MYSQL_TEST=1 REDIS_TEST=1 go test ./server/datastore/mysql/ \
  -run 'HostIntegrationStatus|Coverage|20260703' -count=1
# our community plugins + coverage service (no MySQL needed):
go test ./server/community/... ./server/service/ -run 'Coverage|Integration' -count=1
```

(`mysql_test` is tmpfs/ephemeral by design — fast, and separate from the persistent `mysql` service the app
uses.)

## 7. CI: the same MySQL-included setup

Our CI must stand up MySQL too (the datastore tests are only meaningful against a real MySQL 8). Two ways,
mirroring the self-contained VM:

- **GitHub Actions (recommended for our fork):** [`../ci/community-tests.yml`](../ci/community-tests.yml) is
  a ready-to-copy workflow that brings up the repo's **own `docker-compose` test MySQL + Redis**
  (`docker compose up -d mysql_test redis`) and runs the community + coverage + datastore tests with
  `MYSQL_TEST=1`. Copy it to `.github/workflows/` on the fork.
- **Ansible-in-CI:** run this same playbook against an ephemeral CI VM/runner for a truly identical full
  box (heavier; use when you need the whole environment, not just the DB).

Because the VM and CI now use the **same compose services**, there's no drift and no port-override
juggling — `mysql_test` is 3307 in both. Standing MySQL up via compose *is* the shared "MySQL setup."

## 6. DR warm spare

When the spare exists: uncomment `fleet-dr` in `inventory/hosts.ini`, add `group_vars/fleet_dr.yml` with
`fleet_service_state: stopped` and `fleet_run_migrations: false`, and run the **same** playbook with
`--limit fleet_dr`. Identical build; service idle until failover. DR data currency = MySQL replication on
the LAN box (async replica at the DR site) — see [../../INFRA-vm-management.md](../../INFRA-vm-management.md) §3.

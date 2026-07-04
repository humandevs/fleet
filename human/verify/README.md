# Fleet verification harness + single-VM dev server

Run the whole Fleet stack **natively in one Hyper-V VM** (no Docker, no WSL2 — completely self-contained
and easy to keep updated with `dnf`/`apt`). Drive it from the Windows host with **Puppeteer** (already
installed here).

- **Why native, not Docker:** one VM holds MySQL + Redis + the Fleet server; no container runtime overhead,
  and your broken WSL2/Docker-Desktop is irrelevant. Puppeteer runs on the host for fast local testing.
- **Why Rocky 9 (recommended):** Fleet requires **MySQL 8.0** (not MariaDB) and Redis 6+. Rocky/RHEL 9 ship
  both in the default repos (`dnf install mysql-server redis`). On Debian 12 the default DB is MariaDB, so
  you'd add Oracle's MySQL APT repo first — an extra step. Either works; Rocky is fewer moving parts.

## 1. Create the VM (your elevated PowerShell / Hyper-V Manager)

Gen-2 VM, ~4 GB RAM, 2 vCPU, on the **Default Switch** (gives a host-reachable NAT IP). Attach the Rocky 9
ISO. (Run this yourself — VM creation needs elevation.)

```powershell
New-VM -Name fleet-dev -Generation 2 -MemoryStartupBytes 4GB -NewVHDPath C:\HyperV\fleet-dev.vhdx -NewVHDSizeBytes 60GB -SwitchName "Default Switch"
Set-VMProcessor fleet-dev -Count 2
Add-VMDvdDrive -VMName fleet-dev -Path C:\isos\Rocky-9-latest-x86_64-minimal.iso
# Gen-2: allow booting the ISO
Set-VMFirmware fleet-dev -EnableSecureBoot Off
Start-VM fleet-dev
```

Install Rocky (minimal), set a user, enable SSH. Note the VM's IP: `ip -4 addr show` (Default Switch →
usually `172.x.x.x`). That IP is `FLEET_URL` below.

## 2. Provision the stack (inside the VM)

```bash
# --- MySQL 8.0 (Fleet's --dev defaults expect db/user/pass = fleet/fleet/insecure) ---
sudo dnf install -y mysql-server
sudo systemctl enable --now mysqld
sudo mysql <<'SQL'
CREATE DATABASE fleet;
CREATE USER 'fleet'@'localhost' IDENTIFIED BY 'insecure';
GRANT ALL PRIVILEGES ON fleet.* TO 'fleet'@'localhost';
FLUSH PRIVILEGES;
SQL

# --- Redis 6+ ---
sudo dnf install -y redis
sudo systemctl enable --now redis

# --- Build toolchain: Go 1.26, Node 20, yarn, git, make, gcc (cgo) ---
sudo dnf groupinstall -y "Development Tools"
sudo dnf install -y git make gcc
curl -LO https://go.dev/dl/go1.26.4.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.26.4.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh && source /etc/profile.d/go.sh
curl -fsSL https://rpm.nodesource.com/setup_20.x | sudo bash -
sudo dnf install -y nodejs
sudo corepack enable
```

> **Debian 12 instead?** Same, but for MySQL 8 add Oracle's repo first:
> `curl -LO https://dev.mysql.com/get/mysql-apt-config_0.8.33-1_all.deb && sudo dpkg -i mysql-apt-config*.deb && sudo apt update && sudo apt install -y mysql-server` (choose MySQL 8.0), then `apt install -y redis-server build-essential git make`. Go/Node as above.

## 3. Get our code into the VM + run Fleet

```bash
git clone https://github.com/<your-fork>/fleet.git   # or rsync the working tree over SSH for fast iteration
cd fleet && git checkout human-dev
make deps
make generate-dev            # dev JS assets — `serve --dev` needs these (not bundled)
make build                   # -> ./build/fleet + ./build/fleetctl
./build/fleet prepare db --dev          # run migrations (uses fleet/insecure/fleet @ 127.0.0.1:3306)
./build/fleet serve --dev --dev_license --server_address=0.0.0.0:8080 &
./build/fleetctl setup --email admin@example.com --password 'Fleet1234!' --org-name Dev --address https://localhost:8080
```

`--server_address=0.0.0.0:8080` makes it reachable from the Windows host. Fleet's dev cert is self-signed
(the harness ignores that). Authoritative flags: `docs/Contributing/getting-started/building-fleet.md`.

## 4. Verify from the host (here)

```bash
cd human/verify
FLEET_URL=https://<vm-ip>:8080 FLEET_USER=admin@example.com FLEET_PASS='Fleet1234!' npm run smoke
# → screenshots/01-landing.png, 02-dashboard.png, 03-hosts.png
```

Today this verifies base Fleet (login → dashboard → host list). When we build the **coverage matrix** and
**coverage filter** (RFC §5/§6), we extend `fleet-smoke.mjs` to open them and assert the cells/filters
render — this harness is how we'll actually *see* the frontend as we build it.

## Keeping it updated

`sudo dnf upgrade` in the VM; `git pull` + `make build` for Fleet. One box, one update path.

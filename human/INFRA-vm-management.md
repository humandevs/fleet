# Managing Fleet-instance VMs: creation, config, DR, and scaling

How to create and manage state on the Fleet VM(s): one prod instance now + one DR warm spare, scaling to
many per-client Fleet instances later. Companion: [verify/README.md](./verify/README.md) (the single-VM
bring-up).

## TL;DR recommendation

- **Now (1 prod + 1 DR spare): use Ansible.** Agentless (SSH), idempotent, trivial to start. One playbook
  provisions *both* boxes identically → the DR spare is guaranteed to match prod. Keep updated by re-running
  the playbook (which includes a `dnf upgrade` + `git pull && make build` step).
- **SaltStack? It works, but it's the wrong default here.** Salt's value is its **event bus / reactor and
  master→minion real-time orchestration at hundreds+ of nodes**. For 2 VMs that's pure overhead (a master,
  minions, key management), and Salt's momentum has faded since the Broadcom/VMware acquisition of its
  sponsor. Revisit Salt (or Ansible + AWX) only when you're running many instances and want central,
  event-driven control.
- **Scaling to many per-client instances: shift to immutable golden images.** Build a "Fleet node" image
  once (Packer, using the *same* Ansible playbook as its provisioner), then stamp identical VMs from it;
  inject per-instance config (org, DB creds, secrets) at first boot via cloud-init. This turns "configure
  each pet VM" into "stamp cattle," which is what a fleet-of-Fleets needs.

## DB reality check (drives everything below)

**Fleet is MySQL-only** — MySQL 8.0.36+, **not** PostgreSQL and **not** MariaDB. So every VM/image runs
**MySQL 8**, and DR replication is **MySQL replication** specifically. Redis is ephemeral (live-query /
caching) and rebuildable — it is *not* your source of truth.

## 1. VM creation — don't hand-build pets

Even for the first VM, make it reproducible so the DR spare and future instances are identical:

- **Unattended OS install:** Rocky **kickstart** (or Debian **preseed**) so the base OS is scripted, not
  clicked. Store the kickstart file in this repo.
- **First-boot config:** **cloud-init** (Rocky/Debian both support it) for users, SSH keys, hostname, and a
  hook that runs the Ansible pull — or have Ansible push from your workstation.
- **Hyper-V template:** once one VM is provisioned + generalized, export its VHDX as a **template**; new
  VMs are `New-VM` clones of it (seconds, identical). This is your on-prem "golden image" until you move to
  Packer.
- Provisioning tooling by target: on-prem Hyper-V → scripted `New-VM` / SCVMM (the Terraform Hyper-V
  provider is weak — don't rely on it); cloud/Proxmox later → Terraform + Packer.

## 2. State management — Ansible layout

One repo (`infra/` or a sibling repo), one playbook, roles per concern:

```
roles/
  common/      # base packages, users, firewalld, dnf upgrade, timezone
  mysql/       # MySQL 8 install, fleet db/user, my.cnf, (replica config on DR)
  redis/       # redis install + bind/localhost
  fleet/       # Go/Node toolchain OR pre-built binary drop, systemd unit, migrations, config
inventory/
  prod.ini     # the prod VM
  dr.ini       # the DR spare — SAME roles, group_vars differ (replica=true)
group_vars/
  all.yml      # versions (fleet, go, node, mysql), non-secret config
  vault.yml    # SECRETS — encrypted (see §4)
```

Run `ansible-playbook -i inventory/prod.ini site.yml` for prod and `-i inventory/dr.ini` for the spare.
**Same playbook, both hosts** = identical software; the only delta is `group_vars` (replica flag, DNS name).
Updates = re-run the playbook (idempotent); it upgrades packages and rebuilds/pins Fleet to the target
version. Pin the Fleet version in `group_vars/all.yml` so prod and DR never drift.

## 3. DR warm spare — it's really about MySQL

Fleet servers are near-stateless; the warm spare is mostly a **caught-up MySQL replica** + an
identically-configured (but idle) Fleet server:

1. **MySQL async replication** prod → DR (GTID-based). DR's DB stays current ("warm").
2. **Fleet** on DR: installed + configured to the *same version + migrations* (same playbook/image), but the
   systemd service is **stopped** (or running read-pointed). Because the schema is identical, no migration
   surprise on failover.
3. **Redis** on DR: fresh/empty is fine (ephemeral) — optionally replicate for a hotter spare.
4. **Failover:** promote the DR replica (`STOP REPLICA; RESET REPLICA ALL;` → primary), start Fleet on DR,
   repoint DNS/load balancer. **Rehearse this regularly** — an untested DR plan is a hope, not a plan.
5. Keep the **agents'** server URL behind a stable DNS name / LB so hosts follow the failover without
   re-enrollment.

## 4. Secrets — never plaintext in the CM layer

Our integration secrets (Bitdefender API key, Action1 client secret, ScreenConnect `AccessSecret`, Mosyle
token) are **envelope-encrypted via KMS at runtime** (human/RISK-REGISTER.md). For the config-management
layer:

- Encrypt CM secrets with **Ansible Vault** or **SOPS** (age/KMS-backed) — never commit plaintext to
  `group_vars`/pillar.
- Keep the **KMS key material off the VM image** (instance role / injected at boot), so a cloned template
  never carries usable keys.
- Per-instance secrets are injected at first boot (cloud-init/Ansible vars), not baked into the golden image
  — the image is identical across instances; only the injected config differs.

## 5. Scaling to many Fleet instances

- **Golden image + per-instance cloud-init.** Packer bakes the "Fleet node" image (same Ansible roles);
  each client instance is a clone parameterized by cloud-init (org name, DB creds, secrets, DNS). One image,
  N cattle — no per-VM hand-config.
- **Day-2 ops across the fleet:** Ansible against a dynamic inventory, or graduate to **AWX/Semaphore** (a UI
  + scheduler over Ansible) — or **Salt** if by then you genuinely want its event-driven reactor at scale.
- **Isolation:** one MySQL + Redis + Fleet **per client instance** (multi-tenant isolation, per
  human/RISK-REGISTER.md) — don't co-mingle client data in one DB. The image/playbook makes standing up an
  isolated instance cheap.

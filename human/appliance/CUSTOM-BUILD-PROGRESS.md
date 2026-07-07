# Custom (fork) build - progress & plan

Status of building **our fork** of Fleet (community plugins, coverage matrix, etc.) into a runnable artifact.

## Decision: build once as an image/binary, don't compile per-VM

The first approach - the self-provisioning appliance that compiles Fleet **from source on each VM's first
boot** - proved fragile (a long tail of environment issues, below). We're switching to the standard model:

- **Run Fleet as a container.** For now, the official `fleetdm/fleet` image via
  [`fleet-stack/`](./fleet-stack/) gives a working UI with zero building.
- **Build the fork ONCE** into a Docker image (in a controlled dev/CI environment where Fleet's own build
  tooling handles everything), push/save it, then run it via the same `fleet-stack` compose by setting
  `FLEET_IMAGE`. No per-VM compile, none of the issues below.

The VM stays a generic Docker host (Rocky/Debian + Docker); the "appliance" becomes a compose file + an image.

## From-source appliance: walls hit (all fixed in the scripts)

The self-provisioning path (`Build-FleetAppliance.ps1` + Ansible) got progressively further; each failure was
an artifact of packaging a source tarball + building headless under systemd, not of Fleet itself:

1. `tar`/`gzip` not on Rocky minimal -> installed in first-boot.
2. Anaconda halted on missing `git` package -> `%packages --ignoremissing`, git dropped (installed later).
3. **Login lockout**: admin user `fleet` collided with the Fleet **service** user (`shell=/sbin/nologin`)
   -> service user = `fleet`, admin login = `fleetadmin` (distinct accounts).
4. `make generate` runs `git clean -fx assets`, which needs a repo, but the tarball excluded `.git`
   -> recreate a repo on the VM: `git init && git add -A && git commit` (respects `.gitignore`).
5. Build ran headless under systemd with **no `$HOME`**, so Go couldn't derive `GOPATH`/`GOCACHE`
   ("neither GOMODCACHE nor GOPATH is set") -> `export HOME="${HOME:-/root}"` in the build step.
6. The tarball exclude `build`/`build/*` matched **any** `build` path component in bsdtar, dropping the real
   source package `orbit/pkg/build` ("no required module provides package .../orbit/pkg/build")
   -> removed the `build` excludes (bsdtar can't anchor to top-level only; the harmless top-level `build/`
   now ships and is rebuilt on the VM).

After #6 the from-source build should complete, but we're not relying on it - see the decision above.

## Console/ops niceties added along the way (kept, useful regardless)

- Live tar progress + timestamped logs in `Build-FleetAppliance.ps1`.
- `/etc/issue` console banner showing IP + Fleet status + management URL (no login needed).
- `fleet.service` rate-limited (`StartLimitBurst`) so a failing Fleet stops crash-looping/flooding the console.
- Hyper-V guest daemons installed early so the host can report the VM IP.
- Safe VHDX lifecycle: never overwrite; archive to `-old-N`; guarded `Reset-FleetVM.ps1` + `-PruneArchives`.

## Next steps

1. **Now:** stand up `fleet-stack/` (official image) -> live UI, confirm the platform end-to-end.
2. **Build the fork image:** use Fleet's build/Docker tooling in dev/CI to produce `yourorg/fleet:ceplus`
   (this compiles our `server/community/*`, coverage endpoint, frontend card, etc. into one image).
3. **Swap it in:** `FLEET_IMAGE=yourorg/fleet:ceplus docker compose up -d` in `fleet-stack/`.
4. Golden image / DR spare then just clone the Docker host + pull the image - no compiling.

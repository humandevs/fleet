#!/usr/bin/env bash
# Resync the Fleet source on a running appliance VM and re-run provisioning.
#
# Use when the FLEETSRC ISO snapshot is stale (the VM builds old code, or its playbook has a bug
# fixed in the current tree). Deliberately does NOT restart fleet-firstboot: that unit re-extracts
# the stale ISO over /opt/fleet-src on every run, which would clobber the sync.
#
# From the Windows host (PowerShell):
#   scp -i <key> <local fleet-src.tar.gz>      fleetadmin@<vm>:/tmp/fleet-src.tar.gz
#   scp -i <key> .\resync-and-provision.sh     fleetadmin@<vm>:/tmp/resync-and-provision.sh
#   ssh -i <key> fleetadmin@<vm> "tr -d '\r' < /tmp/resync-and-provision.sh | sudo bash"
#
# The playbook runs detached; watch /tmp/provision.log for progress.
set -euo pipefail

SRC=/opt/fleet-src
TARBALL=/tmp/fleet-src.tar.gz

[ -f "$TARBALL" ] || { echo "missing $TARBALL - scp it first" >&2; exit 1; }

# Verify the archive BEFORE touching the existing tree: a truncated/corrupt scp (disk full, aborted
# transfer) must not half-overwrite /opt/fleet-src and then get reinstalled as "successful".
echo "== verifying $TARBALL integrity =="
gzip -t "$TARBALL"

echo "== extracting $TARBALL over $SRC (keeps node_modules/build caches for an incremental build) =="
mkdir -p "$SRC"
# rm build/fleet FIRST: the build task is guarded by creates=build/fleet, so a partial extract that
# leaves the old binary in place would make the playbook skip the build and reinstall stale code. With
# it gone up front, any extract failure (set -e aborts below) can never masquerade as a good build.
rm -f "$SRC/build/fleet"
tar -xzf "$TARBALL" -C "$SRC"
rm -f "$TARBALL"

cd "$SRC"
# make generate runs 'git clean -fx assets', which needs a git repo; the ISO/tar delivery has none.
if [ ! -d .git ]; then
  echo "== git init (make generate needs a repo; takes a minute on the full tree) =="
  git init -q
  git add -A
  git -c user.email=appliance@fleet.local -c user.name=fleet-appliance commit -qm "appliance base"
fi

cd "$SRC/human/appliance/ansible"
# Idempotent; collections normally already installed by firstboot. Ignore failures (e.g. offline).
ansible-galaxy collection install -r requirements.yml >/dev/null 2>&1 || true

echo "== launching ansible-playbook (detached; log: /tmp/provision.log) =="
export HOME="${HOME:-/root}"
nohup ansible-playbook -i inventory/localhost.ini site.yml > /tmp/provision.log 2>&1 < /dev/null &
echo "KICKED_OFF pid=$!"

#!/bin/bash
# Bake step: install Ansible, clone the fork, run the appliance playbook against localhost. This does the
# heavy, slow work once (Docker + Go/Node + `make build` + pulled images) so it lives in the golden image.
# A throwaway server private key is used here only so the playbook completes; generalize.sh strips it.
set -euxo pipefail

dnf install -y ansible-core git

# Source into /opt/fleet-src (the single source location the ansible roles expect). Prefer a local archive
# uploaded by a Packer file provisioner (/tmp/fleet-src.tar.gz); otherwise git-clone the remote.
FLEET_SRC=/opt/fleet-src
if [ -f /tmp/fleet-src.tar.gz ]; then
  mkdir -p "$FLEET_SRC"; tar -xzf /tmp/fleet-src.tar.gz -C "$FLEET_SRC"
else
  git clone --branch "$BRANCH" "$REPO_URL" "$FLEET_SRC"
fi
cd "$FLEET_SRC/human/appliance/ansible"

# Throwaway key so the fleet role's template renders; replaced per-clone by the personalize service.
umask 077
echo 'fleet_server_private_key: "BAKE-PLACEHOLDER-REPLACED-ON-FIRST-BOOT"' > group_vars/vault.yml

ansible-galaxy collection install -r requirements.yml
ansible-playbook -i inventory/localhost.ini site.yml

# Prove the build once, then stop it — the clone will start Fleet after personalization.
for i in $(seq 1 60); do
  curl -fsk https://127.0.0.1:8080/healthz >/dev/null 2>&1 && { echo "bake smoke PASSED"; break; }
  sleep 5
  [ "$i" = 60 ] && { echo "bake smoke FAILED"; exit 1; }
done

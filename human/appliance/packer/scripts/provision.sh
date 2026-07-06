#!/bin/bash
# Bake step: install Ansible, clone the fork, run the appliance playbook against localhost. This does the
# heavy, slow work once (Docker + Go/Node + `make build` + pulled images) so it lives in the golden image.
# A throwaway server private key is used here only so the playbook completes; generalize.sh strips it.
set -euxo pipefail

dnf install -y ansible-core git

REPO_DIR=/opt/fleet-appliance
git clone --branch "$BRANCH" "$REPO_URL" "$REPO_DIR"
cd "$REPO_DIR/human/appliance/ansible"

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

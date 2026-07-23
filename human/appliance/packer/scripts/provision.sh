#!/bin/bash
# Bake step: install Ansible, clone the fork, run the appliance playbook against localhost. This does the
# heavy, slow work once (Docker + Go/Node + `make build` + pulled images) so it lives in the golden image.
# A throwaway server private key is used here only so the playbook completes; generalize.sh strips it.
set -euxo pipefail

# tar + gzip are not on the Rocky minimal install but are needed to extract the source archive.
dnf install -y ansible-core git tar gzip

# Source into /opt/fleet-src (the single source location the ansible roles expect). Prefer a local archive
# uploaded by a Packer file provisioner (/tmp/fleet-src.tar.gz); otherwise git-clone the remote.
FLEET_SRC=/opt/fleet-src
if [ -f /tmp/fleet-src.tar.gz ]; then
  mkdir -p "$FLEET_SRC"; tar -xzf /tmp/fleet-src.tar.gz -C "$FLEET_SRC"
else
  git clone --branch "$BRANCH" "$REPO_URL" "$FLEET_SRC"
fi
# make generate runs 'git clean -fx assets' which needs a repo; recreate one if the source lacks .git.
if [ ! -d "$FLEET_SRC/.git" ]; then
  ( cd "$FLEET_SRC" && git init -q && git add -A && \
    git -c user.email=appliance@fleet.local -c user.name=fleet-appliance commit -q -m "appliance base" )
fi

cd "$FLEET_SRC/human/appliance/ansible"

# Throwaway secrets so the fleet role's templates render; replaced per-clone by the personalize service
# (which must generate the real private key + DB passwords into human/secrets/vault.yml, same as the
# kickstart firstboot). Secrets live in the git-ignored human/secrets/ tree — see human/SECRETS.md.
umask 077
mkdir -p "$FLEET_SRC/human/secrets"
{
  echo 'fleet_server_private_key: "BAKE-PLACEHOLDER-REPLACED-ON-FIRST-BOOT"'
  echo 'vault_mysql_password: "insecure"'
  echo 'vault_mysql_root_password: "toor"'
} > "$FLEET_SRC/human/secrets/vault.yml"

ansible-galaxy collection install -r requirements.yml
ansible-playbook -i inventory/localhost.ini site.yml

# Prove the build once, then stop it — the clone will start Fleet after personalization.
for i in $(seq 1 60); do
  curl -fsk https://127.0.0.1:8080/healthz >/dev/null 2>&1 && { echo "bake smoke PASSED"; break; }
  sleep 5
  [ "$i" = 60 ] && { echo "bake smoke FAILED"; exit 1; }
done

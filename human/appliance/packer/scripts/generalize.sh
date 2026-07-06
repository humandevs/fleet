#!/bin/bash
# Generalize step: strip everything instance-specific so the image is a clean template, and install a
# first-boot "personalize" service that gives each clone its own identity + fresh DB + running Fleet.
set -euxo pipefail

FLEET_SRC=/opt/fleet-src            # where the fleet role cloned + built (compose file lives here)
FLEET_ENV=/etc/fleet/fleet.env

# 1. Stop Fleet + tear down the DB volume so clones start with a fresh, empty database.
systemctl stop fleet || true
systemctl disable fleet || true     # personalize re-enables it after the DB is ready
( cd "$FLEET_SRC" && docker compose down -v ) || true   # -v drops the mysql/redis data volumes; images stay

# 2. Remove the throwaway private key — personalize generates a unique one per clone.
sed -i '/^FLEET_SERVER_PRIVATE_KEY=/d' "$FLEET_ENV" || true
rm -f /var/lib/fleet-appliance/.provisioned

# 3. Install the first-boot personalize service.
cat > /usr/local/sbin/fleet-personalize.sh <<'PERS'
#!/bin/bash
set -euxo pipefail
exec > /var/log/fleet-personalize.log 2>&1
FLEET_SRC=/opt/fleet-src
FLEET_ENV=/etc/fleet/fleet.env

# Unique server private key for THIS clone (encrypts secrets at rest).
if ! grep -q '^FLEET_SERVER_PRIVATE_KEY=' "$FLEET_ENV"; then
  echo "FLEET_SERVER_PRIVATE_KEY=$(openssl rand -base64 32)" >> "$FLEET_ENV"
fi

# Fresh deps + schema, then start Fleet.
cd "$FLEET_SRC"
docker compose up -d mysql redis
for i in $(seq 1 30); do
  docker compose exec -T mysql mysqladmin ping --host=127.0.0.1 --user=root --password=toor --silent && break
  sleep 4
done
set -a; . "$FLEET_ENV"; set +a
/usr/local/bin/fleet prepare db --no-prompt
systemctl enable --now fleet

# Smoke: only mark done once Fleet answers.
for i in $(seq 1 60); do
  curl -fsk https://127.0.0.1:8080/healthz >/dev/null 2>&1 && { echo "personalize smoke PASSED"; exit 0; }
  sleep 5
done
echo "personalize smoke FAILED" >&2
exit 1
PERS
chmod 0755 /usr/local/sbin/fleet-personalize.sh

cat > /etc/systemd/system/fleet-personalize.service <<'UNIT'
[Unit]
Description=Fleet clone first-boot personalization
After=network-online.target docker.service
Wants=network-online.target
ConditionPathExists=!/var/lib/fleet-appliance/.provisioned

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/fleet-personalize.sh
ExecStartPost=/usr/bin/mkdir -p /var/lib/fleet-appliance
ExecStartPost=/usr/bin/touch /var/lib/fleet-appliance/.provisioned

[Install]
WantedBy=multi-user.target
UNIT
systemctl enable fleet-personalize.service

# 4. De-identify the image so clones don't share machine-id / SSH host keys / logs.
rm -f /etc/ssh/ssh_host_*        # regenerated on first boot
: > /etc/machine-id              # regenerated on first boot
userdel -r packer || true        # remove the Packer build user
rm -f /etc/sudoers.d/90-packer
dnf clean all || true
: > /var/log/fleet-personalize.log 2>/dev/null || true

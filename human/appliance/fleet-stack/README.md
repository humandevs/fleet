# Fleet stack (official image) - working UI in minutes

Runs a **pre-built** Fleet (official `fleetdm/fleet` image) + MySQL + Redis via Docker Compose. No source,
no toolchain, no compiling - the opposite of the from-source appliance. Use this to get a live Fleet UI now;
swap in your fork image once it's built (see [../CUSTOM-BUILD-PROGRESS.md](../CUSTOM-BUILD-PROGRESS.md)).

## 1. Generate secrets (.env)

All secrets live in `.env` (never committed). Generate strong random values -- compose refuses to start
without them:

```bash
cd fleet-stack
gen() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "$1"; }
cat > .env <<EOF
MYSQL_ROOT_PASSWORD=$(gen 28)
MYSQL_PASSWORD=$(gen 28)
FLEET_SERVER_PRIVATE_KEY=$(gen 32)
EOF
chmod 600 .env
```

(28-char alphanumeric DB passwords, a 32-char server key -- no weak defaults anywhere.)

## 2. Run (on the Docker host / VM)

```bash
sudo docker compose up -d
sudo docker compose logs -f fleet    # watch it come up
```

Then open **http://<vm-ip>:8080** - the first visit is the create-admin wizard (set email, password, org).

- Port 8080 must be free. If a failed native build left `fleet.service` crash-looping:
  `sudo systemctl disable --now fleet`.
- Self-contained: its own MySQL/Redis (internal only; just 8080 is published), so it won't collide with the
  dev `docker-compose` even if that's still up.

## Run the FORK instead of official

Once you've built a fork image (`yourorg/fleet:ceplus`), point the same stack at it:

```bash
FLEET_IMAGE=yourorg/fleet:ceplus docker compose up -d
```

Everything else (DB, Redis, migrations, wiring) stays identical - only the image changes.

## Manage

```bash
sudo docker compose ps
sudo docker compose logs -f fleet
sudo docker compose down          # stop (keeps the mysql-data volume)
sudo docker compose down -v       # stop + wipe the DB
```

# TEST-DEPLOY-FLEETD.md — enrolling a test endpoint, and the `--enable-scripts` gotcha

> How to deploy the **fleetd agent** to a Windows test VM against a Fleet test server, so the agent can
> install and keep-installed our RMM tools (ScreenConnect, Action1, Bitdefender). The agent is the ONE
> thing ever hand-installed — every managed tool is deployed *by* fleetd (osquery presence policy fails →
> orbit runs the vendor install script as SYSTEM). See [../PLUGINS.md](../PLUGINS.md) and the managed-app
> lifecycle framework for the deploy/keep-installed/uninstall/update model this enables.

## ⚠️ THE GOTCHA: build the package with `--enable-scripts` or the whole RMM model silently no-ops

fleetd/orbit will **not execute server-queued scripts** unless the enrollment package was built with
`--enable-scripts`. Our entire agent-driven deploy path runs through the scripts engine
(`ds.NewHostScriptExecutionRequest` → orbit runs the vendor `WindowsInstallScript` as SYSTEM). Omit the
flag and the host still enrolls and still reports coverage — but every install/reinstall/update script the
reconciler queues is **ignored with no error**. Symptom: coverage stays `not_installed`/`at_risk` forever
and nothing ever installs, with a green agent and empty logs. **Always pass `--enable-scripts`.**

Quick proof it took: after enrolling, queue a trivial script (UI → host → *Run script*, or `fleetctl`) and
confirm it runs as SYSTEM. If it stays *pending* forever, the package was built without `--enable-scripts`.

## Prereqs
- A reachable Fleet **test server** (e.g. the fleet-test appliance at `https://<server-ip>:8080`). The
  Windows VM must route to it — same Hyper-V **Default Switch** ⇒ it can (`Test-NetConnection <server-ip> -Port 8080`).
- The **build host needs `fleetctl` + Docker** — `fleetctl package --type=msi` builds the MSI inside the
  `fleetdm/wix` container. The dev box's Docker is broken (WSL2), so **build on the appliance** (it has both;
  `/opt/fleet-src/build/fleetctl` + Docker).

## 1. Make sure the server is set up (this is what creates the enroll secret)
A freshly-built appliance has **no org / admin / enroll secret** until initial setup — `enroll_secrets` is
empty and there is nothing to bake into a package. Set it up once (on the server host):

```bash
cd /opt/fleet-src
# the server address comes from the fleetctl CONTEXT, NOT a --fleet-url flag on `setup`:
./build/fleetctl config set --address https://<server-ip>:8080 --tls-skip-verify
./build/fleetctl setup --email you@human-ism.com --name You --org-name Human-ISM --password '<generated>'
```

`setup` requires `--email --name --org-name` (and a password); it generates the global enroll secret and
logs fleetctl in. Save the admin creds off-box (e.g. `C:\HyperV\fleet-test-ssh\fleet-admin-credentials.txt`).

## 2. Build the enrollment MSI (scripts enabled, insecure TLS for the self-signed cert)
```bash
# on the build host (fleetctl + Docker). Grab the global enroll secret from the Fleet UI
# (Hosts → Add hosts) or via fleetctl, into $SECRET without echoing it to a shared transcript:
./build/fleetctl package --type=msi \
  --fleet-url=https://<server-ip>:8080 \
  --enroll-secret="$SECRET" \
  --enable-scripts \        `# ← MANDATORY — see the gotcha above` \
  --insecure                `# self-signed test cert; pairs with --tls-skip-verify on the CLI`
# → produces ./fleet-osquery.msi   (first run pulls the fleetdm/wix image — a few minutes)
```

## 3. Install on the Windows VM (runs in the operator's own shell)
```powershell
# get the MSI onto the VM — scp, Copy-VMFile (PowerShell Direct), or an enhanced-session drag-drop — then, elevated:
msiexec /i C:\Windows\Temp\fleet-osquery.msi /quiet /norestart
Start-Sleep 20
Get-Service *fleet*, *orbit* | Select-Object Name, Status   # expect "Fleet osquery" = Running
```

## 4. Verify enrollment
The host appears in the Fleet UI / `fleetctl get hosts` within a minute. If it doesn't:
- `Test-NetConnection <server-ip> -Port 8080` on the VM — routing / firewall.
- Confirm the MSI's baked `--fleet-url` points at a reachable address (see the reboot note below).

## Other gotchas worth remembering
- **Build where Docker is.** No Docker → `fleetctl package --type=msi` can't run WiX → no MSI.
- **Self-signed cert:** `--insecure` on the *package* (agent) **and** `--tls-skip-verify` on `fleetctl config`
  (CLI). Skip either and enrollment/CLI fail TLS verification.
- **Default Switch IPs change on reboot.** The `--fleet-url` is baked into the MSI — if the server VM's IP
  moves, the agent can't reach it. Rebuild, or point at a stable `<name>.mshome.net` (with `--insecure` the
  cert CN doesn't need to match).
- **(Server test-runs, not deploy)** the appliance runs SELinux enforcing; running the Go test suite from
  `/opt/fleet-src` needs `sudo setenforce 0` first (it blocks the compiler reading the 350 MB bindata file).
  Unrelated to agent deploy, but the same VM — noted so it's not rediscovered.

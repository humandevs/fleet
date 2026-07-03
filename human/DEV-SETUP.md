# DEV-SETUP.md — local dev environment (Windows-first)

> How to get a working Go toolchain + dependencies for building/testing this fork. Lives in `human/` (not
> the upstream README) to stay rebasable per [FORK-STRATEGY.md](./FORK-STRATEGY.md).

## Go toolchain — via `gobrew`

Fleet pins **`go 1.26.4`** in `go.mod`, so we use a version manager to match it.

**Why `gobrew` (not `g`/`gvm`/`goenv`/`asdf`):** those are POSIX-only and need WSL. **`gobrew`**
(kevincobain2000/gobrew) runs **natively on Windows/PowerShell**, ships a standalone binary (no bootstrap
Go required), and mirrors nvm (`install` / `use` / `ls` / `ls-remote`).

### 1. Install (run in your own PowerShell)
```powershell
iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/kevincobain2000/gobrew/master/git.io.ps1'))
```
Installs `gobrew.exe` to `%USERPROFILE%\.gobrew\bin` and adds `%USERPROFILE%\.gobrew\current\bin` +
`%USERPROFILE%\.gobrew\bin` to your **User PATH**.

### 2. Activate Fleet's Go version
```powershell
cd F:\Users\Nate\GitHub\fleet
gobrew use mod        # reads go.mod -> installs & activates the pinned version
# or explicitly:  gobrew use 1.26.4
gobrew ls-remote      # if a version "isn't found", list what's actually downloadable
```
> Go 1.21+ also has **`GOTOOLCHAIN`** built in: once any recent Go is active, building inside the repo
> auto-downloads and uses the `go.mod` toolchain — version-matching is automatic per project.

### 3. Verify (open a **new** terminal so the PATH refresh takes)
```powershell
go version    # -> go version go1.26.4 ...
```

macOS/Linux: use `gobrew` (cross-platform), `g`, or `gvm`, then `gobrew use mod`.

## Dependencies — Docker

MySQL + Redis (for integration/datastore tests) run in Docker; installed here: **Docker 28.5.1**. Fleet's
pattern is **build Go natively, run the DB deps in containers.**
```
docker run -d --name fleet-mysql -e MYSQL_ROOT_PASSWORD=toor -e MYSQL_DATABASE=fleet -p 3306:3306 mysql:8.0
docker run -d --name fleet-redis -p 6379:6379 redis:7
# or, from the repo:  docker-compose up -d mysql redis   (see docker-compose.yml)
```

## Build & test
```
go build ./...                                        # compile everything

# after adding a method to fleet.Datastore or fleet.Service, regenerate mocks:
make generate-mock
#   (or the underlying command if `make` isn't available on Windows:)
go generate github.com/fleetdm/fleet/v4/server/mock github.com/fleetdm/fleet/v4/server/mock/mockresult \
            github.com/fleetdm/fleet/v4/server/service/mock github.com/fleetdm/fleet/v4/server/mdm/android/mock

go test ./server/service/ -run TestApplyIntegrationStaleness                                  # pure-logic unit test (no DB)
MYSQL_TEST=1 go test ./server/datastore/mysql/migrations/tables/ -run TestUp_20260703000000   # migration test (needs MySQL)
MYSQL_TEST=1 REDIS_TEST=1 go test ./server/service/...                                         # service integration (needs MySQL+Redis)
```
> **After adding a datastore/service interface method, always regenerate mocks and run
> `go test ./server/service/`** — uninitialized mocks crash sibling tests (CLAUDE.md). If `go generate`
> reports a missing `mockimpl`, run `make deps` first (installs the tool).

## Notes
- **WSL2 is skipped:** the repo lives on `F:\`, so WSL would force `/mnt/f` (slow 9p filesystem) or a
  re-clone (fragments the working copy). Native Windows Go + Docker-for-deps keeps everything in one place.
- `g` (stefanmaric) and `gvm`/`goenv`/`asdf` are POSIX-only — relevant only inside WSL.
- **Run installers (gobrew) in your own shell.** This fork's agent automation stays Bash-only and invokes
  `go.exe` by full path, so it never touches a `PowerShell` deny rule.
- **Invoking gobrew's Go non-interactively** (a script/shell without gobrew's PATH+env): use the **real
  distribution binary** at `%USERPROFILE%\.gobrew\versions\<ver>\go\bin\go.exe` (POSIX:
  `~/.gobrew/versions/<ver>/go/bin/go.exe`) — it self-locates GOROOT. Do **not** call
  `.gobrew\current\bin\go.exe` directly: it's a trimmed shim that errors `GOROOT is not set` without
  gobrew's environment. (Or `export GOROOT=~/.gobrew/versions/<ver>/go` and call the shim.)

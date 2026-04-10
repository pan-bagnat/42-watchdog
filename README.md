# 📡 42 Watchdog Webhook

A Golang-based HTTP server that listens to your school's access control system and posts daily attendance data for apprentices.

---

## 🛠️ Prerequisites

* Go 1.21+
* systemd (Linux)
* A 42 API token
* A 42 Chronos API token
* Access control webhook support (from your school)

---

## 📇 Architecture Overview

This project contains **two executables**:

* **watchdog-server**: The main HTTP service that listens to webhook events and manages users.
* **watchdog-client**: A CLI tool used to interact with the server (start/stop tracking, notify students, get status, etc.).

Containerized deployment now adds:

* **backend**: this Go repository packaged as the API service
* **frontend**: a React control UI
* **nginx**: an HTTP reverse proxy in front of both services

Repository layout:

* `backend/` contains the Go service, its config, and its Dockerfile
* `frontend/` contains the React app
* `nginx/` contains the reverse proxy

---

## 🚀 Setup Instructions

### 1. Clone the repo & enter the project directory

```bash
cd live-attendance
```

### 2. Configure your files

```bash
cp backend/config-default.yml backend/config.yml
cp backend/.env.example backend/.env
```

Edit:

* `backend/config.yml` for structured watchtime periods
* `backend/.env` for all runtime, secret, API, mailer, webhook, and auth values

### 3. Build locally (no install)

```bash
cd backend
make local-build
```

This will compile the binaries inside `backend/`.

### 4. Run with Docker Compose

```bash
docker compose up --build
```

This starts:

* `watchdog-db` for Watchdog persistence
* `adminer` on `http://localhost:8088` to inspect Watchdog data
* `backend` on the internal Docker network
* `frontend` on the internal Docker network
* `nginx` on `http://localhost`

When `AUTH_MODE=local`, Compose also starts:

* `auth-db` for the local auth service session store
* `auth-migrator` to initialize the auth database schema
* `auth` for local authentication

Compose files are split like this:

* `docker-compose.yml`: base stack, always used
* `docker-compose.auth-local.yml`: extra auth services for `AUTH_MODE=local`

The root `Makefile` reads `AUTH_MODE` from `.env` and automatically picks the right Compose files:

* `AUTH_MODE=local` → `docker-compose.yml` + `docker-compose.auth-local.yml`
* `AUTH_MODE=panbagnat` → `docker-compose.yml` only

Equivalent raw Docker Compose commands:

```bash
# External auth
docker compose -f docker-compose.yml up --build

# Local auth
docker compose -f docker-compose.yml -f docker-compose.auth-local.yml up --build
```

Routing:

* `/` → React frontend
* `/login` → React login page
* `/auth/*` → backend auth bridge
* `/api/auth/me` → current authenticated user
* `/api/admin/commands` → backend admin command route
* `/api/admin/students` → backend admin student routes
* `/api/student/me` → backend student self route
* `/commands` → backend `/commands`
* `/webhook/access-control` → backend webhook
* `/healthz` → backend health endpoint

### 5. Install the server as a systemd service

```bash
cd backend
make server-install
```

This will:

* Copy `watchdog-server` to `/usr/local/bin/`
* Copy your config from `backend/config.yml` to `/etc/watchdog/config.yml`
* Copy your env file from `backend/.env` to `/etc/watchdog/.env` if it exists
* Create a systemd unit at `/etc/systemd/system/watchdog.service`
* Load runtime variables from `/etc/watchdog/.env`

### 6. Start the service

```bash
cd backend
make server-start
```

### 7. View logs

```bash
cd backend
make server-logs
```

### 8. Install autocompletion (optional)

```bash
cd backend
make client-install-cmd-completion-zsh    # for zsh
make client-install-cmd-completion-bash   # for bash
```

### 9. Set up automated cron commands

```bash
cd backend
make cron-setup
```

This installs:

* `watchdog-client start` at 07:30
* `watchdog-client notify` at 19:30
* `watchdog-client stop --post-attendance` at 20:30

---

## 🔎 Interacting with the server via CLI

```bash
watchdog-client start                   # Begin listening
watchdog-client stop                    # Stop listening
watchdog-client stop --post-attendance  # Stop & post attendances
watchdog-client status                  # View current user access states
watchdog-client notify                  # Notify students with low time
```

All commands are sent by default to `http://localhost:8042/commands` — override with:

```bash
watchdog-client --url <custom-url> <command>
```

When calling a remote secured server, the client can forward auth headers:

```bash
watchdog-client \
  --url https://watchdog.example/commands \
  --authorization "Bearer <token>" \
  status
```

You can also use `--cookie` or `--session-id`, or set `WATCHDOG_AUTHORIZATION`, `WATCHDOG_COOKIE`, or `WATCHDOG_SESSION_ID`.

The React frontend now uses the same `/login` flow as the reference example and relies on browser cookies / upstream auth routing rather than manual header entry.

## ⚙️ Configuration split

The repo now uses:

* `backend/config.yml` for structured attendance settings only
* `backend/.env` for environment-specific values and secrets

Moved to `backend/.env`:

* Access-control endpoint and credentials
* 42 API v2 endpoint, credentials, campus id, apprentice projects
* 42 Chronos endpoint, credentials, and `autoPost`
* 42 CFA endpoint and credentials
* Mailer settings
* `WATCHDOG_POSTGRES_URL` for Watchdog persistent storage
* `WEBHOOK_SECRET`
* Auth mode and auth service settings

For Docker Compose:

* `backend/.env` is injected into the backend container
* `backend/config.yml` is mounted read-only into the backend container
* Watchdog persistence is stored in `watchdog-db` (PostgreSQL)
* Adminer is available on `http://localhost:8088`

## 🔐 Authentication

Admin routes and student routes are now separated.

Admin:

* `/commands` remains as a compatibility admin alias.
* `/api/admin/commands` is the container/frontend-facing admin command route.
* `/api/admin/students` and `/api/admin/students/{login}` expose admin student state/update routes.

Student:

* `/api/student/me` returns the current authenticated user's latest watchdog state.

Auth:

* Local admin requests to `localhost` / `127.0.0.1` keep working without extra auth, which preserves the VM's local automation and cron usage.
* `AUTH_MODE=local` makes Watchdog trust the local `auth` service from this compose stack at `AUTH_SERVICE_URL`, using the same `/internal/auth/user` and `/internal/auth/admin` checks as the reference example.
* `AUTH_MODE=panbagnat` makes Watchdog trust Pan Bagnat directly through `/api/v1/users/me`.
* The login page always uses the same `/auth/42/login` entrypoint. In `local` mode it proxies to the local auth service. In `panbagnat` mode it redirects to Pan Bagnat.
* Local auth mode also needs the auth service settings in `backend/.env`: `HOST_NAME`, `POSTGRES_*`, `FT_CLIENT_ID`, `FT_CLIENT_SECRET`, `FT_CALLBACK_URL`, and `ADMIN_LOGINS`.
* For plain HTTP localhost development, the bundled local auth service now uses `SESSION_COOKIE_SAME_SITE=lax` so the login cookie works without HTTPS.
* Pan Bagnat determines whether a user is admin or not.
* Access to admin routes is granted only to admins/staff. If `AUTH_ADMIN_ROLES` is set, those role ids/names are accepted as admins too in Pan Bagnat mode.
* The access-control webhook still uses its HMAC signature and is not authenticated through Panbagnat.
* `WEBHOOK_SECRET` must now be set in `backend/.env`, otherwise webhook requests are rejected.

## 🐳 Containers

Files added for the container stack:

* [backend/Dockerfile](/home/Heinz/Documents/42-watchdog/live-attendance/backend/Dockerfile)
* [docker-compose.yml](/home/Heinz/Documents/42-watchdog/live-attendance/docker-compose.yml)
* [frontend/package.json](/home/Heinz/Documents/42-watchdog/live-attendance/frontend/package.json)
* [nginx/nginx.conf](/home/Heinz/Documents/42-watchdog/live-attendance/nginx/nginx.conf)

---

## 🛠️ Makefile Targets

### 📁 Local build & clean

| Command            | Description                            |
| ------------------ | -------------------------------------- |
| `make local-build` | Build both binaries in the current dir |
| `make local-clean` | Remove local binaries                  |

### 📂 System install

| Command             | Description                             |
| ------------------- | --------------------------------------- |
| `make system-build` | Copy built binaries to `/usr/local/bin` |
| `make system-clean` | Remove binaries from `/usr/local/bin`   |

### ⚖️ Server management

| Command                 | Description                             |
| ----------------------- | --------------------------------------- |
| `make server-install`   | Install service and config              |
| `make server-start`     | Start and enable the service            |
| `make server-stop`      | Stop and disable the service            |
| `make server-restart`   | Stop, rebuild, and restart the service  |
| `make server-status`    | View current service status             |
| `make server-logs`      | View live logs                          |
| `make server-uninstall` | Remove the service & config (keep logs) |

### ⏰ Cron setup

| Command            | Description                            |
| ------------------ | -------------------------------------- |
| `make cron-setup`  | Add 3 daily client commands to crontab |
| `make cron-remove` | Remove all cron jobs for the client    |

### 🔢 Autocompletion

| Command                                     | Description             |
| ------------------------------------------- | ----------------------- |
| `make client-install-cmd-completion-zsh`    | Install zsh completion  |
| `make client-install-cmd-completion-bash`   | Install bash completion |
| `make client-uninstall-cmd-completion-zsh`  | Remove zsh completion   |
| `make client-uninstall-cmd-completion-bash` | Remove bash completion  |

### 🚮 Full cleanup

| Command      | Description                                                              |
| ------------ | ------------------------------------------------------------------------ |
| `make purge` | Prompt to delete all binaries, service, cron, autocompletion (logs stay) |

Use `make help` to list all commands grouped by category.

---

## 🔎 Notes

* `watchdog-server` logs to `/var/log/watchdog.log`
* You can adjust logging, config paths, and more from the `Makefile`

---

MIT — Made at 42 Nice by [@TheKrainBow](https://github.com/TheKrainBow)

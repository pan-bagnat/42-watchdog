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

* `backend` on the internal Docker network
* `frontend` on the internal Docker network
* `nginx` on `http://localhost:8080`

Routing:

* `/` → React frontend
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

When calling a remote secured server, the client can forward Panbagnat auth headers:

```bash
watchdog-client \
  --url https://watchdog.example/commands \
  --authorization "Bearer <token>" \
  status
```

You can also use `--cookie` or `--session-id`, or set `WATCHDOG_AUTHORIZATION`, `WATCHDOG_COOKIE`, or `WATCHDOG_SESSION_ID`.

The React frontend offers the same command flow through nginx and lets you send:

* `Authorization`
* `X-Session-Id`

It does not try to forge a raw `Cookie` header in the browser.

## ⚙️ Configuration split

The repo now uses:

* `backend/config.yml` for structured attendance settings only
* `backend/.env` for environment-specific values and secrets

Moved to `backend/.env`:

* Access-control endpoint and credentials
* 42 API v2 endpoint, credentials, campus id, apprentice projects
* 42 Chronos endpoint, credentials, and `autoPost`
* Mailer settings
* `WEBHOOK_SECRET`
* Panbagnat auth settings

For Docker Compose:

* `backend/.env` is injected into the backend container
* `backend/config.yml` is mounted read-only into the backend container

## 🔐 Remote command authentication

Admin routes and student routes are now separated.

Admin:

* `/commands` remains as a compatibility admin alias.
* `/api/admin/commands` is the container/frontend-facing admin command route.
* `/api/admin/students` and `/api/admin/students/{login}` expose admin student state/update routes.

Student:

* `/api/student/me` returns the current authenticated user's latest watchdog state.

Auth:

* Local admin requests to `localhost` / `127.0.0.1` keep working without extra auth, which preserves the VM's local automation and cron usage.
* Remote admin and student requests require `AUTH_MODE=panbagnat`.
* Watchdog forwards the incoming session/auth headers to Panbagnat and trusts its `/api/v1/users/me` response.
* Panbagnat determines whether a user is admin or not.
* Access to admin routes is granted only to Panbagnat admins/staff. If `AUTH_ADMIN_ROLES` is set, those role ids/names are accepted as admins too.
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

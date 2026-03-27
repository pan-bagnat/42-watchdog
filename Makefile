#########################################################################################
#                                       CONFIG                                          #
#########################################################################################
ENV_FILE ?= .env
ifneq (,$(wildcard $(ENV_FILE)))
	COMPOSE_ENV_FILE := --env-file $(ENV_FILE)
endif
-include .env

AUTH_MODE ?= local
AUTH_MODE := $(strip $(AUTH_MODE))
COMPOSE_FILES := docker-compose.yml
ifeq ($(AUTH_MODE),local)
	COMPOSE_FILES += docker-compose.auth-local.yml
endif
COMPOSE_DEV_FILES := $(COMPOSE_FILES)
ifneq (,$(wildcard docker-compose.override.yml))
	COMPOSE_DEV_FILES += docker-compose.override.yml
endif
DOCKER_COMPOSE_BASE := docker compose $(COMPOSE_ENV_FILE)
DOCKER_COMPOSE := $(DOCKER_COMPOSE_BASE) $(foreach file,$(COMPOSE_FILES),-f $(file))
DOCKER_COMPOSE_DEV := $(DOCKER_COMPOSE_BASE) $(foreach file,$(COMPOSE_DEV_FILES),-f $(file))
POSTGRES_URL_LOCAL = postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@auth-db:5432/auth?sslmode=disable
POSTGRES_TEST_ADMIN_URL_LOCAL = postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@auth-db:5432/postgres?sslmode=disable

#########################################################################################
#                                                                                       #
#                      DO NOT CHANGE ANYTHING AFTER THIS BLOCK                          #
#                                                                                       #
#########################################################################################
#                                         HELP                                          #
#########################################################################################
.PHONY: help
help:																					## Help | I am pretty sure you know what this one is doing!
	@printf "\033[1;34m📦 Makefile commands:\033[0m\n"
	@grep -hE '^[a-zA-Z0-9_-]+:.*?##[A-Za-z0-9 _-]+\|.*$$' $(MAKEFILE_LIST) \
	| awk 'BEGIN {FS = ":.*?##|\\|"} \
	{ gsub(/^ +| +$$/, "", $$2); \
	  if (!seen[$$2]++) order[++i] = $$2; \
	  data[$$2] = data[$$2] sprintf("      \033[36m%-36s\033[0m %s\n", $$1, $$3) } \
	END { for (j = 1; j <= i; j++) { cat = order[j]; printf "   \033[32m%s\033[0m:\n%s", cat, data[cat] } }'

#########################################################################################
#                                        LOCAL                                          #
#########################################################################################
.PHONY: local-front local-back
local-front:																			## Local | Frontend locally (dev mode)
	$(DOCKER_COMPOSE) stop frontend
	cd frontend && BROWSER=none pnpm dev --host 0.0.0.0 --port 5173

local-back:																				## Local | Stop backend docker container, and run back locally (dev mode)
	@$(DOCKER_COMPOSE) stop backend
	cd backend/srcs && AUTH_SERVICE_URL=http://localhost:8081 POSTGRES_URL=$(POSTGRES_URL_LOCAL) go run main.go

#########################################################################################
#                                      DATABASE                                         #
#########################################################################################
.PHONY: prune-database

BE_WAS_RUNNING := $(shell docker inspect -f '{{.State.Running}}' backend-1 2>/dev/null)

stop-backend-if-needed:
	@if [ "$(BE_WAS_RUNNING)" = "true" ]; then \
	  echo "⛔ Stopping backend..."; \
	  docker stop backend-1; \
	fi

restart-backend-if-needed:
	@if [ "$(BE_WAS_RUNNING)" = "true" ]; then \
	  echo "▶️  Restarting backend..."; \
	  docker start backend-1; \
	fi

prune-database:																			## Database | Reset application database (WARNING: all app data will be lost)
	@echo "⚠️  WARNING: This will reset the application database."
	@echo "⚠️  All data in the ADM database will be permanently lost."
	@echo "ℹ️  This target truncates all tables in schema 'public' (except migration metadata)."
	@printf "Continue? [yes/no]: "; \
	read ans; \
	if [ "$$ans" = "yes" ]; then \
	  stmt=$$(docker exec -i $(DATABASE_NAME) \
	    psql -v ON_ERROR_STOP=1 -At -U admin -d auth -c "SELECT \
	      'TRUNCATE TABLE ' || string_agg(format('%I.%I', schemaname, tablename), ', ') || ' RESTART IDENTITY CASCADE;' \
	      FROM pg_tables \
	      WHERE schemaname = 'public' \
	        AND tablename <> 'schema_migrations'"); \
	  if [ -n "$$stmt" ]; then \
	    docker exec -i $(DATABASE_NAME) psql -v ON_ERROR_STOP=1 -U admin -d auth -c "$$stmt"; \
	  else \
	    echo "No tables to truncate."; \
	  fi; \
	else \
	  echo "Aborted."; \
	  exit 1; \
	fi

#########################################################################################
#                                       DOCKER                                          #
#########################################################################################
.PHONY: up up-dev down down-dev build build-dev build-back build-front prune fprune
up:																						## Docker Core | Up docker containers.
	@echo "🚀 Bringing up services (AUTH_MODE=$(AUTH_MODE))…"
	$(DOCKER_COMPOSE) up -d

up-dev:																									## Docker Core | Up docker containers with overrides.
	@echo "🚀 Bringing up dev services (AUTH_MODE=$(AUTH_MODE))…"
	$(DOCKER_COMPOSE_DEV) up -d

stop:																					## Docker Core | Stop docker containers.
	$(DOCKER_COMPOSE) stop

down:																					## Docker Core | Down docker containers.
	$(DOCKER_COMPOSE) down

down-dev:																			## Docker Core | Down docker containers with overrides.
	$(DOCKER_COMPOSE_DEV) down

prune:																					## Docker Core | Delete created images
	@echo "🛑 Bringing down containers…"
	$(DOCKER_COMPOSE) down
	@echo "🗑  Pruning images…"
	docker image prune -f

build: 																					## Docker Core | Build and up docker images.
	$(DOCKER_COMPOSE) build
	@$(MAKE) -s up

build-dev:														## Docker Core | Build and up docker images with overrides.
	$(DOCKER_COMPOSE_DEV) build
	@$(MAKE) -s up-dev

build-back: 																			## Docker Core | Build and up backend docker images.
	$(DOCKER_COMPOSE) build backend
	@$(MAKE) -s up

build-front: 																			## Docker Core | Build and up frontend docker images.
	$(DOCKER_COMPOSE) build frontend
	@$(MAKE) -s up

fprune: prune																			## Docker Core | Stop all containers, volumes, and networks.
	$(DOCKER_COMPOSE) down --volumes --remove-orphans || true
	docker system prune -af --volumes || true

#########################################################################################
#                                       TESTS                                           #
#########################################################################################
.PHONY: test-backend test-backend-verbose
test-backend:																			## Tests | Start tests for backend
	@echo "🧪 Running backend tests…"
	cd backend/srcs && POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=$(POSTGRES_USER) POSTGRES_PASSWORD=$(POSTGRES_PASSWORD) POSTGRES_URL=$(POSTGRES_TEST_ADMIN_URL_LOCAL) go test -timeout 30s ./...

test-backend-verbose: 																	## Tests | Start tests for backend with verbose enabled
	@echo "🧪 Running backend tests…"
	cd backend/srcs && POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=$(POSTGRES_USER) POSTGRES_PASSWORD=$(POSTGRES_PASSWORD) POSTGRES_URL=$(POSTGRES_TEST_ADMIN_URL_LOCAL) go test -v -timeout 30s ./...

#########################################################################################
#                                       SWAGGER                                         #
#########################################################################################
HOST_NAME ?= localhost
swagger:
	cd backend/srcs && \
	swag init -g main.go --parseInternal && \
	sed -i 's/{{HOST_PLACEHOLDER}}/$(HOST_NAME)/' ./docs/docs.go && \
	sed -i 's/{{HOST_PLACEHOLDER}}/$(HOST_NAME)/' ./docs/swagger.json && \
	sed -i 's/{{HOST_PLACEHOLDER}}/$(HOST_NAME)/' ./docs/swagger.yaml

#########################################################################################
#                                      MIGRATIONS                                       #
#########################################################################################
# Config (override if needed)
ENV_FILE        ?= .env
MIGRATE_NETWORK ?= watchdog-network
MIGRATIONS_DIR  ?= db/migrations
MIGRATE_IMAGE   ?= migrate/migrate:latest
POSTGRES_URL    ?= ${POSTGRES_URL}

# Internal: docker-run wrapper; expands $$POSTGRES_URL inside the container
define _MIGRATE_RUN
bash -lc 'set -a; source $(ENV_FILE); \
docker run --rm \
  --network $(MIGRATE_NETWORK) \
  -v "$(PWD)/$(MIGRATIONS_DIR)":/migrations:ro \
  -e POSTGRES_URL \
  $(MIGRATE_IMAGE) \
  -path=/migrations \
  -database "$$POSTGRES_URL" \
  $(1)'
endef

.PHONY: migrate-up migrate-down1 migrate-steps migrate-goto migrate-version migrate-force migrate-new

migrate-up:                                  ## Migrations | Apply all pending migrations (up)
	@$(call _MIGRATE_RUN,up)

migrate-down1:                               ## Migrations | Roll back the last migration (down 1)
	@$(call _MIGRATE_RUN,down 1)

migrate-steps:                               ## Migrations | Move N steps (use N=-2 to roll back two)
	@if [ -z "$(N)" ]; then echo "Usage: make migrate-steps N=-2"; exit 2; fi
	@$(call _MIGRATE_RUN,steps $(N))

migrate-goto:                                ## Migrations | Go to version V (e.g., V=2)
	@if [ -z "$(V)" ]; then echo "Usage: make migrate-goto V=2"; exit 2; fi
	@$(call _MIGRATE_RUN,goto $(V))

migrate-version:                             ## Migrations | Show current migration version
	@$(call _MIGRATE_RUN,version)

migrate-force:                               ## Migrations | Force version to V (clears dirty), e.g., V=1
	@if [ -z "$(V)" ]; then echo "Usage: make migrate-force V=1"; exit 2; fi
	@$(call _MIGRATE_RUN,force $(V))

migrate-new:                                 ## Migrations | Create new pair: 000X_NAME.(up or down).sql (NAME=xxx)
	@if [ -z "$(NAME)" ]; then echo "Usage: make migrate-new NAME=add_users_debug_note"; exit 2; fi
	@mkdir -p "$(MIGRATIONS_DIR)"
	@last=$$(ls -1 "$(MIGRATIONS_DIR)"/*_*.up.sql 2>/dev/null | sed -E 's#.*/([0-9]{4}).*#\1#' | sort -n | tail -1); \
	v=$$([ -z "$$last" ] && printf "0001" || printf "%04d" $$(($$last+1))); \
	up="$(MIGRATIONS_DIR)/$${v}_$(NAME).up.sql"; \
	down="$(MIGRATIONS_DIR)/$${v}_$(NAME).down.sql"; \
	printf "BEGIN;\n-- TODO: write migration\nCOMMIT;\n" > "$$up"; \
	printf "BEGIN;\n-- TODO: write rollback\nCOMMIT;\n" > "$$down"; \
	echo "Created: $$up and $$down"

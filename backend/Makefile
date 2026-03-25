#########################################################################################
#                                       CONFIG                                          #
#########################################################################################
# Binaries names (system-clean and local-clean before updating)
SERVER_BIN		:= watchdog-server
CLIENT_BIN		:= watchdog-client

# Service file path
SYSTEMD_FILE	:= /etc/systemd/system/watchdog.service
INSTALL_PATH	:= /usr/local/bin
CONFIG_SRC		:= ./config.yml
CONFIG_DST		:= /etc/watchdog/config.yml
ENV_SRC			:= ./.env
ENV_DST			:= /etc/watchdog/.env
LOGFILE			:= /var/log/42watchdog/watchdog.log

#########################################################################################
#                                                                                       #
#                      DO NOT CHANGE ANYTHING AFTER THIS BLOCK                          #
#                                                                                       #
#########################################################################################

# Path to main files
SERVER_DIR := ./cmd/server
CLIENT_DIR := ./cmd/client

# Information needed for service creation
USER := $(shell whoami)
WORKDIR := $(shell pwd)

# List of go files to avoid rebuild
GOFILES := $(shell find . -type f -name '*.go')

#########################################################################################
#                                         HELP                                          #
#########################################################################################
.PHONY: help
help:																					## Help | I am pretty sure you know what this one is doing!
	@printf "\033[1;34m📦 Makefile commands:\033[0m\n"
	@grep -E '^[a-zA-Z0-9_-]+:.*?##[A-Za-z0-9 _-]+\|.*$$' $(MAKEFILE_LIST) \
	| awk 'BEGIN {FS = ":.*?##|\\|"} \
	{ gsub(/^ +| +$$/, "", $$2); \
	  if (!seen[$$2]++) order[++i] = $$2; \
	  data[$$2] = data[$$2] sprintf("      \033[36m%-36s\033[0m %s\n", $$1, $$3) } \
	END { for (j = 1; j <= i; j++) { cat = order[j]; printf "   \033[32m%s\033[0m:\n%s", cat, data[cat] } }'

#########################################################################################
#                                         LOCAL                                         #
#########################################################################################
.PHONY: local-build local-clean
local-build: $(SERVER_BIN) $(CLIENT_BIN) 												## Local | Build server and client binaries locally
local-clean:																			## Local | Clean local binaries
	rm -f $(SERVER_BIN) $(CLIENT_BIN)

$(SERVER_BIN): $(GOFILES) 																## Local | Build server binary locally
	go build -o $(SERVER_BIN) $(SERVER_DIR)
$(CLIENT_BIN): $(GOFILES)																## Local | Build client binary locally
	go build -o $(CLIENT_BIN) $(CLIENT_DIR)

#########################################################################################
#                                        SYSTEM                                         #
#########################################################################################
.PHONY: system-build system-clean
system-build: $(SERVER_BIN)	$(CLIENT_BIN)												## System | Build server and client binaries in /usr/local/bin
	sudo cp $(SERVER_BIN) $(CLIENT_BIN) $(INSTALL_PATH)
system-clean: 																			## System | Clean built binaries in /usr/local/bin
	sudo rm -f $(INSTALL_PATH)/$(SERVER_BIN) $(INSTALL_PATH)/$(CLIENT_BIN)

#########################################################################################
#                                       SERVER                                          #
#########################################################################################
.PHONY: server-start server-stop server-restart server-reload server-status server-logs\
		server-install server-uninstall
server-start: 																			## Server | Start and enable watchdog service
	sudo systemctl start watchdog.service
	sudo systemctl enable watchdog.service
server-stop: 																			## Server | Stop watchdog service
	sudo systemctl stop watchdog.service
	sudo systemctl disable watchdog.service
server-restart: 																		## Server | Restart watchdog service
	sudo systemctl stop watchdog.service
	$(MAKE) -s system-build
	sudo systemctl start watchdog.service
server-reload: 																			## Server | Reload server using watchdog.service file, without stopping the service
	sudo systemctl daemon-reload
server-status: 																			## Server | Display server service status
	systemctl status watchdog.service
server-logs: 																			## Server | Display server logs in real time
	journalctl -u watchdog.service -f
server-uninstall: 																		## Server | Delete watchdog.service, service file and config. (Keep logs)
	@echo "Stopping and disabling watchdog service..."
	-sudo systemctl stop watchdog.service
	-sudo systemctl disable watchdog.service
	-sudo rm -f $(SYSTEMD_FILE)
	-sudo rm -f $(INSTALL_PATH)/$(SERVER_BIN)
	-sudo rm -f $(CONFIG_DST)
	-sudo rm -f $(ENV_DST)
	sudo systemctl daemon-reload
	@echo "✅ Watchdog server uninstalled (logs kept at $(LOGFILE))"
server-install: $(SERVER_BIN)															## Server | Uninstall watchdog service
	sudo mkdir -p /etc/watchdog
	sudo mkdir -p $(shell dirname $(LOGFILE))
	sudo cp $(CONFIG_SRC) $(CONFIG_DST)
	@if [ -f "$(ENV_SRC)" ]; then sudo cp $(ENV_SRC) $(ENV_DST); fi
	sudo touch $(LOGFILE)
	sudo chown $(USER) $(LOGFILE)
	sudo cp $(SERVER_BIN) $(INSTALL_PATH)/$(SERVER_BIN)
	@echo "Creating systemd service file $(SYSTEMD_FILE) ..."
	@echo
	@echo "[Unit]" | sudo tee $(SYSTEMD_FILE) > /dev/null
	@echo "Description=Watchdog Server Service" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "After=network.target" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "[Service]" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "Type=simple" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "EnvironmentFile=-$(ENV_DST)" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "ExecStart=$(INSTALL_PATH)/$(SERVER_BIN) $(CONFIG_DST) $(LOGFILE)" \
	| sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "Restart=on-failure" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "User=$(USER)" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "WorkingDirectory=$(WORKDIR)" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "TimeoutStopSec=10" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "KillMode=process" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "[Install]" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@echo "WantedBy=multi-user.target" | sudo tee -a $(SYSTEMD_FILE) > /dev/null
	@cat $(SYSTEMD_FILE)
	@echo
	sudo systemctl daemon-reload
	@echo "✅ Server installed. Use 'make server-start' to start it."

#########################################################################################
#                                        CRON                                           #
#########################################################################################
.PHONY: cron-setup cron-remove
cron-setup: 			 																## Cron | Install crontab to start/notify/stop server daily
	@echo "Installing cron jobs for watchdog-client..."
	@crontab -l 2>/dev/null | grep -v 'watchdog-client' > cron.tmp || true
	@echo "30 7  * * * /usr/local/bin/watchdog-client start" >> cron.tmp
	@echo "30 19 * * * /usr/local/bin/watchdog-client notify" >> cron.tmp
	@echo "30 20 * * * /usr/local/bin/watchdog-client stop --post-attendance" >> cron.tmp
	@crontab cron.tmp
	@rm cron.tmp
	@echo "✅ Cron jobs installed:"
	@crontab -l | grep watchdog-client
cron-remove:  																			## Cron | Remove all watchdog-client-related cron jobs
	@echo "Removing watchdog-client cron jobs..."
	@crontab -l 2>/dev/null | grep -v 'watchdog-client' > cron.tmp || true 
	@crontab cron.tmp
	@rm cron.tmp
	@echo "✅ Cron jobs cleaned"

#########################################################################################
#                                 COMMAND COMPLETION                                    #
#########################################################################################
client-install-cmd-completion-zsh:														## Command completion | Install zsh completion for watchdog-client
	@echo "Installing zsh completion for watchdog-client..."
	@mkdir -p ~/.oh-my-zsh/completions
	@watchdog-client completion zsh > ~/.oh-my-zsh/completions/_watchdog-client
	@if ! grep -q "autoload -U compinit" ~/.zshrc; then \
		echo "\n# Enable zsh completion for watchdog-client" >> ~/.zshrc; \
		echo "autoload -U compinit; compinit" >> ~/.zshrc; \
		echo "🔧 Added 'autoload -U compinit; compinit' to ~/.zshrc"; \
	else \
		echo "✅ 'autoload -U compinit' already present in ~/.zshrc"; \
	fi
	@echo "✅ Zsh completion installed. Restart your shell or run: source ~/.zshrc"

client-install-cmd-completion-bash:  													## Command completion | Install bash completion for watchdog-client
	@echo "Installing bash completion for watchdog-client..."
	@mkdir -p /etc/bash_completion.d
	@watchdog-client completion bash | sudo tee /etc/bash_completion.d/watchdog-client > /dev/null
	@echo "✅ Bash completion installed. You may need to restart your shell."

client-uninstall-cmd-completion-zsh:  													## Command completion | Remove zsh completion for watchdog-client
	@echo "Removing zsh completion for watchdog-client..."
	@rm -f ~/.oh-my-zsh/completions/_watchdog-client
	@echo "✅ Zsh completion file removed."
	@echo "📝 If needed, remove 'autoload -U compinit; compinit' manually from ~/.zshrc"

client-uninstall-cmd-completion-bash:  													## Command completion | Remove bash completion for watchdog-client
	@echo "Removing bash completion for watchdog-client..."
	@sudo rm -f /etc/bash_completion.d/watchdog-client
	@echo "✅ Bash completion file removed."

#########################################################################################
#                                        PURGE                                          #
#########################################################################################
purge:																					## Purge | Remove binaries, services, autocompletion, and cron settings (logs kept)
	@printf "🚨 \033[1mThis will completely remove Watchdog components:\033[0m\n"
	@printf "  • 🗑  Local and system binaries\n"
	@printf "  • 🔧 Systemd service and its configuration\n"
	@printf "  • 🕒 Cron jobs for watchdog-client\n"
	@printf "  • 🧠 Command-line autocompletion (bash and zsh)\n"
	@printf "  • ✅ Logs will be \033[32mkept\033[0m at: $(LOGFILE)\n\n"
	@read -p "❗ Proceed with full purge? [y/N] " confirm; \
	if [ "$$confirm" = "y" ] || [ "$$confirm" = "Y" ]; then \
		sudo -k && sudo -v;\
		$(MAKE) -s system-clean local-clean client-uninstall-cmd-completion-bash \
				client-uninstall-cmd-completion-zsh cron-remove server-uninstall; \
		printf "✅ Purge complete.\n"; \
		printf "🔄 You may want to reload your terminal to refresh shell completions.\n"; \
	else \
		printf "❌ Purge cancelled.\n"; \
	fi

#########################################################################################
#                                       ANONYMIZE                                       #
#########################################################################################
.PHONY: anonymize-log
anonymize-log:                                                                                                                                                         ## Anonymize | Remove any user related data from the LOGFILE.
       @read -p "Enter username to anonymize: " TARGET; \
       sed -i "s/^\(\[[^]]*\] \[WATCHDOG\]\) .*$$TARGET.*/\1 Some anonymized events/" $(LOGFILE)

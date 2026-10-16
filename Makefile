.PHONY: build install update test clean daemon-install daemon-restart

# Build the binary locally
build:
	go build -o motorsaw ./cmd/motorsaw

# Install to ~/.local/bin (make sure it's in your PATH)
install:
	@mkdir -p ~/.local/bin
	go build -o ~/.local/bin/motorsaw ./cmd/motorsaw
	@echo "✅ Installed motorsaw to ~/.local/bin/motorsaw"
	@~/.local/bin/motorsaw migrate
	@echo "   Make sure ~/.local/bin is in your PATH"

# Update existing installation
update: install

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f motorsaw
	go clean

# Install systemd daemon (user scope)
daemon-install: install
	@mkdir -p ~/.config/systemd/user
	@cp examples/motorsawd.service ~/.config/systemd/user/
	systemctl --user daemon-reload
	systemctl --user enable motorsawd
	systemctl --user start motorsawd
	@echo "✅ Daemon installed and started"
	@echo "   Check status: systemctl --user status motorsawd"

# Restart daemon (useful after update)
daemon-restart: install
	systemctl --user restart motorsawd
	@echo "✅ Daemon restarted with new binary"
	@journalctl --user -u motorsawd -n 20 --no-pager

# Development: build and restart daemon
dev: daemon-restart

# Show version
version:
	@grep 'version = ' cmd/motorsaw/main.go | head -1 | cut -d'"' -f2

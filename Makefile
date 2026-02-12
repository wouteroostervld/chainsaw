.PHONY: build install update test clean daemon-install daemon-restart

# Build the binary locally
build:
	go build -o chainsaw ./cmd/chainsaw

# Install to ~/.local/bin (make sure it's in your PATH)
install:
	@mkdir -p ~/.local/bin
	go build -o ~/.local/bin/chainsaw ./cmd/chainsaw
	@echo "✅ Installed chainsaw to ~/.local/bin/chainsaw"
	@echo "   Make sure ~/.local/bin is in your PATH"

# Update existing installation
update: install

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f chainsaw
	go clean

# Install systemd daemon (user scope)
daemon-install: install
	@mkdir -p ~/.config/systemd/user
	@cp examples/chainsawd.service ~/.config/systemd/user/
	systemctl --user daemon-reload
	systemctl --user enable chainsawd
	systemctl --user start chainsawd
	@echo "✅ Daemon installed and started"
	@echo "   Check status: systemctl --user status chainsawd"

# Restart daemon (useful after update)
daemon-restart: install
	systemctl --user restart chainsawd
	@echo "✅ Daemon restarted with new binary"
	@journalctl --user -u chainsawd -n 20 --no-pager

# Development: build and restart daemon
dev: daemon-restart

# Show version
version:
	@grep 'version = ' cmd/chainsaw/main.go | head -1 | cut -d'"' -f2

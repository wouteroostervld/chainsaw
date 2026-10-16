#!/bin/bash
# Motorsaw Daemon Installation Script
# This script installs motorsawd as a systemd user service

set -e

BINARY_NAME="motorsawd"
SERVICE_NAME="motorsawd.service"
INSTALL_DIR="$HOME/.local/bin"
SERVICE_DIR="$HOME/.config/systemd/user"

echo "=== Motorsaw Daemon Installer ==="
echo

# Check if binary exists
if [ ! -f "$BINARY_NAME" ]; then
    echo "Error: $BINARY_NAME binary not found in current directory"
    echo "Please build it first with: go build -o motorsawd ./cmd/motorsawd"
    exit 1
fi

# Create directories if they don't exist
echo "Creating directories..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$SERVICE_DIR"
mkdir -p "$HOME/.motorsaw"

# Install binary
echo "Installing $BINARY_NAME to $INSTALL_DIR..."
cp "$BINARY_NAME" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

# Check if ~/.local/bin is in PATH
if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
    echo
    echo "Warning: $HOME/.local/bin is not in your PATH"
    echo "Add this line to your ~/.bashrc or ~/.zshrc:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo
fi

# Install systemd service
echo "Installing systemd service..."
cat > "$SERVICE_DIR/$SERVICE_NAME" << 'EOF'
[Unit]
Description=Motorsaw GraphRAG Indexing Daemon
Documentation=https://github.com/wouteroostervld/motorsaw
After=network.target

[Service]
Type=simple
ExecStart=%h/.local/bin/motorsawd
Restart=on-failure
RestartSec=5s

# Graceful shutdown
TimeoutStopSec=30s
KillMode=mixed
KillSignal=SIGTERM

# Environment
Environment=HOME=%h

# Resource limits
MemoryMax=2G
CPUQuota=80%

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%h/.motorsaw

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=motorsawd

[Install]
WantedBy=default.target
EOF

# Reload systemd
echo "Reloading systemd daemon..."
systemctl --user daemon-reload

echo
echo "=== Installation Complete! ==="
echo
echo "Next steps:"
echo "  1. Create config: mkdir -p ~/.motorsaw && vi ~/.motorsaw/config.yaml"
echo "     (See examples/global-config.yaml for reference)"
echo
echo "  2. Enable service: systemctl --user enable $SERVICE_NAME"
echo "  3. Start service:  systemctl --user start $SERVICE_NAME"
echo "  4. Check status:   systemctl --user status $SERVICE_NAME"
echo "  5. View logs:      journalctl --user -u $SERVICE_NAME -f"
echo
echo "To enable the daemon to start on boot (even when not logged in):"
echo "  sudo loginctl enable-linger $USER"
echo
echo "To uninstall:"
echo "  systemctl --user stop $SERVICE_NAME"
echo "  systemctl --user disable $SERVICE_NAME"
echo "  rm $INSTALL_DIR/$BINARY_NAME"
echo "  rm $SERVICE_DIR/$SERVICE_NAME"
echo

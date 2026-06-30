#!/bin/bash
# TLS Shunt Proxy Deployment Script for Linux
# Supports Ubuntu, Debian, CentOS, and other systemd-based distributions

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
BINARY_NAME="tls-shunt-proxy"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/tls-shunt-proxy"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
SERVICE_FILE="/etc/systemd/system/tls-shunt-proxy.service"
REPO_URL="https://gitee.com/feixion/tls-shunt-proxy.git"

# Functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root or with sudo"
        exit 1
    fi
}

check_os() {
    if [[ ! -f /etc/os-release ]]; then
        log_error "Cannot detect OS. /etc/os-release not found."
        exit 1
    fi
    source /etc/os-release
    log_info "Detected OS: $ID $VERSION"
}

install_dependencies() {
    log_info "Installing dependencies..."
    
    if command -v apt-get &> /dev/null; then
        apt-get update
        apt-get install -y git curl wget
    elif command -v yum &> /dev/null; then
        yum install -y git curl wget
    elif command -v dnf &> /dev/null; then
        dnf install -y git curl wget
    else
        log_warn "Package manager not detected. Please ensure git, curl, and wget are installed."
    fi
}

install_go() {
    if command -v go &> /dev/null; then
        log_info "Go is already installed: $(go version)"
        return
    fi
    
    log_info "Installing Go 1.22+..."
    GO_VERSION="1.22.0"
    
    # Detect architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            GO_ARCH="amd64"
            ;;
        aarch64)
            GO_ARCH="arm64"
            ;;
        *)
            log_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
    
    wget -O /tmp/go.tar.gz "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    tar -C /usr/local -xzf /tmp/go.tar.gz
    
    # Add Go to PATH
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    export PATH=$PATH:/usr/local/go/bin
    
    rm /tmp/go.tar.gz
    log_info "Go installed successfully"
}

clone_and_build() {
    log_info "Cloning TLS Shunt Proxy repository..."
    
    TEMP_DIR=$(mktemp -d)
    cd "$TEMP_DIR"
    
    git clone "$REPO_URL"
    cd tls-shunt-proxy
    
    log_info "Building TLS Shunt Proxy..."
    go build -o "$BINARY_NAME"
    
    # Install binary
    cp "$BINARY_NAME" "$INSTALL_DIR/"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    
    log_info "Binary installed to: $INSTALL_DIR/$BINARY_NAME"
    
    # Cleanup
    cd /
    rm -rf "$TEMP_DIR"
}

create_config_dir() {
    log_info "Creating configuration directory..."
    mkdir -p "$CONFIG_DIR"
}

create_systemd_service() {
    log_info "Creating systemd service..."
    
    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=TLS Shunt Proxy
After=network.target

[Service]
Type=simple
User=root
ExecStart=$INSTALL_DIR/$BINARY_NAME -config $CONFIG_FILE
Restart=on-failure
RestartSec=5s

# Security settings
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

    log_info "Service file created: $SERVICE_FILE"
}

setup_permissions() {
    log_info "Setting up permissions..."
    
    # Grant permission to bind privileged ports (80, 443)
    if command -v setcap &> /dev/null; then
        setcap "cap_net_bind_service=+ep" "$INSTALL_DIR/$BINARY_NAME"
        log_info "Granted CAP_NET_BIND_SERVICE permission"
    else
        log_warn "setcap not found. Service will need to run as root to bind ports 80/443"
    fi
}

install_config() {
    local config_path="$1"
    
    if [[ -z "$config_path" ]]; then
        log_warn "No config file provided. Creating a default config..."
        return
    fi
    
    if [[ ! -f "$config_path" ]]; then
        log_error "Config file not found: $config_path"
        exit 1
    fi
    
    log_info "Installing config file: $config_path -> $CONFIG_FILE"
    cp "$config_path" "$CONFIG_FILE"
    chmod 644 "$CONFIG_FILE"
}

enable_and_start_service() {
    log_info "Enabling and starting TLS Shunt Proxy service..."
    
    systemctl daemon-reload
    systemctl enable tls-shunt-proxy
    systemctl restart tls-shunt-proxy
    
    log_info "Service status:"
    systemctl status tls-shunt-proxy --no-pager || true
}

open_firewall() {
    log_info "Configuring firewall..."
    
    if command -v ufw &> /dev/null; then
        ufw allow 80/tcp
        ufw allow 443/tcp
        log_info "Firewall configured (ufw)"
    elif command -v firewall-cmd &> /dev/null; then
        firewall-cmd --permanent --add-port=80/tcp
        firewall-cmd --permanent --add-port=443/tcp
        firewall-cmd --reload
        log_info "Firewall configured (firewalld)"
    elif command -v iptables &> /dev/null; then
        iptables -A INPUT -p tcp --dport 80 -j ACCEPT
        iptables -A INPUT -p tcp --dport 443 -j ACCEPT
        # Save rules based on distro
        if command -v netfilter-persistent &> /dev/null; then
            netfilter-persistent save
        fi
        log_info "Firewall configured (iptables)"
    else
        log_warn "No firewall detected. Please manually open ports 80 and 443."
    fi
}

print_summary() {
    echo ""
    log_info "========================================="
    log_info "TLS Shunt Proxy Deployment Complete!"
    log_info "========================================="
    echo ""
    echo "Binary: $INSTALL_DIR/$BINARY_NAME"
    echo "Config: $CONFIG_FILE"
    echo "Service: $SERVICE_FILE"
    echo ""
    echo "Useful commands:"
    echo "  Start service:   systemctl start tls-shunt-proxy"
    echo "  Stop service:    systemctl stop tls-shunt-proxy"
    echo "  Restart service: systemctl restart tls-shunt-proxy"
    echo "  View status:     systemctl status tls-shunt-proxy"
    echo "  View logs:       journalctl -u tls-shunt-proxy -f"
    echo ""
    log_warn "Don't forget to:"
    echo "  1. Update your domain's DNS to point to this server"
    echo "  2. Ensure ports 80 and 443 are accessible from the internet"
    echo "  3. Configure your backend service to listen on 127.0.0.1:8080"
    echo ""
}

# Main deployment function
deploy() {
    local config_path="$1"
    
    log_info "Starting TLS Shunt Proxy deployment..."
    
    check_root
    check_os
    install_dependencies
    install_go
    clone_and_build
    create_config_dir
    create_systemd_service
    setup_permissions
    
    if [[ -n "$config_path" ]]; then
        install_config "$config_path"
    fi
    
    open_firewall
    enable_and_start_service
    print_summary
}

# Parse arguments
CONFIG_PATH=""
SKIP_BUILD=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --config)
            CONFIG_PATH="$2"
            shift 2
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        --help)
            echo "Usage: $0 [--config <path>] [--skip-build]"
            echo ""
            echo "Options:"
            echo "  --config <path>     Path to config file to install"
            echo "  --skip-build        Skip building from source (use existing binary)"
            echo "  --help              Show this help message"
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Run deployment
if [[ "$SKIP_BUILD" = true ]]; then
    check_root
    create_config_dir
    create_systemd_service
    setup_permissions
    if [[ -n "$CONFIG_PATH" ]]; then
        install_config "$CONFIG_PATH"
    fi
    open_firewall
    enable_and_start_service
    print_summary
else
    deploy "$CONFIG_PATH"
fi
---
name: tls-proxy-config
description: Generate TLS Shunt Proxy configuration files and deploy TLS Shunt Proxy on Linux systems for domain forwarding to 127.0.0.1:8080. Use when a user provides a domain name (e.g., abc.com) and needs to configure and deploy TLS Shunt Proxy to forward all traffic from that domain to a local backend service on port 8080. Supports both Let's Encrypt automatic certificate management and custom certificate configurations, plus full deployment automation on Linux.
---

# TLS Shunt Proxy Configuration & Deployment

## Quick Start

### Generate Configuration

Generate a TLS Shunt Proxy configuration that forwards all traffic from the specified domain to 127.0.0.1:8080.

**Basic usage with Let's Encrypt auto-cert:**
```bash
python scripts/generate_config.py abc.com --managed-cert -o config.yaml
```

**Usage with custom certificates:**
```bash
python scripts/generate_config.py abc.com --cert /path/to/cert.pem --key /path/to/key.pem -o config.yaml
```

### Deploy on Linux

Deploy TLS Shunt Proxy on a Linux system with optional configuration installation.

**Full deployment with config:**
```bash
sudo bash scripts/deploy.sh --config config.yaml
```

**Deploy only (skip build, use existing binary):**
```bash
sudo bash scripts/deploy.sh --skip-build --config config.yaml
```

## Configuration Options

### Required Arguments (generate_config.py)

- `domain`: The domain name to configure (e.g., abc.com, example.com)

### Optional Arguments (generate_config.py)

- `--listen`: Listen address (default: `0.0.0.0:443`)
- `--redirect-https`: HTTP redirect address (default: `0.0.0.0:80`)
- `--inbound-buffer`: Inbound buffer size in KB (default: `4`)
- `--outbound-buffer`: Outbound buffer size in KB (default: `32`)
- `--fallback`: Fallback address for unrecognized SNI (default: `127.0.0.1:8443`)
- `--managed-cert`: Enable Let's Encrypt automatic certificate management
- `--cert`: Custom certificate path (required if not using `--managed-cert`)
- `--key`: Custom key path (required if not using `--managed-cert`)
- `--output, -o`: Output file path (default: stdout)

### Deployment Options (deploy.sh)

- `--config <path>`: Path to config file to install
- `--skip-build`: Skip building from source (use existing binary in /usr/local/bin)
- `--help`: Show help message

## Generated Configuration Structure

The generated configuration includes:

1. **Basic settings**: Listen addresses, buffer sizes, fallback configuration
2. **Virtual host**: Configured for the specified domain with TLS offloading enabled
3. **Certificate management**: Either Let's Encrypt auto-management or custom certificates
4. **Traffic handlers**:
   - HTTP traffic → forwarded to 127.0.0.1:8080
   - HTTP/2 traffic → forwarded to 127.0.0.1:8080
   - Default/other traffic → forwarded to 127.0.0.1:8080

## Deployment Workflow

### Full Deployment (Recommended)

1. **Generate configuration**: Create config file for your domain
   ```bash
   python scripts/generate_config.py abc.com --managed-cert -o config.yaml
   ```

2. **Deploy to Linux server**: Copy files and run deployment script
   ```bash
   # Copy config to server
   scp config.yaml user@server:/tmp/
   
   # SSH to server and run deployment
   ssh user@server
   sudo bash scripts/deploy.sh --config /tmp/config.yaml
   ```

3. **Update DNS**: Point your domain's A record to the server's IP address

4. **Verify**: Check service status and test connectivity
   ```bash
   sudo systemctl status tls-shunt-proxy
   curl -I https://abc.com
   ```

### Deployment Process Details

The deployment script automatically:

1. **Checks prerequisites**: Verifies root/sudo access and OS compatibility
2. **Installs dependencies**: git, curl, wget, Go 1.22+
3. **Builds from source**: Clones repository and compiles binary
4. **Installs binary**: Copies to `/usr/local/bin/tls-shunt-proxy`
5. **Creates config directory**: `/etc/tls-shunt-proxy/`
6. **Installs config file** (if provided): Copies to `/etc/tls-shunt-proxy/config.yaml`
7. **Creates systemd service**: Sets up auto-start on boot
8. **Sets permissions**: Grants CAP_NET_BIND_SERVICE for binding ports 80/443
9. **Configures firewall**: Opens ports 80 and 443 (ufw, firewalld, or iptables)
10. **Starts service**: Enables and starts TLS Shunt Proxy

### Supported Linux Distributions

- Ubuntu 18.04+
- Debian 9+
- CentOS 7+
- RHEL 7+
- Any systemd-based distribution

## Post-Deployment Management

### Service Commands

```bash
# Start service
sudo systemctl start tls-shunt-proxy

# Stop service
sudo systemctl stop tls-shunt-proxy

# Restart service
sudo systemctl restart tls-shunt-proxy

# View status
sudo systemctl status tls-shunt-proxy

# View logs
sudo journalctl -u tls-shunt-proxy -f

# Enable auto-start on boot
sudo systemctl enable tls-shunt-proxy

# Disable auto-start
sudo systemctl disable tls-shunt-proxy
```

### Update Configuration

```bash
# Generate new config
python scripts/generate_config.py newdomain.com --managed-cert -o new_config.yaml

# Copy to server
scp new_config.yaml user@server:/tmp/

# Update on server
sudo cp /tmp/new_config.yaml /etc/tls-shunt-proxy/config.yaml
sudo systemctl restart tls-shunt-proxy
```

### Update Binary

```bash
# Re-run deployment script
sudo bash scripts/deploy.sh --config /etc/tls-shunt-proxy/config.yaml
```

## Troubleshooting

### Service fails to start

```bash
# Check logs for errors
sudo journalctl -u tls-shunt-proxy -n 50

# Common issues:
# - Port conflicts: Check if ports 80/443 are in use
sudo netstat -tlnp | grep -E ':(80|443)'

# - Permission issues: Verify binary permissions
ls -la /usr/local/bin/tls-shunt-proxy

# - Config errors: Validate YAML syntax
python -c "import yaml; yaml.safe_load(open('/etc/tls-shunt-proxy/config.yaml'))"
```

### Certificate issues (Let's Encrypt)

```bash
# Check if domain DNS is pointing to server
nslookup abc.com

# Verify ports 80 and 443 are accessible from internet
curl -I http://abc.com
curl -I https://abc.com

# Check firewall rules
sudo ufw status
sudo firewall-cmd --list-all
sudo iptables -L -n
```

### Backend service not reachable

```bash
# Verify backend service is running on 127.0.0.1:8080
curl -I http://127.0.0.1:8080

# Check if backend is listening
sudo netstat -tlnp | grep 8080
```

## Example Scenarios

**Scenario 1: Quick setup with Let's Encrypt**
```bash
# Generate config
python scripts/generate_config.py myapp.com --managed-cert -o config.yaml

# Deploy
sudo bash scripts/deploy.sh --config config.yaml
```

**Scenario 2: Custom certificates on different ports**
```bash
# Generate config
python scripts/generate_config.py api.example.com --cert /etc/ssl/cert.pem --key /etc/ssl/key.pem --listen 0.0.0.0:8443 -o config.yaml

# Deploy
sudo bash scripts/deploy.sh --config config.yaml
```

**Scenario 3: Adjusted buffer sizes for high traffic**
```bash
# Generate config
python scripts/generate_config.py high-traffic.com --managed-cert --inbound-buffer 8 --outbound-buffer 64 -o config.yaml

# Deploy
sudo bash scripts/deploy.sh --config config.yaml
```

**Scenario 4: Update existing deployment**
```bash
# Generate new config
python scripts/generate_config.py newdomain.com --managed-cert -o new_config.yaml

# Deploy with new config (skip building)
sudo bash scripts/deploy.sh --skip-build --config new_config.yaml
```

## Resources

### scripts/generate_config.py

Python script that generates TLS Shunt Proxy configuration YAML files. The script:

- Creates a complete configuration with all necessary sections
- Supports both Let's Encrypt and custom certificates
- Validates required arguments
- Outputs to file or stdout
- Follows TLS Shunt Proxy configuration schema

**Usage:**
```bash
python scripts/generate_config.py <domain> [options]
```

### scripts/deploy.sh

Bash script that automates TLS Shunt Proxy deployment on Linux systems. The script:

- Detects OS and installs dependencies (git, curl, wget, Go)
- Builds TLS Shunt Proxy from source
- Installs binary to system path
- Creates systemd service for auto-start
- Configures firewall to open ports 80/443
- Sets up proper permissions for binding privileged ports
- Installs configuration file (if provided)
- Starts and enables the service

**Requirements:**
- Linux system with systemd
- Root or sudo access
- Internet connection for downloading dependencies

**Usage:**
```bash
sudo bash scripts/deploy.sh [--config <path>] [--skip-build]
```

**Installed locations:**
- Binary: `/usr/local/bin/tls-shunt-proxy`
- Config: `/etc/tls-shunt-proxy/config.yaml`
- Service: `/etc/systemd/system/tls-shunt-proxy.service`
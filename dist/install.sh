#!/usr/bin/env bash
PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin:/usr/local/sbin:~/bin
export PATH

VSRC_ROOT='/tmp/tls-shunt-proxy'
REPO_URL="https://gitee.com/feixion/tls-shunt-proxy.git"

if command -v apt-get &> /dev/null; then
    apt-get update -qq && apt-get install -y -qq git curl wget
elif command -v yum &> /dev/null; then
    yum install -y git curl wget
fi

if ! command -v go &> /dev/null; then
    echo "安装 Go..."
    GO_VERSION="1.22.0"
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) GO_ARCH="amd64" ;;
        aarch64) GO_ARCH="arm64" ;;
        *) echo "不支持的架构: $ARCH"; exit 1 ;;
    esac
    wget -O /tmp/go.tar.gz "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    tar -C /usr/local -xzf /tmp/go.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    rm /tmp/go.tar.gz
fi

mkdir -p "${VSRC_ROOT}"
git clone --depth=1 "${REPO_URL}" "${VSRC_ROOT}/source"

cd "${VSRC_ROOT}/source"
go build -o /usr/local/bin/tls-shunt-proxy
chmod +x /usr/local/bin/tls-shunt-proxy

useradd tls-shunt-proxy -s /usr/sbin/nologin 2>/dev/null || true

mkdir -p '/etc/systemd/system'
cp "${VSRC_ROOT}/source/dist/tls-shunt-proxy.service" '/etc/systemd/system/'

if [ ! -f "/etc/tls-shunt-proxy/config.yaml" ]; then
  mkdir -p '/etc/tls-shunt-proxy'
  cp "${VSRC_ROOT}/source/config.simple.yaml" '/etc/tls-shunt-proxy/config.yaml'
fi

mkdir -p '/etc/ssl/tls-shunt-proxy'
chown -R tls-shunt-proxy:tls-shunt-proxy /etc/ssl/tls-shunt-proxy

rm -rf "${VSRC_ROOT}"

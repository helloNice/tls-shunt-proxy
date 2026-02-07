#!/usr/bin/env python3
"""
Generate TLS Shunt Proxy configuration for domain forwarding to 127.0.0.1:8080
"""

import sys
import argparse


def generate_config(domain, listen="0.0.0.0:443", redirect_https="0.0.0.0:80", 
                   inbound_buffer=4, outbound_buffer=32, fallback="127.0.0.1:8443",
                   managed_cert=False, cert_path="", key_path=""):
    """Generate TLS Shunt Proxy configuration YAML"""
    
    # Basic config
    config = f"""# TLS Shunt Proxy Configuration
# Auto-generated for domain: {domain}

# listen: 监听地址
listen: {listen}

# redirecthttps: HTTP 重定向到 HTTPS
redirecthttps: {redirect_https}

# inboundbuffersize: 入站缓冲区大小 (KB)
inboundbuffersize: {inbound_buffer}

# outboundbuffersize: 出站缓冲区大小 (KB)
outboundbuffersize: {outbound_buffer}

# 无法识别 SNI 的回落地址
fallback: {fallback}

# vhosts: 按照 TLS SNI 扩展划分为多个虚拟 host
vhosts:
  - name: {domain}

    # tlsoffloading: 解开 TLS，支持 HTTP 流量识别
    tlsoffloading: true

    # managedcert: 自动管理 LetsEncrypt 证书
    managedcert: {str(managed_cert).lower()}
"""
    
    if not managed_cert and cert_path and key_path:
        config += f"""
    # 自定义证书路径
    cert: {cert_path}
    key: {key_path}
"""
    
    config += """
    # keytype: 证书密钥类型 (ed25519, p256, p384, rsa2048, rsa4096, rsa8192)
    keytype: p256

    # alpn: ALPN 协议列表
    alpn: h2,http/1.1

    # protocols: TLS 协议版本
    protocols: tls12,tls13

    # http: HTTP 流量处理
    http:
      handler: proxyPass
      args: 127.0.0.1:8080

    # http2: HTTP/2 流量处理
    http2:
      - path: /
        handler: proxyPass
        args: 127.0.0.1:8080

    # default: 其他流量处理
    default:
      handler: proxyPass
      args: 127.0.0.1:8080
"""
    
    return config


def main():
    parser = argparse.ArgumentParser(
        description="Generate TLS Shunt Proxy configuration for domain forwarding"
    )
    parser.add_argument("domain", help="Domain name (e.g., abc.com)")
    parser.add_argument("--listen", default="0.0.0.0:443", 
                       help="Listen address (default: 0.0.0.0:443)")
    parser.add_argument("--redirect-https", default="0.0.0.0:80",
                       help="HTTP redirect address (default: 0.0.0.0:80)")
    parser.add_argument("--inbound-buffer", type=int, default=4,
                       help="Inbound buffer size in KB (default: 4)")
    parser.add_argument("--outbound-buffer", type=int, default=32,
                       help="Outbound buffer size in KB (default: 32)")
    parser.add_argument("--fallback", default="127.0.0.1:8443",
                       help="Fallback address for unrecognized SNI (default: 127.0.0.1:8443)")
    parser.add_argument("--managed-cert", action="store_true",
                       help="Enable Let's Encrypt automatic certificate management")
    parser.add_argument("--cert", help="Custom certificate path (required if not using managed cert)")
    parser.add_argument("--key", help="Custom key path (required if not using managed cert)")
    parser.add_argument("--output", "-o", help="Output file path (default: stdout)")
    
    args = parser.parse_args()
    
    # Validate cert/key if not using managed cert
    if not args.managed_cert and (not args.cert or not args.key):
        parser.error("--cert and --key are required when not using --managed-cert")
    
    # Generate configuration
    config = generate_config(
        domain=args.domain,
        listen=args.listen,
        redirect_https=args.redirect_https,
        inbound_buffer=args.inbound_buffer,
        outbound_buffer=args.outbound_buffer,
        fallback=args.fallback,
        managed_cert=args.managed_cert,
        cert_path=args.cert,
        key_path=args.key
    )
    
    # Output
    if args.output:
        with open(args.output, 'w') as f:
            f.write(config)
        print(f"Configuration written to: {args.output}")
    else:
        print(config)


if __name__ == "__main__":
    main()
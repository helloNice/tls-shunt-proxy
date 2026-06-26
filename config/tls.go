package config

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/caddyserver/certmagic"
	"github.com/go-acme/lego/v3/challenge/tlsalpn01"
	"log"
	"strings"
)

const (
	tlsDefaultMin = tls.VersionTLS12
	tlsDefaultMax = tls.VersionTLS13
)

func init() {
	certmagic.DefaultACME.Agreed = true
}

func getTlsConfig(managedCert bool, serverName, cert, key, keyType, alpn, protocols string, wildcardManager *WildcardManager) (*tls.Config, error) {
	certificateFunc, err := getCertificateFunc(managedCert, serverName, cert, key, keyType, wildcardManager)
	if err != nil {
		return nil, err
	}

	var min, max uint16
	min = tlsDefaultMin
	max = tlsDefaultMax
	if protocols != "" {
		ps := strings.Split(protocols, ",")
		min = getTlsVersion(ps[0])
		if len(ps) > 1 {
			max = getTlsVersion(ps[1])
		} else {
			max = getTlsVersion(ps[0])
		}
	}

	tlsConfig := &tls.Config{
		GetCertificate: certificateFunc,
		NextProtos:     append(strings.Split(alpn, ","), tlsalpn01.ACMETLS1Protocol),
		MinVersion:     min,
		MaxVersion:     max,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,

			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		},
	}
	return tlsConfig, nil
}

func getCertificateFunc(managedCert bool, serverName, cert, key, keyType string, wildcardManager *WildcardManager) (func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error), error) {
	var keyGenerator = certmagic.DefaultKeyGenerator
	if keyType != "" {
		keyGenerator = certmagic.StandardKeyGenerator{KeyType: certmagic.KeyType(keyType)}
	}

	// 如果 managedCert=false 且没有自定义证书，说明完全依赖通配符证书
	// 不要创建 CertMagic 实例，避免与通配符的 CertMagic 实例冲突
	if !managedCert && cert == "" && key == "" {
		log.Printf("✓ 域名 %s 完全依赖通配符证书管理器（不创建独立 CertMagic 实例）", serverName)
		return func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// 直接使用通配符证书
			if wildcardManager != nil {
				if wildcardCert := wildcardManager.GetCertificate(clientHello.ServerName); wildcardCert != nil {
					cert, err := wildcardCert.GetCertificate(clientHello)
					if err == nil && cert != nil {
						return cert, nil
					}
					// 通配符证书获取失败
					return nil, fmt.Errorf("通配符证书获取失败 %s: %v", clientHello.ServerName, err)
				}
			}
			// 没有匹配的通配符证书
			return nil, fmt.Errorf("没有可用的证书（通配符证书未匹配）")
		}, nil
	}

	// 需要创建 CertMagic 实例的情况：
	// 1. managedCert=true（自动申请证书）
	// 2. cert 和 key 都有值（加载自定义证书）
	// 注意：必须先完全初始化 config，再创建 cache 和 magic
	// 避免 GetConfigForCert 闭包捕获未完成初始化的配置指针

	config := certmagic.Config{
		Storage:   &certmagic.FileStorage{Path: "./"},
		KeySource: keyGenerator,
	}

	// 创建 cache 和 magic
	// GetConfigForCert 使用 certmagic.Default 作为 fallback，确保永远不会返回 nil
	// 解决循环依赖问题：闭包引用的 magic 变量在 certmagic.New() 执行期间可能为 nil
	var magic *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certificate certmagic.Certificate) (c *certmagic.Config, err error) {
			if magic != nil {
				return magic, nil
			}
			// fallback 到 certmagic.Default，确保永远不会返回 nil
			return &certmagic.Default, nil
		},
	})

	magic = certmagic.New(cache, config)

	if managedCert {
		err := magic.ManageAsync(context.Background(), []string{serverName})
		if err != nil {
			return nil, err
		}
	} else if cert != "" && key != "" {
		// 只有配置了自定义证书才加载文件
		_, err := magic.CacheUnmanagedCertificatePEMFile(context.TODO(), cert, key, nil)
		if err != nil {
			err = fmt.Errorf("fail to load tls key pair for %s: %v", serverName, err)
			return nil, err
		}
	}

	return func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		// 优先使用通配符证书（如果匹配）
		if wildcardManager != nil {
			if wildcardCert := wildcardManager.GetCertificate(clientHello.ServerName); wildcardCert != nil {
				cert, err := wildcardCert.GetCertificate(clientHello)
				if err == nil && cert != nil {
					return cert, nil
				}
				// 通配符证书获取失败，记录日志并回退到 vhost 自己的证书
				log.Printf("⚠️ 通配符证书获取失败 %s: %v，回退到 vhost 证书", clientHello.ServerName, err)
			}
		}
		// 回退到 vhost 自己的证书
		return magic.GetCertificate(clientHello)
	}, nil
}

func getTlsVersion(ver string) uint16 {
	switch ver {
	case "tls12":
		return tls.VersionTLS12
	case "tls13":
		return tls.VersionTLS13
	default:
		log.Fatalf("unsupported TLS version")
	}
	return 0
}

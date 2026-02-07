package config

import (
	"crypto/tls"
	"github.com/liberal-boy/tls-shunt-proxy/config/raw"
	"github.com/liberal-boy/tls-shunt-proxy/handler"
	"github.com/liberal-boy/tls-shunt-proxy/handler/http2"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"strings"
)

type (
	Config struct {
		Listen            string
		RedirectHttps     string
		Fallback          handler.Handler
		VHosts            map[string]VHost
		// WebUIListen: 管理界面监听地址，来自 raw config (webui_listen)
		WebUIListen       string
		WildcardManager   *WildcardManager
	}
	VHost struct {
		TlsConfig    *tls.Config
		Http         handler.Handler
		Http2        handler.Handler
		PathHandlers []PathHandler
		Trojan       handler.Handler
		Default      handler.Handler
	}
	PathHandler struct {
		Path, TrimPrefix string
		Handler          handler.Handler
	}
)

func readRawConfig(path string) (conf raw.RawConfig, err error) {
	conf = raw.RawConfig{InboundBufferSize: 4, OutboundBufferSize: 32}
	yamlFile, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}
	err = yaml.Unmarshal(yamlFile, &conf)
	if err != nil {
		return
	}
	return
}

func ReadConfig(path string) (conf Config, err error) {
	rawConf, err := readRawConfig(path)
	if err != nil {
		return
	}

	handler.InitBufferPools(rawConf.InboundBufferSize*1024, rawConf.OutboundBufferSize*1024)

	conf.Listen = rawConf.Listen
	conf.RedirectHttps = rawConf.RedirectHttps
	conf.WebUIListen = rawConf.WebUIListen

	conf.WildcardManager = NewWildcardManager()
	if len(rawConf.WildcardCerts) > 0 {
		if err := conf.WildcardManager.LoadFromConfig(rawConf.WildcardCerts); err != nil {
			log.Printf("⚠️ 加载通配符证书失败: %v", err)
			log.Printf("⚠️ 通配符证书功能将不可用，请检查配置和网络连接")
			// 注意：这里不返回错误，允许服务继续启动
			// 虚拟主机仍然可以使用单独的证书
		}
	}

	if rawConf.Fallback != "" {
		conf.Fallback = handler.NewProxyPassHandler(rawConf.Fallback)
	} else {
		conf.Fallback = handler.NoopHandler
	}
	conf.VHosts = make(map[string]VHost, len(rawConf.VHosts))

	for _, vh := range rawConf.VHosts {
		var tlsConfig *tls.Config

		if vh.TlsOffloading {
			// 判断是否应该使用通配符证书
			// 优先级：自定义证书 > 通配符证书 > 单独申请证书
			useWildcard := false

			// 如果没有配置自定义证书（cert/key 为空），且启用了 managedcert
			// 检查是否有通配符证书匹配
			if vh.Cert == "" && vh.Key == "" && vh.ManagedCert {
				if conf.WildcardManager != nil {
					// 检查是否有通配符证书匹配此域名
					// 注意：这里需要模拟匹配逻辑
					// 简化处理：如果有通配符配置，就尝试使用
					if conf.WildcardManager.GetCertificate(vh.Name) != nil {
						useWildcard = true
						log.Printf("域名 %s 将使用通配符证书", vh.Name)
					}
				}
			}

			// 如果使用通配符证书，则禁用 managedcert（避免单独申请）
			effectiveManagedCert := vh.ManagedCert && !useWildcard

			tlsConfig, err = getTlsConfig(effectiveManagedCert, vh.Name, vh.Cert, vh.Key, vh.KeyType, vh.Alpn, vh.Protocols, conf.WildcardManager)
		}

		pathHandlers := make([]PathHandler, len(vh.Http.Paths))

		for i, p := range vh.Http.Paths {
			pathHandlers[i] = PathHandler{
				Path:       p.Path,
				TrimPrefix: p.TrimPrefix,
				Handler:    newHandler(p.Handler, p.Args),
			}
		}

		var http2Handler handler.Handler
		if len(vh.Http2) != 0 {
			http2Handler = http2.NewHttpMuxHandler(vh.Http2)
		} else {
			http2Handler = handler.NoopHandler
		}

		conf.VHosts[strings.ToLower(vh.Name)] = VHost{
			TlsConfig:    tlsConfig,
			Http:         newHandler(vh.Http.Handler, vh.Http.Args),
			PathHandlers: pathHandlers,
			Http2:        http2Handler,
			Trojan:       newHandler(vh.Trojan.Handler, vh.Trojan.Args),
			Default:      newHandler(vh.Default.Handler, vh.Default.Args),
		}
	}
	return
}

func newHandler(name, args string) handler.Handler {
	switch name {
	case "":
		return handler.NoopHandler
	case "proxyPass":
		return handler.NewProxyPassHandler(args)
	case "fileServer":
		return handler.NewFileServerHandler(args)
	case "dohServer":
		return handler.NewDohServer(args)
	}
	return handler.NoopHandler
}
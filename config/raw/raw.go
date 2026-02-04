package raw

type (
	RawConfig struct {
		Listen                                string
		RedirectHttps                         string
		InboundBufferSize, OutboundBufferSize int
		Fallback                              string
		VHosts                                []RawVHost
		// WebUIListen: 管理界面监听地址，示例 127.0.0.1:8080
		WebUIListen                           string
	}
	RawVHost struct {
		Name          string
		TlsOffloading bool
		ManagedCert   bool
		Cert          string
		Key           string
		KeyType       string
		Alpn          string
		Protocols     string
		Http          RawHttpHandler
		Http2         []RawPathHandler
		Trojan        RawHandler
		Default       RawHandler
	}
	RawHandler struct {
		Handler string
		Args    string
	}
	RawHttpHandler struct {
		Paths   []RawPathHandler
		Handler string
		Args    string
	}
	RawPathHandler struct {
		Path       string
		Handler    string
		Args       string
		TrimPrefix string
	}

	// WizardFormData Wizard 表单数据结构
	WizardFormData struct {
		Domain               string `json:"domain"`
		CloudflareAPIKey     string `json:"cloudflare_api_key"`
		CloudflareAccountID  string `json:"cloudflare_account_id"`
		Architecture         string `json:"architecture"`
		ServerIP             string `json:"server_ip"`
		EnableHTTP           bool   `json:"enable_http"`
		EnableHTTP2          bool   `json:"enable_http2"`
		EnableTrojan         bool   `json:"enable_trojan"`
		HTTPBackendPort      int    `json:"http_backend_port"`
		HTTP2BackendPort     int    `json:"http2_backend_port"`
		TrojanBackendPort    int    `json:"trojan_backend_port"`
		DefaultBackendPort   int    `json:"default_backend_port"`
		StaticFilePath       string `json:"static_file_path"`
	}

	// ConfigTemplate 配置模板结构
	ConfigTemplate struct {
		Listen              string     `yaml:"listen"`
		RedirectHttps       string     `yaml:"redirecthttps"`
		InboundBufferSize   int        `yaml:"inboundbuffersize"`
		OutboundBufferSize  int        `yaml:"outboundbuffersize"`
		Fallback            string     `yaml:"fallback"`
		VHosts              []RawVHost `yaml:"vhosts"`
		WebUIListen         string     `yaml:"webui_listen"`
	}
)
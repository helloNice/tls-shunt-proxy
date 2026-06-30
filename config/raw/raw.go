package raw

type (
	RawConfig struct {
		Listen             string `yaml:"listen"`
		RedirectHttps      string `yaml:"redirecthttps"`
		InboundBufferSize  int    `yaml:"inboundbuffersize"`
		OutboundBufferSize int    `yaml:"outboundbuffersize"`
		Fallback           string `yaml:"fallback"`
		VHosts             []RawVHost `yaml:"vhosts"`
		// WebUIListen: 管理界面监听地址，示例 127.0.0.1:8080
		WebUIListen        string `yaml:"webui_listen"`
		WildcardCerts      []WildcardCertConfig `yaml:"wildcard_certs"`
	}
	WildcardCertConfig struct {
		Domain         string `yaml:"domain"`
		DNSProvider    string `yaml:"dns_provider"`     // DNS 提供商类型：cloudflare
		DNSCredentials string `yaml:"dns_credentials"`  // DNS 凭据（JSON 格式）
		// 保留旧配置字段以向后兼容
		CloudflareKey  string `yaml:"cloudflare_key"`

		// 新增：CA 配置
		CAProvider     string  `yaml:"ca_provider"`      // 主 CA: letsencrypt, zerossl, buypass, google
		Email          string  `yaml:"email"`            // 某些 CA 需要邮箱（如 ZeroSSL）
		EnableBackupCA *bool   `yaml:"enable_backup_ca"` // 是否启用备用 CA（nil=默认true, true=启用, false=禁用）
		BackupCA       string  `yaml:"backup_ca"`        // 备用 CA，默认 zerossl

		// 新增：重试配置
		MaxRetries     int    `yaml:"max_retries"`       // 最大重试次数，默认 3
		RetryDelay     int    `yaml:"retry_delay"`       // 重试间隔（秒），默认 30
	}
	RawVHost struct {
		Name          string         `yaml:"name"`
		TlsOffloading bool           `yaml:"tlsoffloading"`
		ManagedCert   bool           `yaml:"managedcert"`
		Cert          string         `yaml:"cert"`
		Key           string         `yaml:"key"`
		KeyType       string         `yaml:"keytype"`
		Alpn          string         `yaml:"alpn"`
		Protocols     string         `yaml:"protocols"`
		Http          RawHttpHandler `yaml:"http"`
		Http2         []RawPathHandler `yaml:"http2"`
		Trojan        RawHandler      `yaml:"trojan"`
		Default       RawHandler      `yaml:"default"`
	}
	RawHandler struct {
		Handler string `yaml:"handler"`
		Args    string `yaml:"args"`
	}
	RawHttpHandler struct {
		Paths   []RawPathHandler `yaml:"paths"`
		Handler string            `yaml:"handler"`
		Args    string            `yaml:"args"`
	}
	RawPathHandler struct {
		Path       string `yaml:"path"`
		Handler    string `yaml:"handler"`
		Args       string `yaml:"args"`
		TrimPrefix string `yaml:"trimprefix"`
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
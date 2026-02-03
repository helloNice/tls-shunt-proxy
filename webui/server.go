package webui

import (
	"encoding/base64"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"gopkg.in/yaml.v2"
)

// Start 启动 web 管理界面（在新 goroutine 中），configPath 为配置文件路径
// 绑定到 127.0.0.1:8080，启用 Basic Auth（凭据来自环境变量 WEBUI_USER/WEBUI_PASS）
func Start(configPath string) {
	tmpl := template.Must(template.ParseFiles("webui/templates/index.html"))

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "仅支持 GET", http.StatusMethodNotAllowed)
			return
		}
		data, err := ioutil.ReadFile(configPath)
		if err != nil {
			http.Error(w, "读取配置失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_ = tmpl.Execute(w, struct{ ConfigYAML string }{ConfigYAML: string(data)})
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "解析表单失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		yamlText := r.FormValue("config")
		// 基本语法校验
		var tmp interface{}
		if err := yaml.Unmarshal([]byte(yamlText), &tmp); err != nil {
			http.Error(w, "YAML 校验失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		// 原子写入
		tmpFile := configPath + ".tmp"
		if err := ioutil.WriteFile(tmpFile, []byte(yamlText), 0644); err != nil {
			http.Error(w, "写入临时文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmpFile, configPath); err != nil {
			http.Error(w, "替换配置文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// 启动新进程并退出当前进程 -> 实现 reload
		execPath, err := os.Executable()
		if err != nil {
			http.Error(w, "获取可执行文件路径失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		cmd := exec.Command(execPath, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			http.Error(w, "启动新进程失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "保存成功，正在重启服务以应用新配置...\n")
		go func() {
			_ = cmd.Process.Release()
			os.Exit(0)
		}()
	})

	// 包装 Basic Auth：从环境变量读取 WEBUI_USER / WEBUI_PASS，默认 admin/admin
	handler := basicAuthMiddleware(mux)

	bind := "127.0.0.1:8080"
	log.Println("web 管理界面启动，访问 http://" + bind + " （默认账户 admin/admin，如已在环境中设置 WEBUI_USER/WEBUI_PASS 则使用设置的）")
	go func() {
		if err := http.ListenAndServe(bind, handler); err != nil {
			log.Println("webui 启动失败:", err)
		}
	}()
}

// basicAuthMiddleware 简单的 Basic Auth 中间件，凭据来自环境变量
func basicAuthMiddleware(next http.Handler) http.Handler {
	user := os.Getenv("WEBUI_USER")
	pass := os.Getenv("WEBUI_PASS")
	if user == "" {
		user = "admin"
	}
	if pass == "" {
		pass = "admin"
	}
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected {
			w.Header().Set("WWW-Authenticate", `Basic realm="tls-shunt-proxy 管理界面"`)
			http.Error(w, "未授权", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
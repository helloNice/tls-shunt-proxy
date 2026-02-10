package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/liberal-boy/tls-shunt-proxy/config"
	"github.com/liberal-boy/tls-shunt-proxy/handler"
	"github.com/liberal-boy/tls-shunt-proxy/sniffer"
	"github.com/liberal-boy/tls-shunt-proxy/webui"
	"github.com/stevenjohnstone/sni"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

const version = "0.9.3"

var (
	conf       config.Config
	configPath string
	reloadMgr  *config.HotReloadManager
	inheritedLn net.Listener // 存储继承的监听器
)

func main() {
	configFlag := flag.String("config", "./config.yaml", "Path to config file")
	signalFlag := flag.String("s", "", "Send signal to master process: reload, stop, quit")
	testConfigFlag := flag.Bool("t", false, "Test configuration and exit")
	helpFlag := flag.Bool("h", false, "Show this help message")
	flag.Parse()
	configPath = *configFlag

	// 处理 -h 参数：显示帮助
	if *helpFlag {
		printHelp()
		os.Exit(0)
	}

	// 处理 -t 参数：测试配置
	if *testConfigFlag {
		if err := testConfig(configPath); err != nil {
			fmt.Printf("配置测试失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("配置测试成功")
		os.Exit(0)
	}

	// 处理 -s 参数：发送信号
	if *signalFlag != "" {
		if err := sendSignal(*signalFlag); err != nil {
			fmt.Printf("发送信号失败: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	fmt.Println("tls-shunt-proxy version", version)

	// 初始化热重载管理器
	reloadMgr = config.NewHotReloadManager()

	// 检查是否为零停机重载模式
	if os.Getenv("TLS_SHUNT_RELOAD_MODE") == "zero-downtime" {
		if err := startInheritedListener(); err != nil {
			log.Fatalf("启动继承的监听器失败: %v", err)
		}
	}

	// 启动内置管理界面（绑定到 127.0.0.1:8080），使用相同的配置文件路径
	webui.Start(configPath, reloadMgr)

	var err error
	conf, err = config.ReadConfig(configPath)
	if err != nil {
		log.Fatalf("failed to read config %s: %v", configPath, err)
	}

	// 设置 SIGHUP 信号处理器，用于配置热重载
	setupSignalHandler()

	if conf.RedirectHttps != "" {
		handler.ServeRedirectHttps(conf.RedirectHttps)
	}

	listenAndServe()
}

// startInheritedListener 从继承的文件描述符启动监听器
func startInheritedListener() error {
	fdStr := os.Getenv("TLS_SHUNT_LISTENER_FD")
	if fdStr == "" {
		return fmt.Errorf("未找到继承的文件描述符")
	}

	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		return fmt.Errorf("无效的文件描述符: %w", err)
	}

	log.Printf("从继承的文件描述符 %d 创建监听器", fd)

	// 从文件描述符创建文件对象
	// 使用 "tcp" 作为名称，表示这是一个 TCP socket
	file := os.NewFile(uintptr(fd), "tcp")
	if file == nil {
		return fmt.Errorf("无法从文件描述符 %d 创建文件对象", fd)
	}

	// 先尝试创建 FileConn 来验证文件描述符是否有效
	conn, err := net.FileConn(file)
	if err != nil {
		file.Close()
		return fmt.Errorf("从文件描述符创建连接失败: %w", err)
	}
	conn.Close()

	// 重新创建文件对象（因为 FileConn 可能会改变文件描述符状态）
	file = os.NewFile(uintptr(fd), "tcp")
	if file == nil {
		return fmt.Errorf("无法重新创建文件对象")
	}

	// 从文件描述符创建监听器
	listener, err := net.FileListener(file)
	if err != nil {
		file.Close()
		return fmt.Errorf("从文件描述符创建监听器失败: %w", err)
	}

	if err := file.Close(); err != nil {
		log.Printf("关闭文件描述符失败: %v", err)
	}

	// 保存到全局变量
	inheritedLn = listener

	// 设置监听器到热重载管理器
	reloadMgr.SetListener(listener)

	// 获取监听地址
	addr := listener.Addr()
	if addr != nil {
		log.Printf("继承的监听器地址: %s", addr.String())
	}

	return nil
}

// setupSignalHandler 设置信号处理器，监听 SIGHUP 信号触发配置重载
func setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	go func() {
		for range sigChan {
			log.Println("收到 SIGHUP 信号，开始零停机配置重载...")
			if err := zeroDowntimeReload(); err != nil {
				log.Printf("配置重载失败: %v", err)
			}
		}
	}()

	log.Println("SIGHUP 信号处理器已启动，发送 `kill -HUP <pid>` 可触发零停机配置重载")
}

// zeroDowntimeReload 零停机配置重载
func zeroDowntimeReload() error {
	// 使用热重载管理器进行零停机重载
	if err := reloadMgr.ZeroDowntimeReload(configPath); err != nil {
		return fmt.Errorf("零停机重载失败: %w", err)
	}
	return nil
}

// reloadConfig 在当前进程中重新加载配置（不重启进程）
// 注意：这个函数不会被直接使用，因为零停机重载是通过进程继承实现的
// 但保留此函数以便将来支持非零停机的配置重载
func reloadConfig() error {
	// 保存旧配置的 redirecthttps 地址
	oldRedirectAddr := conf.RedirectHttps

	// 使用 Config.Reload 方法重新加载配置
	if err := conf.Reload(configPath); err != nil {
		return fmt.Errorf("配置重载失败: %w", err)
	}

	// 如果 redirecthttps 配置变更，重启服务
	if oldRedirectAddr != conf.RedirectHttps {
		log.Printf("HTTPS 重定向地址变更: %s -> %s，重启服务", oldRedirectAddr, conf.RedirectHttps)
		handler.RestartRedirectHttps(conf.RedirectHttps)
	}

	return nil
}

func listenAndServe() {
	var ln net.Listener
	var err error

	// 如果有继承的监听器，使用它
	if inheritedLn != nil {
		ln = inheritedLn
		log.Println("使用继承的监听器，无需重新创建")
	} else {
		// 否则创建新的监听器
		ln, err = net.Listen("tcp", conf.Listen)
		if err != nil {
			log.Fatalf("failed to listen on %s: %v", conf.Listen, err)
		}
		reloadMgr.SetListener(ln)
		log.Printf("创建新的监听器: %s", conf.Listen)
	}

	defer func() { _ = ln.Close() }()

	log.Printf("开始接受连接: %s", ln.Addr().String())

	for {
		conn, err := ln.Accept()
		if err != nil {
			// 检查是否正在关闭
			if reloadMgr.IsShuttingDown() {
				log.Println("正在关闭，停止接受新连接")
				break
			}
			log.Printf("accept connection failed: %v\n", err)
			continue
		}

		// 检查是否可以接受新连接
		if !reloadMgr.CanAcceptConnection() {
			log.Println("正在关闭，拒绝新连接")
			conn.Close()
			continue
		}

		// 增加连接计数
		reloadMgr.IncrementConnection()

		go func(c net.Conn) {
			defer func() {
				// 减少连接计数
				reloadMgr.DecrementConnection()
				c.Close()
			}()

			handle(c)
		}(conn)
	}

	log.Println("监听循环结束")
}

func handle(conn net.Conn) {
	serverName, sniConn, err := sni.ServerNameFromConn(conn)
	if err != nil {
		if conf.Fallback != handler.NoopHandler {
			conf.Fallback.Handle(sniConn)
		} else {
			log.Printf("fail to obtain server name: %v\n", err)
			handler.NewPlainTextHandler(handler.SentHttpToHttps).Handle(conn)
		}
		return
	}

	handleWithServerName(sniConn, serverName)
}

func handleWithServerName(conn net.Conn, serverName string) {
	vh, has := conf.VHosts[strings.ToLower(serverName)]
	if !has {
		if conf.Fallback != handler.NoopHandler {
			conf.Fallback.Handle(conn)
		} else {
			log.Printf("no available vhost for %s\n", serverName)
			handler.NewPlainTextHandler(handler.NoCertificateAvailable).Handle(conn)
		}
		return
	}

	if vh.TlsConfig != nil {
		tlsConn := tlsOffloading(conn, vh.TlsConfig)
		
		// 进行 TLS 握手以获取 ALPN 协商结果
		if err := tlsConn.Handshake(); err != nil {
			log.Printf("TLS handshake failed for %s: %v\n", serverName, err)
			return
		}
		
		// 检查 ALPN 协商结果
		negotiatedProtocol := tlsConn.ConnectionState().NegotiatedProtocol
		if negotiatedProtocol == "h2" {
			// HTTP/2 通过 ALPN 协商成功
			log.Printf("HTTP/2 negotiated for %s via ALPN\n", serverName)
			if vh.Http2 != handler.NoopHandler {
				// 如果配置了 Http2 处理器，使用它
				vh.Http2.Handle(tlsConn)
				return
			}
			// 如果没有配置 Http2 处理器，继续使用嗅探器
			// 这样可以让 HTTP 流量被正确路由到 Http 处理器
			log.Printf("No Http2 handler configured for %s, using sniffer to detect traffic type\n", serverName)
		}
		
		// HTTP/1.1 或 HTTP/2（无 Http2 处理器时），继续使用嗅探
		sniffConn := sniffer.NewPeekPreDataConn(tlsConn)
		conn = sniffConn

		switch sniffConn.Type {
		case sniffer.TypeHttp:
			if handleHttp(sniffConn, vh) {
				return
			}
		case sniffer.TypeHttp2:
			if handleHttp2(sniffConn, vh) {
				return
			}
		case sniffer.TypeTrojan:
			if handleTrojan(sniffConn, vh) {
				return
			}
		}
		vh.Default.Handle(tlsConn)
	} else {
		vh.Default.Handle(conn)
	}
}

func handleHttp(conn *sniffer.SniffConn, vh config.VHost) bool {
	for _, p := range vh.PathHandlers {
		if strings.HasPrefix(conn.GetPath(), p.Path) {
			conn.SetPath(strings.TrimPrefix(conn.GetPath(), p.TrimPrefix))
			p.Handler.Handle(conn)
			return true
		}
	}

	if vh.Http != handler.NoopHandler {
		vh.Http.Handle(conn)
		return true
	}

	return false
}

func handleHttp2(conn *sniffer.SniffConn, vh config.VHost) bool {
	if vh.Http2 != handler.NoopHandler {
		vh.Http2.Handle(conn)
		return true
	}
	return handleHttp(conn, vh)
}

func handleTrojan(conn *sniffer.SniffConn, vh config.VHost) bool {
	if vh.Trojan != handler.NoopHandler {
		vh.Trojan.Handle(conn)
		return true
	}
	return false
}

func tlsOffloading(conn net.Conn, tlsConfig *tls.Config) *tls.Conn {
	return tls.Server(conn, tlsConfig)
}

// testConfig 测试配置文件是否正确
func testConfig(configPath string) error {
	fmt.Printf("测试配置文件: %s\n", configPath)
	
	// 读取配置
	conf, err := config.ReadConfig(configPath)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	
	fmt.Println("✓ 配置文件语法正确")
	fmt.Printf("  - 监听地址: %s\n", conf.Listen)
	fmt.Printf("  - 虚拟主机数量: %d\n", len(conf.VHosts))
	
	// 验证虚拟主机配置
	i := 1
	for name, vhost := range conf.VHosts {
		fmt.Printf("  - 虚拟主机 %d: %s\n", i, name)
		if vhost.TlsConfig != nil {
			fmt.Printf("    - TLS 启用: 是\n")
		} else {
			fmt.Printf("    - TLS 启用: 否\n")
		}
		i++
	}
	
	return nil
}

// sendSignal 向主进程发送信号
func sendSignal(signalType string) error {
	// 查找主进程 PID
	pid, err := findMasterPID()
	if err != nil {
		return fmt.Errorf("查找主进程失败: %w", err)
	}
	
	fmt.Printf("找到主进程 PID: %d\n", pid)
	
	// 根据信号类型发送相应的信号
	switch signalType {
	case "reload":
		// 发送 SIGHUP 信号触发配置重载
		fmt.Println("发送重载信号...")
		process, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("查找进程失败: %w", err)
		}
		if err := process.Signal(syscall.SIGHUP); err != nil {
			return fmt.Errorf("发送信号失败: %w", err)
		}
		fmt.Println("配置重载信号已发送")
		return nil
		
	case "stop":
		// 发送 SIGTERM 信号优雅停止
		fmt.Println("发送停止信号...")
		process, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("查找进程失败: %w", err)
		}
		if err := process.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("发送信号失败: %w", err)
		}
		fmt.Println("停止信号已发送")
		return nil
		
	case "quit":
		// 发送 SIGQUIT 信号立即停止
		fmt.Println("发送退出信号...")
		process, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("查找进程失败: %w", err)
		}
		if err := process.Signal(syscall.SIGQUIT); err != nil {
			return fmt.Errorf("发送信号失败: %w", err)
		}
		fmt.Println("退出信号已发送")
		return nil
		
	default:
		return fmt.Errorf("未知的信号类型: %s (支持: reload, stop, quit)", signalType)
	}
}

// findMasterPID 查找主进程 PID
func findMasterPID() (int, error) {
	// 获取当前进程 PID，避免向自己发送信号
	currentPID := os.Getpid()

	// 使用 pgrep 查找进程，使用更精确的匹配
	cmd := exec.Command("pgrep", "-x", "tls-shunt-proxy") // -x 参数精确匹配进程名
	output, err := cmd.Output()
	if err != nil {
		// pgrep 不可用，尝试使用 ps
		cmd = exec.Command("ps", "-eo", "pid,comm") // 只获取 PID 和命令名
		output, err = cmd.Output()
		if err != nil {
			return 0, fmt.Errorf("无法查找进程: %w", err)
		}

		// 解析 ps 输出
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				// 检查命令名是否匹配
				comm := fields[1]
				// 提取命令名部分（去除路径）
				if slashIndex := strings.LastIndex(comm, "/"); slashIndex != -1 {
					comm = comm[slashIndex+1:]
				}
				
				if comm == "tls-shunt-proxy" || comm == "tls-shunt-proxy.exe" {
					pid, err := strconv.Atoi(fields[0])
					if err == nil && pid != currentPID { // 确保不是当前进程
						return pid, nil
					}
				}
			}
		}

		return 0, fmt.Errorf("未找到 tls-shunt-proxy 进程")
	}

	// 解析 pgrep 输出
	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, pidStr := range pids {
		pidStr = strings.TrimSpace(pidStr)
		if pidStr == "" {
			continue
		}
		
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		
		// 确保不是当前进程
		if pid != currentPID {
			return pid, nil
		}
	}

	return 0, fmt.Errorf("未找到其他 tls-shunt-proxy 进程")
}

// printHelp 打印帮助信息
func printHelp() {
	fmt.Printf("tls-shunt-proxy version %s\n\n", version)
	fmt.Println("用法:")
	fmt.Println("  tls-shunt-proxy [选项]")
	fmt.Println()
	fmt.Println("选项:")
	fmt.Println("  -config string")
	fmt.Println("        配置文件路径 (默认 \"./config.yaml\")")
	fmt.Println("  -s string")
	fmt.Println("        向主进程发送信号: reload, stop, quit")
	fmt.Println("  -t")
	fmt.Println("        测试配置文件并退出")
	fmt.Println("  -h")
	fmt.Println("        显示此帮助信息")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  # 启动服务")
	fmt.Println("  ./tls-shunt-proxy")
	fmt.Println()
	fmt.Println("  # 指定配置文件启动")
	fmt.Println("  ./tls-shunt-proxy -config /path/to/config.yaml")
	fmt.Println()
	fmt.Println("  # 测试配置文件")
	fmt.Println("  ./tls-shunt-proxy -t")
	fmt.Println()
	fmt.Println("  # 重载配置（类似 nginx -s reload）")
	fmt.Println("  ./tls-shunt-proxy -s reload")
	fmt.Println()
	fmt.Println("  # 优雅停止服务")
	fmt.Println("  ./tls-shunt-proxy -s stop")
	fmt.Println()
	fmt.Println("  # 立即停止服务")
	fmt.Println("  ./tls-shunt-proxy -s quit")
}
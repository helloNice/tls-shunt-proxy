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
	fmt.Println("tls-shunt-proxy version", version)

	configFlag := flag.String("config", "./config.yaml", "Path to config file")
	flag.Parse()
	configPath = *configFlag

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

	// 从文件描述符创建监听器
	file := os.NewFile(uintptr(fd), "listener")
	listener, err := net.FileListener(file)
	if err != nil {
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
		conn = tlsOffloading(conn, vh.TlsConfig)
		sniffConn := sniffer.NewPeekPreDataConn(conn)
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
	}
	vh.Default.Handle(conn)
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
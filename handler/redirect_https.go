package handler

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	redirectHttpsServer   *http.Server
	redirectHttpsMutex    sync.RWMutex
	currentRedirectAddr   string
)

func redirectHttps(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}

func ServeRedirectHttps(listen string) {
	redirectHttpsMutex.Lock()
	defer redirectHttpsMutex.Unlock()

	// 如果已经运行且地址相同，跳过
	if redirectHttpsServer != nil && currentRedirectAddr == listen {
		log.Println("HTTPS 重定向服务已在运行，跳过启动")
		return
	}

	// 如果有旧的服务器且地址不同，先关闭
	if redirectHttpsServer != nil {
		log.Println("正在停止旧的 HTTPS 重定向服务...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := redirectHttpsServer.Shutdown(ctx); err != nil {
			log.Printf("关闭 HTTPS 重定向服务失败: %v", err)
		}
		redirectHttpsServer = nil
	}

	currentRedirectAddr = listen
	redirectHttpsServer = &http.Server{
		Addr:    listen,
		Handler: http.HandlerFunc(redirectHttps),
	}

	go func() {
		err := redirectHttpsServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Println("HTTPS 重定向服务错误:", err)
		}
	}()
	log.Printf("HTTPS 重定向服务已启动，监听地址: %s", listen)
}

func RestartRedirectHttps(listen string) {
	redirectHttpsMutex.Lock()
	defer redirectHttpsMutex.Unlock()

	// 如果地址相同，无需重启
	if currentRedirectAddr == listen {
		log.Printf("HTTPS 重定向服务地址未变更，跳过重启")
		return
	}

	// 关闭旧的服务器
	if redirectHttpsServer != nil {
		log.Println("正在停止旧的 HTTPS 重定向服务...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := redirectHttpsServer.Shutdown(ctx); err != nil {
			log.Printf("关闭 HTTPS 重定向服务失败: %v", err)
		}
		redirectHttpsServer = nil
	}

	// 如果新地址为空，不启动新服务
	if listen == "" {
		currentRedirectAddr = ""
		log.Println("HTTPS 重定向服务已禁用")
		return
	}

	// 启动新的服务器
	currentRedirectAddr = listen
	redirectHttpsServer = &http.Server{
		Addr:    listen,
		Handler: http.HandlerFunc(redirectHttps),
	}

	go func() {
		err := redirectHttpsServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Println("HTTPS 重定向服务错误:", err)
		}
	}()
	log.Printf("HTTPS 重定向服务已重启，监听地址: %s", listen)
}

package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/shisheng820/res-sniffer/internal/api"
	"github.com/shisheng820/res-sniffer/internal/downloader"
	"github.com/shisheng820/res-sniffer/internal/proxy"
	"github.com/shisheng820/res-sniffer/internal/store"
	"github.com/shisheng820/res-sniffer/pkg/types"
)

var (
	proxyPort    int
	apiPort      int
	certDir      string
	saveDir      string
	downloadThreads int
	autoStart    bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "res-sniffer",
		Short: "网络资源嗅探下载工具 - 支持代理嗅探、CLI 和 HTTP API",
		Long: `res-sniffer 是一个基于本地代理的网络资源嗅探下载工具。
通过启动本地 HTTP/HTTPS 代理，自动拦截并识别网络流量中的
视频、音频、图片、m3u8 等资源，支持 CLI 和 HTTP API 两种使用方式。`,
	}

	// 全局标志
	rootCmd.PersistentFlags().IntVarP(&proxyPort, "proxy-port", "p", 8899, "代理服务器端口")
	rootCmd.PersistentFlags().IntVarP(&apiPort, "api-port", "a", 9999, "API 服务器端口")
	rootCmd.PersistentFlags().StringVar(&certDir, "cert-dir", "", "证书存储目录")
	rootCmd.PersistentFlags().StringVar(&saveDir, "save-dir", "", "下载保存目录")
	rootCmd.PersistentFlags().IntVar(&downloadThreads, "threads", 4, "下载线程数")

	// start 命令
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "启动代理和 API 服务器",
		Run:   runStart,
	}
	startCmd.Flags().BoolVar(&autoStart, "auto-start", true, "自动启动代理")
	rootCmd.AddCommand(startCmd)

	// serve 命令（仅 API 服务器）
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "仅启动 API 服务器",
		Run:   runServe,
	}
	rootCmd.AddCommand(serveCmd)

	// proxy 命令
	proxyCmd := &cobra.Command{
		Use:   "proxy",
		Short: "代理相关操作",
	}
	proxyStartCmd := &cobra.Command{
		Use:   "start",
		Short: "启动代理服务器",
		Run:   runProxyStart,
	}
	proxyStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "停止代理服务器",
		Run:   runProxyStop,
	}
	proxyCertCmd := &cobra.Command{
		Use:   "cert",
		Short: "管理 CA 证书",
	}
	proxyCertInstallCmd := &cobra.Command{
		Use:   "install",
		Short: "安装 CA 证书到系统信任库",
		Run:   runCertInstall,
	}
	proxyCertPathCmd := &cobra.Command{
		Use:   "path",
		Short: "显示 CA 证书路径",
		Run:   runCertPath,
	}
	proxyCertCmd.AddCommand(proxyCertInstallCmd, proxyCertPathCmd)
	proxyCmd.AddCommand(proxyStartCmd, proxyStopCmd, proxyCertCmd)
	rootCmd.AddCommand(proxyCmd)

	// download 命令
	downloadCmd := &cobra.Command{
		Use:   "download [url]",
		Short: "下载指定 URL 的资源",
		Args:  cobra.ExactArgs(1),
		Run:   runDownload,
	}
	downloadCmd.Flags().StringP("output", "o", "", "输出文件名")
	rootCmd.AddCommand(downloadCmd)

	// version 命令
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "显示版本信息",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("res-sniffer v0.1.0")
		},
	}
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runStart 启动代理和 API
func runStart(cmd *cobra.Command, args []string) {
	st := store.NewStore()
	dl := downloader.NewManager(saveDir, downloadThreads)

	// 资源回调：存储 + 广播
	var apiServer *api.Server

	// 创建代理
	p, err := proxy.NewProxy(proxyPort, certDir, func(r *types.Resource) {
		st.AddResource(r)
		if apiServer != nil {
			apiServer.BroadcastResource(r)
		}
	})
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}

	// 创建 API 服务器
	apiServer = api.NewServer(p, st, dl, apiPort)

	// 自动启动代理
	if autoStart {
		if err := p.Start(); err != nil {
			log.Printf("Warning: failed to start proxy: %v", err)
		} else {
			log.Printf("Proxy started on port %d", proxyPort)
			log.Printf("CA certificate: %s", p.GetCertManager().GetCACertPath())
		}
	}

	log.Printf("API server starting on port %d", apiPort)
	log.Printf("API docs: http://127.0.0.1:%d/api/status", apiPort)
	log.Printf("WebSocket: ws://127.0.0.1:%d/ws", apiPort)

	// 在 goroutine 中启动 API 服务器
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Printf("API server error: %v", err)
		}
	}()

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	p.Stop()
	apiServer.Stop()
}

// runServe 仅启动 API
func runServe(cmd *cobra.Command, args []string) {
	st := store.NewStore()
	dl := downloader.NewManager(saveDir, downloadThreads)
	p, err := proxy.NewProxy(proxyPort, certDir, nil)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}

	apiServer := api.NewServer(p, st, dl, apiPort)
	log.Printf("API server starting on port %d", apiPort)

	if err := apiServer.Start(); err != nil {
		log.Fatalf("API server error: %v", err)
	}
}

// runProxyStart 启动代理
func runProxyStart(cmd *cobra.Command, args []string) {
	p, err := proxy.NewProxy(proxyPort, certDir, nil)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}

	if err := p.Start(); err != nil {
		log.Fatalf("Failed to start proxy: %v", err)
	}

	log.Printf("Proxy started on port %d", proxyPort)
	log.Printf("CA certificate: %s", p.GetCertManager().GetCACertPath())
	log.Println("Press Ctrl+C to stop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	p.Stop()
	log.Println("Proxy stopped")
}

// runProxyStop 停止代理（占位，实际需要进程间通信）
func runProxyStop(cmd *cobra.Command, args []string) {
	fmt.Println("Use Ctrl+C in the proxy process to stop it")
}

// runCertInstall 安装证书
func runCertInstall(cmd *cobra.Command, args []string) {
	cm, err := proxy.NewCertManager(certDir)
	if err != nil {
		log.Fatalf("Failed to create cert manager: %v", err)
	}

	fmt.Printf("CA certificate path: %s\n", cm.GetCACertPath())
	fmt.Println("Installing CA certificate to system trust store...")

	if err := cm.InstallCACert(); err != nil {
		log.Printf("Warning: auto install failed: %v", err)
		fmt.Println("\nPlease manually install the CA certificate:")
		fmt.Printf("  Certificate: %s\n", cm.GetCACertPath())
		return
	}

	fmt.Println("CA certificate installed successfully!")
}

// runCertPath 显示证书路径
func runCertPath(cmd *cobra.Command, args []string) {
	cm, err := proxy.NewCertManager(certDir)
	if err != nil {
		log.Fatalf("Failed to create cert manager: %v", err)
	}
	fmt.Println(cm.GetCACertPath())
}

// runDownload 直接下载
func runDownload(cmd *cobra.Command, args []string) {
	url := args[0]
	output, _ := cmd.Flags().GetString("output")

	dl := downloader.NewManager(saveDir, downloadThreads)
	task, err := dl.CreateTaskFromURL(url, output, saveDir, downloadThreads, nil)
	if err != nil {
		log.Fatalf("Failed to create download task: %v", err)
	}

	if err := dl.StartTask(task.ID); err != nil {
		log.Fatalf("Failed to start download: %v", err)
	}

	fmt.Printf("Downloading: %s\n", url)
	fmt.Printf("Save to: %s\n", task.SavePath)

	// 等待下载完成
	for {
		t, ok := dl.GetTask(task.ID)
		if !ok {
			break
		}
		if t.Status == "completed" {
			fmt.Printf("\nDownload completed: %s\n", t.SavePath)
			break
		}
		if t.Status == "failed" {
			fmt.Printf("\nDownload failed: %s\n", t.Error)
			os.Exit(1)
		}
		fmt.Printf("\rProgress: %d / %d bytes (%.1f%%)",
			t.Downloaded, t.TotalSize,
			float64(t.Downloaded)/float64(t.TotalSize)*100)
	}
}

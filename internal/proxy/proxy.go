package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisheng820/res-sniffer/pkg/types"
)

// ResourceCallback 资源发现回调函数
type ResourceCallback func(resource *types.Resource)

// Proxy 代理服务器
type Proxy struct {
	listener    net.Listener
	port        int
	certManager *CertManager
	callback    ResourceCallback
	running     bool
	mu          sync.RWMutex

	// 统计
	totalRequests  int64
	totalResources int64

	// 传输客户端
	transport *http.Transport
}

// NewProxy 创建代理服务器
func NewProxy(port int, certDir string, callback ResourceCallback) (*Proxy, error) {
	cm, err := NewCertManager(certDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert manager: %w", err)
	}

	return &Proxy{
		port:        port,
		certManager: cm,
		callback:    callback,
		transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}, nil
}

// Start 启动代理服务器
func (p *Proxy) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("proxy already running")
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", p.port))
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("failed to listen on port %d: %w", p.port, err)
	}

	p.listener = listener
	p.running = true
	p.mu.Unlock()

	go p.acceptLoop()
	return nil
}

// Stop 停止代理服务器
func (p *Proxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	p.running = false
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

// IsRunning 检查代理是否运行
func (p *Proxy) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// GetPort 获取代理端口
func (p *Proxy) GetPort() int {
	return p.port
}

// GetCertManager 获取证书管理器
func (p *Proxy) GetCertManager() *CertManager {
	return p.certManager
}

// GetStats 获取统计信息
func (p *Proxy) GetStats() (totalRequests, totalResources int64) {
	return atomic.LoadInt64(&p.totalRequests), atomic.LoadInt64(&p.totalResources)
}

// acceptLoop 接受连接循环
func (p *Proxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			if !p.IsRunning() {
				return
			}
			continue
		}
		go p.handleConnection(conn)
	}
}

// handleConnection 处理客户端连接
func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	// 读取第一个请求
	reader := bufio.NewReader(clientConn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	atomic.AddInt64(&p.totalRequests, 1)

	// 处理 CONNECT 方法（HTTPS）
	if req.Method == http.MethodConnect {
		p.handleConnect(clientConn, req)
		return
	}

	// 处理普通 HTTP 请求
	p.handleHTTP(clientConn, req, reader)
}

// handleConnect 处理 HTTPS CONNECT 隧道
func (p *Proxy) handleConnect(clientConn net.Conn, req *http.Request) {
	// 解析目标地址
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}

	// 告诉客户端连接已建立
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// 进行 TLS 握手（中间人）
	tlsConfig := &tls.Config{
		GetCertificate: p.certManager.GetCertificate,
	}
	tlsConn := tls.Server(clientConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		return
	}
	defer tlsConn.Close()

	// 读取 HTTPS 请求
	reader := bufio.NewReader(tlsConn)
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}

		atomic.AddInt64(&p.totalRequests, 1)

		// 构建完整 URL
		req.URL.Scheme = "https"
		req.URL.Host = host

		// 处理请求并获取响应
		resp, resource, err := p.forwardRequest(req)
		if err != nil {
			return
		}

		// 发送响应给客户端
		if err := resp.Write(tlsConn); err != nil {
			resp.Body.Close()
			return
		}
		resp.Body.Close()

		// 触发资源回调
		if resource != nil && p.callback != nil {
			atomic.AddInt64(&p.totalResources, 1)
			p.callback(resource)
		}

		// 检查是否关闭连接
		if req.Close {
			return
		}
	}
}

// handleHTTP 处理普通 HTTP 请求
func (p *Proxy) handleHTTP(clientConn net.Conn, req *http.Request, reader *bufio.Reader) {
	for {
		// 确保 URL 完整
		if req.URL.Scheme == "" {
			req.URL.Scheme = "http"
		}
		if req.URL.Host == "" {
			req.URL.Host = req.Host
		}

		// 处理请求并获取响应
		resp, resource, err := p.forwardRequest(req)
		if err != nil {
			return
		}

		// 发送响应给客户端
		if err := resp.Write(clientConn); err != nil {
			resp.Body.Close()
			return
		}
		resp.Body.Close()

		// 触发资源回调
		if resource != nil && p.callback != nil {
			atomic.AddInt64(&p.totalResources, 1)
			p.callback(resource)
		}

		// 检查是否关闭连接
		if req.Close {
			return
		}

		// 读取下一个请求
		req, err = http.ReadRequest(reader)
		if err != nil {
			return
		}
		atomic.AddInt64(&p.totalRequests, 1)
	}
}

// forwardRequest 转发请求到目标服务器并返回响应
func (p *Proxy) forwardRequest(req *http.Request) (*http.Response, *types.Resource, error) {
	// 移除代理相关头
	req.RequestURI = ""
	req.Header.Del("Proxy-Connection")

	// 保存请求头用于下载
	headers := make(map[string]string)
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	// 转发请求
	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		return nil, nil, err
	}

	// 分析响应，判断是否为可下载资源
	resource := p.analyzeResponse(req, resp, headers)

	// 包装响应体，确保完整读取后再回调
	originalBody := resp.Body
	resp.Body = &wrapBody{
		ReadCloser: originalBody,
		onClose: func() {
			// 资源回调在响应发送后触发
		},
	}

	return resp, resource, nil
}

// analyzeResponse 分析响应是否为可下载资源
func (p *Proxy) analyzeResponse(req *http.Request, resp *http.Response, headers map[string]string) *types.Resource {
	contentType := resp.Header.Get("Content-Type")
	contentLength := resp.ContentLength

	// 判断资源类型
	resourceType := detectResourceType(req.URL.String(), contentType)

	// 非媒体资源不记录
	if resourceType == types.ResourceTypeOther {
		return nil
	}

	// 跳过太小的资源（小于 10KB 的图片等可能是图标）
	if contentLength > 0 && contentLength < 10*1024 && resourceType == types.ResourceTypeImage {
		return nil
	}

	// 生成文件名
	fileName := generateFileName(req.URL.String(), contentType, resourceType)

	// 构建资源对象
	resource := &types.Resource{
		ID:          generateResourceID(req.URL.String()),
		URL:         req.URL.String(),
		Type:        resourceType,
		ContentType: contentType,
		FileName:    fileName,
		FileSize:    contentLength,
		Referer:     req.Header.Get("Referer"),
		Host:        req.URL.Host,
		Method:      req.Method,
		Status:      resp.StatusCode,
		CreatedAt:   time.Now(),
		Headers:     headers,
	}

	return resource
}

// detectResourceType 根据 URL 和 Content-Type 判断资源类型
func detectResourceType(rawURL, contentType string) types.ResourceType {
	lowerCT := strings.ToLower(contentType)
	lowerURL := strings.ToLower(rawURL)

	// m3u8 / HLS
	if strings.Contains(lowerCT, "vnd.apple.mpegurl") ||
		strings.Contains(lowerCT, "application/x-mpegurl") ||
		strings.HasSuffix(lowerURL, ".m3u8") {
		return types.ResourceTypeM3U8
	}

	// 视频
	if strings.HasPrefix(lowerCT, "video/") ||
		strings.Contains(lowerCT, "mp4") ||
		strings.Contains(lowerCT, "webm") ||
		strings.Contains(lowerCT, "ogg") {
		return types.ResourceTypeVideo
	}

	// 音频
	if strings.HasPrefix(lowerCT, "audio/") ||
		strings.Contains(lowerCT, "mpeg") ||
		strings.Contains(lowerCT, "wav") ||
		strings.Contains(lowerCT, "flac") ||
		strings.Contains(lowerCT, "aac") {
		// 排除 m3u8
		if !strings.Contains(lowerCT, "mpegurl") {
			return types.ResourceTypeAudio
		}
	}

	// 图片
	if strings.HasPrefix(lowerCT, "image/") {
		return types.ResourceTypeImage
	}

	// 字幕
	if strings.Contains(lowerCT, "subtitle") ||
		strings.Contains(lowerCT, "srt") ||
		strings.Contains(lowerCT, "vtt") {
		return types.ResourceTypeSubtitle
	}

	// 根据 URL 后缀判断
	switch {
	case strings.HasSuffix(lowerURL, ".m3u8"):
		return types.ResourceTypeM3U8
	case strings.HasSuffix(lowerURL, ".mp4"), strings.HasSuffix(lowerURL, ".webm"),
		strings.HasSuffix(lowerURL, ".avi"), strings.HasSuffix(lowerURL, ".mkv"),
		strings.HasSuffix(lowerURL, ".mov"), strings.HasSuffix(lowerURL, ".flv"):
		return types.ResourceTypeVideo
	case strings.HasSuffix(lowerURL, ".mp3"), strings.HasSuffix(lowerURL, ".wav"),
		strings.HasSuffix(lowerURL, ".flac"), strings.HasSuffix(lowerURL, ".aac"),
		strings.HasSuffix(lowerURL, ".ogg"), strings.HasSuffix(lowerURL, ".m4a"):
		return types.ResourceTypeAudio
	case strings.HasSuffix(lowerURL, ".jpg"), strings.HasSuffix(lowerURL, ".jpeg"),
		strings.HasSuffix(lowerURL, ".png"), strings.HasSuffix(lowerURL, ".gif"),
		strings.HasSuffix(lowerURL, ".webp"), strings.HasSuffix(lowerURL, ".bmp"):
		return types.ResourceTypeImage
	case strings.HasSuffix(lowerURL, ".srt"), strings.HasSuffix(lowerURL, ".vtt"),
		strings.HasSuffix(lowerURL, ".ass"):
		return types.ResourceTypeSubtitle
	}

	return types.ResourceTypeOther
}

// generateFileName 从 URL 生成文件名
func generateFileName(rawURL, contentType string, resourceType types.ResourceType) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}

	path := u.Path
	lastSlash := strings.LastIndex(path, "/")
	fileName := path
	if lastSlash >= 0 {
		fileName = path[lastSlash+1:]
	}

	// URL 解码
	if decoded, err := url.QueryUnescape(fileName); err == nil {
		fileName = decoded
	}

	// 如果没有扩展名，根据 Content-Type 添加
	if !strings.Contains(fileName, ".") {
		ext := getExtensionFromContentType(contentType, resourceType)
		if ext != "" {
			fileName = fileName + ext
		}
	}

	// 清理非法字符
	fileName = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, fileName)

	if fileName == "" || fileName == "." {
		fileName = "resource"
	}

	return fileName
}

// getExtensionFromContentType 根据 Content-Type 获取扩展名
func getExtensionFromContentType(contentType string, resourceType types.ResourceType) string {
	lowerCT := strings.ToLower(contentType)
	switch {
	case strings.Contains(lowerCT, "mp4"):
		return ".mp4"
	case strings.Contains(lowerCT, "webm"):
		return ".webm"
	case strings.Contains(lowerCT, "quicktime"):
		return ".mov"
	case strings.Contains(lowerCT, "x-flv"):
		return ".flv"
	case strings.Contains(lowerCT, "mpeg") && !strings.Contains(lowerCT, "url"):
		return ".mp3"
	case strings.Contains(lowerCT, "wav"):
		return ".wav"
	case strings.Contains(lowerCT, "flac"):
		return ".flac"
	case strings.Contains(lowerCT, "aac"):
		return ".aac"
	case strings.Contains(lowerCT, "ogg"):
		return ".ogg"
	case strings.Contains(lowerCT, "jpeg"):
		return ".jpg"
	case strings.Contains(lowerCT, "png"):
		return ".png"
	case strings.Contains(lowerCT, "gif"):
		return ".gif"
	case strings.Contains(lowerCT, "webp"):
		return ".webp"
	case strings.Contains(lowerCT, "mpegurl"):
		return ".m3u8"
	case strings.Contains(lowerCT, "vtt"):
		return ".vtt"
	case strings.Contains(lowerCT, "srt"):
		return ".srt"
	}

	// 根据类型默认
	switch resourceType {
	case types.ResourceTypeVideo:
		return ".mp4"
	case types.ResourceTypeAudio:
		return ".mp3"
	case types.ResourceTypeImage:
		return ".jpg"
	case types.ResourceTypeM3U8:
		return ".m3u8"
	}
	return ""
}

// generateResourceID 生成资源 ID
func generateResourceID(rawURL string) string {
	// 使用 URL 的哈希作为 ID（简单实现）
	h := fmt.Sprintf("%x", len(rawURL))
	return fmt.Sprintf("res_%s_%d", h, time.Now().UnixNano())
}

// wrapBody 包装响应体
type wrapBody struct {
	io.ReadCloser
	onClose func()
}

func (w *wrapBody) Close() error {
	err := w.ReadCloser.Close()
	if w.onClose != nil {
		w.onClose()
	}
	return err
}

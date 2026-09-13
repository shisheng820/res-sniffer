package types

import "time"

// ResourceType 资源类型
type ResourceType string

const (
	ResourceTypeVideo    ResourceType = "video"
	ResourceTypeAudio    ResourceType = "audio"
	ResourceTypeImage    ResourceType = "image"
	ResourceTypeM3U8     ResourceType = "m3u8"
	ResourceTypeLive     ResourceType = "live"
	ResourceTypeSubtitle ResourceType = "subtitle"
	ResourceTypeOther    ResourceType = "other"
)

// Resource 嗅探到的网络资源
type Resource struct {
	ID          string       `json:"id"`
	URL         string       `json:"url"`
	Type        ResourceType `json:"type"`
	ContentType string       `json:"content_type"`
	FileName    string       `json:"file_name"`
	FileSize    int64        `json:"file_size"`
	Referer     string       `json:"referer"`
	Host        string       `json:"host"`
	Method      string       `json:"method"`
	Status      int          `json:"status"`
	CreatedAt   time.Time    `json:"created_at"`
	// 额外的请求头，用于下载时携带
	Headers map[string]string `json:"headers,omitempty"`
}

// DownloadStatus 下载状态
type DownloadStatus string

const (
	DownloadPending   DownloadStatus = "pending"
	DownloadRunning   DownloadStatus = "running"
	DownloadPaused    DownloadStatus = "paused"
	DownloadCompleted DownloadStatus = "completed"
	DownloadFailed    DownloadStatus = "failed"
)

// DownloadTask 下载任务
type DownloadTask struct {
	ID           string         `json:"id"`
	ResourceID   string         `json:"resource_id"`
	URL          string         `json:"url"`
	FileName     string         `json:"file_name"`
	SavePath     string         `json:"save_path"`
	TotalSize    int64          `json:"total_size"`
	Downloaded   int64          `json:"downloaded"`
	Speed        int64          `json:"speed"` // bytes per second
	Status       DownloadStatus `json:"status"`
	Error        string         `json:"error,omitempty"`
	Threads      int            `json:"threads"`
	ResourceType ResourceType   `json:"resource_type"`
	Headers      map[string]string `json:"headers,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	// m3u8 专用
	SegmentsTotal    int `json:"segments_total,omitempty"`
	SegmentsDone     int `json:"segments_done,omitempty"`
}

// ProxyStatus 代理状态
type ProxyStatus struct {
	Running   bool   `json:"running"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	StartTime string `json:"start_time,omitempty"`
	// 统计
	TotalRequests   int64 `json:"total_requests"`
	TotalResources  int64 `json:"total_resources"`
	TotalDownloaded int64 `json:"total_downloaded"`
}

// APIResponse 统一 API 响应
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// WSMessage WebSocket 消息
type WSMessage struct {
	Type string      `json:"type"` // resource_new, download_update, proxy_status
	Data interface{} `json:"data"`
}

// StartProxyRequest 启动代理请求
type StartProxyRequest struct {
	Port int `json:"port"`
}

// CreateDownloadRequest 创建下载请求
type CreateDownloadRequest struct {
	ResourceID string `json:"resource_id,omitempty"`
	URL        string `json:"url,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	SaveDir    string `json:"save_dir,omitempty"`
	Threads    int    `json:"threads,omitempty"`
}

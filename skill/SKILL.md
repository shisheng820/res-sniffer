---
name: res-sniffer
description: 网络资源嗅探下载助手。通过本地代理自动拦截网络流量中的视频、音频、图片、m3u8 等资源，支持查看资源列表、下载资源、管理下载任务。适用于需要从网页或应用中提取媒体资源的场景。当用户需要下载视频、音频、图片，或需要嗅探网页中的媒体资源时使用。
---

# res-sniffer Skill

网络资源嗅探下载工具，通过本地 HTTP/HTTPS 代理自动识别和下载网络流量中的媒体资源。

## 前置条件

1. res-sniffer 已安装并启动（`res-sniffer start`）
2. CA 证书已安装（`res-sniffer proxy cert install`）
3. 系统或浏览器代理已配置为 `127.0.0.1:8899`

默认 API 地址：`http://127.0.0.1:9999`

## 核心能力

### 1. 检查服务状态

检查代理和 API 是否正常运行。

```bash
curl -s http://127.0.0.1:9999/api/status
```

返回示例：
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "running": true,
    "address": "127.0.0.1",
    "port": 8899,
    "total_requests": 1523,
    "total_resources": 42
  }
}
```

### 2. 启动/停止代理

如果代理未运行，启动它：

```bash
curl -s -X POST http://127.0.0.1:9999/api/proxy/start \
  -H "Content-Type: application/json" \
  -d '{"port": 8899}'
```

停止代理：

```bash
curl -s -X POST http://127.0.0.1:9999/api/proxy/stop
```

### 3. 查看嗅探到的资源

获取所有资源列表：

```bash
curl -s "http://127.0.0.1:9999/api/resources?page=1&page_size=20"
```

按类型过滤（video/audio/image/m3u8）：

```bash
curl -s "http://127.0.0.1:9999/api/resources?type=video&page=1&page_size=20"
```

返回示例：
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "res_abc123",
        "url": "https://example.com/video.mp4",
        "type": "video",
        "content_type": "video/mp4",
        "file_name": "video.mp4",
        "file_size": 104857600,
        "host": "example.com",
        "created_at": "2026-09-13T12:00:00Z"
      }
    ],
    "total": 42,
    "page": 1
  }
}
```

### 4. 下载嗅探到的资源

通过资源 ID 下载：

```bash
curl -s -X POST "http://127.0.0.1:9999/api/resources/{resource_id}" \
  -H "Content-Type: application/json" \
  -d '{"save_dir": "", "threads": 4}'
```

直接通过 URL 下载：

```bash
curl -s -X POST http://127.0.0.1:9999/api/downloads \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/video.mp4",
    "file_name": "my_video.mp4",
    "save_dir": "",
    "threads": 4
  }'
```

返回下载任务信息：
```json
{
  "code": 201,
  "message": "success",
  "data": {
    "id": "dl_123456",
    "url": "https://example.com/video.mp4",
    "file_name": "video.mp4",
    "save_path": "/Users/xxx/Downloads/res-sniffer/video.mp4",
    "status": "running",
    "total_size": 104857600,
    "downloaded": 0
  }
}
```

### 5. 查看下载任务

查看所有下载任务：

```bash
curl -s "http://127.0.0.1:9999/api/downloads?page=1&page_size=20"
```

按状态过滤（pending/running/paused/completed/failed）：

```bash
curl -s "http://127.0.0.1:9999/api/downloads?status=running"
```

查看单个下载任务详情：

```bash
curl -s http://127.0.0.1:9999/api/downloads/{task_id}
```

### 6. 控制下载任务

暂停下载：
```bash
curl -s -X POST http://127.0.0.1:9999/api/downloads/{task_id}/pause
```

恢复下载：
```bash
curl -s -X POST http://127.0.0.1:9999/api/downloads/{task_id}/resume
```

取消下载：
```bash
curl -s -X POST http://127.0.0.1:9999/api/downloads/{task_id}/cancel
```

### 7. 清空资源列表

```bash
curl -s -X DELETE http://127.0.0.1:9999/api/resources
```

## 使用流程

### 场景一：下载网页中的视频

1. 确认 res-sniffer 正在运行：`GET /api/status`
2. 提示用户在浏览器中打开目标网页并播放视频（确保代理已配置）
3. 等待几秒后查询资源列表：`GET /api/resources?type=video`
4. 从列表中选择目标视频，记录其 ID
5. 发起下载：`POST /api/resources/{id}`
6. 轮询下载状态直到完成：`GET /api/downloads/{task_id}`
7. 告知用户文件保存路径

### 场景二：直接下载已知 URL 的资源

1. 确认服务运行：`GET /api/status`
2. 直接创建下载任务：`POST /api/downloads`，传入 URL
3. 等待下载完成，返回文件路径

### 场景三：批量下载音频资源

1. 确认服务运行
2. 查询音频资源列表：`GET /api/resources?type=audio`
3. 遍历资源 ID，逐个发起下载
4. 查询所有下载任务状态：`GET /api/downloads?status=running`
5. 等待全部完成

## 注意事项

1. **首次使用必须安装 CA 证书**，否则 HTTPS 流量无法解密
2. **代理配置**：确保浏览器或系统代理指向 `127.0.0.1:8899`
3. **资源嗅探需要流量经过代理**：用户必须在配置了代理的浏览器中访问目标网页
4. **m3u8 视频**：会自动下载所有分片并合并为单个文件
5. **下载路径**：默认保存在用户下载目录下的 `res-sniffer` 文件夹
6. **WebSocket 实时推送**：可通过 `ws://127.0.0.1:9999/ws` 实时接收新资源和下载进度

## 故障排查

- **代理无法启动**：检查端口 8899 是否被占用
- **HTTPS 网站无法访问**：确认 CA 证书已正确安装
- **嗅探不到资源**：确认代理已配置，且目标网站流量经过代理
- **下载失败**：检查网络连接，部分资源可能需要特定的 Referer 或 Cookie

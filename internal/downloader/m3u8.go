package downloader

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisheng820/res-sniffer/pkg/types"
)

// m3u8Segment m3u8 分片
type m3u8Segment struct {
	index    int
	uri      string
	duration float64
	keyURI   string
	iv       []byte
}

// m3u8Playlist m3u8 播放列表
type m3u8Playlist struct {
	version        int
	targetDuration int
	mediaSequence  int
	isLive         bool
	segments       []*m3u8Segment
	keyURI         string
	iv             []byte
	masterURL      string
}

// downloadM3U8 下载 m3u8 视频
func (m *Manager) downloadM3U8(task *types.DownloadTask, stopChan chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			task.Status = types.DownloadFailed
			task.Error = fmt.Sprintf("panic: %v", r)
			m.notify(task)
		}
	}()

	// 创建临时目录
	tmpDir := task.SavePath + ".tmp"
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// 解析 m3u8 播放列表
	playlist, err := m.parseM3U8(task.URL, task.Headers)
	if err != nil {
		task.Status = types.DownloadFailed
		task.Error = "parse m3u8: " + err.Error()
		m.notify(task)
		return
	}

	// 如果是 master playlist，选择最高质量
	if len(playlist.segments) == 0 && playlist.masterURL != "" {
		playlist, err = m.parseM3U8(playlist.masterURL, task.Headers)
		if err != nil {
			task.Status = types.DownloadFailed
			task.Error = "parse master m3u8: " + err.Error()
			m.notify(task)
			return
		}
	}

	if len(playlist.segments) == 0 {
		task.Status = types.DownloadFailed
		task.Error = "no segments found"
		m.notify(task)
		return
	}

	task.SegmentsTotal = len(playlist.segments)
	task.TotalSize = int64(len(playlist.segments)) * 1024 * 1024 // 估算
	m.notify(task)

	// 下载所有分片
	segDir := filepath.Join(tmpDir, "segments")
	os.MkdirAll(segDir, 0755)

	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0
	failed := 0
	sem := make(chan struct{}, task.Threads)

	for _, seg := range playlist.segments {
		wg.Add(1)
		sem <- struct{}{}
		go func(seg *m3u8Segment) {
			defer wg.Done()
			defer func() { <-sem }()

			// 检查停止
			select {
			case <-stopChan:
				mu.Lock()
				failed++
				mu.Unlock()
				return
			default:
			}

			// 检查暂停
			for {
				m.mu.RLock()
				currentTask := m.tasks[task.ID]
				paused := currentTask.Status == types.DownloadPaused
				m.mu.RUnlock()
				if !paused {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}

			segPath := filepath.Join(segDir, fmt.Sprintf("%05d.ts", seg.index))
			err := m.downloadSegment(seg, segPath, task.Headers)
			if err != nil {
				// 重试一次
				time.Sleep(time.Second)
				err = m.downloadSegment(seg, segPath, task.Headers)
			}

			mu.Lock()
			if err != nil {
				failed++
			} else {
				completed++
			}
			task.SegmentsDone = completed
			task.Downloaded = int64(completed) * 1024 * 1024 // 估算
			m.notify(task)
			mu.Unlock()
		}(seg)
	}

	wg.Wait()

	if failed > 0 && completed == 0 {
		task.Status = types.DownloadFailed
		task.Error = fmt.Sprintf("all %d segments failed", failed)
		m.notify(task)
		return
	}

	// 合并分片
	task.Status = types.DownloadRunning
	m.notify(task)

	outputPath := task.SavePath
	if !strings.HasSuffix(outputPath, ".mp4") && !strings.HasSuffix(outputPath, ".ts") {
		outputPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".ts"
		task.SavePath = outputPath
	}

	err = m.mergeSegments(segDir, outputPath, len(playlist.segments))
	if err != nil {
		task.Status = types.DownloadFailed
		task.Error = "merge: " + err.Error()
		m.notify(task)
		return
	}

	task.Status = types.DownloadCompleted
	task.SegmentsDone = completed
	task.Downloaded = task.TotalSize
	task.Speed = 0
	m.notify(task)
}

// parseM3U8 解析 m3u8 播放列表
func (m *Manager) parseM3U8(rawURL string, headers map[string]string) (*m3u8Playlist, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	baseURL, _ := url.Parse(rawURL)
	playlist := &m3u8Playlist{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var currentSeg *m3u8Segment
	segIndex := 0
	variants := make([]struct {
		bandwidth int
		url       string
	}, 0)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#EXTM3U") {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-VERSION:") {
			fmt.Sscanf(line, "#EXT-X-VERSION:%d", &playlist.version)
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			fmt.Sscanf(line, "#EXT-X-TARGETDURATION:%d", &playlist.targetDuration)
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			fmt.Sscanf(line, "#EXT-X-MEDIA-SEQUENCE:%d", &playlist.mediaSequence)
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
			if strings.Contains(line, "VOD") {
				playlist.isLive = false
			} else if strings.Contains(line, "EVENT") {
				playlist.isLive = false
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-ENDLIST") {
			playlist.isLive = false
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			// 解析加密密钥
			parts := parseAttributes(line[len("#EXT-X-KEY:"):])
			if method, ok := parts["METHOD"]; ok && method != "NONE" {
				if uri, ok := parts["URI"]; ok {
					keyURL := resolveURL(baseURL, uri)
					playlist.keyURI = keyURL
					if currentSeg != nil {
						currentSeg.keyURI = keyURL
					}
				}
				if iv, ok := parts["IV"]; ok {
					playlist.iv = parseIV(iv)
					if currentSeg != nil {
						currentSeg.iv = playlist.iv
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			durationStr := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.Index(durationStr, ","); idx >= 0 {
				durationStr = durationStr[:idx]
			}
			duration, _ := strconv.ParseFloat(durationStr, 64)
			currentSeg = &m3u8Segment{
				index:    segIndex,
				duration: duration,
				keyURI:   playlist.keyURI,
				iv:       playlist.iv,
			}
			segIndex++
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			// Master playlist
			parts := parseAttributes(line[len("#EXT-X-STREAM-INF:"):])
			bandwidth := 0
			if b, ok := parts["BANDWIDTH"]; ok {
				bandwidth, _ = strconv.Atoi(b)
			}
			// 下一行是 URL
			if scanner.Scan() {
				variantURL := resolveURL(baseURL, strings.TrimSpace(scanner.Text()))
				variants = append(variants, struct {
					bandwidth int
					url       string
				}{bandwidth, variantURL})
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}

		// 这是一个分片 URL
		if currentSeg != nil {
			currentSeg.uri = resolveURL(baseURL, line)
			playlist.segments = append(playlist.segments, currentSeg)
			currentSeg = nil
		}
	}

	// 如果是 master playlist，选择最高带宽
	if len(variants) > 0 {
		sort.Slice(variants, func(i, j int) bool {
			return variants[i].bandwidth > variants[j].bandwidth
		})
		playlist.masterURL = variants[0].url
	}

	return playlist, nil
}

// downloadSegment 下载单个分片
func (m *Manager) downloadSegment(seg *m3u8Segment, savePath string, headers map[string]string) error {
	// 如果已下载，跳过
	if _, err := os.Stat(savePath); err == nil {
		return nil
	}

	req, err := http.NewRequest("GET", seg.uri, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 解密（如果需要）
	if seg.keyURI != "" {
		key, err := m.downloadKey(seg.keyURI, headers)
		if err != nil {
			return err
		}
		iv := seg.iv
		if iv == nil {
			iv = make([]byte, 16)
			// 使用分片序号作为 IV
			seqBytes := make([]byte, 4)
			seqBytes[0] = byte(seg.index >> 24)
			seqBytes[1] = byte(seg.index >> 16)
			seqBytes[2] = byte(seg.index >> 8)
			seqBytes[3] = byte(seg.index)
			copy(iv[12:], seqBytes)
		}
		data, err = decryptAES128(data, key, iv)
		if err != nil {
			return err
		}
	}

	return os.WriteFile(savePath, data, 0644)
}

// downloadKey 下载解密密钥
func (m *Manager) downloadKey(keyURI string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", keyURI, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// decryptAES128 AES-128-CBC 解密
func decryptAES128(data, key, iv []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("invalid key length: %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		// 尝试填充
		padding := aes.BlockSize - len(data)%aes.BlockSize
		if padding != aes.BlockSize {
			data = append(data, make([]byte, padding)...)
		}
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted := make([]byte, len(data))
	mode.CryptBlocks(decrypted, data)

	// 去除 PKCS7 填充
	if len(decrypted) > 0 {
		padding := int(decrypted[len(decrypted)-1])
		if padding > 0 && padding <= aes.BlockSize {
			decrypted = decrypted[:len(decrypted)-padding]
		}
	}
	return decrypted, nil
}

// mergeSegments 合并所有分片
func (m *Manager) mergeSegments(segDir, outputPath string, total int) error {
	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	writer := bufio.NewWriter(out)
	defer writer.Flush()

	for i := 0; i < total; i++ {
		segPath := filepath.Join(segDir, fmt.Sprintf("%05d.ts", i))
		data, err := os.ReadFile(segPath)
		if err != nil {
			// 跳过缺失的分片
			continue
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}

	return nil
}

// parseAttributes 解析属性字符串
func parseAttributes(s string) map[string]string {
	result := make(map[string]string)
	var key, val string
	inQuote := false
	current := &key

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == '=' && !inQuote:
			current = &val
		case c == ',' && !inQuote:
			result[key] = val
			key, val = "", ""
			current = &key
		default:
			*current += string(c)
		}
	}
	if key != "" {
		result[key] = val
	}
	return result
}

// resolveURL 解析相对 URL
func resolveURL(base *url.URL, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(refURL).String()
}

// parseIV 解析 IV
func parseIV(iv string) []byte {
	iv = strings.TrimPrefix(iv, "0x")
	result := make([]byte, 16)
	for i := 0; i < len(iv) && i < 32; i += 2 {
		if i+1 < len(iv) {
			val, _ := strconv.ParseUint(iv[i:i+2], 16, 8)
			result[i/2] = byte(val)
		}
	}
	return result
}

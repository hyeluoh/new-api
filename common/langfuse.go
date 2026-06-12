package common

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"

	"github.com/bytedance/gopkg/util/gopool"
	langfuse "github.com/git-hulk/langfuse-go"
	"github.com/git-hulk/langfuse-go/pkg/traces"
)

// LangfuseConfig 存储 Langfuse 连接配置
type LangfuseConfig struct {
	PublicKey string
	SecretKey string
	Host      string
}

// LangfuseTraceData 存储需要记录到 Langfuse 的请求元数据
type LangfuseTraceData struct {
	RequestID        string
	UserID           int
	TokenName        string
	ModelName        string
	ChannelID        int
	Group            string
	IsStream         bool
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	UseTimeMs        int64
	Quota            int64
	Success          bool
	StatusCode       int
	ErrorMessage     string
}

// langfuseManager 管理 Langfuse 客户端实例的缓存池
type langfuseManager struct {
	mu      sync.RWMutex
	clients map[string]*langfuse.Langfuse
}

var langfuseManagerInstance *langfuseManager
var langfuseOnce sync.Once

// GetLangfuseManager 获取 Langfuse 管理器单例
func GetLangfuseManager() *langfuseManager {
	langfuseOnce.Do(func() {
		langfuseManagerInstance = &langfuseManager{
			clients: make(map[string]*langfuse.Langfuse),
		}
	})
	return langfuseManagerInstance
}

// clientKey 生成客户端缓存 key
func clientKey(publicKey, secretKey, host string) string {
	h := sha256.New()
	h.Write([]byte(publicKey + secretKey + host))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// GetClient 获取或创建 Langfuse 客户端
func (m *langfuseManager) GetClient(publicKey, secretKey, host string) (*langfuse.Langfuse, error) {
	key := clientKey(publicKey, secretKey, host)

	m.mu.RLock()
	if client, ok := m.clients[key]; ok {
		m.mu.RUnlock()
		return client, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[key]; ok {
		return client, nil
	}

	client := langfuse.NewClient(host, publicKey, secretKey)
	m.clients[key] = client
	return client, nil
}

// RecordTrace 异步记录请求元数据到 Langfuse
func RecordTrace(config LangfuseConfig, data LangfuseTraceData) {
	if config.PublicKey == "" || config.SecretKey == "" || config.Host == "" {
		return
	}

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				SysLog(fmt.Sprintf("langfuse RecordTrace panic: %v", r))
			}
		}()

		mgr := GetLangfuseManager()
		client, err := mgr.GetClient(config.PublicKey, config.SecretKey, config.Host)
		if err != nil {
			SysLog(fmt.Sprintf("langfuse GetClient error: %v", err))
			return
		}

		ctx := context.Background()

		tags := []string{data.ModelName}
		if data.IsStream {
			tags = append(tags, "stream")
		} else {
			tags = append(tags, "non-stream")
		}
		if data.Success {
			tags = append(tags, "success")
		} else {
			tags = append(tags, "error")
		}

		traceName := data.ModelName
		if traceName == "" {
			traceName = "unknown-model"
		}

		trace := client.StartTrace(ctx, traceName)
		trace.UserID = strconv.Itoa(data.UserID)
		trace.Metadata = map[string]interface{}{
			"token_name": data.TokenName,
			"channel_id": data.ChannelID,
			"group":      data.Group,
			"request_id": data.RequestID,
		}
		trace.Tags = tags

		generation := trace.StartGeneration("completion")
		generation.Model = data.ModelName
		generation.Usage = traces.Usage{
			Input:  data.PromptTokens,
			Output: data.CompletionTokens,
			Total:  data.TotalTokens,
			Unit:   traces.UnitTokens,
		}
		generation.Metadata = map[string]interface{}{
			"latency_ms": data.UseTimeMs,
			"quota_cost": data.Quota,
		}

		if !data.Success {
			generation.StatusMessage = data.ErrorMessage
		}

		generation.End()

		trace.Output = map[string]interface{}{
			"success":     data.Success,
			"status_code": data.StatusCode,
		}

		trace.End()
		client.Flush()
	})
}

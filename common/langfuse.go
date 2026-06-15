package common

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"
	"time"

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
	UserName         string
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
	mu         sync.RWMutex
	clients    map[string]*langfuse.Langfuse
	lastUsed   map[string]time.Time
	maxClients int
}

var langfuseManagerInstance *langfuseManager
var langfuseOnce sync.Once

const defaultMaxLangfuseClients = 128

// GetLangfuseManager 获取 Langfuse 管理器单例
func GetLangfuseManager() *langfuseManager {
	langfuseOnce.Do(func() {
		langfuseManagerInstance = &langfuseManager{
			clients:    make(map[string]*langfuse.Langfuse),
			lastUsed:   make(map[string]time.Time),
			maxClients: defaultMaxLangfuseClients,
		}
	})
	return langfuseManagerInstance
}

// clientKey 生成客户端缓存 key
func clientKey(publicKey, secretKey, host string) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%d:%s|%d:%s|%d:%s", len(publicKey), publicKey, len(secretKey), secretKey, len(host), host)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// evictOldestClient 清理最久未使用的客户端，避免缓存无限增长
func (m *langfuseManager) evictOldestClient() {
	if len(m.clients) < m.maxClients {
		return
	}

	var oldestKey string
	var oldestTime time.Time
	for key, t := range m.lastUsed {
		if oldestTime.IsZero() || t.Before(oldestTime) {
			oldestTime = t
			oldestKey = key
		}
	}
	if oldestKey != "" {
		delete(m.clients, oldestKey)
		delete(m.lastUsed, oldestKey)
	}
}

// GetClient 获取或创建 Langfuse 客户端
func (m *langfuseManager) GetClient(publicKey, secretKey, host string) *langfuse.Langfuse {
	key := clientKey(publicKey, secretKey, host)

	m.mu.RLock()
	if client, ok := m.clients[key]; ok {
		m.mu.RUnlock()
		m.mu.Lock()
		m.lastUsed[key] = time.Now()
		m.mu.Unlock()
		return client
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[key]; ok {
		m.lastUsed[key] = time.Now()
		return client
	}

	m.evictOldestClient()

	client := langfuse.NewClient(host, publicKey, secretKey)
	if client == nil {
		return nil
	}
	m.clients[key] = client
	m.lastUsed[key] = time.Now()
	return client
}

// RecordTrace 异步记录请求元数据到 Langfuse
func RecordTrace(config LangfuseConfig, data LangfuseTraceData) {
	if config.PublicKey == "" || config.SecretKey == "" || config.Host == "" {
		return
	}

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				SysLog("langfuse RecordTrace panic recovered")
			}
		}()

		mgr := GetLangfuseManager()
		client := mgr.GetClient(config.PublicKey, config.SecretKey, config.Host)
		if client == nil {
			SysLog("langfuse GetClient returned nil")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

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
		// Langfuse trace 顶层仅 userId 一个用户槽，把 username 放顶层；
		// username 为空时 fallback 到数字 id，保证顶层始终有用户标识
		userID := data.UserName
		if userID == "" {
			userID = strconv.Itoa(data.UserID)
		}
		trace.UserID = userID
		trace.Metadata = map[string]interface{}{
			"user_id":    data.UserID,
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

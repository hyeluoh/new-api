package common

import (
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

// RecordLangfuseTraceFromContext 从 gin context 提取 Langfuse 配置并记录 trace
func RecordLangfuseTraceFromContext(c *gin.Context, modelName string, promptTokens int, completionTokens int, totalTokens int, quota int64, success bool, statusCode int, errMsg string) {
	publicKey := GetContextKeyString(c, constant.ContextKeyLangfusePublicKey)
	secretKey := GetContextKeyString(c, constant.ContextKeyLangfuseSecretKey)
	host := GetContextKeyString(c, constant.ContextKeyLangfuseHost)

	if publicKey == "" || secretKey == "" || host == "" {
		return
	}

	startTime := GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	useTimeMs := time.Since(startTime).Milliseconds()

	RecordTrace(
		LangfuseConfig{
			PublicKey: publicKey,
			SecretKey: secretKey,
			Host:      host,
		},
		LangfuseTraceData{
			RequestID:        GetContextKeyString(c, RequestIdKey),
			UserID:           GetContextKeyInt(c, constant.ContextKeyUserId),
			TokenName:        c.GetString("token_name"),
			ModelName:        modelName,
			ChannelID:        GetContextKeyInt(c, constant.ContextKeyChannelId),
			Group:            GetContextKeyString(c, constant.ContextKeyUsingGroup),
			IsStream:         GetContextKeyBool(c, constant.ContextKeyIsStream),
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			UseTimeMs:        useTimeMs,
			Quota:            quota,
			Success:          success,
			StatusCode:       statusCode,
			ErrorMessage:     errMsg,
		},
	)
}

// RecordLangfuseErrorTrace 从 gin context 记录错误 trace
func RecordLangfuseErrorTrace(c *gin.Context, modelName string, statusCode int, errMsg string) {
	publicKey := GetContextKeyString(c, constant.ContextKeyLangfusePublicKey)
	secretKey := GetContextKeyString(c, constant.ContextKeyLangfuseSecretKey)
	host := GetContextKeyString(c, constant.ContextKeyLangfuseHost)

	if publicKey == "" || secretKey == "" || host == "" {
		return
	}

	startTime := GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	useTimeMs := time.Since(startTime).Milliseconds()

	RecordTrace(
		LangfuseConfig{
			PublicKey: publicKey,
			SecretKey: secretKey,
			Host:      host,
		},
		LangfuseTraceData{
			RequestID:    GetContextKeyString(c, RequestIdKey),
			UserID:       GetContextKeyInt(c, constant.ContextKeyUserId),
			TokenName:    c.GetString("token_name"),
			ModelName:    modelName,
			ChannelID:    GetContextKeyInt(c, constant.ContextKeyChannelId),
			Group:        GetContextKeyString(c, constant.ContextKeyUsingGroup),
			UseTimeMs:    useTimeMs,
			Success:      false,
			StatusCode:   statusCode,
			ErrorMessage: errMsg,
		},
	)
}

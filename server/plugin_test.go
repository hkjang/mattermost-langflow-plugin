package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBotDefinitions(t *testing.T) {
	bots, err := parseBotDefinitions(`[
		{"id":"support","username":"support-bot","display_name":"Support","flow_id":"support","input_schema":[{"name":"tone","type":"text"}]},
		{"id":"summary","username":"summary-bot","display_name":"Thread Summary","flow_id":"thread-summary"}
	]`)
	require.NoError(t, err)
	require.Len(t, bots, 2)
	require.Equal(t, "support", bots[0].ID)
	require.Equal(t, "summary-bot", bots[1].Username)
	require.Equal(t, "thread-summary", bots[1].FlowID)
}

func TestParseBotDefinitionsAutoAssignsIDFromUsername(t *testing.T) {
	bots, err := parseBotDefinitions(`[
		{"username":"summary-bot","display_name":"Thread Summary","flow_id":"thread-summary"}
	]`)
	require.NoError(t, err)
	require.Len(t, bots, 1)
	require.Equal(t, "summary-bot", bots[0].ID)
}

func TestConfigurationGetStoredPluginConfigFromLegacy(t *testing.T) {
	cfg := &configuration{
		LangflowBaseURL:       "https://langflow.example.com",
		LangflowAuthMode:      "bearer",
		LangflowAuthToken:     "secret",
		AllowHosts:            "langflow.example.com",
		BotDefinitions:        `[{"username":"summary-bot","display_name":"Thread Summary","flow_id":"thread-summary"}]`,
		DefaultTimeoutSeconds: "45",
		StreamingUpdateMS:     "700",
		MaxInputLength:        "5000",
		MaxOutputLength:       "9000",
		ContextPostLimit:      "12",
		EnableStreaming:       true,
		EnableDebugLogs:       true,
		EnableUsageLogs:       true,
	}

	stored, source, err := cfg.getStoredPluginConfig()
	require.NoError(t, err)
	require.Equal(t, "legacy", source)
	require.Equal(t, "https://langflow.example.com", stored.Service.BaseURL)
	require.Equal(t, "secret", stored.Service.AuthToken)
	require.Equal(t, 45, stored.Runtime.DefaultTimeoutSeconds)
	require.Len(t, stored.Bots, 1)
	require.Equal(t, "summary-bot", stored.Bots[0].ID)
}

func TestConfigurationNormalizeFromConfig(t *testing.T) {
	cfg := &configuration{
		Config: `{
			"service": {
				"base_url": "https://langflow.example.com",
				"auth_mode": "x-api-key",
				"auth_token": "secret"
			},
			"runtime": {
				"default_timeout_seconds": 55,
				"enable_streaming": true,
				"streaming_update_ms": 900,
				"max_input_length": 5000,
				"max_output_length": 9000,
				"context_post_limit": 12,
				"enable_debug_logs": true,
				"enable_usage_logs": false
			},
			"bots": [
				{"username":"summary-bot","display_name":"Thread Summary","flow_id":"thread-summary"}
			]
		}`,
	}

	runtimeCfg, err := cfg.normalize()
	require.NoError(t, err)
	require.Equal(t, "https://langflow.example.com", runtimeCfg.LangflowBaseURL)
	require.Equal(t, "x-api-key", runtimeCfg.LangflowAuthMode)
	require.Equal(t, "secret", runtimeCfg.LangflowAuthToken)
	require.True(t, runtimeCfg.EnableStreaming)
	require.Equal(t, 55, int(runtimeCfg.DefaultTimeout.Seconds()))
	require.Equal(t, int64(900), runtimeCfg.StreamingUpdateInterval.Milliseconds())
	require.False(t, runtimeCfg.EnableUsageLogs)
	require.Len(t, runtimeCfg.BotDefinitions, 1)
	require.Equal(t, "summary-bot", runtimeCfg.BotDefinitions[0].ID)
}

func TestExtractLangflowText(t *testing.T) {
	body := []byte(`{"outputs":[{"outputs":[{"results":{"message":{"text":"Hello from Langflow"}}}]}]}`)
	require.Equal(t, "Hello from Langflow", extractLangflowText(body))
}

func TestParseLangflowStreamJSONLine(t *testing.T) {
	parser := langflowStreamParser{}

	event, err := parser.parseLine(`{"event":"token","data":{"chunk":"Hello"}}`)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "token", event.Event)
	require.Equal(t, "Hello", extractLangflowStreamChunk(event.Data))
}

func TestParseLangflowStreamSSELine(t *testing.T) {
	parser := langflowStreamParser{}

	event, err := parser.parseLine(`event: token`)
	require.NoError(t, err)
	require.Nil(t, event)

	event, err = parser.parseLine(`data: {"data":{"chunk":"World"}}`)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "token", event.Event)
	require.Equal(t, "World", extractLangflowStreamChunk(event.Data))
}

func TestParseLangflowStreamFallbackJSONPayload(t *testing.T) {
	parser := langflowStreamParser{}

	event, err := parser.parseLine(`{"outputs":[{"outputs":[{"results":{"message":{"text":"Hello from fallback"}}}]}]}`)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "end", event.Event)
	require.Equal(t, "Hello from fallback", extractLangflowTextFromValue(event.Data))
}

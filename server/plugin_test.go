package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/url"
	"strings"
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

	event, _, err := parser.readEvent(bufio.NewReader(strings.NewReader(`{"event":"token","data":{"chunk":"Hello"}}`)))
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "token", event.Event)
	require.Equal(t, "Hello", extractLangflowStreamChunk(event.Data))
}

func TestParseLangflowStreamSSELine(t *testing.T) {
	parser := langflowStreamParser{}

	event, _, err := parser.readEvent(bufio.NewReader(strings.NewReader("event: token\ndata: {\"data\":{\"chunk\":\"World\"}}\n\n")))
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "token", event.Event)
	require.Equal(t, "World", extractLangflowStreamChunk(event.Data))
}

func TestParseLangflowStreamFallbackJSONPayload(t *testing.T) {
	parser := langflowStreamParser{}

	event, _, err := parser.readEvent(bufio.NewReader(strings.NewReader(`{"outputs":[{"outputs":[{"results":{"message":{"text":"Hello from fallback"}}}]}]}`)))
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "end", event.Event)
	require.Equal(t, "Hello from fallback", extractLangflowTextFromValue(event.Data))
}

func TestParseLangflowStreamMultiLineData(t *testing.T) {
	parser := langflowStreamParser{}

	stream := "event: token\ndata: 첫 번째 줄\ndata: 두 번째 줄\n\n"
	event, _, err := parser.readEvent(bufio.NewReader(strings.NewReader(stream)))
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "token", event.Event)
	require.Equal(t, "첫 번째 줄\n두 번째 줄", extractLangflowStreamChunk(event.Data))
}

func TestParseLangflowStreamFlushesOnEOF(t *testing.T) {
	parser := langflowStreamParser{}

	stream := "event: token\ndata: {\"data\":{\"chunk\":\"EOF chunk\"}}"
	event, _, err := parser.readEvent(bufio.NewReader(strings.NewReader(stream)))
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "token", event.Event)
	require.Equal(t, "EOF chunk", extractLangflowStreamChunk(event.Data))
}

func TestParseLangflowStreamRawChunkDefaultsToToken(t *testing.T) {
	parser := langflowStreamParser{}

	event, _, err := parser.readEvent(bufio.NewReader(strings.NewReader(`{"chunk":"Hello"}`)))
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "token", event.Event)
	require.Equal(t, "Hello", extractLangflowStreamChunk(event.Data))
}

func TestMergeLangflowStreamOutputPrefersNewestSnapshot(t *testing.T) {
	require.Equal(t, "Hello", mergeLangflowStreamOutput("", "Hello"))
	require.Equal(t, "Hello world", mergeLangflowStreamOutput("Hello", "Hello world"))
	require.Equal(t, "Hello world", mergeLangflowStreamOutput("Hello", " world"))
}

func TestStripLeadingLangflowLabel(t *testing.T) {
	require.Equal(t, "실제 응답", stripLeadingLangflowLabel("### Langflow\n\n실제 응답"))
	require.Equal(t, "실제 응답", stripLeadingLangflowLabel("langflow\n실제 응답"))
	require.Equal(t, "실제 응답", stripLeadingLangflowLabel("**Langflow**\n실제 응답"))
}

func TestBuildLangflowTweaksIncludesMattermostUserContext(t *testing.T) {
	tweaks := buildLangflowTweaks(BotRunRequest{
		UserID:   "user-id",
		UserName: "alice",
		Inputs: map[string]any{
			"priority": "high",
		},
	})

	require.Equal(t, "user-id", tweaks["mattermost_user_id"])
	require.Equal(t, "alice", tweaks["mattermost_user_name"])
	require.Equal(t, "high", tweaks["priority"])
}

func TestNewLangflowRunRequestIncludesChatFields(t *testing.T) {
	parsedURL, err := url.Parse("https://langflow.example.com")
	require.NoError(t, err)

	plugin := &Plugin{}
	cfg := &runtimeConfiguration{
		ParsedBaseURL:     parsedURL,
		LangflowAuthMode:  "bearer",
		LangflowAuthToken: "secret",
	}

	request, err := plugin.newLangflowRunRequest(
		context.Background(),
		cfg,
		BotDefinition{FlowID: "support-flow"},
		"사용자 메시지 내용",
		BotRunRequest{
			UserID:   "mm-user-id",
			UserName: "alice",
			Inputs: map[string]any{
				"tone": "friendly",
			},
		},
		"session-123",
		"corr-123",
		true,
	)
	require.NoError(t, err)

	var payload langflowRunPayload
	err = json.NewDecoder(request.Body).Decode(&payload)
	require.NoError(t, err)
	require.Equal(t, "사용자 메시지 내용", payload.InputValue)
	require.Equal(t, "chat", payload.OutputType)
	require.Equal(t, "chat", payload.InputType)
	require.Equal(t, "User", payload.Sender)
	require.Equal(t, "alice", payload.SenderName)
	require.Equal(t, "session-123", payload.SessionID)
	require.Equal(t, "friendly", payload.Tweaks["tone"])
	require.Equal(t, "mm-user-id", payload.Tweaks["mattermost_user_id"])
	require.Equal(t, "alice", payload.Tweaks["mattermost_user_name"])
}

func TestBuildLangflowRunURLPreservesSubpath(t *testing.T) {
	testCases := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{
			name:     "instance root",
			baseURL:  "https://langflow.example.com",
			expected: "https://langflow.example.com/api/v1/run/support-assistant?stream=true",
		},
		{
			name:     "mounted subpath",
			baseURL:  "https://langflow.example.com/langflow",
			expected: "https://langflow.example.com/langflow/api/v1/run/support-assistant?stream=true",
		},
		{
			name:     "api root",
			baseURL:  "https://langflow.example.com/langflow/api",
			expected: "https://langflow.example.com/langflow/api/v1/run/support-assistant?stream=true",
		},
		{
			name:     "api v1 root",
			baseURL:  "https://langflow.example.com/langflow/api/v1",
			expected: "https://langflow.example.com/langflow/api/v1/run/support-assistant?stream=true",
		},
		{
			name:     "full run endpoint",
			baseURL:  "https://langflow.example.com/langflow/api/v1/run/thread-summary",
			expected: "https://langflow.example.com/langflow/api/v1/run/support-assistant?stream=true",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parsedURL, err := url.Parse(testCase.baseURL)
			require.NoError(t, err)

			endpointURL, err := buildLangflowRunURL(parsedURL, "support-assistant", true)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, endpointURL.String())
		})
	}
}

func TestBuildLangflowHealthURLs(t *testing.T) {
	parsedURL, err := url.Parse("https://langflow.example.com/langflow/api")
	require.NoError(t, err)

	targets := buildLangflowHealthURLs(parsedURL)
	require.Len(t, targets, 2)
	require.Equal(t, "https://langflow.example.com/langflow/api/v1/health", targets[0].String())
	require.Equal(t, "https://langflow.example.com/langflow/health", targets[1].String())
}

func TestLooksLikeHTMLResponse(t *testing.T) {
	body := []byte("<!doctype html><html><body><noscript>You need to enable JavaScript to run this app.</noscript></body></html>")

	require.True(t, looksLikeHTMLResponse("text/html; charset=utf-8", body))
	require.True(t, looksLikeHTMLResponse("", body))
	require.False(t, looksLikeHTMLResponse("application/json", []byte(`{"result":"ok"}`)))
}

func TestClassifyLangflowHTTPErrorUnauthorized(t *testing.T) {
	err := classifyLangflowHTTPError(
		"https://langflow.example.com/api/v1/run/support",
		401,
		nil,
		[]byte(`{"detail":"invalid token"}`),
	)

	require.Equal(t, "auth_failed", err.Code)
	require.Equal(t, "Langflow 인증에 실패했습니다.", err.Summary)
	require.Contains(t, err.Detail, "invalid token")
	require.False(t, err.Retryable)
}

func TestClassifyLangflowRequestErrorTimeout(t *testing.T) {
	err := classifyLangflowRequestError(
		"https://langflow.example.com/api/v1/run/support",
		context.DeadlineExceeded,
	)

	require.Equal(t, "network_timeout", err.Code)
	require.True(t, err.Retryable)
	require.Contains(t, err.Error(), "시간 초과")
}

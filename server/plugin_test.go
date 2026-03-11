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

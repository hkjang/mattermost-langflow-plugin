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

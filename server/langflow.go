package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type langflowRunPayload struct {
	InputValue string `json:"input_value"`
	SessionID  string `json:"session_id,omitempty"`
}

type langflowConnectionStatus struct {
	OK         bool   `json:"ok"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
}

type langflowStreamEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type langflowStreamParser struct {
	pendingEvent string
}

func (p *Plugin) invokeLangflow(ctx context.Context, cfg *runtimeConfiguration, bot BotDefinition, prompt, sessionID, correlationID string) (string, int, error) {
	request, err := p.newLangflowRunRequest(ctx, cfg, bot, prompt, sessionID, correlationID, false)
	if err != nil {
		return "", 0, err
	}

	client := &http.Client{Timeout: cfg.DefaultTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("failed to contact Langflow: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, int64(cfg.MaxOutputLength*4)))
	if err != nil {
		return "", response.StatusCode, fmt.Errorf("failed to read Langflow response: %w", err)
	}

	if response.StatusCode >= http.StatusBadRequest {
		return "", response.StatusCode, fmt.Errorf("Langflow returned %d: %s", response.StatusCode, summarizeErrorBody(responseBody))
	}

	output := extractLangflowText(responseBody)
	if output == "" {
		output = strings.TrimSpace(string(responseBody))
	}
	return truncateString(output, cfg.MaxOutputLength), response.StatusCode, nil
}

func (p *Plugin) invokeLangflowStream(
	ctx context.Context,
	cfg *runtimeConfiguration,
	bot BotDefinition,
	prompt, sessionID, correlationID string,
	onUpdate func(string, bool),
) (string, int, error) {
	request, err := p.newLangflowRunRequest(ctx, cfg, bot, prompt, sessionID, correlationID, true)
	if err != nil {
		return "", 0, err
	}

	client := &http.Client{Timeout: cfg.DefaultTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("failed to contact Langflow: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, int64(cfg.MaxOutputLength*4)))
		if readErr != nil {
			return "", response.StatusCode, fmt.Errorf("Langflow returned %d and the error body could not be read: %w", response.StatusCode, readErr)
		}
		return "", response.StatusCode, fmt.Errorf("Langflow returned %d: %s", response.StatusCode, summarizeErrorBody(responseBody))
	}

	parser := langflowStreamParser{}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 32*1024), maxInt(cfg.MaxOutputLength*8, 512*1024))

	var fallback bytes.Buffer
	var streamOutput strings.Builder
	finalOutput := ""

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			fallback.WriteString(trimmed)
			fallback.WriteByte('\n')
		}

		event, parseErr := parser.parseLine(line)
		if parseErr != nil {
			if cfg.EnableDebugLogs {
				p.API.LogDebug("Ignoring Langflow stream line that could not be parsed", "error", parseErr, "correlation_id", correlationID)
			}
			continue
		}
		if event == nil {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(event.Event)) {
		case "token":
			chunk := extractLangflowStreamChunk(event.Data)
			if chunk == "" {
				continue
			}
			streamOutput.WriteString(chunk)
			if onUpdate != nil {
				onUpdate(truncateString(streamOutput.String(), cfg.MaxOutputLength), false)
			}
		case "error":
			message := extractLangflowTextFromValue(event.Data)
			if message == "" {
				message = "Langflow streaming request failed"
			}
			return "", response.StatusCode, fmt.Errorf("%s", message)
		case "end":
			if candidate := extractLangflowTextFromValue(event.Data); candidate != "" {
				finalOutput = candidate
			}
		default:
			if finalOutput == "" {
				finalOutput = extractAssistantMessageText(*event)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", response.StatusCode, fmt.Errorf("failed to read Langflow stream: %w", err)
	}

	output := strings.TrimSpace(finalOutput)
	if output == "" {
		output = strings.TrimSpace(streamOutput.String())
	}
	if output == "" && fallback.Len() > 0 {
		output = extractLangflowText(fallback.Bytes())
	}
	output = truncateString(output, cfg.MaxOutputLength)
	if output == "" {
		return "", response.StatusCode, fmt.Errorf("Langflow streaming response did not contain any text")
	}

	if onUpdate != nil {
		onUpdate(output, true)
	}

	return output, response.StatusCode, nil
}

func (p *Plugin) newLangflowRunRequest(
	ctx context.Context,
	cfg *runtimeConfiguration,
	bot BotDefinition,
	prompt, sessionID, correlationID string,
	stream bool,
) (*http.Request, error) {
	if cfg.ParsedBaseURL == nil {
		return nil, fmt.Errorf("Langflow base URL is not configured")
	}
	if !hostAllowed(cfg.ParsedBaseURL.Hostname(), cfg.AllowHosts) {
		return nil, fmt.Errorf("Langflow host %q is not allowed by configuration", cfg.ParsedBaseURL.Hostname())
	}

	payload := langflowRunPayload{
		InputValue: prompt,
		SessionID:  sessionID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Langflow payload: %w", err)
	}

	endpointURL, err := buildLangflowRunURL(cfg.ParsedBaseURL, bot.FlowID, stream)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build Langflow request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Correlation-ID", correlationID)
	if stream {
		request.Header.Set("Accept", "text/event-stream, application/json")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	p.applyAuthHeader(request, cfg)

	return request, nil
}

func buildLangflowRunURL(baseURL *url.URL, flowID string, stream bool) (*url.URL, error) {
	endpointURL, err := baseURL.Parse("/api/v1/run/" + url.PathEscape(flowID))
	if err != nil {
		return nil, fmt.Errorf("failed to build Langflow endpoint: %w", err)
	}
	if stream {
		query := endpointURL.Query()
		query.Set("stream", "true")
		endpointURL.RawQuery = query.Encode()
	}
	return endpointURL, nil
}

func (p *Plugin) testLangflowConnection(ctx context.Context, cfg *runtimeConfiguration) (*langflowConnectionStatus, error) {
	if cfg.ParsedBaseURL == nil {
		return &langflowConnectionStatus{OK: false, Message: "Langflow base URL is not configured"}, nil
	}
	if !hostAllowed(cfg.ParsedBaseURL.Hostname(), cfg.AllowHosts) {
		return &langflowConnectionStatus{
			OK:      false,
			URL:     cfg.ParsedBaseURL.String(),
			Message: fmt.Sprintf("host %q is not allowlisted", cfg.ParsedBaseURL.Hostname()),
		}, nil
	}

	candidates := []string{"/health", "/api/v1/health"}
	client := &http.Client{Timeout: minDuration(cfg.DefaultTimeout, 10*time.Second)}
	for _, candidate := range candidates {
		target, err := cfg.ParsedBaseURL.Parse(candidate)
		if err != nil {
			return nil, fmt.Errorf("failed to build health URL: %w", err)
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create health request: %w", err)
		}
		p.applyAuthHeader(request, cfg)

		response, err := client.Do(request)
		if err != nil {
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		response.Body.Close()

		status := &langflowConnectionStatus{
			OK:         response.StatusCode < http.StatusBadRequest,
			URL:        target.String(),
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
		if status.OK && status.Message == "" {
			status.Message = "Connection succeeded"
		}
		return status, nil
	}

	return &langflowConnectionStatus{
		OK:      false,
		URL:     cfg.ParsedBaseURL.String(),
		Message: "Unable to reach Langflow health endpoints",
	}, nil
}

func (p *Plugin) applyAuthHeader(request *http.Request, cfg *runtimeConfiguration) {
	if cfg.LangflowAuthToken == "" {
		return
	}
	if cfg.LangflowAuthMode == "x-api-key" {
		request.Header.Set("x-api-key", cfg.LangflowAuthToken)
		return
	}
	request.Header.Set("Authorization", "Bearer "+cfg.LangflowAuthToken)
}

func hostAllowed(host string, allowHosts []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if len(allowHosts) == 0 {
		return true
	}
	for _, pattern := range allowHosts {
		if pattern == host {
			return true
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) {
				return true
			}
		}
	}
	return false
}

func summarizeErrorBody(body []byte) string {
	text := extractLangflowText(body)
	if text != "" {
		return truncateString(text, 280)
	}
	return truncateString(strings.TrimSpace(string(body)), 280)
}

func extractLangflowText(body []byte) string {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body))
	}

	text := extractLangflowTextFromValue(payload)
	if text != "" {
		return text
	}

	pretty, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(pretty)
}

func extractLangflowTextFromValue(value any) string {
	candidates := make([]string, 0, 8)
	collectTextCandidates(value, &candidates)

	best := ""
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if len(candidate) > len(best) {
			best = candidate
		}
	}

	return best
}

func collectTextCandidates(value any, candidates *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			lowerKey := strings.ToLower(key)
			if isLikelyTextKey(lowerKey) {
				switch nestedValue := nested.(type) {
				case string:
					*candidates = append(*candidates, nestedValue)
				case map[string]any, []any:
					collectTextCandidates(nestedValue, candidates)
				}
				continue
			}
			collectTextCandidates(nested, candidates)
		}
	case []any:
		for _, item := range typed {
			collectTextCandidates(item, candidates)
		}
	case string:
		if strings.TrimSpace(typed) != "" {
			*candidates = append(*candidates, typed)
		}
	}
}

func isLikelyTextKey(key string) bool {
	return strings.Contains(key, "text") ||
		strings.Contains(key, "message") ||
		strings.Contains(key, "output") ||
		strings.Contains(key, "result") ||
		strings.Contains(key, "content") ||
		strings.Contains(key, "response") ||
		strings.Contains(key, "chunk")
}

func (p *langflowStreamParser) parseLine(line string) (*langflowStreamEvent, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		p.pendingEvent = ""
		return nil, nil
	}
	if strings.HasPrefix(line, ":") {
		return nil, nil
	}
	if strings.HasPrefix(line, "event:") {
		p.pendingEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return nil, nil
	}
	if strings.HasPrefix(line, "data:") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	}
	if line == "" {
		return nil, nil
	}
	if line == "[DONE]" {
		return &langflowStreamEvent{Event: "end", Data: map[string]any{}}, nil
	}

	var event langflowStreamEvent
	if err := json.Unmarshal([]byte(line), &event); err == nil && (event.Event != "" || event.Data != nil) {
		if event.Event == "" && p.pendingEvent != "" {
			event.Event = p.pendingEvent
		}
		return &event, nil
	}

	var payload any
	if err := json.Unmarshal([]byte(line), &payload); err == nil {
		eventName := "end"
		if p.pendingEvent != "" {
			eventName = p.pendingEvent
		}
		return &langflowStreamEvent{
			Event: eventName,
			Data:  payload,
		}, nil
	}

	if p.pendingEvent != "" {
		return &langflowStreamEvent{
			Event: p.pendingEvent,
			Data:  map[string]any{"chunk": line},
		}, nil
	}

	return &langflowStreamEvent{
		Event: "message",
		Data:  map[string]any{"text": line},
	}, nil
}

func extractLangflowStreamChunk(data any) string {
	if typed, ok := data.(map[string]any); ok {
		for _, key := range []string{"chunk", "token", "text", "content", "message"} {
			if value, ok := typed[key]; ok {
				switch chunk := value.(type) {
				case string:
					return chunk
				case nil:
					return ""
				default:
					return fmt.Sprint(chunk)
				}
			}
		}
	}
	return extractLangflowTextFromValue(data)
}

func extractAssistantMessageText(event langflowStreamEvent) string {
	eventName := strings.ToLower(strings.TrimSpace(event.Event))
	if eventName != "add_message" && eventName != "message" && eventName != "update_message" {
		return ""
	}

	if typed, ok := event.Data.(map[string]any); ok {
		sender := strings.ToLower(stringifyValue(typed["sender"]))
		senderName := strings.ToLower(stringifyValue(typed["sender_name"]))
		if sender != "" && sender != "machine" && sender != "assistant" && sender != "bot" {
			return ""
		}
		if sender == "" && senderName != "" && !strings.Contains(senderName, "assistant") && !strings.Contains(senderName, "machine") && !strings.Contains(senderName, "bot") {
			return ""
		}
	}

	return extractLangflowTextFromValue(event.Data)
}

func truncateString(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if maxLength <= 0 || len(value) <= maxLength {
		return value
	}
	if maxLength <= 3 {
		return value[:maxLength]
	}
	return value[:maxLength-3] + "..."
}

func minDuration(values ...time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	maximum := values[0]
	for _, value := range values[1:] {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func stringifyValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

package main

import (
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
}

type langflowConnectionStatus struct {
	OK         bool   `json:"ok"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
}

func (p *Plugin) invokeLangflow(ctx context.Context, cfg *runtimeConfiguration, bot BotDefinition, prompt, correlationID string) (string, int, error) {
	if cfg.ParsedBaseURL == nil {
		return "", 0, fmt.Errorf("Langflow base URL is not configured")
	}
	if !hostAllowed(cfg.ParsedBaseURL.Hostname(), cfg.AllowHosts) {
		return "", 0, fmt.Errorf("Langflow host %q is not allowed by configuration", cfg.ParsedBaseURL.Hostname())
	}

	payload := langflowRunPayload{
		InputValue: prompt,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal Langflow payload: %w", err)
	}

	endpointURL, err := cfg.ParsedBaseURL.Parse("/api/v1/run/" + url.PathEscape(bot.FlowID))
	if err != nil {
		return "", 0, fmt.Errorf("failed to build Langflow endpoint: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL.String(), bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("failed to build Langflow request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Correlation-ID", correlationID)
	p.applyAuthHeader(request, cfg)

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

	candidates := make([]string, 0, 8)
	collectTextCandidates(payload, &candidates)

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

	if best != "" {
		return best
	}

	pretty, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(pretty)
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
		strings.Contains(key, "response")
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

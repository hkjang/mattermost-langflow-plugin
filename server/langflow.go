package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type langflowRunPayload struct {
	InputValue string         `json:"input_value"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	OutputType string         `json:"output_type,omitempty"`
	InputType  string         `json:"input_type,omitempty"`
	Tweaks     map[string]any `json:"tweaks,omitempty"`
	Files      []string       `json:"files,omitempty"`
	Sender     string         `json:"sender,omitempty"`
	SenderName string         `json:"sender_name,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
}

type langflowConnectionStatus struct {
	OK         bool   `json:"ok"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	ErrorCode  string `json:"error_code,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Retryable  bool   `json:"retryable"`
}

type langflowStreamEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type langflowStreamParser struct{}

type langflowCallError struct {
	Code       string
	Summary    string
	Detail     string
	Hint       string
	RequestURL string
	StatusCode int
	Retryable  bool
}

type langflowAuthSettings struct {
	Mode   string
	Token  string
	Source string
}

func (e *langflowCallError) Error() string {
	if e == nil {
		return ""
	}

	lines := []string{}
	if e.Summary != "" {
		lines = append(lines, e.Summary)
	}
	if e.Detail != "" {
		lines = append(lines, "상세: "+e.Detail)
	}
	if e.Hint != "" {
		lines = append(lines, "조치: "+e.Hint)
	}
	if e.StatusCode > 0 {
		lines = append(lines, fmt.Sprintf("HTTP 상태: %d", e.StatusCode))
	}
	if e.RequestURL != "" {
		lines = append(lines, "요청 URL: "+e.RequestURL)
	}

	return strings.Join(lines, "\n")
}

func (e *langflowCallError) toConnectionStatus() *langflowConnectionStatus {
	if e == nil {
		return &langflowConnectionStatus{}
	}

	return &langflowConnectionStatus{
		OK:         false,
		URL:        e.RequestURL,
		StatusCode: e.StatusCode,
		Message:    e.Summary,
		ErrorCode:  e.Code,
		Detail:     e.Detail,
		Hint:       e.Hint,
		Retryable:  e.Retryable,
	}
}

func (p *Plugin) invokeLangflow(ctx context.Context, cfg *runtimeConfiguration, bot BotDefinition, prompt string, requestContext BotRunRequest, sessionID, correlationID string) (string, int, error) {
	request, err := p.newLangflowRunRequest(ctx, cfg, bot, prompt, requestContext, sessionID, correlationID, false)
	if err != nil {
		return "", 0, err
	}

	client := &http.Client{Timeout: cfg.DefaultTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", 0, classifyLangflowRequestError(request.URL.String(), err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, int64(cfg.MaxOutputLength*4)))
	if err != nil {
		return "", response.StatusCode, newLangflowCallError(
			"response_read_failed",
			"Langflow 응답 본문을 읽는 중 오류가 발생했습니다.",
			err.Error(),
			"Langflow 서버 로그와 프록시 설정을 확인한 뒤 다시 시도하세요.",
			request.URL.String(),
			response.StatusCode,
			true,
		)
	}

	if response.StatusCode >= http.StatusBadRequest {
		return "", response.StatusCode, classifyLangflowHTTPError(request.URL.String(), response.StatusCode, response.Header, responseBody)
	}
	if looksLikeHTMLResponse(response.Header.Get("Content-Type"), responseBody) {
		return "", response.StatusCode, newUnexpectedHTMLResponseError(request.URL.String())
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
	prompt string,
	requestContext BotRunRequest,
	sessionID, correlationID string,
	onUpdate func(string, bool),
) (string, int, error) {
	request, err := p.newLangflowRunRequest(ctx, cfg, bot, prompt, requestContext, sessionID, correlationID, true)
	if err != nil {
		return "", 0, err
	}

	client := &http.Client{Timeout: cfg.DefaultTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", 0, classifyLangflowRequestError(request.URL.String(), err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, int64(cfg.MaxOutputLength*4)))
		if readErr != nil {
			return "", response.StatusCode, newLangflowCallError(
				"response_read_failed",
				"Langflow 오류 응답 본문을 읽는 중 문제가 발생했습니다.",
				readErr.Error(),
				"프록시 또는 Langflow 서버 로그를 확인하세요.",
				request.URL.String(),
				response.StatusCode,
				true,
			)
		}
		return "", response.StatusCode, classifyLangflowHTTPError(request.URL.String(), response.StatusCode, response.Header, responseBody)
	}

	reader := bufio.NewReader(response.Body)
	if responseBody, isHTML, detectErr := detectUnexpectedHTMLResponse(reader, response.Header.Get("Content-Type"), cfg.MaxOutputLength*4); detectErr != nil {
		return "", response.StatusCode, newLangflowCallError(
			"stream_inspect_failed",
			"Langflow 스트리밍 응답 형식을 확인하는 중 오류가 발생했습니다.",
			detectErr.Error(),
			"Langflow 서버와 프록시가 SSE 응답을 그대로 전달하는지 확인하세요.",
			request.URL.String(),
			response.StatusCode,
			true,
		)
	} else if isHTML {
		if cfg.EnableDebugLogs {
			p.API.LogDebug("Langflow streaming endpoint returned HTML instead of SSE", "body_preview", truncateString(strings.TrimSpace(string(responseBody)), 240), "correlation_id", correlationID)
		}
		return "", response.StatusCode, newUnexpectedHTMLResponseError(request.URL.String())
	}

	parser := langflowStreamParser{}

	var fallback bytes.Buffer
	currentOutput := ""
	lastPublishedOutput := ""
	finalOutput := ""

	for {
		event, rawEvent, parseErr := parser.readEvent(reader)
		if parseErr != nil {
			if errors.Is(parseErr, io.EOF) {
				break
			}
			if cfg.EnableDebugLogs {
				p.API.LogDebug("Ignoring Langflow stream event that could not be parsed", "error", parseErr, "correlation_id", correlationID)
			}
			continue
		}
		trimmed := strings.TrimSpace(rawEvent)
		if trimmed != "" {
			fallback.WriteString(trimmed)
			fallback.WriteByte('\n')
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
			currentOutput = mergeLangflowStreamOutput(currentOutput, chunk)
			nextOutput := truncateString(currentOutput, cfg.MaxOutputLength)
			if onUpdate != nil && nextOutput != "" && nextOutput != lastPublishedOutput {
				onUpdate(nextOutput, false)
				lastPublishedOutput = nextOutput
			}
		case "error":
			message := extractLangflowTextFromValue(event.Data)
			if message == "" {
				message = "Langflow streaming request failed"
			}
			return "", response.StatusCode, newLangflowCallError(
				"stream_error_event",
				"Langflow가 스트리밍 중 오류 이벤트를 반환했습니다.",
				message,
				"Flow 내부 노드 오류나 입력 스키마를 확인하세요.",
				request.URL.String(),
				response.StatusCode,
				false,
			)
		case "end":
			if candidate := extractLangflowTextFromValue(event.Data); candidate != "" {
				finalOutput = candidate
			}
		default:
			if snapshot := extractAssistantMessageText(*event); snapshot != "" {
				currentOutput = mergeLangflowStreamOutput(currentOutput, snapshot)
				nextOutput := truncateString(currentOutput, cfg.MaxOutputLength)
				if onUpdate != nil && nextOutput != "" && nextOutput != lastPublishedOutput {
					onUpdate(nextOutput, false)
					lastPublishedOutput = nextOutput
				}
				if finalOutput == "" || len(snapshot) >= len(finalOutput) {
					finalOutput = snapshot
				}
			}
		}
	}

	output := strings.TrimSpace(finalOutput)
	if output == "" {
		output = strings.TrimSpace(currentOutput)
	}
	if output == "" && fallback.Len() > 0 {
		output = extractLangflowText(fallback.Bytes())
	}
	output = truncateString(output, cfg.MaxOutputLength)
	if output == "" {
		return "", response.StatusCode, newLangflowCallError(
			"empty_stream_response",
			"Langflow 스트리밍 응답에 표시할 텍스트가 없었습니다.",
			"응답 이벤트는 도착했지만 텍스트 필드를 찾지 못했습니다.",
			"Flow 출력 노드가 실제 텍스트를 반환하는지 확인하세요.",
			request.URL.String(),
			response.StatusCode,
			false,
		)
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
	prompt string,
	requestContext BotRunRequest,
	sessionID, correlationID string,
	stream bool,
) (*http.Request, error) {
	if cfg.ParsedBaseURL == nil {
		return nil, fmt.Errorf("Langflow base URL is not configured")
	}
	if !hostAllowed(cfg.ParsedBaseURL.Hostname(), cfg.AllowHosts) {
		return nil, fmt.Errorf("Langflow host %q is not allowed by configuration", cfg.ParsedBaseURL.Hostname())
	}

	preparedAttachments, err := p.prepareLangflowAttachments(ctx, cfg, bot, requestContext, correlationID)
	if err != nil {
		return nil, err
	}

	metadata := buildLangflowMetadata(requestContext)
	tweaks := buildLangflowTweaks(requestContext)
	if preparedAttachments != nil {
		metadata = mergeLangflowMetadata(metadata, map[string]any{
			"mattermost_attachments": preparedAttachments.Metadata,
		})
		tweaks = mergeLangflowTweaks(tweaks, preparedAttachments.Tweaks)
	}

	payload := langflowRunPayload{
		InputValue: prompt,
		Metadata:   metadata,
		OutputType: "chat",
		InputType:  "chat",
		Tweaks:     tweaks,
		Sender:     "User",
		SenderName: defaultIfEmpty(strings.TrimSpace(requestContext.UserName), "User"),
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
	p.applyAuthHeader(request, cfg, &bot)

	return request, nil
}

func buildLangflowMetadata(request BotRunRequest) map[string]any {
	metadata := map[string]any{}
	if request.UserID != "" {
		metadata["user_id"] = request.UserID
	}
	if request.UserName != "" {
		metadata["username"] = request.UserName
	}

	if len(metadata) == 0 {
		return nil
	}

	return metadata
}

func buildLangflowTweaks(request BotRunRequest) map[string]any {
	tweaks := map[string]any{}
	for key, value := range request.Inputs {
		tweaks[key] = value
	}

	if request.UserID != "" {
		tweaks["mattermost_user_id"] = request.UserID
		tweaks["user_id"] = request.UserID
	}
	if request.UserName != "" {
		tweaks["mattermost_user_name"] = request.UserName
		tweaks["username"] = request.UserName
	}

	if len(tweaks) == 0 {
		return nil
	}

	return tweaks
}

func mergeLangflowMetadata(base map[string]any, additions map[string]any) map[string]any {
	if len(additions) == 0 {
		return base
	}
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range additions {
		if value == nil {
			continue
		}
		base[key] = value
	}
	if len(base) == 0 {
		return nil
	}
	return base
}

func mergeLangflowTweaks(base map[string]any, additions map[string]any) map[string]any {
	if len(additions) == 0 {
		return base
	}
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range additions {
		if value == nil {
			continue
		}
		existingMap, hasExistingMap := base[key].(map[string]any)
		addedMap, hasAddedMap := value.(map[string]any)
		if hasExistingMap && hasAddedMap {
			merged := map[string]any{}
			for nestedKey, nestedValue := range existingMap {
				merged[nestedKey] = nestedValue
			}
			for nestedKey, nestedValue := range addedMap {
				merged[nestedKey] = nestedValue
			}
			base[key] = merged
			continue
		}
		base[key] = value
	}
	if len(base) == 0 {
		return nil
	}
	return base
}

func buildLangflowRunURL(baseURL *url.URL, flowID string, stream bool) (*url.URL, error) {
	endpointURL := buildURLWithPathSegments(baseURL, append(langflowAPIPathSegments(baseURL), "run", flowID)...)
	if stream {
		query := endpointURL.Query()
		query.Set("stream", "true")
		endpointURL.RawQuery = query.Encode()
	}
	return endpointURL, nil
}

func (p *Plugin) testLangflowConnection(ctx context.Context, cfg *runtimeConfiguration) (*langflowConnectionStatus, error) {
	if cfg.ParsedBaseURL == nil {
		return &langflowConnectionStatus{OK: false, Message: "Langflow 기본 URL이 설정되지 않았습니다."}, nil
	}
	if !hostAllowed(cfg.ParsedBaseURL.Hostname(), cfg.AllowHosts) {
		return &langflowConnectionStatus{
			OK:      false,
			URL:     cfg.ParsedBaseURL.String(),
			Message: "허용 호스트 정책에 의해 Langflow 호출이 차단되었습니다.",
			Detail:  fmt.Sprintf("현재 호스트 %q 가 allowlist에 없습니다.", cfg.ParsedBaseURL.Hostname()),
			Hint:    "System Console에서 허용 호스트를 추가하거나 기본 URL을 확인하세요.",
		}, nil
	}

	candidates := buildLangflowHealthURLs(cfg.ParsedBaseURL)
	client := &http.Client{Timeout: minDuration(cfg.DefaultTimeout, 10*time.Second)}
	var lastStatus *langflowConnectionStatus
	for _, target := range candidates {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create health request: %w", err)
		}
		p.applyAuthHeader(request, cfg, nil)

		response, err := client.Do(request)
		if err != nil {
			lastStatus = classifyLangflowRequestError(target.String(), err).toConnectionStatus()
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		response.Body.Close()

		if response.StatusCode >= http.StatusBadRequest {
			lastStatus = classifyLangflowHTTPError(target.String(), response.StatusCode, response.Header, body).toConnectionStatus()
			continue
		}
		if looksLikeHTMLResponse(response.Header.Get("Content-Type"), body) {
			lastStatus = newUnexpectedHTMLResponseError(target.String()).toConnectionStatus()
			continue
		}

		status := &langflowConnectionStatus{
			OK:         true,
			URL:        target.String(),
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
		if status.Message == "" {
			status.Message = "연결에 성공했습니다."
			return status, nil
		}
		return status, nil
	}
	if lastStatus != nil {
		return lastStatus, nil
	}

	return &langflowConnectionStatus{
		OK:        false,
		URL:       cfg.ParsedBaseURL.String(),
		Message:   "Langflow 상태 확인 엔드포인트에 연결하지 못했습니다.",
		Hint:      "기본 URL과 방화벽, 프록시, Langflow 프로세스 상태를 확인하세요.",
		Retryable: true,
	}, nil
}

func resolveLangflowAuth(cfg *runtimeConfiguration, bot *BotDefinition) langflowAuthSettings {
	auth := langflowAuthSettings{
		Mode:   normalizeAuthMode(cfg.LangflowAuthMode),
		Token:  strings.TrimSpace(cfg.LangflowAuthToken),
		Source: "service",
	}

	if bot == nil {
		return auth
	}

	if overrideMode := normalizeBotAuthMode(bot.AuthMode); overrideMode != "" {
		auth.Mode = overrideMode
		auth.Source = "bot"
	}
	if overrideToken := strings.TrimSpace(bot.AuthToken); overrideToken != "" {
		auth.Token = overrideToken
		auth.Source = "bot"
	}

	return auth
}

func (p *Plugin) applyAuthHeader(request *http.Request, cfg *runtimeConfiguration, bot *BotDefinition) {
	auth := resolveLangflowAuth(cfg, bot)
	if auth.Token == "" {
		return
	}
	if auth.Mode == "x-api-key" {
		request.Header.Set("x-api-key", auth.Token)
		return
	}
	request.Header.Set("Authorization", "Bearer "+auth.Token)
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

func (p *langflowStreamParser) readEvent(reader *bufio.Reader) (*langflowStreamEvent, string, error) {
	if reader == nil {
		return nil, "", io.EOF
	}

	var eventName string
	dataLines := make([]string, 0, 4)
	rawLines := make([]string, 0, 4)

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, "", err
		}

		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine != "" {
			rawLines = append(rawLines, trimmedLine)
		}

		if trimmedLine == "" {
			event, parseErr := p.parsePayload(strings.Join(dataLines, "\n"), eventName)
			if parseErr != nil {
				return nil, strings.Join(rawLines, "\n"), parseErr
			}
			if event != nil {
				return event, strings.Join(rawLines, "\n"), nil
			}
			if errors.Is(err, io.EOF) {
				return nil, "", io.EOF
			}
			continue
		}

		if strings.HasPrefix(trimmedLine, ":") {
			if errors.Is(err, io.EOF) {
				event, parseErr := p.parsePayload(strings.Join(dataLines, "\n"), eventName)
				if parseErr != nil {
					return nil, strings.Join(rawLines, "\n"), parseErr
				}
				if event != nil {
					return event, strings.Join(rawLines, "\n"), nil
				}
				return nil, "", io.EOF
			}
			continue
		}

		field, value, hasColon := strings.Cut(line, ":")
		if hasColon {
			value = strings.TrimPrefix(value, " ")
		}

		switch {
		case hasColon && field == "event":
			eventName = strings.TrimSpace(value)
		case hasColon && field == "data":
			dataLines = append(dataLines, value)
		case strings.HasPrefix(trimmedLine, "{") || strings.HasPrefix(trimmedLine, "["):
			dataLines = append(dataLines, trimmedLine)
		default:
			dataLines = append(dataLines, line)
		}

		if errors.Is(err, io.EOF) {
			event, parseErr := p.parsePayload(strings.Join(dataLines, "\n"), eventName)
			if parseErr != nil {
				return nil, strings.Join(rawLines, "\n"), parseErr
			}
			if event != nil {
				return event, strings.Join(rawLines, "\n"), nil
			}
			return nil, "", io.EOF
		}
	}
}

func (p *langflowStreamParser) parsePayload(payload, eventName string) (*langflowStreamEvent, error) {
	payload = strings.TrimSpace(payload)
	eventName = strings.TrimSpace(eventName)
	if payload == "" {
		return nil, nil
	}
	if payload == "[DONE]" {
		return &langflowStreamEvent{Event: "end", Data: map[string]any{}}, nil
	}

	var event langflowStreamEvent
	if err := json.Unmarshal([]byte(payload), &event); err == nil && (event.Event != "" || event.Data != nil) {
		if event.Event == "" {
			event.Event = defaultIfEmpty(eventName, "message")
		}
		return &event, nil
	}

	var decoded any
	if err := json.Unmarshal([]byte(payload), &decoded); err == nil {
		if eventName == "" && looksLikeLangflowTokenPayload(decoded) {
			return &langflowStreamEvent{
				Event: "token",
				Data:  decoded,
			}, nil
		}
		return &langflowStreamEvent{
			Event: defaultIfEmpty(eventName, "end"),
			Data:  decoded,
		}, nil
	}

	if eventName != "" {
		return &langflowStreamEvent{
			Event: eventName,
			Data:  map[string]any{"chunk": payload},
		}, nil
	}

	return &langflowStreamEvent{
		Event: "message",
		Data:  map[string]any{"text": payload},
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

func mergeLangflowStreamOutput(current, next string) string {
	switch {
	case next == "":
		return current
	case current == "":
		return next
	case strings.HasPrefix(next, current):
		return next
	case strings.HasPrefix(current, next):
		return current
	default:
		return current + next
	}
}

func looksLikeLangflowTokenPayload(value any) bool {
	typed, ok := value.(map[string]any)
	if !ok {
		return false
	}

	for _, key := range []string{"chunk", "token", "delta"} {
		if _, exists := typed[key]; exists {
			return true
		}
	}
	return false
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

func detectUnexpectedHTMLResponse(reader *bufio.Reader, contentType string, limit int) ([]byte, bool, error) {
	if reader == nil {
		return nil, false, nil
	}
	inspectionLimit := maxInt(limit, 2048)
	if looksLikeHTMLContentType(contentType) {
		body, err := io.ReadAll(io.LimitReader(reader, int64(inspectionLimit)))
		return body, true, err
	}

	preview, err := reader.Peek(minInt(inspectionLimit, 2048))
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, false, err
	}
	if looksLikeHTMLResponse(contentType, preview) {
		body, readErr := io.ReadAll(io.LimitReader(reader, int64(inspectionLimit)))
		return body, true, readErr
	}

	return nil, false, nil
}

func buildLangflowHealthURLs(baseURL *url.URL) []*url.URL {
	serviceSegments := langflowServicePathSegments(baseURL)
	apiSegments := langflowAPIPathSegments(baseURL)
	candidates := [][]string{
		append(append([]string{}, apiSegments...), "health"),
	}
	if !equalStringSlices(serviceSegments, apiSegments) {
		candidates = append(candidates, append(append([]string{}, serviceSegments...), "health"))
	}

	urls := make([]*url.URL, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, segments := range candidates {
		target := buildURLWithPathSegments(baseURL, segments...)
		key := target.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		urls = append(urls, target)
	}
	return urls
}

func langflowAPIPathSegments(baseURL *url.URL) []string {
	segments := append([]string{}, langflowServicePathSegments(baseURL)...)
	segments = append(segments, "api", "v1")
	return segments
}

func langflowServicePathSegments(baseURL *url.URL) []string {
	segments := normalizeLangflowBasePathSegments(baseURL)
	count := len(segments)
	switch {
	case count >= 2 && strings.EqualFold(segments[count-2], "api") && strings.EqualFold(segments[count-1], "v1"):
		return append([]string{}, segments[:count-2]...)
	case count >= 1 && strings.EqualFold(segments[count-1], "api"):
		return append([]string{}, segments[:count-1]...)
	default:
		return append([]string{}, segments...)
	}
}

func normalizeLangflowBasePathSegments(baseURL *url.URL) []string {
	if baseURL == nil {
		return nil
	}
	segments := splitURLPathSegments(baseURL.Path)
	count := len(segments)
	switch {
	case count >= 4 &&
		strings.EqualFold(segments[count-4], "api") &&
		strings.EqualFold(segments[count-3], "v1") &&
		strings.EqualFold(segments[count-2], "run"):
		return append([]string{}, segments[:count-2]...)
	case count >= 3 &&
		strings.EqualFold(segments[count-3], "api") &&
		strings.EqualFold(segments[count-2], "v1") &&
		strings.EqualFold(segments[count-1], "health"):
		return append([]string{}, segments[:count-1]...)
	case count >= 2 &&
		strings.EqualFold(segments[count-2], "api") &&
		strings.EqualFold(segments[count-1], "health"):
		return append([]string{}, segments[:count-1]...)
	default:
		return segments
	}
}

func splitURLPathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}

	parts := strings.Split(trimmed, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		segments = append(segments, part)
	}
	return segments
}

func buildURLWithPathSegments(baseURL *url.URL, segments ...string) *url.URL {
	target := *baseURL
	target.RawQuery = ""
	target.Fragment = ""
	target.Path = joinPathSegments(false, segments...)
	target.RawPath = joinPathSegments(true, segments...)
	return &target
}

func joinPathSegments(escape bool, segments ...string) string {
	filtered := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.Trim(segment, "/")
		if segment == "" {
			continue
		}
		if escape {
			filtered = append(filtered, url.PathEscape(segment))
			continue
		}
		filtered = append(filtered, segment)
	}
	if len(filtered) == 0 {
		return "/"
	}
	return "/" + strings.Join(filtered, "/")
}

func looksLikeHTMLResponse(contentType string, body []byte) bool {
	if looksLikeHTMLContentType(contentType) {
		return true
	}
	sample := strings.ToLower(strings.TrimSpace(string(body)))
	if sample == "" {
		return false
	}
	if strings.HasPrefix(sample, "<!doctype html") || strings.HasPrefix(sample, "<html") {
		return true
	}
	return strings.Contains(sample, "enable javascript to run this app") ||
		(strings.Contains(sample, "<body") && strings.Contains(sample, "</html>"))
}

func looksLikeHTMLContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml")
}

func unexpectedHTMLResponseMessage(targetURL string) string {
	return fmt.Sprintf("Langflow API 대신 웹 화면 HTML이 반환되었습니다. 기본 URL, 역프록시 서브경로, 인증 설정을 확인하세요. 요청 URL: %s", targetURL)
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func minInt(values ...int) int {
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

func newLangflowCallError(code, summary, detail, hint, requestURL string, statusCode int, retryable bool) *langflowCallError {
	return &langflowCallError{
		Code:       code,
		Summary:    strings.TrimSpace(summary),
		Detail:     strings.TrimSpace(detail),
		Hint:       strings.TrimSpace(hint),
		RequestURL: strings.TrimSpace(requestURL),
		StatusCode: statusCode,
		Retryable:  retryable,
	}
}

func newUnexpectedHTMLResponseError(requestURL string) *langflowCallError {
	return newLangflowCallError(
		"unexpected_html",
		"Langflow API 대신 웹 화면 HTML이 반환되었습니다.",
		"플러그인이 API JSON/SSE 대신 웹앱 HTML 문서를 받았습니다.",
		"기본 URL에 Langflow 서비스 경로를 정확히 입력하고, 역프록시가 API 요청을 로그인 화면이나 SPA index.html로 돌려보내지 않는지 확인하세요.",
		requestURL,
		http.StatusOK,
		false,
	)
}

func classifyLangflowHTTPError(requestURL string, statusCode int, headers http.Header, body []byte) *langflowCallError {
	if looksLikeHTMLResponse(headers.Get("Content-Type"), body) {
		return newUnexpectedHTMLResponseError(requestURL)
	}

	bodySummary := summarizeErrorBody(body)
	requestID := firstHeaderValue(headers, "X-Request-Id", "X-Request-ID", "X-Correlation-ID")
	if requestID != "" {
		bodySummary = strings.TrimSpace(bodySummary + " (Langflow request id: " + requestID + ")")
	}

	switch statusCode {
	case http.StatusBadRequest:
		return newLangflowCallError(
			"bad_request",
			"Langflow가 요청을 거부했습니다.",
			defaultIfEmpty(bodySummary, "입력값 또는 Flow 설정이 Langflow 요구사항과 맞지 않습니다."),
			"Flow 입력 스키마, 필수 파라미터, 프롬프트 길이를 확인하세요.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusUnauthorized, http.StatusForbidden:
		return newLangflowCallError(
			"auth_failed",
			"Langflow 인증에 실패했습니다.",
			defaultIfEmpty(bodySummary, "Langflow가 토큰 또는 API 키를 허용하지 않았습니다."),
			"서비스 기본 토큰과 봇별 API 키 오버라이드를 다시 확인하세요. Langflow가 LANGFLOW_API_KEY_SOURCE=env 로 실행 중이면 배포 전체에서 하나의 API 키만 허용됩니다.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusNotFound:
		return newLangflowCallError(
			"not_found",
			"Langflow API 경로 또는 Flow를 찾지 못했습니다.",
			defaultIfEmpty(bodySummary, "요청한 run endpoint 또는 flow_id가 존재하지 않습니다."),
			"기본 URL의 서브경로와 bot에 연결된 flow_id를 다시 확인하세요.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusTooManyRequests:
		retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
		hint := "잠시 후 다시 시도하거나 Langflow 쪽 제한 정책을 확인하세요."
		if retryAfter != "" {
			hint = fmt.Sprintf("Langflow가 Retry-After=%s 를 반환했습니다. 잠시 후 다시 시도하세요.", retryAfter)
		}
		return newLangflowCallError(
			"rate_limited",
			"Langflow 호출 한도에 걸렸습니다.",
			defaultIfEmpty(bodySummary, "Langflow가 너무 많은 요청을 받아 현재 호출을 제한했습니다."),
			hint,
			requestURL,
			statusCode,
			true,
		)
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return newLangflowCallError(
			"upstream_timeout",
			"Langflow가 시간 내에 응답하지 않았습니다.",
			defaultIfEmpty(bodySummary, "업스트림 타임아웃이 발생했습니다."),
			"Flow 실행 시간이 길다면 타임아웃 값을 늘리거나 Langflow 서버 상태를 확인하세요.",
			requestURL,
			statusCode,
			true,
		)
	default:
		if statusCode >= http.StatusInternalServerError {
			return newLangflowCallError(
				"server_error",
				"Langflow 서버 내부 오류가 발생했습니다.",
				defaultIfEmpty(bodySummary, "Langflow 서버가 5xx 오류를 반환했습니다."),
				"Langflow 서버 로그와 Flow 실행 상태를 확인한 뒤 다시 시도하세요.",
				requestURL,
				statusCode,
				true,
			)
		}
		return newLangflowCallError(
			"unexpected_status",
			fmt.Sprintf("Langflow가 예상하지 못한 HTTP 상태 %d 를 반환했습니다.", statusCode),
			bodySummary,
			"응답 본문과 Langflow 로그를 함께 확인하세요.",
			requestURL,
			statusCode,
			statusCode >= 500,
		)
	}
}

func classifyLangflowRequestError(requestURL string, err error) *langflowCallError {
	detail := strings.TrimSpace(err.Error())

	var timeoutError interface{ Timeout() bool }
	if errors.As(err, &timeoutError) && timeoutError.Timeout() {
		return newLangflowCallError(
			"network_timeout",
			"Langflow 서버 연결이 시간 초과되었습니다.",
			detail,
			"Langflow 서버 상태와 네트워크 지연, 플러그인 타임아웃 설정을 확인하세요.",
			requestURL,
			0,
			true,
		)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newLangflowCallError(
			"network_timeout",
			"Langflow 서버 연결이 시간 초과되었습니다.",
			detail,
			"Langflow 서버 상태와 플러그인 타임아웃 값을 확인하세요.",
			requestURL,
			0,
			true,
		)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return newLangflowCallError(
			"dns_error",
			"Langflow 호스트 이름을 찾지 못했습니다.",
			detail,
			"기본 URL의 도메인 이름과 DNS 설정을 확인하세요.",
			requestURL,
			0,
			false,
		)
	}

	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return newLangflowCallError(
			"tls_hostname_error",
			"TLS 인증서의 호스트 이름이 Langflow URL과 일치하지 않습니다.",
			detail,
			"인증서의 SAN/CN과 기본 URL 호스트가 일치하는지 확인하세요.",
			requestURL,
			0,
			false,
		)
	}

	var unknownAuthorityErr x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityErr) {
		return newLangflowCallError(
			"tls_unknown_authority",
			"Langflow TLS 인증서를 신뢰할 수 없습니다.",
			detail,
			"사설 인증서를 사용 중이면 Mattermost 서버가 해당 루트 인증서를 신뢰하도록 구성하세요.",
			requestURL,
			0,
			false,
		)
	}

	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "connection refused"):
		return newLangflowCallError(
			"connection_refused",
			"Langflow 서버가 연결을 거부했습니다.",
			detail,
			"Langflow 프로세스가 실행 중인지, 포트와 방화벽이 올바른지 확인하세요.",
			requestURL,
			0,
			true,
		)
	case strings.Contains(lower, "no such host"):
		return newLangflowCallError(
			"dns_error",
			"Langflow 호스트 이름을 찾지 못했습니다.",
			detail,
			"기본 URL의 도메인 이름과 DNS 설정을 확인하세요.",
			requestURL,
			0,
			false,
		)
	case strings.Contains(lower, "certificate"), strings.Contains(lower, "tls"):
		return newLangflowCallError(
			"tls_error",
			"Langflow TLS 연결을 설정하지 못했습니다.",
			detail,
			"HTTPS 인증서 체인과 프록시 TLS 구성을 확인하세요.",
			requestURL,
			0,
			false,
		)
	default:
		return newLangflowCallError(
			"network_error",
			"Langflow 서버에 연결하지 못했습니다.",
			detail,
			"기본 URL, 네트워크 경로, 방화벽, 프록시 설정을 확인하세요.",
			requestURL,
			0,
			true,
		)
	}
}

func firstHeaderValue(headers http.Header, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

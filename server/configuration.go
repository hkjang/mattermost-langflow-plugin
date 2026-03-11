package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"
)

const (
	defaultAuthMode          = "bearer"
	defaultTimeoutSeconds    = 30
	defaultMaxInputLength    = 4000
	defaultMaxOutputLength   = 8000
	defaultContextPostLimit  = 8
	defaultStreamIntervalMS  = 350
	maxHistoryEntriesPerUser = 20
)

type configuration struct {
	LangflowBaseURL       string `json:"LangflowBaseURL"`
	LangflowAuthMode      string `json:"LangflowAuthMode"`
	LangflowAuthToken     string `json:"LangflowAuthToken"`
	AllowHosts            string `json:"AllowHosts"`
	BotDefinitions        string `json:"BotDefinitions"`
	DefaultTimeoutSeconds string `json:"DefaultTimeoutSeconds"`
	StreamingUpdateMS     string `json:"StreamingUpdateMS"`
	MaxInputLength        string `json:"MaxInputLength"`
	MaxOutputLength       string `json:"MaxOutputLength"`
	ContextPostLimit      string `json:"ContextPostLimit"`
	EnableStreaming       bool   `json:"EnableStreaming"`
	EnableDebugLogs       bool   `json:"EnableDebugLogs"`
	EnableUsageLogs       bool   `json:"EnableUsageLogs"`
	StatusPanel           string `json:"StatusPanel"`
}

type runtimeConfiguration struct {
	LangflowBaseURL         string
	ParsedBaseURL           *url.URL
	LangflowAuthMode        string
	LangflowAuthToken       string
	AllowHosts              []string
	BotDefinitions          []BotDefinition
	DefaultTimeout          time.Duration
	StreamingUpdateInterval time.Duration
	MaxInputLength          int
	MaxOutputLength         int
	ContextPostLimit        int
	EnableStreaming         bool
	EnableDebugLogs         bool
	EnableUsageLogs         bool
}

func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

func (c *configuration) normalize() (*runtimeConfiguration, error) {
	cfg := &runtimeConfiguration{
		LangflowBaseURL:   strings.TrimSpace(c.LangflowBaseURL),
		LangflowAuthMode:  normalizeAuthMode(c.LangflowAuthMode),
		LangflowAuthToken: strings.TrimSpace(c.LangflowAuthToken),
		EnableStreaming:   c.EnableStreaming,
		MaxInputLength:    parsePositiveInt(c.MaxInputLength, defaultMaxInputLength),
		MaxOutputLength:   parsePositiveInt(c.MaxOutputLength, defaultMaxOutputLength),
		ContextPostLimit:  parsePositiveInt(c.ContextPostLimit, defaultContextPostLimit),
		EnableDebugLogs:   c.EnableDebugLogs,
		EnableUsageLogs:   c.EnableUsageLogs,
	}
	cfg.DefaultTimeout = time.Duration(parsePositiveInt(c.DefaultTimeoutSeconds, defaultTimeoutSeconds)) * time.Second
	cfg.StreamingUpdateInterval = time.Duration(parsePositiveInt(c.StreamingUpdateMS, defaultStreamIntervalMS)) * time.Millisecond

	if cfg.LangflowBaseURL != "" {
		parsedURL, err := url.Parse(cfg.LangflowBaseURL)
		if err != nil {
			return nil, fmt.Errorf("invalid Langflow base URL: %w", err)
		}
		if parsedURL.Scheme == "" || parsedURL.Host == "" {
			return nil, fmt.Errorf("Langflow base URL must include scheme and host")
		}
		cfg.LangflowBaseURL = strings.TrimRight(parsedURL.String(), "/")
		cfg.ParsedBaseURL = parsedURL
	}

	bots, err := parseBotDefinitions(c.BotDefinitions)
	if err != nil {
		return nil, err
	}
	cfg.BotDefinitions = bots
	cfg.AllowHosts = normalizeAllowHosts(c.AllowHosts, cfg.ParsedBaseURL)

	return cfg, nil
}

func normalizeAuthMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "bearer":
		return defaultAuthMode
	case "x-api-key":
		return "x-api-key"
	default:
		return defaultAuthMode
	}
}

func normalizeAllowHosts(raw string, parsedBaseURL *url.URL) []string {
	parts := strings.Split(raw, ",")
	hosts := make([]string, 0, len(parts)+1)
	seen := map[string]struct{}{}

	appendHost := func(host string) {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			return
		}
		if _, ok := seen[host]; ok {
			return
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}

	for _, part := range parts {
		appendHost(part)
	}

	if len(hosts) == 0 && parsedBaseURL != nil {
		appendHost(parsedBaseURL.Hostname())
	}

	return hosts
}

func parsePositiveInt(raw string, fallback int) int {
	var value int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &value); err != nil || value <= 0 {
		return fallback
	}
	return value
}

func defaultIfEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parseBotDefinitions(raw string) ([]BotDefinition, error) {
	if strings.TrimSpace(raw) == "" {
		return []BotDefinition{}, nil
	}

	var bots []BotDefinition
	if err := json.Unmarshal([]byte(raw), &bots); err != nil {
		return nil, fmt.Errorf("invalid bot definitions JSON: %w", err)
	}

	normalized := make([]BotDefinition, 0, len(bots))
	seenIDs := map[string]struct{}{}
	seenUsernames := map[string]struct{}{}
	for _, bot := range bots {
		item, err := bot.normalize()
		if err != nil {
			return nil, err
		}
		if _, ok := seenIDs[item.ID]; ok {
			return nil, fmt.Errorf("duplicate bot id %q", item.ID)
		}
		if _, ok := seenUsernames[item.Username]; ok {
			return nil, fmt.Errorf("duplicate bot username %q", item.Username)
		}
		seenIDs[item.ID] = struct{}{}
		seenUsernames[item.Username] = struct{}{}
		normalized = append(normalized, item)
	}

	return normalized, nil
}

func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

func (p *Plugin) getRuntimeConfiguration() (*runtimeConfiguration, error) {
	return p.getConfiguration().normalize()
}

func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}
		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

func (p *Plugin) OnConfigurationChange() error {
	configuration := new(configuration)
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return fmt.Errorf("failed to load plugin configuration: %w", err)
	}

	p.setConfiguration(configuration)

	if p.client != nil {
		if err := p.ensureBots(); err != nil {
			p.API.LogError("Failed to ensure Langflow bots after configuration change", "error", err)
		}
	}

	return nil
}

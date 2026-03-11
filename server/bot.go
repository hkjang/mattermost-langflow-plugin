package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

type BotDefinition struct {
	ID                      string          `json:"id"`
	Username                string          `json:"username"`
	DisplayName             string          `json:"display_name"`
	Description             string          `json:"description"`
	FlowID                  string          `json:"flow_id"`
	IncludeContextByDefault bool            `json:"include_context_by_default"`
	AllowedTeams            []string        `json:"allowed_teams"`
	AllowedChannels         []string        `json:"allowed_channels"`
	AllowedUsers            []string        `json:"allowed_users"`
	InputSchema             []BotInputField `json:"input_schema"`
}

type BotInputField struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Required     bool   `json:"required"`
	Placeholder  string `json:"placeholder"`
	DefaultValue any    `json:"default_value"`
}

func (b BotDefinition) normalize() (BotDefinition, error) {
	b.ID = strings.TrimSpace(b.ID)
	b.Username = strings.ToLower(strings.TrimSpace(b.Username))
	b.DisplayName = strings.TrimSpace(b.DisplayName)
	b.Description = strings.TrimSpace(b.Description)
	b.FlowID = strings.TrimSpace(b.FlowID)

	if b.Username == "" {
		return BotDefinition{}, fmt.Errorf("bot definition is missing username")
	}
	if b.FlowID == "" {
		return BotDefinition{}, fmt.Errorf("bot %q is missing flow_id", b.Username)
	}
	if b.ID == "" {
		b.ID = b.Username
	}
	if b.DisplayName == "" {
		b.DisplayName = b.Username
	}

	b.AllowedTeams = normalizeStringSlice(b.AllowedTeams)
	b.AllowedChannels = normalizeStringSlice(b.AllowedChannels)
	b.AllowedUsers = normalizeStringSlice(b.AllowedUsers)

	inputs := make([]BotInputField, 0, len(b.InputSchema))
	seen := map[string]struct{}{}
	for _, field := range b.InputSchema {
		field.Name = strings.TrimSpace(field.Name)
		field.Label = defaultIfEmpty(strings.TrimSpace(field.Label), field.Name)
		field.Description = strings.TrimSpace(field.Description)
		field.Placeholder = strings.TrimSpace(field.Placeholder)
		field.Type = defaultIfEmpty(strings.ToLower(strings.TrimSpace(field.Type)), "text")
		if field.Name == "" {
			return BotDefinition{}, fmt.Errorf("bot %q has an input field without a name", b.Username)
		}
		if _, ok := seen[field.Name]; ok {
			return BotDefinition{}, fmt.Errorf("bot %q defines duplicate input %q", b.Username, field.Name)
		}
		seen[field.Name] = struct{}{}
		inputs = append(inputs, field)
	}
	b.InputSchema = inputs

	return b, nil
}

func normalizeStringSlice(items []string) []string {
	normalized := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func (cfg *runtimeConfiguration) getBotByID(botID string) *BotDefinition {
	botID = strings.TrimSpace(botID)
	for _, bot := range cfg.BotDefinitions {
		if bot.ID == botID {
			item := bot
			return &item
		}
	}
	return nil
}

func (cfg *runtimeConfiguration) getBotByUsername(username string) *BotDefinition {
	username = strings.ToLower(strings.TrimSpace(username))
	for _, bot := range cfg.BotDefinitions {
		if bot.Username == username {
			item := bot
			return &item
		}
	}
	return nil
}

func (cfg *runtimeConfiguration) getAllowedBots(user *model.User, channel *model.Channel, team *model.Team) []BotDefinition {
	allowed := make([]BotDefinition, 0, len(cfg.BotDefinitions))
	for _, bot := range cfg.BotDefinitions {
		if bot.isAllowedFor(user, channel, team) {
			allowed = append(allowed, bot)
		}
	}
	return allowed
}

func (b BotDefinition) isAllowedFor(user *model.User, channel *model.Channel, team *model.Team) bool {
	if user == nil || channel == nil {
		return false
	}

	if len(b.AllowedUsers) > 0 && !matchesAccessEntry(b.AllowedUsers, user.Id, user.Username) {
		return false
	}

	if len(b.AllowedChannels) > 0 && !matchesAccessEntry(b.AllowedChannels, channel.Id, channel.Name) {
		return false
	}

	teamName := ""
	if team != nil {
		teamName = team.Name
	}
	if len(b.AllowedTeams) > 0 && !matchesAccessEntry(b.AllowedTeams, channel.TeamId, teamName) {
		return false
	}

	return true
}

func matchesAccessEntry(entries []string, values ...string) bool {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		for _, entry := range entries {
			if entry == value {
				return true
			}
		}
	}
	return false
}

func botUsageExamples(bot BotDefinition) []string {
	return []string{
		fmt.Sprintf("- `@%s Summarize this thread`", bot.Username),
		fmt.Sprintf("- DM `%s` with `What should I do next?`", bot.Username),
	}
}

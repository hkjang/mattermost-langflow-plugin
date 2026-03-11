package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

type BotRunRequest struct {
	BotID          string         `json:"bot_id"`
	UserID         string         `json:"user_id"`
	ChannelID      string         `json:"channel_id"`
	RootID         string         `json:"root_id"`
	Prompt         string         `json:"prompt"`
	IncludeContext bool           `json:"include_context"`
	Inputs         map[string]any `json:"inputs"`
	Source         string         `json:"source"`
	TriggerPostID  string         `json:"trigger_post_id"`
}

type BotRunResult struct {
	CorrelationID string `json:"correlation_id"`
	BotID         string `json:"bot_id"`
	BotUsername   string `json:"bot_username"`
	BotName       string `json:"bot_name"`
	FlowID        string `json:"flow_id"`
	PostID        string `json:"post_id,omitempty"`
	Status        string `json:"status"`
	Output        string `json:"output,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
	Retryable     bool   `json:"retryable"`
}

func (p *Plugin) executeBotAndPost(ctx context.Context, request BotRunRequest) (*BotRunResult, error) {
	startedAt := time.Now()
	correlationID := uuid.NewString()

	cfg, err := p.getRuntimeConfiguration()
	if err != nil {
		return nil, err
	}
	if request.Inputs == nil {
		request.Inputs = map[string]any{}
	}

	channel, appErr := p.API.GetChannel(request.ChannelID)
	if appErr != nil {
		return nil, fmt.Errorf("failed to load channel: %w", appErr)
	}
	user, appErr := p.API.GetUser(request.UserID)
	if appErr != nil {
		return nil, fmt.Errorf("failed to load user: %w", appErr)
	}
	team := p.getTeamForChannel(channel)

	bot := cfg.getBotByID(request.BotID)
	if bot == nil {
		return nil, fmt.Errorf("unknown bot %q", request.BotID)
	}
	if !bot.isAllowedFor(user, channel, team) {
		return nil, fmt.Errorf("bot %q is not allowed in this context", bot.Username)
	}
	if !p.client.User.HasPermissionToChannel(request.UserID, request.ChannelID, model.PermissionReadChannel) {
		return nil, fmt.Errorf("user does not have access to the selected channel")
	}

	account, ok := p.getBotAccount(bot.ID)
	if !ok {
		if err := p.ensureBots(); err != nil {
			return nil, err
		}
		account, ok = p.getBotAccount(bot.ID)
		if !ok {
			return nil, fmt.Errorf("bot account %q is not available", bot.ID)
		}
	}

	prompt, err := p.buildExecutionPrompt(cfg, request, *bot)
	if err != nil {
		return nil, err
	}
	if prompt == "" {
		return nil, fmt.Errorf("prompt is empty")
	}

	output, statusCode, runErr := p.invokeLangflow(ctx, cfg, *bot, prompt, correlationID)
	completedAt := time.Now()
	if runErr != nil {
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", prompt, runErr.Error(), statusCode >= 500 || statusCode == 0, startedAt, completedAt)
		p.appendExecutionHistory(request.UserID, record)
		p.logUsage(cfg, correlationID, request, account.Definition, "failed", runErr.Error())
		postErr := p.postFailure(channel, request.RootID, account, correlationID, runErr.Error())
		if postErr != nil {
			p.API.LogError("Failed to post Langflow error response", "error", postErr, "correlation_id", correlationID)
		}
		return &BotRunResult{
			CorrelationID: correlationID,
			BotID:         account.Definition.ID,
			BotUsername:   account.Definition.Username,
			BotName:       account.Definition.DisplayName,
			FlowID:        account.Definition.FlowID,
			Status:        "failed",
			ErrorMessage:  runErr.Error(),
			Retryable:     statusCode >= 500 || statusCode == 0,
		}, runErr
	}

	post, err := p.postSuccess(channel, request.RootID, account, correlationID, output)
	if err != nil {
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", prompt, err.Error(), true, startedAt, time.Now())
		p.appendExecutionHistory(request.UserID, record)
		return nil, err
	}

	record := newExecutionRecord(request, account.Definition, correlationID, "completed", prompt, "", false, startedAt, completedAt)
	p.appendExecutionHistory(request.UserID, record)
	p.logUsage(cfg, correlationID, request, account.Definition, "completed", "")

	return &BotRunResult{
		CorrelationID: correlationID,
		BotID:         account.Definition.ID,
		BotUsername:   account.Definition.Username,
		BotName:       account.Definition.DisplayName,
		FlowID:        account.Definition.FlowID,
		PostID:        post.Id,
		Status:        "completed",
		Output:        output,
	}, nil
}

func (p *Plugin) buildExecutionPrompt(cfg *runtimeConfiguration, request BotRunRequest, bot BotDefinition) (string, error) {
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return "", nil
	}
	if len(prompt) > cfg.MaxInputLength {
		return "", fmt.Errorf("prompt exceeds the maximum input length of %d characters", cfg.MaxInputLength)
	}
	if err := validateRequestedInputs(bot, request.Inputs); err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString(prompt)

	extraInputs := buildPromptInputs(bot, request.Inputs)
	if len(extraInputs) > 0 {
		builder.WriteString("\n\nAdditional inputs:\n")
		for _, line := range extraInputs {
			builder.WriteString("- ")
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}

	if request.IncludeContext {
		contextBlock, err := p.collectContextBlock(request.ChannelID, request.RootID, request.TriggerPostID, cfg.ContextPostLimit)
		if err != nil {
			return "", err
		}
		if contextBlock != "" {
			builder.WriteString("\n\nRecent Mattermost context:\n")
			builder.WriteString(contextBlock)
		}
	}

	return truncateString(builder.String(), cfg.MaxInputLength), nil
}

func buildPromptInputs(bot BotDefinition, inputs map[string]any) []string {
	lines := make([]string, 0, len(bot.InputSchema))
	for _, field := range bot.InputSchema {
		value, ok := inputs[field.Name]
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", defaultIfEmpty(field.Label, field.Name), text))
	}
	return lines
}

func (p *Plugin) collectContextBlock(channelID, rootID, triggerPostID string, limit int) (string, error) {
	if limit <= 0 {
		return "", nil
	}

	var postList *model.PostList
	var appErr *model.AppError
	if rootID != "" {
		postList, appErr = p.API.GetPostThread(rootID)
	} else {
		postList, appErr = p.API.GetPostsForChannel(channelID, 0, limit)
	}
	if appErr != nil || postList == nil {
		return "", nil
	}

	posts := make([]*model.Post, 0, len(postList.Order))
	for _, postID := range postList.Order {
		post := postList.Posts[postID]
		if post == nil || post.Id == triggerPostID || p.isManagedBotUserID(post.UserId) {
			continue
		}
		if strings.TrimSpace(post.Message) == "" {
			continue
		}
		posts = append(posts, post)
	}

	sort.Slice(posts, func(i, j int) bool {
		return posts[i].CreateAt < posts[j].CreateAt
	})
	if len(posts) > limit {
		posts = posts[len(posts)-limit:]
	}

	lines := make([]string, 0, len(posts))
	for _, post := range posts {
		user, appErr := p.API.GetUser(post.UserId)
		username := post.UserId
		if appErr == nil && user != nil {
			username = user.Username
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", username, strings.TrimSpace(post.Message)))
	}
	return strings.Join(lines, "\n"), nil
}

func (p *Plugin) ensureBots() error {
	cfg, err := p.getRuntimeConfiguration()
	if err != nil {
		p.setBotAccounts(map[string]botAccount{})
		p.setBotSyncState(botSyncState{
			LastError: err.Error(),
			UpdatedAt: time.Now().UnixMilli(),
			Entries:   []botSyncEntry{},
		})
		return err
	}
	if len(cfg.BotDefinitions) == 0 {
		p.setBotAccounts(map[string]botAccount{})
		if err := p.deactivateManagedBots(nil); err != nil {
			p.setBotSyncState(botSyncState{
				LastError: err.Error(),
				UpdatedAt: time.Now().UnixMilli(),
				Entries:   []botSyncEntry{},
			})
			return err
		}
		p.setBotSyncState(botSyncState{
			UpdatedAt: time.Now().UnixMilli(),
			Entries:   []botSyncEntry{},
		})
		return nil
	}

	bots, err := p.client.Bot.List(0, 200, pluginapi.BotOwner(manifest.Id), pluginapi.BotIncludeDeleted())
	if err != nil {
		p.setBotSyncState(botSyncState{
			LastError: err.Error(),
			UpdatedAt: time.Now().UnixMilli(),
			Entries:   []botSyncEntry{},
		})
		return fmt.Errorf("failed to list plugin bots: %w", err)
	}

	existingByUsername := map[string]*model.Bot{}
	for _, bot := range bots {
		if bot == nil {
			continue
		}
		existingByUsername[strings.ToLower(bot.Username)] = bot
	}

	accounts := make(map[string]botAccount, len(cfg.BotDefinitions))
	syncEntries := make([]botSyncEntry, 0, len(cfg.BotDefinitions))
	configuredUsernames := make(map[string]struct{}, len(cfg.BotDefinitions))
	for _, definition := range cfg.BotDefinitions {
		configuredUsernames[definition.Username] = struct{}{}
		userID, err := p.ensureSingleBot(existingByUsername, definition)
		if err != nil {
			p.setBotSyncState(botSyncState{
				LastError: err.Error(),
				UpdatedAt: time.Now().UnixMilli(),
				Entries:   syncEntries,
			})
			return err
		}
		accounts[definition.ID] = botAccount{
			Definition: definition,
			UserID:     userID,
		}
		syncEntries = append(syncEntries, botSyncEntry{
			BotID:       definition.ID,
			Username:    definition.Username,
			DisplayName: definition.DisplayName,
			FlowID:      definition.FlowID,
			UserID:      userID,
			Registered:  true,
			Active:      true,
		})
	}

	if err := p.deactivateManagedBots(configuredUsernames); err != nil {
		p.setBotSyncState(botSyncState{
			LastError: err.Error(),
			UpdatedAt: time.Now().UnixMilli(),
			Entries:   syncEntries,
		})
		return err
	}

	p.setBotAccounts(accounts)
	p.setBotSyncState(botSyncState{
		UpdatedAt: time.Now().UnixMilli(),
		Entries:   syncEntries,
	})
	return nil
}

func (p *Plugin) ensureSingleBot(existingByUsername map[string]*model.Bot, definition BotDefinition) (string, error) {
	existing := existingByUsername[definition.Username]
	description := botDescription(definition)

	if existing != nil {
		displayName := definition.DisplayName
		_, err := p.client.Bot.Patch(existing.UserId, &model.BotPatch{
			DisplayName: &displayName,
			Description: &description,
		})
		if err != nil {
			return "", fmt.Errorf("failed to update Langflow bot %q: %w", definition.Username, err)
		}
		if _, err := p.client.Bot.UpdateActive(existing.UserId, true); err != nil {
			return "", fmt.Errorf("failed to activate Langflow bot %q: %w", definition.Username, err)
		}
		p.API.LogInfo("Ensured Langflow bot", "bot_username", definition.Username, "flow_id", definition.FlowID, "action", "updated")
		return existing.UserId, nil
	}

	newBot := &model.Bot{
		Username:    definition.Username,
		DisplayName: definition.DisplayName,
		Description: description,
	}
	if err := p.client.Bot.Create(newBot); err != nil {
		return "", fmt.Errorf("failed to create Langflow bot %q: %w", definition.Username, err)
	}

	p.API.LogInfo("Ensured Langflow bot", "bot_username", definition.Username, "flow_id", definition.FlowID, "action", "created")
	return newBot.UserId, nil
}

func (p *Plugin) deactivateManagedBots(configuredUsernames map[string]struct{}) error {
	bots, err := p.client.Bot.List(0, 200, pluginapi.BotOwner(manifest.Id), pluginapi.BotIncludeDeleted())
	if err != nil {
		return fmt.Errorf("failed to list plugin bots for deactivation: %w", err)
	}

	for _, bot := range bots {
		if bot == nil {
			continue
		}
		if _, keep := configuredUsernames[strings.ToLower(bot.Username)]; keep {
			continue
		}
		if _, err := p.client.Bot.UpdateActive(bot.UserId, false); err != nil {
			return fmt.Errorf("failed to deactivate removed Langflow bot %q: %w", bot.Username, err)
		}
		p.API.LogInfo("Deactivated removed Langflow bot", "bot_username", bot.Username, "user_id", bot.UserId)
	}

	return nil
}

func (p *Plugin) ensureBotInChannel(channelID, botUserID string) error {
	if channelID == "" || botUserID == "" {
		return nil
	}
	if _, appErr := p.API.GetChannelMember(channelID, botUserID); appErr == nil {
		return nil
	}
	if _, appErr := p.API.AddUserToChannel(channelID, botUserID, ""); appErr != nil {
		return fmt.Errorf("failed to add bot to channel: %w", appErr)
	}
	return nil
}

func (p *Plugin) postSuccess(channel *model.Channel, rootID string, account botAccount, correlationID, output string) (*model.Post, error) {
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return nil, err
	}

	post, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Message: strings.TrimSpace(fmt.Sprintf(
			"### %s\n\n%s\n\n_Correlation ID:_ `%s`",
			account.Definition.DisplayName,
			output,
			correlationID,
		)),
		Props: map[string]any{
			"from_bot":         "true",
			"langflow_bot_id":  account.Definition.ID,
			"langflow_flow_id": account.Definition.FlowID,
		},
	})
	if appErr != nil {
		return nil, fmt.Errorf("failed to create Langflow response post: %w", appErr)
	}
	return post, nil
}

func (p *Plugin) postFailure(channel *model.Channel, rootID string, account botAccount, correlationID, message string) error {
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return err
	}

	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Message: strings.TrimSpace(fmt.Sprintf(
			"Langflow flow `%s` failed for `@%s`.\n\n%s\n\n_Correlation ID:_ `%s`",
			account.Definition.FlowID,
			account.Definition.Username,
			message,
			correlationID,
		)),
		Props: map[string]any{
			"from_bot":         "true",
			"langflow_bot_id":  account.Definition.ID,
			"langflow_flow_id": account.Definition.FlowID,
			"langflow_error":   "true",
		},
	})
	if appErr != nil {
		return fmt.Errorf("failed to create Langflow error post: %w", appErr)
	}
	return nil
}

func (p *Plugin) postInstruction(channel *model.Channel, rootID string, account botAccount, message string) error {
	if channel == nil || strings.TrimSpace(message) == "" {
		return nil
	}
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return err
	}

	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Message:   strings.TrimSpace(message),
		Props: map[string]any{
			"from_bot":        "true",
			"langflow_bot_id": account.Definition.ID,
		},
	})
	if appErr != nil {
		return fmt.Errorf("failed to create Langflow instruction post: %w", appErr)
	}
	return nil
}

func responseRootID(post *model.Post) string {
	if post == nil {
		return ""
	}
	if post.RootId != "" {
		return post.RootId
	}
	return post.Id
}

func (p *Plugin) logUsage(cfg *runtimeConfiguration, correlationID string, request BotRunRequest, bot BotDefinition, status, errorMessage string) {
	if !cfg.EnableUsageLogs {
		return
	}
	p.API.LogInfo("Langflow execution", "correlation_id", correlationID, "bot_id", bot.ID, "bot_username", bot.Username, "flow_id", bot.FlowID, "user_id", request.UserID, "channel_id", request.ChannelID, "source", request.Source, "status", status, "error", errorMessage)
}

func validateRequestedInputs(bot BotDefinition, inputs map[string]any) error {
	for _, field := range bot.InputSchema {
		value, ok := inputs[field.Name]
		if !ok {
			if field.Required {
				return fmt.Errorf("missing required input %q", field.Name)
			}
			continue
		}
		switch field.Type {
		case "number":
			text := strings.TrimSpace(fmt.Sprint(value))
			if text == "" && field.Required {
				return fmt.Errorf("missing required input %q", field.Name)
			}
		default:
			if strings.TrimSpace(fmt.Sprint(value)) == "" && field.Required {
				return fmt.Errorf("missing required input %q", field.Name)
			}
		}
	}
	return nil
}

func botDescription(bot BotDefinition) string {
	description := strings.TrimSpace(bot.Description)
	if description != "" {
		return description
	}
	return fmt.Sprintf("Langflow bot for flow %s", bot.FlowID)
}

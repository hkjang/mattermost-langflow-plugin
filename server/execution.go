package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	UserName       string         `json:"user_name"`
	ChannelID      string         `json:"channel_id"`
	RootID         string         `json:"root_id"`
	Prompt         string         `json:"prompt"`
	IncludeContext bool           `json:"include_context"`
	Inputs         map[string]any `json:"inputs"`
	FileIDs        []string       `json:"file_ids,omitempty"`
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
	ErrorCode     string `json:"error_code,omitempty"`
	ErrorDetail   string `json:"error_detail,omitempty"`
	ErrorHint     string `json:"error_hint,omitempty"`
	RequestURL    string `json:"request_url,omitempty"`
	HTTPStatus    int    `json:"http_status,omitempty"`
	Retryable     bool   `json:"retryable"`
}

type streamingPostUpdater struct {
	plugin        *Plugin
	post          *model.Post
	account       botAccount
	correlationID string
	interval      time.Duration
	lastRendered  string
	lastUpdateAt  time.Time
	started       bool
	finished      bool
}

const (
	postStreamingControlStart = "start"
	postStreamingControlEnd   = "end"
	langflowBotPostType       = "custom_langflow_bot"
)

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
	request.UserName = user.Username
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

	sessionID := buildLangflowSessionID(request, account.Definition)
	var streamUpdater *streamingPostUpdater
	if cfg.EnableStreaming {
		placeholder, placeholderErr := p.createStreamingPost(channel, request.RootID, account, correlationID)
		if placeholderErr != nil {
			return nil, placeholderErr
		}
		streamUpdater = &streamingPostUpdater{
			plugin:        p,
			post:          placeholder,
			account:       account,
			correlationID: correlationID,
			interval:      cfg.StreamingUpdateInterval,
			lastRendered:  placeholder.Message,
		}
	}

	var output string
	var statusCode int
	var runErr error
	if cfg.EnableStreaming {
		output, statusCode, runErr = p.invokeLangflowStream(ctx, cfg, *bot, prompt, request, sessionID, correlationID, streamUpdater.update)
	} else {
		output, statusCode, runErr = p.invokeLangflow(ctx, cfg, *bot, prompt, request, sessionID, correlationID)
	}
	completedAt := time.Now()
	if runErr != nil {
		failure := describeExecutionFailure(runErr, statusCode >= 500 || statusCode == 0)
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", prompt, failure.Message, failure.ErrorCode, failure.Retryable, startedAt, completedAt)
		p.appendExecutionHistory(request.UserID, record)
		p.logUsage(cfg, correlationID, request, account.Definition, "failed", failure.Message)
		var postErr error
		if streamUpdater != nil {
			postErr = streamUpdater.fail(failure)
		} else {
			postErr = p.postFailure(channel, request.RootID, account, correlationID, failure)
		}
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
			ErrorMessage:  failure.Message,
			ErrorCode:     failure.ErrorCode,
			ErrorDetail:   failure.Detail,
			ErrorHint:     failure.Hint,
			RequestURL:    failure.RequestURL,
			HTTPStatus:    failure.HTTPStatus,
			Retryable:     failure.Retryable,
		}, runErr
	}

	var post *model.Post
	if streamUpdater != nil {
		post, err = streamUpdater.complete(output)
	} else {
		post, err = p.postSuccess(channel, request.RootID, account, correlationID, output)
	}
	if err != nil {
		failure := describeExecutionFailure(err, true)
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", prompt, failure.Message, failure.ErrorCode, failure.Retryable, startedAt, time.Now())
		p.appendExecutionHistory(request.UserID, record)
		return nil, err
	}

	record := newExecutionRecord(request, account.Definition, correlationID, "completed", prompt, "", "", false, startedAt, completedAt)
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
		deactivateErr := p.deactivateManagedBots(nil)
		lastError := ""
		if deactivateErr != nil {
			lastError = deactivateErr.Error()
		}
		p.setBotSyncState(botSyncState{
			LastError: lastError,
			UpdatedAt: time.Now().UnixMilli(),
			Entries:   []botSyncEntry{},
		})
		return nil
	}

	accounts := make(map[string]botAccount, len(cfg.BotDefinitions))
	syncEntries := make([]botSyncEntry, 0, len(cfg.BotDefinitions))
	configuredUsernames := make(map[string]struct{}, len(cfg.BotDefinitions))
	syncIssues := make([]string, 0)
	for _, definition := range cfg.BotDefinitions {
		configuredUsernames[definition.Username] = struct{}{}
		userID, statusMessage, ensureErr := p.ensureSingleBot(definition)
		entry := botSyncEntry{
			BotID:         definition.ID,
			Username:      definition.Username,
			DisplayName:   definition.DisplayName,
			FlowID:        definition.FlowID,
			UserID:        userID,
			Registered:    ensureErr == nil && userID != "",
			Active:        ensureErr == nil && userID != "",
			StatusMessage: statusMessage,
		}
		if ensureErr != nil {
			entry.StatusMessage = ensureErr.Error()
			entry.Active = false
			syncEntries = append(syncEntries, entry)
			syncIssues = append(syncIssues, ensureErr.Error())
			continue
		}
		accounts[definition.ID] = botAccount{
			Definition: definition,
			UserID:     userID,
		}
		syncEntries = append(syncEntries, entry)
	}

	if deactivateErr := p.deactivateManagedBots(configuredUsernames); deactivateErr != nil {
		syncIssues = append(syncIssues, deactivateErr.Error())
	}

	p.setBotAccounts(accounts)
	p.setBotSyncState(botSyncState{
		LastError: joinSyncIssues(syncIssues),
		UpdatedAt: time.Now().UnixMilli(),
		Entries:   syncEntries,
	})
	return nil
}

func (p *Plugin) ensureSingleBot(definition BotDefinition) (string, string, error) {
	description := botDescription(definition)
	displayName := definition.DisplayName

	existingUser, appErr := p.API.GetUserByUsername(definition.Username)
	if appErr == nil && existingUser != nil {
		if !existingUser.IsBot {
			return "", "", fmt.Errorf("username @%s is already used by a regular Mattermost account", definition.Username)
		}

		statusMessage := ""
		if _, err := p.client.Bot.Get(existingUser.Id, true); err == nil {
			if _, err := p.client.Bot.Patch(existingUser.Id, &model.BotPatch{
				DisplayName: &displayName,
				Description: &description,
			}); err != nil && !isBotNotFoundError(err) {
				return "", "", fmt.Errorf("failed to update Langflow bot @%s: %w", definition.Username, err)
			}
			if _, err := p.client.Bot.UpdateActive(existingUser.Id, true); err != nil && !isBotNotFoundError(err) {
				return "", "", fmt.Errorf("failed to activate Langflow bot @%s: %w", definition.Username, err)
			}
			p.API.LogInfo("Ensured Langflow bot", "bot_username", definition.Username, "flow_id", definition.FlowID, "action", "linked_existing")
			return existingUser.Id, statusMessage, nil
		} else {
			statusMessage = fmt.Sprintf("기존 봇 사용자 계정을 연결했습니다. Bot 메타데이터 조회는 실패했지만 메시지 전송은 계속 시도합니다: %s", err.Error())
			p.API.LogWarn("Linked Langflow bot user without bot metadata", "bot_username", definition.Username, "user_id", existingUser.Id, "error", err.Error())
			return existingUser.Id, statusMessage, nil
		}
	}

	if appErr != nil && appErr.StatusCode != http.StatusNotFound {
		return "", "", fmt.Errorf("failed to look up Mattermost user @%s: %w", definition.Username, appErr)
	}

	newBot := &model.Bot{
		Username:    definition.Username,
		DisplayName: definition.DisplayName,
		Description: description,
	}
	if err := p.client.Bot.Create(newBot); err != nil {
		existingUser, existingErr := p.API.GetUserByUsername(definition.Username)
		if existingErr == nil && existingUser != nil && existingUser.IsBot {
			p.API.LogWarn("Recovered Langflow bot by linking an already existing bot user", "bot_username", definition.Username, "user_id", existingUser.Id, "error", err.Error())
			return existingUser.Id, "이미 존재하는 봇 사용자 계정에 연결했습니다.", nil
		}
		return "", "", fmt.Errorf("failed to create Langflow bot @%s: %w", definition.Username, err)
	}

	p.API.LogInfo("Ensured Langflow bot", "bot_username", definition.Username, "flow_id", definition.FlowID, "action", "created")
	return newBot.UserId, "", nil
}

func (p *Plugin) deactivateManagedBots(configuredUsernames map[string]struct{}) error {
	bots, err := p.client.Bot.List(0, 200, pluginapi.BotOwner(manifest.Id))
	if err != nil {
		return fmt.Errorf("failed to list plugin bots for deactivation: %w", err)
	}

	issues := make([]string, 0)
	for _, bot := range bots {
		if bot == nil {
			continue
		}
		if _, keep := configuredUsernames[strings.ToLower(bot.Username)]; keep {
			continue
		}
		if _, err := p.client.Bot.UpdateActive(bot.UserId, false); err != nil {
			if isBotNotFoundError(err) {
				p.API.LogWarn("Skipped deactivation for missing Langflow bot metadata", "bot_username", bot.Username, "user_id", bot.UserId, "error", err.Error())
				continue
			}
			issues = append(issues, fmt.Sprintf("failed to deactivate removed Langflow bot @%s: %s", bot.Username, err.Error()))
			continue
		}
		p.API.LogInfo("Deactivated removed Langflow bot", "bot_username", bot.Username, "user_id", bot.UserId)
	}

	if len(issues) > 0 {
		return fmt.Errorf("%s", strings.Join(issues, "; "))
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
		Type:      langflowBotPostType,
		Message:   buildBotResponseMessage(account.Definition.DisplayName, output, correlationID, false),
		Props: map[string]any{
			"from_bot":                    "true",
			"langflow_bot_id":             account.Definition.ID,
			"langflow_correlation_id":     correlationID,
			"langflow_flow_id":            account.Definition.FlowID,
			"langflow_stream":             "false",
			"langflow_stream_placeholder": "false",
		},
	})
	if appErr != nil {
		return nil, fmt.Errorf("failed to create Langflow response post: %w", appErr)
	}
	return post, nil
}

func (p *Plugin) postFailure(channel *model.Channel, rootID string, account botAccount, correlationID string, failure executionFailureView) error {
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return err
	}

	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Type:      langflowBotPostType,
		Message:   buildBotFailureMessage(account.Definition, correlationID, failure),
		Props: map[string]any{
			"from_bot":                    "true",
			"langflow_bot_id":             account.Definition.ID,
			"langflow_correlation_id":     correlationID,
			"langflow_flow_id":            account.Definition.FlowID,
			"langflow_error":              "true",
			"langflow_stream":             "false",
			"langflow_stream_placeholder": "false",
			"langflow_error_code":         failure.ErrorCode,
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
		Type:      langflowBotPostType,
		Message:   strings.TrimSpace(message),
		Props: map[string]any{
			"from_bot":                    "true",
			"langflow_bot_id":             account.Definition.ID,
			"langflow_stream_placeholder": "false",
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
	p.API.LogInfo("Langflow execution", "correlation_id", correlationID, "bot_id", bot.ID, "bot_username", bot.Username, "flow_id", bot.FlowID, "user_id", request.UserID, "channel_id", request.ChannelID, "source", request.Source, "status", status, "error", errorMessage, "attachment_count", len(request.FileIDs))
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

func (p *Plugin) createStreamingPost(channel *model.Channel, rootID string, account botAccount, correlationID string) (*model.Post, error) {
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return nil, err
	}

	post, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Type:      langflowBotPostType,
		Message:   buildBotStreamingMessage(account.Definition.DisplayName, ""),
		Props: map[string]any{
			"from_bot":                    "true",
			"langflow_bot_id":             account.Definition.ID,
			"langflow_correlation_id":     correlationID,
			"langflow_flow_id":            account.Definition.FlowID,
			"langflow_stream":             "true",
			"langflow_streaming":          "true",
			"langflow_stream_status":      "streaming",
			"langflow_stream_placeholder": "true",
		},
	})
	if appErr != nil {
		return nil, fmt.Errorf("failed to create Langflow streaming post: %w", appErr)
	}
	return post, nil
}

func (u *streamingPostUpdater) update(content string, final bool) {
	if u == nil || u.post == nil {
		return
	}
	u.start()
	if !final && u.interval > 0 && !u.lastUpdateAt.IsZero() && time.Since(u.lastUpdateAt) < u.interval {
		return
	}
	if _, err := u.render(content, final, executionFailureView{}); err != nil {
		u.plugin.API.LogError("Failed to update Langflow streaming post", "error", err, "correlation_id", u.correlationID)
	}
}

func (u *streamingPostUpdater) complete(content string) (*model.Post, error) {
	return u.render(content, true, executionFailureView{})
}

func (u *streamingPostUpdater) fail(failure executionFailureView) error {
	_, err := u.render("", false, failure)
	return err
}

func (u *streamingPostUpdater) render(content string, completed bool, failure executionFailureView) (*model.Post, error) {
	if u == nil || u.post == nil {
		return nil, fmt.Errorf("streaming post is not initialized")
	}
	u.start()

	previewMessage := buildBotStreamingMessage(u.account.Definition.DisplayName, content)
	message := buildBotResponseMessage(u.account.Definition.DisplayName, content, u.correlationID, !failure.HasFailure && !completed)
	if failure.HasFailure {
		message = buildBotFailureMessage(u.account.Definition, u.correlationID, failure)
	}
	if !completed && !failure.HasFailure {
		message = previewMessage
	}
	if message == u.lastRendered {
		return u.post, nil
	}

	if !completed && !failure.HasFailure {
		u.post.Message = message
		u.sendUpdateEvent(message)
		u.lastRendered = message
		u.lastUpdateAt = time.Now()
		return u.post, nil
	}
	defer u.finish()

	updatedPost := *u.post
	updatedPost.Message = message
	updatedPost.Props = clonePostProps(u.post.Props)
	if updatedPost.Props == nil {
		updatedPost.Props = map[string]any{}
	}
	if failure.HasFailure {
		updatedPost.Props["langflow_error"] = "true"
		updatedPost.Props["langflow_error_code"] = failure.ErrorCode
		updatedPost.Props["langflow_stream_status"] = "failed"
		updatedPost.Props["langflow_streaming"] = "false"
		updatedPost.Props["langflow_stream_placeholder"] = "false"
	} else if completed {
		updatedPost.Props["langflow_stream_status"] = "completed"
		updatedPost.Props["langflow_streaming"] = "false"
		updatedPost.Props["langflow_stream_placeholder"] = "false"
	} else {
		updatedPost.Props["langflow_stream_status"] = "streaming"
		updatedPost.Props["langflow_streaming"] = "true"
		updatedPost.Props["langflow_stream_placeholder"] = "false"
	}

	post, appErr := u.plugin.API.UpdatePost(&updatedPost)
	if appErr != nil {
		return nil, fmt.Errorf("failed to update Langflow streaming post: %w", appErr)
	}

	if failure.HasFailure {
		u.sendUpdateEvent(message)
	}
	u.post = post
	u.lastRendered = message
	u.lastUpdateAt = time.Now()
	return post, nil
}

func (u *streamingPostUpdater) start() {
	if u == nil || u.post == nil || u.started {
		return
	}
	u.started = true
	u.publishControlEvent(postStreamingControlStart)
}

func (u *streamingPostUpdater) finish() {
	if u == nil || u.post == nil || u.finished {
		return
	}
	u.finished = true
	u.publishControlEvent(postStreamingControlEnd)
}

func (u *streamingPostUpdater) sendUpdateEvent(message string) {
	if u == nil || u.post == nil {
		return
	}
	u.plugin.API.PublishWebSocketEvent("postupdate", map[string]any{
		"post_id": u.post.Id,
		"next":    message,
	}, &model.WebsocketBroadcast{ChannelId: u.post.ChannelId})
}

func (u *streamingPostUpdater) publishControlEvent(control string) {
	if u == nil || u.post == nil || strings.TrimSpace(control) == "" {
		return
	}
	u.plugin.API.PublishWebSocketEvent("postupdate", map[string]any{
		"post_id": u.post.Id,
		"control": control,
	}, &model.WebsocketBroadcast{ChannelId: u.post.ChannelId})
}

func buildBotStreamingMessage(displayName, output string) string {
	body := stripLeadingLangflowLabel(strings.TrimSpace(output))
	if body == "" {
		body = "_응답 생성 중입니다..._"
	}
	return body
}

func buildBotResponseMessage(displayName, output, correlationID string, streaming bool) string {
	body := stripLeadingLangflowLabel(strings.TrimSpace(output))
	if body == "" && streaming {
		body = "_응답 생성 중입니다..._"
	}
	if body == "" {
		body = "_빈 응답이 반환되었습니다._"
	}

	parts := []string{body, "", fmt.Sprintf("_Correlation ID:_ `%s`", correlationID)}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

type executionFailureView struct {
	HasFailure bool
	Message    string
	ErrorCode  string
	Detail     string
	Hint       string
	RequestURL string
	HTTPStatus int
	Retryable  bool
}

func describeExecutionFailure(err error, defaultRetryable bool) executionFailureView {
	if err == nil {
		return executionFailureView{}
	}

	var callErr *langflowCallError
	if errors.As(err, &callErr) {
		return executionFailureView{
			HasFailure: true,
			Message:    callErr.Error(),
			ErrorCode:  callErr.Code,
			Detail:     callErr.Detail,
			Hint:       callErr.Hint,
			RequestURL: callErr.RequestURL,
			HTTPStatus: callErr.StatusCode,
			Retryable:  callErr.Retryable,
		}
	}

	return executionFailureView{
		HasFailure: true,
		Message:    strings.TrimSpace(err.Error()),
		Retryable:  defaultRetryable,
	}
}

func buildBotFailureMessage(bot BotDefinition, correlationID string, failure executionFailureView) string {
	lines := []string{
		fmt.Sprintf("Flow `%s` 호출에 실패했습니다.", bot.FlowID),
	}

	if failure.Message != "" {
		lines = append(lines, "", failure.Message)
	}
	if failure.Detail != "" && !strings.Contains(failure.Message, "상세: "+failure.Detail) {
		lines = append(lines, "", "상세: "+failure.Detail)
	}
	if failure.Hint != "" && !strings.Contains(failure.Message, "조치: "+failure.Hint) {
		lines = append(lines, "", "조치: "+failure.Hint)
	}
	if failure.HTTPStatus > 0 && !strings.Contains(failure.Message, "HTTP 상태:") {
		lines = append(lines, "", fmt.Sprintf("HTTP 상태: `%d`", failure.HTTPStatus))
	}
	if failure.RequestURL != "" && !strings.Contains(failure.Message, "요청 URL:") {
		lines = append(lines, "", "요청 URL: "+failure.RequestURL)
	}
	if failure.Retryable {
		lines = append(lines, "", "_재시도 가능:_ 예")
	}
	lines = append(lines, "", fmt.Sprintf("_Correlation ID:_ `%s`", correlationID))
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildLangflowSessionID(request BotRunRequest, bot BotDefinition) string {
	scope := request.RootID
	if scope == "" {
		scope = request.ChannelID
	}
	if scope == "" {
		scope = request.UserID
	}
	return truncateString(fmt.Sprintf("mattermost:%s:%s:%s", bot.ID, scope, request.UserID), 190)
}

func clonePostProps(source model.StringInterface) model.StringInterface {
	if source == nil {
		return nil
	}
	props := make(model.StringInterface, len(source))
	for key, value := range source {
		props[key] = value
	}
	return props
}

func isBotNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "resource bot not found") ||
		strings.Contains(lower, "bot does not exist") ||
		strings.Contains(lower, "unable to get bot")
}

func joinSyncIssues(issues []string) string {
	filtered := make([]string, 0, len(issues))
	for _, issue := range issues {
		issue = strings.TrimSpace(issue)
		if issue == "" {
			continue
		}
		filtered = append(filtered, issue)
	}
	return strings.Join(filtered, " | ")
}

func stripLeadingLangflowLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	lines := strings.Split(value, "\n")
	for len(lines) > 0 {
		normalized := strings.ToLower(strings.TrimSpace(lines[0]))
		normalized = strings.TrimPrefix(normalized, "#")
		normalized = strings.TrimPrefix(normalized, "#")
		normalized = strings.TrimPrefix(normalized, "#")
		normalized = strings.TrimSpace(strings.Trim(normalized, "*`:_-"))
		if normalized != "langflow" {
			break
		}
		lines = lines[1:]
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

type botAttachment struct {
	FileID    string
	Name      string
	MIMEType  string
	Extension string
	Size      int64
	Content   []byte
	IsImage   bool
}

type langflowPreparedAttachments struct {
	Tweaks             map[string]any
	Metadata           []map[string]any
	UploadedFilePaths  []string
	UploadedImagePaths []string
}

type langflowV2FileUploadResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type langflowV1ImageUploadResponse struct {
	FlowID   string `json:"flowId"`
	FilePath string `json:"file_path"`
	Image    string `json:"image"`
}

func (p *Plugin) collectBotAttachments(fileIDs []string, channelID string) ([]botAttachment, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}

	attachments := make([]botAttachment, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		fileID = strings.TrimSpace(fileID)
		if fileID == "" {
			continue
		}

		info, appErr := p.API.GetFileInfo(fileID)
		if appErr != nil {
			return nil, fmt.Errorf("failed to load Mattermost file info %q: %w", fileID, appErr)
		}
		if strings.TrimSpace(channelID) != "" && strings.TrimSpace(info.ChannelId) != "" && info.ChannelId != channelID {
			return nil, fmt.Errorf("Mattermost file %q does not belong to channel %q", attachmentLabel(info), channelID)
		}

		content, appErr := p.API.GetFile(fileID)
		if appErr != nil {
			return nil, fmt.Errorf("failed to download Mattermost file %q: %w", fileID, appErr)
		}

		attachment := botAttachment{
			FileID:    fileID,
			Name:      defaultIfEmpty(strings.TrimSpace(info.Name), fileID),
			MIMEType:  detectAttachmentMIMEType(info, content),
			Extension: strings.ToLower(strings.TrimPrefix(strings.TrimSpace(info.Extension), ".")),
			Size:      info.Size,
			Content:   content,
		}
		attachment.IsImage = isImageAttachment(attachment)
		attachments = append(attachments, attachment)
	}

	return attachments, nil
}

func detectAttachmentMIMEType(info *model.FileInfo, content []byte) string {
	if info != nil {
		if value := strings.TrimSpace(info.MimeType); value != "" {
			return value
		}
		if value := strings.TrimSpace(info.Extension); value != "" {
			if detected := mime.TypeByExtension("." + strings.TrimPrefix(value, ".")); detected != "" {
				return detected
			}
		}
	}

	if len(content) > 0 {
		return http.DetectContentType(content)
	}

	return "application/octet-stream"
}

func isImageAttachment(attachment botAttachment) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(attachment.MIMEType)), "image/") {
		return true
	}

	switch attachment.Extension {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "svgz", "heic", "heif":
		return true
	default:
		return false
	}
}

func buildAttachmentMetadata(attachment botAttachment) map[string]any {
	item := map[string]any{
		"file_id":   attachment.FileID,
		"name":      attachment.Name,
		"mime_type": attachment.MIMEType,
		"size":      attachment.Size,
		"is_image":  attachment.IsImage,
	}
	if attachment.Extension != "" {
		item["extension"] = attachment.Extension
	}
	return item
}

func (p *Plugin) prepareLangflowAttachments(
	ctx context.Context,
	cfg *runtimeConfiguration,
	bot BotDefinition,
	request BotRunRequest,
	correlationID string,
) (*langflowPreparedAttachments, error) {
	attachments, err := p.collectBotAttachments(request.FileIDs, request.ChannelID)
	if err != nil {
		return nil, err
	}
	if len(attachments) == 0 {
		return nil, nil
	}

	prepared := &langflowPreparedAttachments{
		Tweaks:   map[string]any{},
		Metadata: make([]map[string]any, 0, len(attachments)),
	}

	filePaths := make([]string, 0, len(attachments))
	imagePaths := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		prepared.Metadata = append(prepared.Metadata, buildAttachmentMetadata(attachment))

		if attachment.IsImage {
			if bot.ImageComponentID == "" {
				return nil, newLangflowCallError(
					"image_component_missing",
					"이미지 첨부를 전달할 Langflow 컴포넌트가 설정되지 않았습니다.",
					fmt.Sprintf("봇 @%s 에는 image_component_id 가 없어서 이미지 %q 를 보낼 수 없습니다.", bot.Username, attachment.Name),
					"관리자 설정에서 이 봇의 이미지 첨부용 컴포넌트 ID를 입력하세요.",
					"",
					0,
					false,
				)
			}

			imagePath, uploadErr := p.uploadLangflowImage(ctx, cfg, bot, attachment, correlationID)
			if uploadErr != nil {
				return nil, uploadErr
			}
			imagePaths = append(imagePaths, imagePath)
			continue
		}

		if bot.FileComponentID == "" {
			return nil, newLangflowCallError(
				"file_component_missing",
				"첨부 파일을 전달할 Langflow 컴포넌트가 설정되지 않았습니다.",
				fmt.Sprintf("봇 @%s 에는 file_component_id 가 없어서 파일 %q 를 보낼 수 없습니다.", bot.Username, attachment.Name),
				"관리자 설정에서 이 봇의 문서 파일용 컴포넌트 ID를 입력하세요.",
				"",
				0,
				false,
			)
		}

		filePath, uploadErr := p.uploadLangflowFile(ctx, cfg, bot, attachment, correlationID)
		if uploadErr != nil {
			return nil, uploadErr
		}
		filePaths = append(filePaths, filePath)
	}

	if len(filePaths) > 0 {
		prepared.UploadedFilePaths = append(prepared.UploadedFilePaths, filePaths...)
		prepared.Tweaks = mergeLangflowComponentSetting(prepared.Tweaks, bot.FileComponentID, "path", filePaths)
	}
	if len(imagePaths) > 0 {
		prepared.UploadedImagePaths = append(prepared.UploadedImagePaths, imagePaths...)
		imageValue := any(imagePaths)
		if len(imagePaths) == 1 {
			imageValue = imagePaths[0]
		}
		prepared.Tweaks = mergeLangflowComponentSetting(prepared.Tweaks, bot.ImageComponentID, "files", imageValue)
	}

	return prepared, nil
}

func attachmentLabel(info *model.FileInfo) string {
	if info == nil {
		return ""
	}
	if value := strings.TrimSpace(info.Name); value != "" {
		return value
	}
	return strings.TrimSpace(info.Id)
}

func mergeLangflowComponentSetting(tweaks map[string]any, componentID, key string, value any) map[string]any {
	componentID = strings.TrimSpace(componentID)
	key = strings.TrimSpace(key)
	if componentID == "" || key == "" || value == nil {
		return tweaks
	}
	if tweaks == nil {
		tweaks = map[string]any{}
	}

	componentSettings := map[string]any{}
	if existing, ok := tweaks[componentID].(map[string]any); ok {
		for field, fieldValue := range existing {
			componentSettings[field] = fieldValue
		}
	}
	componentSettings[key] = value
	tweaks[componentID] = componentSettings
	return tweaks
}

func (p *Plugin) uploadLangflowFile(ctx context.Context, cfg *runtimeConfiguration, bot BotDefinition, attachment botAttachment, correlationID string) (string, error) {
	endpointURL := buildURLWithPathSegments(cfg.ParsedBaseURL, append(langflowServicePathSegments(cfg.ParsedBaseURL), "api", "v2", "files")...)
	return p.uploadLangflowMultipartFile(ctx, cfg, bot, endpointURL, attachment, correlationID, func(body []byte) (string, error) {
		var response langflowV2FileUploadResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return "", fmt.Errorf("failed to decode Langflow file upload response: %w", err)
		}
		path := strings.TrimSpace(response.Path)
		if path == "" {
			return "", fmt.Errorf("Langflow file upload response did not include a path")
		}
		return path, nil
	})
}

func (p *Plugin) uploadLangflowImage(ctx context.Context, cfg *runtimeConfiguration, bot BotDefinition, attachment botAttachment, correlationID string) (string, error) {
	endpointURL := buildURLWithPathSegments(cfg.ParsedBaseURL, append(langflowAPIPathSegments(cfg.ParsedBaseURL), "files", "upload", bot.FlowID)...)
	return p.uploadLangflowMultipartFile(ctx, cfg, bot, endpointURL, attachment, correlationID, func(body []byte) (string, error) {
		var response langflowV1ImageUploadResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return "", fmt.Errorf("failed to decode Langflow image upload response: %w", err)
		}

		path := strings.TrimSpace(response.FilePath)
		if path == "" {
			path = strings.TrimSpace(response.Image)
		}
		if path == "" {
			return "", fmt.Errorf("Langflow image upload response did not include a file path")
		}
		return path, nil
	})
}

func (p *Plugin) uploadLangflowMultipartFile(
	ctx context.Context,
	cfg *runtimeConfiguration,
	bot BotDefinition,
	endpointURL *url.URL,
	attachment botAttachment,
	correlationID string,
	parse func([]byte) (string, error),
) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", sanitizeUploadFilename(attachment.Name))
	if err != nil {
		return "", fmt.Errorf("failed to create Langflow multipart upload: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(attachment.Content)); err != nil {
		return "", fmt.Errorf("failed to write Langflow multipart upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to finalize Langflow multipart upload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL.String(), &body)
	if err != nil {
		return "", fmt.Errorf("failed to create Langflow file upload request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Correlation-ID", correlationID)
	p.applyAuthHeader(request, cfg, &bot)

	client := &http.Client{Timeout: cfg.DefaultTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", classifyLangflowRequestError(endpointURL.String(), err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return "", newLangflowCallError(
			"file_upload_read_failed",
			"Langflow 파일 업로드 응답을 읽지 못했습니다.",
			err.Error(),
			"Langflow 서버 로그와 프록시 제한을 확인하세요.",
			endpointURL.String(),
			response.StatusCode,
			true,
		)
	}
	if response.StatusCode >= http.StatusBadRequest {
		return "", classifyLangflowHTTPError(endpointURL.String(), response.StatusCode, response.Header, responseBody)
	}
	if looksLikeHTMLResponse(response.Header.Get("Content-Type"), responseBody) {
		return "", newUnexpectedHTMLResponseError(endpointURL.String())
	}

	path, parseErr := parse(responseBody)
	if parseErr != nil {
		return "", newLangflowCallError(
			"file_upload_parse_failed",
			"Langflow 파일 업로드 응답을 해석하지 못했습니다.",
			parseErr.Error(),
			"Langflow 버전과 파일 업로드 응답 형식을 확인하세요.",
			endpointURL.String(),
			response.StatusCode,
			false,
		)
	}
	return path, nil
}

func sanitizeUploadFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "attachment"
	}
	base := filepath.Base(name)
	if strings.TrimSpace(base) == "" || base == "." || base == string(filepath.Separator) {
		return "attachment"
	}
	return base
}

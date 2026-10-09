/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package suchuang

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// buildGeminiPayload 将 Gemini generateContent 协议请求转换为速创 nanobanana 系列图片接口的请求体。
// 参考图 inlineData(base64) 会先通过已配置的 OSS 转为公网 URL（速创仅接受 URL）。
func (a *Adaptor) buildGeminiPayload(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (map[string]any, error) {
	payload := map[string]any{
		"prompt": extractGeminiText(request),
	}

	aspectRatio, imageSize := parseGeminiImageConfig(request.GenerationConfig.ImageConfig)
	if imageSize != "" {
		payload["size"] = imageSize
	}
	if aspectRatio != "" {
		payload["aspectRatio"] = aspectRatio
	}

	inlineImages := extractGeminiInlineImages(request)
	if len(inlineImages) > 0 {
		urls := make([]string, 0, len(inlineImages))
		for i, img := range inlineImages {
			url, err := uploadBase64ToOSS(c.Request.Context(), info.RequestId, img.MimeType, img.Data, i)
			if err != nil {
				return nil, fmt.Errorf("upload reference image to OSS failed: %w", err)
			}
			urls = append(urls, url)
		}
		// nanobanana 的 urls 为数组（最多 14 张参考图）
		payload["urls"] = urls
	}

	return payload, nil
}

// geminiHandler 提交并轮询后，把图片结果转成 Gemini generateContent 响应（inlineData base64）。
func (a *Adaptor) geminiHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.Usage) {
	images, apiErr := a.submitAndPoll(c, info, resp)
	if apiErr != nil {
		return apiErr, nil
	}

	geminiResponse, err := buildGeminiImageResponse(c, images)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	jsonResponse, err := common.Marshal(geminiResponse)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	service.IOCopyBytesGracefully(c, resp, jsonResponse)

	return nil, &dto.Usage{}
}

// buildGeminiImageResponse 把图片列表转成 Gemini generateContent 响应。
func buildGeminiImageResponse(c *gin.Context, images []dto.ImageData) (*dto.GeminiChatResponse, error) {
	parts := make([]dto.GeminiPart, 0, len(images))
	for _, img := range images {
		base64Data, mimeType, err := imageToBase64(c, img)
		if err != nil {
			return nil, err
		}
		parts = append(parts, dto.GeminiPart{
			InlineData: &dto.GeminiInlineData{
				MimeType: mimeType,
				Data:     base64Data,
			},
		})
	}

	stop := "STOP"
	return &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content:      dto.GeminiChatContent{Role: "model", Parts: parts},
				FinishReason: &stop,
			},
		},
	}, nil
}

// imageToBase64 将图片结果（URL 或 base64）统一转为纯 base64 与 mimeType。
func imageToBase64(c *gin.Context, img dto.ImageData) (string, string, error) {
	if img.B64Json != "" {
		if strings.Contains(img.B64Json, ",") {
			mimeType, base64Data, err := service.DecodeBase64FileData(img.B64Json)
			if err != nil {
				return "", "", err
			}
			return base64Data, mimeType, nil
		}
		return img.B64Json, detectImageMimeType(img.B64Json), nil
	}
	if img.Url != "" {
		base64Data, mimeType, err := service.GetBase64Data(c, types.NewURLFileSource(img.Url), "download suchuang image for gemini response")
		if err != nil {
			return "", "", err
		}
		return base64Data, mimeType, nil
	}
	return "", "", errors.New("empty image data in suchuang result")
}

// uploadBase64ToOSS 将 base64 图片上传到已配置 OSS，返回公网 URL。
func uploadBase64ToOSS(ctx context.Context, requestID, mimeType, base64Data string, index int) (string, error) {
	if mimeType == "" {
		mimeType = detectImageMimeType(base64Data)
	}
	meta := service.GeneratedImageUploadMeta{
		RequestID:      requestID,
		CandidateIndex: index,
		PartIndex:      0,
		MimeType:       mimeType,
	}
	url, _, _, err := uploadGeneratedImage(ctx, meta, base64Data)
	if err != nil {
		return "", err
	}
	return url, nil
}

// detectImageMimeType 解码 base64 前若干字节并探测图片 MIME 类型，失败时返回 image/png。
func detectImageMimeType(base64Data string) string {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil || len(data) == 0 {
		return "image/png"
	}
	if mimeType := http.DetectContentType(data); strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}
	return "image/png"
}

// extractGeminiText 提取所有 contents 中的文本 part 并拼接为 prompt。
func extractGeminiText(request *dto.GeminiChatRequest) string {
	var texts []string
	for _, content := range request.Contents {
		for _, part := range content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

// extractGeminiInlineImages 提取 contents 中的参考图（inlineData，仅 image/* 类型）。
func extractGeminiInlineImages(request *dto.GeminiChatRequest) []dto.GeminiInlineData {
	var images []dto.GeminiInlineData
	for _, content := range request.Contents {
		for _, part := range content.Parts {
			if part.InlineData == nil {
				continue
			}
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(part.InlineData.MimeType)), "image/") {
				continue
			}
			if strings.TrimSpace(part.InlineData.Data) == "" {
				continue
			}
			images = append(images, *part.InlineData)
		}
	}
	return images
}

// parseGeminiImageConfig 从 generationConfig.imageConfig 中解析 aspectRatio 与 imageSize，
// 同时兼容 camelCase 与 snake_case 字段名。
func parseGeminiImageConfig(raw json.RawMessage) (aspectRatio, imageSize string) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" || strings.TrimSpace(string(raw)) == "{}" {
		return "", ""
	}
	var m map[string]any
	if err := common.Unmarshal(raw, &m); err != nil {
		return "", ""
	}
	aspectRatio = geminiImageConfigString(m, "aspectRatio", "aspect_ratio")
	imageSize = geminiImageConfigString(m, "imageSize", "image_size")
	return aspectRatio, imageSize
}

func geminiImageConfigString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

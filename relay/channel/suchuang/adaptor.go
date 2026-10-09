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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

// GetRequestURL 模型名即速创API的异步端点 slug：
// POST {base}/api/async/{model} 提交任务，之后轮询 {base}/api/async/detail。
func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	model := info.UpstreamModelName
	if model == "" {
		model = info.OriginModelName
	}
	if model == "" {
		return "", errors.New("model is required")
	}
	return fmt.Sprintf("%s/api/async/%s", strings.TrimSuffix(info.ChannelBaseUrl, "/"), url.PathEscape(model)), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	// 速创API鉴权：Authorization 直接传密钥，不带 Bearer 前缀
	req.Set("Authorization", info.ApiKey)
	req.Set("Content-Type", "application/json")
	return nil
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	payload, err := a.buildGeminiPayload(c, info, request)
	if err != nil {
		return nil, err
	}
	c.Set(suchuangPayloadContextKey, payload)
	return payload, nil
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations:
		payload, err := buildImagePayload(request)
		if err != nil {
			return nil, err
		}
		c.Set(suchuangPayloadContextKey, payload)
		return payload, nil
	case relayconstant.RelayModeImagesEdits:
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, fmt.Errorf("parse multipart form failed: %w", err)
		}
		payload, err := buildImageEditPayload(c.Request.Context(), form, request, info.RequestId)
		if err != nil {
			return nil, err
		}
		c.Set(suchuangPayloadContextKey, payload)
		return payload, nil
	default:
		return nil, fmt.Errorf("SuChuang channel only supports image generations and edits, relay mode %d is not supported", info.RelayMode)
	}
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		apiErr, usage := a.imageHandler(c, resp, info)
		return usage, apiErr
	case relayconstant.RelayModeGemini:
		apiErr, usage := a.geminiHandler(c, resp, info)
		return usage, apiErr
	default:
		return nil, types.NewError(fmt.Errorf("SuChuang channel only supports image generations, edits and Gemini image generation"), types.ErrorCodeBadResponse)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

// buildImagePayload 将 OpenAI 图片请求转换为速创异步图片接口的请求体。
// 平台参数：prompt/urls/aspectRatio/quality/background/mask，
// 其余未识别字段原样透传，保证平台不同模型的差异化参数可用。
func buildImagePayload(request dto.ImageRequest) (map[string]any, error) {
	payload := map[string]any{
		"prompt": request.Prompt,
	}

	if request.Size != "" {
		// 速创 aspectRatio 直接接受 "1024x1024" 这类分辨率串
		payload["aspectRatio"] = request.Size
	}
	if request.Quality != "" {
		payload["quality"] = request.Quality
	}
	if val := jsonRawToString(request.Background); val != "" {
		payload["background"] = val
	}
	if val := jsonRawToString(request.Mask); val != "" {
		payload["mask"] = val
	}

	// 参考图：urls（平台原生字段，多个以英文逗号拼接）
	if urls := collectReferenceUrls(request); len(urls) > 0 {
		payload["urls"] = strings.Join(urls, ",")
	}

	for key, raw := range request.Extra {
		if _, exists := payload[key]; exists {
			continue
		}
		var val any
		if err := common.Unmarshal(raw, &val); err != nil {
			return nil, fmt.Errorf("invalid extra field %q: %w", key, err)
		}
		payload[key] = val
	}
	return payload, nil
}

// collectReferenceUrls 按优先级收集参考图：urls（平台原生）> images > image。
func collectReferenceUrls(request dto.ImageRequest) []string {
	if urls := jsonRawToStringSlice(request.Extra["urls"]); len(urls) > 0 {
		return urls
	}
	if urls := jsonRawToStringSlice(request.Images); len(urls) > 0 {
		return urls
	}
	return jsonRawToStringSlice(request.Image)
}

// jsonRawToString 提取 JSON 字符串字面量的值，非字符串返回空。
func jsonRawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := common.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// jsonRawToStringSlice 支持 JSON 字符串（单个）或字符串数组，返回字符串切片。
func jsonRawToStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if err := common.Unmarshal(raw, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			return nil
		}
		return splitUrlList(single)
	}
	var list []string
	if err := common.Unmarshal(raw, &list); err == nil {
		var out []string
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}

func splitUrlList(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

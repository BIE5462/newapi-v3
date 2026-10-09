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
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractGeminiText(t *testing.T) {
	req := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{
				{Text: "把这只猫"},
				{Text: "变成宇航员"},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "aGVsbG8="}},
			}},
		},
	}
	assert.Equal(t, "把这只猫\n变成宇航员", extractGeminiText(req))
}

func TestExtractGeminiInlineImages(t *testing.T) {
	req := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Parts: []dto.GeminiPart{
				{Text: "prompt"},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "aGVsbG8="}},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/jpeg", Data: "d29ybGQ="}},
				{InlineData: &dto.GeminiInlineData{MimeType: "video/mp4", Data: "bm9wZQ=="}}, // 非图片忽略
			}},
		},
	}
	images := extractGeminiInlineImages(req)
	require.Len(t, images, 2)
	assert.Equal(t, "image/png", images[0].MimeType)
	assert.Equal(t, "image/jpeg", images[1].MimeType)
}

func TestParseGeminiImageConfig(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantRatio string
		wantSize  string
	}{
		{"camelCase", `{"aspectRatio":"16:9","imageSize":"2K"}`, "16:9", "2K"},
		{"snake_case", `{"aspect_ratio":"1:1","image_size":"4K"}`, "1:1", "4K"},
		{"empty", ``, "", ""},
		{"null", `null`, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, size := parseGeminiImageConfig(json.RawMessage(tt.raw))
			assert.Equal(t, tt.wantRatio, ratio)
			assert.Equal(t, tt.wantSize, size)
		})
	}
}

func TestBuildGeminiPayload(t *testing.T) {
	stubUpload(t, []string{"https://oss.example.com/ref1.png", "https://oss.example.com/ref2.png"})

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1beta/models/image_nanoBanana2:generateContent", nil)

	info := &relaycommon.RelayInfo{RequestId: "req-1"}
	adaptor := &Adaptor{}

	req := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Parts: []dto.GeminiPart{
				{Text: "把这只猫变成宇航员"},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "aGVsbG8="}},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/jpeg", Data: "d29ybGQ="}},
			}},
		},
		GenerationConfig: dto.GeminiChatGenerationConfig{
			ImageConfig: json.RawMessage(`{"aspectRatio":"1:1","imageSize":"2K"}`),
		},
	}

	payload, err := adaptor.buildGeminiPayload(c, info, req)
	require.NoError(t, err)

	assert.Equal(t, "把这只猫变成宇航员", payload["prompt"])
	assert.Equal(t, "2K", payload["size"])
	assert.Equal(t, "1:1", payload["aspectRatio"])

	urls, ok := payload["urls"].([]string)
	require.True(t, ok, "urls 应为数组")
	assert.Equal(t, []string{"https://oss.example.com/ref1.png", "https://oss.example.com/ref2.png"}, urls)
}

func TestBuildGeminiPayloadNoImageConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)

	adaptor := &Adaptor{}
	payload, err := adaptor.buildGeminiPayload(c, &relaycommon.RelayInfo{}, &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{Parts: []dto.GeminiPart{{Text: "a cat"}}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "a cat", payload["prompt"])
	assert.NotContains(t, payload, "urls")
	assert.NotContains(t, payload, "size")
	assert.NotContains(t, payload, "aspectRatio")
}

func TestBuildGeminiImageResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	images := []dto.ImageData{
		{B64Json: "aGVsbG8="}, // 纯 base64
	}
	resp, err := buildGeminiImageResponse(c, images)
	require.NoError(t, err)
	require.Len(t, resp.Candidates, 1)
	require.Len(t, resp.Candidates[0].Content.Parts, 1)
	assert.NotNil(t, resp.Candidates[0].Content.Parts[0].InlineData)
	assert.Equal(t, "aGVsbG8=", resp.Candidates[0].Content.Parts[0].InlineData.Data)
}

func TestImageToBase64DataURI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	base64Data, mimeType, err := imageToBase64(c, dto.ImageData{B64Json: "data:image/png;base64,aGVsbG8="})
	require.NoError(t, err)
	assert.Equal(t, "aGVsbG8=", base64Data)
	assert.Equal(t, "image/png", mimeType)
}

func TestDetectImageMimeType(t *testing.T) {
	// 1x1 透明 PNG 的 base64
	mimeType := detectImageMimeType("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	assert.Equal(t, "image/png", mimeType)

	// 非法 base64 回退 png
	assert.Equal(t, "image/png", detectImageMimeType("!!invalid!!"))
}

func TestGeminiPayloadMarshalable(t *testing.T) {
	// 确保 payload 可被 JSON 序列化（urls 为数组而非字符串）
	payload := map[string]any{
		"prompt":      "a cat",
		"urls":        []string{"https://a.com/1.png"},
		"size":        "2K",
		"aspectRatio": "1:1",
	}
	data, err := common.Marshal(payload)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"urls":["https://a.com/1.png"]`)
}

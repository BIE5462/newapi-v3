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
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := common.Marshal(v)
	require.NoError(t, err)
	return data
}

func TestBuildImagePayload(t *testing.T) {
	t.Run("standard openai fields map to suchuang params", func(t *testing.T) {
		payload, err := buildImagePayload(dto.ImageRequest{
			Prompt:     "a cat on the moon",
			Size:       "1024x1024",
			Quality:    "high",
			Background: rawJSON(t, "transparent"),
			Mask:       rawJSON(t, "https://example.com/mask.png"),
		})
		require.NoError(t, err)
		assert.Equal(t, "a cat on the moon", payload["prompt"])
		assert.Equal(t, "1024x1024", payload["aspectRatio"])
		assert.Equal(t, "high", payload["quality"])
		assert.Equal(t, "transparent", payload["background"])
		assert.Equal(t, "https://example.com/mask.png", payload["mask"])
		assert.NotContains(t, payload, "urls")
	})

	t.Run("urls extra field is joined with commas", func(t *testing.T) {
		payload, err := buildImagePayload(dto.ImageRequest{
			Prompt: "edit this",
			Extra: map[string]json.RawMessage{
				"urls": rawJSON(t, "https://a.com/1.jpg, https://a.com/2.png"),
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "https://a.com/1.jpg,https://a.com/2.png", payload["urls"])
	})

	t.Run("images array is used as reference urls", func(t *testing.T) {
		payload, err := buildImagePayload(dto.ImageRequest{
			Prompt: "edit this",
			Images: rawJSON(t, []string{"https://a.com/1.jpg", "https://a.com/2.png"}),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://a.com/1.jpg,https://a.com/2.png", payload["urls"])
	})

	t.Run("urls extra field wins over images", func(t *testing.T) {
		payload, err := buildImagePayload(dto.ImageRequest{
			Prompt: "edit this",
			Images: rawJSON(t, []string{"https://b.com/from-images.jpg"}),
			Extra: map[string]json.RawMessage{
				"urls": rawJSON(t, "https://a.com/from-urls.jpg"),
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "https://a.com/from-urls.jpg", payload["urls"])
	})

	t.Run("unknown extra fields pass through", func(t *testing.T) {
		payload, err := buildImagePayload(dto.ImageRequest{
			Prompt: "custom params",
			Extra: map[string]json.RawMessage{
				"custom_param": rawJSON(t, 42),
			},
		})
		require.NoError(t, err)
		assert.Equal(t, float64(42), payload["custom_param"])
	})
}

func TestGetRequestURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://api.wuyinkeji.com/",
			UpstreamModelName: "image_gpt_2.5_flare",
		},
	}
	got, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://api.wuyinkeji.com/api/async/image_gpt_2.5_flare", got)
}

func TestGetRequestURLRequiresModel(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.wuyinkeji.com",
		},
	}
	_, err := adaptor.GetRequestURL(info)
	assert.Error(t, err)
}

func TestExtractResultImages(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantURL []string
		wantB64 []string
	}{
		{
			name:    "single url string",
			raw:     `"https://cdn.example.com/img.png"`,
			wantURL: []string{"https://cdn.example.com/img.png"},
		},
		{
			name:    "url array",
			raw:     `["https://cdn.example.com/1.png","https://cdn.example.com/2.png"]`,
			wantURL: []string{"https://cdn.example.com/1.png", "https://cdn.example.com/2.png"},
		},
		{
			name:    "object array with url field",
			raw:     `[{"url":"https://cdn.example.com/1.png"},{"url":"https://cdn.example.com/2.png"}]`,
			wantURL: []string{"https://cdn.example.com/1.png", "https://cdn.example.com/2.png"},
		},
		{
			name:    "object with b64_json field",
			raw:     `{"status":2,"b64_json":"aGVsbG8="}`,
			wantB64: []string{"aGVsbG8="},
		},
		{
			name:    "data url is treated as base64",
			raw:     `"data:image/png;base64,aGVsbG8="`,
			wantB64: []string{"data:image/png;base64,aGVsbG8="},
		},
		{
			name:    "unknown nested structure collects http urls",
			raw:     `{"status":2,"result":{"files":["https://cdn.example.com/a.png"]},"message":"ok"}`,
			wantURL: []string{"https://cdn.example.com/a.png"},
		},
		{
			name: "no image values",
			raw:  `{"status":2,"message":"done"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			images := extractResultImages([]byte(tt.raw))
			var gotURLs, gotB64 []string
			for _, img := range images {
				if img.Url != "" {
					gotURLs = append(gotURLs, img.Url)
				}
				if img.B64Json != "" {
					gotB64 = append(gotB64, img.B64Json)
				}
			}
			assert.Equal(t, tt.wantURL, gotURLs)
			assert.Equal(t, tt.wantB64, gotB64)
		})
	}
}

func TestSubmitResponseParsing(t *testing.T) {
	var resp SubmitResponse
	require.NoError(t, common.Unmarshal([]byte(`{"code":200,"msg":"成功","data":{"id":"image_4d39239e","count":10}}`), &resp))
	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, "image_4d39239e", resp.taskID())
	assert.Equal(t, 10, resp.countValue())
}

func TestSubmitResponseDataArrayTolerance(t *testing.T) {
	// 上游失败时 data 可能为空数组，不应导致解析失败
	var resp SubmitResponse
	require.NoError(t, common.Unmarshal([]byte(`{"code":400,"msg":"","data":[]}`), &resp))
	assert.Equal(t, 400, resp.Code)
	assert.Equal(t, "", resp.taskID())
	assert.Equal(t, 0, resp.countValue())
}

func TestSubmitResponseCountTolerance(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{"int", `{"data":{"id":"a","count":10}}`, 10},
		{"string", `{"data":{"id":"a","count":"10"}}`, 10},
		{"float", `{"data":{"id":"a","count":10.0}}`, 10},
		{"missing", `{"data":{"id":"a"}}`, 0},
		{"null", `{"data":{"id":"a","count":null}}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp SubmitResponse
			require.NoError(t, common.Unmarshal([]byte(tt.raw), &resp))
			assert.Equal(t, tt.want, resp.countValue())
		})
	}
}

func TestDetailDataParsing(t *testing.T) {
	var resp DetailResponse
	require.NoError(t, common.Unmarshal([]byte(`{"code":200,"msg":"成功","data":{"status":1,"message":"processing"}}`), &resp))
	var data detailData
	require.NoError(t, common.Unmarshal(resp.Data, &data))
	assert.Equal(t, taskStatusRunning, data.Status)
}

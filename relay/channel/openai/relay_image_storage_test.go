package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenaiImageHandlerLeavesURLAndIneligibleBase64Unchanged(t *testing.T) {
	t.Run("URL response", func(t *testing.T) {
		configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline))
		var uploadCount int
		uploadOpenAIGeneratedImage = func(context.Context, service.GeneratedImageUploadMeta, string) (string, string, int64, error) {
			uploadCount++
			return "", "", 0, nil
		}

		body := `{"created":1710000000,"data":[{"url":"https://upstream.example/image.png","b64_json":"must-not-be-uploaded","provider_extra":{"seed":7}}],"top_level_extra":"kept"}`
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", false)

		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		assert.Equal(t, 0, uploadCount)
		assert.Equal(t, body, recorder.Body.String())
	})

	t.Run("stream response", func(t *testing.T) {
		configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline))
		var uploadCount int
		uploadOpenAIGeneratedImage = func(context.Context, service.GeneratedImageUploadMeta, string) (string, string, int64, error) {
			uploadCount++
			return "", "", 0, nil
		}
		base64Data := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{map[string]any{"b64_json": base64Data}}})
		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", true)

		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		assert.Equal(t, 0, uploadCount)
		assert.Equal(t, body, recorder.Body.Bytes())
	})

	t.Run("storage disabled", func(t *testing.T) {
		cfg := openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline)
		cfg.Enabled = false
		configureOpenAIImageStorageForTest(t, cfg)
		body := `{"data":[{"b64_json":"aW1hZ2U="}]}`
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", false)

		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		assert.Equal(t, body, recorder.Body.String())
	})

	t.Run("image at threshold", func(t *testing.T) {
		configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline))
		base64Data := testOpenAIImageBase64(1024*1024, []byte("\x89PNG\r\n\x1a\n"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{map[string]any{"b64_json": base64Data}}})
		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)

		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		assert.Equal(t, body, recorder.Body.Bytes())
	})
}

func TestOpenaiImageHandlerUploadsBase64AndPreservesResponseFields(t *testing.T) {
	configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline))
	pngBase64 := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
	jpegBase64 := testOpenAIImageBase64(1024*1024+2, []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"))
	body := marshalOpenAIImageTestBody(t, map[string]any{
		"created": 1710000000,
		"data": []any{
			map[string]any{"url": "https://upstream.example/already.png", "b64_json": "must-not-be-uploaded", "provider_extra": "url-kept"},
			map[string]any{"b64_json": pngBase64, "revised_prompt": "draw a cat", "provider_extra": map[string]any{"seed": 7}},
			map[string]any{"b64_json": jpegBase64, "revised_prompt": "draw a dog"},
		},
		"metadata":        map[string]any{"request": "meta"},
		"top_level_extra": "kept",
		"usage": map[string]any{
			"input_tokens":  3,
			"output_tokens": 4,
			"total_tokens":  7,
		},
	})

	var mu sync.Mutex
	var metas []service.GeneratedImageUploadMeta
	uploadOpenAIGeneratedImage = func(_ context.Context, meta service.GeneratedImageUploadMeta, data string) (string, string, int64, error) {
		mu.Lock()
		metas = append(metas, meta)
		mu.Unlock()
		url := fmt.Sprintf("https://cdn.example.com/%s-%d", meta.RequestID, meta.CandidateIndex)
		return url, "gemini/generated/" + meta.RequestID, service.EstimateBase64DecodedBytes(data), nil
	}

	c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)
	info.RequestId = "req-openai-image"
	usage, apiErr := OpenaiImageHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.PromptTokens)
	assert.Equal(t, 4, usage.CompletionTokens)
	assert.Equal(t, 7, usage.TotalTokens)
	assert.ElementsMatch(t, []service.GeneratedImageUploadMeta{
		{RequestID: "req-openai-image", CandidateIndex: 1, PartIndex: 0, MimeType: "image/png"},
		{RequestID: "req-openai-image", CandidateIndex: 2, PartIndex: 0, MimeType: "image/jpeg"},
	}, metas)

	var payload map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(payload["data"], &items))
	require.Len(t, items, 3)
	assert.JSONEq(t, `"https://upstream.example/already.png"`, string(items[0]["url"]))
	assert.JSONEq(t, `"must-not-be-uploaded"`, string(items[0]["b64_json"]))
	assert.JSONEq(t, `"url-kept"`, string(items[0]["provider_extra"]))
	assert.JSONEq(t, `"https://cdn.example.com/req-openai-image-1"`, string(items[1]["url"]))
	assert.NotContains(t, items[1], "b64_json")
	assert.JSONEq(t, `"draw a cat"`, string(items[1]["revised_prompt"]))
	assert.JSONEq(t, `{"seed":7}`, string(items[1]["provider_extra"]))
	assert.JSONEq(t, `"https://cdn.example.com/req-openai-image-2"`, string(items[2]["url"]))
	assert.NotContains(t, items[2], "b64_json")
	assert.JSONEq(t, `"draw a dog"`, string(items[2]["revised_prompt"]))
	assert.JSONEq(t, `{"request":"meta"}`, string(payload["metadata"]))
	assert.JSONEq(t, `"kept"`, string(payload["top_level_extra"]))
	assert.JSONEq(t, `{"input_tokens":3,"output_tokens":4,"total_tokens":7}`, string(payload["usage"]))
	assert.Equal(t, strconv.Itoa(recorder.Body.Len()), recorder.Header().Get("Content-Length"))
}

func TestInspectOpenAIImageBase64DetectsMimeType(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		bytes    []byte
		expected string
	}{
		{name: "declared JPEG", prefix: "data:image/jpeg;base64,", bytes: []byte("not an image"), expected: "image/jpeg"},
		{name: "PNG", bytes: []byte("\x89PNG\r\n\x1a\nrest"), expected: "image/png"},
		{name: "JPEG", bytes: []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00rest"), expected: "image/jpeg"},
		{name: "WebP", bytes: []byte("RIFF\x10\x00\x00\x00WEBPVP8 rest"), expected: "image/webp"},
		{name: "GIF", bytes: []byte("GIF89a-rest"), expected: "image/gif"},
		{name: "HEIC", bytes: []byte("\x00\x00\x00\x18ftypheicrest"), expected: "image/heic"},
		{name: "HEIF", bytes: []byte("\x00\x00\x00\x18ftypmif1rest"), expected: "image/heif"},
		{name: "fallback PNG", bytes: []byte("unknown image bytes"), expected: "image/png"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := test.prefix + base64.StdEncoding.EncodeToString(test.bytes)
			cleanData, decodedBytes, mimeType, err := inspectOpenAIImageBase64(encoded)

			require.NoError(t, err)
			assert.Equal(t, base64.StdEncoding.EncodeToString(test.bytes), cleanData)
			assert.Equal(t, int64(len(test.bytes)), decodedBytes)
			assert.Equal(t, test.expected, mimeType)
		})
	}
}

func TestOpenaiImageHandlerUsesFallbackInlinePolicy(t *testing.T) {
	t.Run("partial upload failure", func(t *testing.T) {
		configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline))
		firstBase64 := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		secondBase64 := testOpenAIImageBase64(1024*1024+2, []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{
			map[string]any{"b64_json": firstBase64},
			map[string]any{"b64_json": secondBase64},
		}})
		uploadOpenAIGeneratedImage = func(_ context.Context, meta service.GeneratedImageUploadMeta, data string) (string, string, int64, error) {
			if meta.CandidateIndex == 1 {
				return "", "", 0, errors.New("fake upload failed")
			}
			return "https://cdn.example.com/ok.png", "gemini/generated/ok.png", service.EstimateBase64DecodedBytes(data), nil
		}

		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)
		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		items := unmarshalOpenAIImageItems(t, recorder.Body.Bytes())
		assert.NotContains(t, items[0], "b64_json")
		assert.JSONEq(t, `"https://cdn.example.com/ok.png"`, string(items[0]["url"]))
		assert.Contains(t, items[1], "b64_json")
		assert.NotContains(t, items[1], "url")
	})

	t.Run("single image size limit", func(t *testing.T) {
		cfg := openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline)
		cfg.MaxImageMB = 1
		configureOpenAIImageStorageForTest(t, cfg)
		var uploadCount int
		uploadOpenAIGeneratedImage = func(context.Context, service.GeneratedImageUploadMeta, string) (string, string, int64, error) {
			uploadCount++
			return "", "", 0, nil
		}
		base64Data := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{map[string]any{"b64_json": base64Data}}})
		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)

		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		assert.Equal(t, 0, uploadCount)
		assert.Equal(t, body, recorder.Body.Bytes())
	})

	t.Run("total size limit", func(t *testing.T) {
		cfg := openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline)
		cfg.MaxImageMB = 2
		cfg.MaxTotalMB = 2
		configureOpenAIImageStorageForTest(t, cfg)
		firstBase64 := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		secondBase64 := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{
			map[string]any{"b64_json": firstBase64},
			map[string]any{"b64_json": secondBase64},
		}})
		uploadOpenAIGeneratedImage = func(_ context.Context, meta service.GeneratedImageUploadMeta, data string) (string, string, int64, error) {
			return fmt.Sprintf("https://cdn.example.com/%d.png", meta.CandidateIndex), "gemini/generated/ok.png", service.EstimateBase64DecodedBytes(data), nil
		}

		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)
		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		items := unmarshalOpenAIImageItems(t, recorder.Body.Bytes())
		assert.NotContains(t, items[0], "b64_json")
		assert.Contains(t, items[1], "b64_json")
	})

	t.Run("invalid base64", func(t *testing.T) {
		configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFallbackInline))
		body := `{"data":[{"b64_json":"%%%invalid%%%"}]}`
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", false)

		_, apiErr := OpenaiImageHandler(c, info, resp)

		require.Nil(t, apiErr)
		assert.Equal(t, body, recorder.Body.String())
	})
}

func TestOpenaiImageHandlerFailRequestIsNotRetried(t *testing.T) {
	t.Run("upload failure", func(t *testing.T) {
		configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFailRequest))
		base64Data := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{map[string]any{"b64_json": base64Data}}})
		uploadOpenAIGeneratedImage = func(context.Context, service.GeneratedImageUploadMeta, string) (string, string, int64, error) {
			return "", "", 0, errors.New("fake upload failed")
		}

		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)
		usage, apiErr := OpenaiImageHandler(c, info, resp)

		assert.Nil(t, usage)
		require.NotNil(t, apiErr)
		assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
		assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.Empty(t, recorder.Body.String())
	})

	t.Run("single image size limit", func(t *testing.T) {
		cfg := openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFailRequest)
		cfg.MaxImageMB = 1
		configureOpenAIImageStorageForTest(t, cfg)
		base64Data := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{map[string]any{"b64_json": base64Data}}})

		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)
		usage, apiErr := OpenaiImageHandler(c, info, resp)

		assert.Nil(t, usage)
		require.NotNil(t, apiErr)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.Empty(t, recorder.Body.String())
	})

	t.Run("total size limit", func(t *testing.T) {
		cfg := openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFailRequest)
		cfg.MaxImageMB = 2
		cfg.MaxTotalMB = 2
		configureOpenAIImageStorageForTest(t, cfg)
		var uploadCount int
		uploadOpenAIGeneratedImage = func(context.Context, service.GeneratedImageUploadMeta, string) (string, string, int64, error) {
			uploadCount++
			return "", "", 0, nil
		}
		base64Data := testOpenAIImageBase64(1024*1024+1, []byte("\x89PNG\r\n\x1a\n"))
		body := marshalOpenAIImageTestBody(t, map[string]any{"data": []any{
			map[string]any{"b64_json": base64Data},
			map[string]any{"b64_json": base64Data},
		}})
		c, recorder, resp, info := newImageTestContext(t, string(body), "application/json", false)

		usage, apiErr := OpenaiImageHandler(c, info, resp)

		assert.Nil(t, usage)
		require.NotNil(t, apiErr)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.Equal(t, 0, uploadCount)
		assert.Empty(t, recorder.Body.String())
	})

	t.Run("invalid base64", func(t *testing.T) {
		configureOpenAIImageStorageForTest(t, openAIImageStorageTestSettings(system_setting.GeneratedImageStorageFailurePolicyFailRequest))
		body := `{"data":[{"b64_json":"%%%invalid%%%"}]}`
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", false)

		usage, apiErr := OpenaiImageHandler(c, info, resp)

		assert.Nil(t, usage)
		require.NotNil(t, apiErr)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.Empty(t, recorder.Body.String())
	})
}

func configureOpenAIImageStorageForTest(t *testing.T, cfg system_setting.GeneratedImageStorageSettings) {
	t.Helper()
	originalSettings := *system_setting.GetGeneratedImageStorageSettings()
	originalUploader := uploadOpenAIGeneratedImage
	*system_setting.GetGeneratedImageStorageSettings() = cfg
	t.Cleanup(func() {
		*system_setting.GetGeneratedImageStorageSettings() = originalSettings
		uploadOpenAIGeneratedImage = originalUploader
	})
}

func openAIImageStorageTestSettings(failurePolicy string) system_setting.GeneratedImageStorageSettings {
	return system_setting.GeneratedImageStorageSettings{
		Enabled:              true,
		Provider:             system_setting.GeneratedImageStorageProviderAliyunOSS,
		CredentialMode:       system_setting.GeneratedImageStorageCredentialModeEnv,
		Bucket:               "test-bucket",
		Region:               "cn-test",
		ThresholdMB:          1,
		MaxImageMB:           64,
		MaxTotalMB:           128,
		MaxUploadConcurrency: 2,
		UploadTimeoutSeconds: 10,
		FailurePolicy:        failurePolicy,
	}
}

func testOpenAIImageBase64(decodedBytes int, signature []byte) string {
	data := make([]byte, decodedBytes)
	copy(data, signature)
	return base64.StdEncoding.EncodeToString(data)
}

func marshalOpenAIImageTestBody(t *testing.T, payload any) []byte {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	return body
}

func unmarshalOpenAIImageItems(t *testing.T, body []byte) []map[string]json.RawMessage {
	t.Helper()
	var payload map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(body, &payload))
	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(payload["data"], &items))
	return items
}

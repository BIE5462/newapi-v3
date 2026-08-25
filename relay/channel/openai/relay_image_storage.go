package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

type openAIImageUploadTask struct {
	item           map[string]json.RawMessage
	imageIndex     int
	mimeType       string
	data           string
	estimatedBytes int64
	url            string
	key            string
	decodedBytes   int64
	err            error
}

var uploadOpenAIGeneratedImage = service.UploadGeneratedImage

func offloadOpenAIImageBase64(c *gin.Context, info *relaycommon.RelayInfo, responseBody []byte) ([]byte, bool, error) {
	cfg := system_setting.GetGeneratedImageStorageSettings().Normalized()
	if !cfg.Enabled || (info != nil && info.IsStream) {
		return responseBody, false, nil
	}

	var payload map[string]json.RawMessage
	if err := common.Unmarshal(responseBody, &payload); err != nil {
		return responseBody, false, nil
	}
	dataJSON, ok := payload["data"]
	if !ok {
		return responseBody, false, nil
	}

	var items []map[string]json.RawMessage
	if err := common.Unmarshal(dataJSON, &items); err != nil {
		return responseBody, false, nil
	}

	failRequest := cfg.FailurePolicy == system_setting.GeneratedImageStorageFailurePolicyFailRequest
	thresholdBytes := int64(cfg.ThresholdMB) * 1024 * 1024
	maxImageBytes := int64(cfg.MaxImageMB) * 1024 * 1024
	maxTotalBytes := int64(cfg.MaxTotalMB) * 1024 * 1024
	eligibleBytes := int64(0)
	tasks := make([]*openAIImageUploadTask, 0, len(items))

	for imageIndex, item := range items {
		var imageURL string
		if rawURL, exists := item["url"]; exists {
			if err := common.Unmarshal(rawURL, &imageURL); err == nil && strings.TrimSpace(imageURL) != "" {
				continue
			}
		}

		rawBase64, exists := item["b64_json"]
		if !exists {
			continue
		}
		var base64Data string
		if err := common.Unmarshal(rawBase64, &base64Data); err != nil {
			if failRequest {
				return responseBody, false, fmt.Errorf("invalid OpenAI image b64_json at data[%d]: %w", imageIndex, err)
			}
			logger.LogWarn(c, fmt.Sprintf("OpenAI generated image storage skipped invalid b64_json: image=%d err=%v", imageIndex, err))
			continue
		}
		if strings.TrimSpace(base64Data) == "" {
			continue
		}

		cleanData, decodedBytes, mimeType, err := inspectOpenAIImageBase64(base64Data)
		if err != nil {
			if failRequest {
				return responseBody, false, fmt.Errorf("invalid OpenAI image b64_json at data[%d]: %w", imageIndex, err)
			}
			logger.LogWarn(c, fmt.Sprintf("OpenAI generated image storage skipped invalid base64: image=%d err=%v", imageIndex, err))
			continue
		}
		if decodedBytes <= thresholdBytes {
			continue
		}
		if decodedBytes > maxImageBytes {
			err = fmt.Errorf("generated image exceeds max_image_mb: image=%d decoded_bytes=%d max_bytes=%d", imageIndex, decodedBytes, maxImageBytes)
			if failRequest {
				return responseBody, false, err
			}
			logger.LogWarn(c, "OpenAI generated image storage skipped: "+err.Error())
			continue
		}
		if eligibleBytes+decodedBytes > maxTotalBytes {
			err = fmt.Errorf("generated images exceed max_total_mb: image=%d decoded_total_bytes=%d max_bytes=%d", imageIndex, eligibleBytes+decodedBytes, maxTotalBytes)
			if failRequest {
				return responseBody, false, err
			}
			logger.LogWarn(c, "OpenAI generated image storage skipped: "+err.Error())
			continue
		}

		eligibleBytes += decodedBytes
		tasks = append(tasks, &openAIImageUploadTask{
			item:           item,
			imageIndex:     imageIndex,
			mimeType:       mimeType,
			data:           cleanData,
			estimatedBytes: decodedBytes,
		})
	}

	if len(tasks) == 0 {
		return responseBody, false, nil
	}

	requestID := "request"
	if info != nil && strings.TrimSpace(info.RequestId) != "" {
		requestID = info.RequestId
	} else if c != nil {
		if contextRequestID := strings.TrimSpace(c.GetString(common.RequestIdKey)); contextRequestID != "" {
			requestID = contextRequestID
		}
	}

	uploadCtx := context.Background()
	if c != nil && c.Request != nil {
		uploadCtx = c.Request.Context()
	}
	var cancel context.CancelFunc
	if failRequest {
		uploadCtx, cancel = context.WithCancel(uploadCtx)
		defer cancel()
	}

	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		go func(task *openAIImageUploadTask) {
			defer wg.Done()
			meta := service.GeneratedImageUploadMeta{
				RequestID:      requestID,
				CandidateIndex: task.imageIndex,
				PartIndex:      0,
				MimeType:       task.mimeType,
			}
			task.url, task.key, task.decodedBytes, task.err = uploadOpenAIGeneratedImage(uploadCtx, meta, task.data)
			if task.err != nil && cancel != nil {
				cancel()
			}
		}(task)
	}
	wg.Wait()

	var firstErr error
	for _, task := range tasks {
		if task.err != nil && firstErr == nil {
			firstErr = task.err
		}
	}
	if failRequest && firstErr != nil {
		return responseBody, false, fmt.Errorf("upload generated OpenAI image failed: %w", firstErr)
	}

	changed := false
	for _, task := range tasks {
		if task.err != nil {
			logger.LogWarn(c, fmt.Sprintf(
				"OpenAI generated image storage upload failed, fallback b64_json: image=%d estimated_bytes=%d err=%v",
				task.imageIndex,
				task.estimatedBytes,
				task.err,
			))
			continue
		}

		urlJSON, err := common.Marshal(task.url)
		if err != nil {
			return responseBody, false, fmt.Errorf("marshal generated OpenAI image URL: %w", err)
		}
		task.item["url"] = urlJSON
		delete(task.item, "b64_json")
		changed = true
		logger.LogDebug(c, "OpenAI generated image stored: image=%d bytes=%d key=%s", task.imageIndex, task.decodedBytes, task.key)
	}
	if !changed {
		return responseBody, false, nil
	}

	dataJSON, err := common.Marshal(items)
	if err != nil {
		return responseBody, false, fmt.Errorf("marshal OpenAI image response data: %w", err)
	}
	payload["data"] = dataJSON
	rewrittenBody, err := common.Marshal(payload)
	if err != nil {
		return responseBody, false, fmt.Errorf("marshal OpenAI image response: %w", err)
	}
	return rewrittenBody, true, nil
}

func inspectOpenAIImageBase64(data string) (cleanData string, decodedBytes int64, mimeType string, err error) {
	data = strings.TrimSpace(data)
	declaredMimeType := ""
	if commaIndex := strings.Index(data, ","); commaIndex >= 0 {
		prefix := data[:commaIndex]
		if strings.Contains(strings.ToLower(prefix), "base64") {
			mediaType := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(prefix, ";", 2)[0], "data:"))
			declaredMimeType = normalizeOpenAIImageMimeType(mediaType)
		}
	}

	cleanData = service.CleanGeneratedImageBase64(data)
	if cleanData == "" {
		return "", 0, "", fmt.Errorf("base64 data is empty")
	}

	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(cleanData))
	buffer := make([]byte, 32*1024)
	signature := make([]byte, 0, 512)
	for {
		n, readErr := decoder.Read(buffer)
		if n > 0 {
			decodedBytes += int64(n)
			if len(signature) < cap(signature) {
				remaining := cap(signature) - len(signature)
				if n < remaining {
					remaining = n
				}
				signature = append(signature, buffer[:remaining]...)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", 0, "", readErr
		}
	}
	if decodedBytes == 0 {
		return "", 0, "", fmt.Errorf("decoded image is empty")
	}

	if declaredMimeType != "" {
		return cleanData, decodedBytes, declaredMimeType, nil
	}
	if detectedMimeType := normalizeOpenAIImageMimeType(http.DetectContentType(signature)); detectedMimeType != "" {
		return cleanData, decodedBytes, detectedMimeType, nil
	}
	if len(signature) >= 12 && string(signature[4:8]) == "ftyp" {
		switch string(signature[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis":
			return cleanData, decodedBytes, "image/heic", nil
		case "mif1", "msf1":
			return cleanData, decodedBytes, "image/heif", nil
		}
	}
	return cleanData, decodedBytes, "image/png", nil
}

func normalizeOpenAIImageMimeType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if semicolonIndex := strings.Index(mimeType, ";"); semicolonIndex >= 0 {
		mimeType = strings.TrimSpace(mimeType[:semicolonIndex])
	}
	switch mimeType {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "image/heic", "image/heif":
		return mimeType
	case "image/jpg":
		return "image/jpeg"
	default:
		return ""
	}
}

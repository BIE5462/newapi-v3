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
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
)

// uploadGeneratedImage 允许在测试中替换为桩函数，避免真实依赖 OSS。
var uploadGeneratedImage = service.UploadGeneratedImage

// buildImageEditPayload 将 OpenAI 图片编辑请求（multipart）转换为速创异步图片接口的请求体。
// 上传的图片/遮罩文件会先通过已配置的 OSS 转为公网 URL（速创接口仅接受 URL）。
func buildImageEditPayload(ctx context.Context, form *multipart.Form, request dto.ImageRequest, requestID string) (map[string]any, error) {
	imageFiles := collectFormFiles(form, "image")
	if len(imageFiles) == 0 {
		return nil, errors.New("image file is required for image edits")
	}

	payload := map[string]any{}
	if request.Prompt != "" {
		payload["prompt"] = request.Prompt
	}
	if request.Size != "" {
		payload["aspectRatio"] = request.Size
	}
	if request.Quality != "" {
		payload["quality"] = request.Quality
	}

	urls := make([]string, 0, len(imageFiles))
	for i, fh := range imageFiles {
		url, err := uploadFileToOSS(ctx, requestID, fh, i)
		if err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	payload["urls"] = strings.Join(urls, ",")

	if maskFiles := collectFormFiles(form, "mask"); len(maskFiles) > 0 {
		maskURL, err := uploadFileToOSS(ctx, requestID, maskFiles[0], len(imageFiles))
		if err != nil {
			return nil, fmt.Errorf("upload mask failed: %w", err)
		}
		payload["mask"] = maskURL
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

// uploadFileToOSS 将 multipart 上传的文件上传到 OSS 并返回公网 URL。
func uploadFileToOSS(ctx context.Context, requestID string, fh *multipart.FileHeader, index int) (string, error) {
	f, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("open file %s failed: %w", fh.Filename, err)
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return "", fmt.Errorf("read file %s failed: %w", fh.Filename, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("file %s is empty", fh.Filename)
	}

	mimeType := http.DetectContentType(data)
	meta := service.GeneratedImageUploadMeta{
		RequestID:      requestID,
		CandidateIndex: index,
		PartIndex:      0,
		MimeType:       mimeType,
	}
	url, _, _, err := uploadGeneratedImage(ctx, meta, base64.StdEncoding.EncodeToString(data))
	if err != nil {
		return "", fmt.Errorf("upload %s to OSS failed: %w", fh.Filename, err)
	}
	return url, nil
}

// collectFormFiles 收集表单中指定字段的文件（兼容 "image"、"image[]" 及 "image[n]" 等写法）。
func collectFormFiles(form *multipart.Form, field string) []*multipart.FileHeader {
	var files []*multipart.FileHeader
	for key, fhs := range form.File {
		if key == field || key == field+"[]" || strings.HasPrefix(key, field+"[") {
			files = append(files, fhs...)
		}
	}
	return files
}

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
	"bytes"
	"context"
	"mime/multipart"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTestForm 构造一个带 image / mask 文件与 prompt 文本字段的 multipart 表单。
func buildTestForm(t *testing.T, withMask bool) *multipart.Form {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	require.NoError(t, w.WriteField("prompt", "edit this"))

	fw, err := w.CreateFormFile("image", "a.png")
	require.NoError(t, err)
	_, err = fw.Write([]byte("fake-image-bytes"))
	require.NoError(t, err)

	if withMask {
		mw, err := w.CreateFormFile("mask", "mask.png")
		require.NoError(t, err)
		_, err = mw.Write([]byte("fake-mask-bytes"))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())
	reader := multipart.NewReader(&buf, w.Boundary())
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)
	return form
}

// stubUpload 替换 uploadGeneratedImage 为固定 URL 的桩函数，避免真实 OSS 依赖。
func stubUpload(t *testing.T, urls []string) {
	t.Helper()
	orig := uploadGeneratedImage
	idx := 0
	uploadGeneratedImage = func(_ context.Context, _ service.GeneratedImageUploadMeta, _ string) (string, string, int64, error) {
		u := urls[idx%len(urls)]
		idx++
		return u, "key", 1, nil
	}
	t.Cleanup(func() { uploadGeneratedImage = orig })
}

func TestBuildImageEditPayload(t *testing.T) {
	t.Run("uploads image and mask to url", func(t *testing.T) {
		stubUpload(t, []string{"https://oss.example.com/a.png", "https://oss.example.com/mask.png"})

		payload, err := buildImageEditPayload(context.Background(), buildTestForm(t, true), dto.ImageRequest{
			Prompt: "edit this",
			Size:   "1024x1024",
		}, "req-1")
		require.NoError(t, err)

		assert.Equal(t, "edit this", payload["prompt"])
		assert.Equal(t, "1024x1024", payload["aspectRatio"])
		assert.Equal(t, "https://oss.example.com/a.png", payload["urls"])
		assert.Equal(t, "https://oss.example.com/mask.png", payload["mask"])
	})

	t.Run("missing image file returns error", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		require.NoError(t, w.WriteField("prompt", "edit this"))
		require.NoError(t, w.Close())
		reader := multipart.NewReader(&buf, w.Boundary())
		form, err := reader.ReadForm(1 << 20)
		require.NoError(t, err)

		_, err = buildImageEditPayload(context.Background(), form, dto.ImageRequest{Prompt: "edit this"}, "req-2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "image file is required")
	})
}

func TestCollectFormFiles(t *testing.T) {
	form := buildTestForm(t, true)

	images := collectFormFiles(form, "image")
	require.Len(t, images, 1)
	assert.Equal(t, "a.png", images[0].Filename)

	masks := collectFormFiles(form, "mask")
	require.Len(t, masks, 1)
	assert.Equal(t, "mask.png", masks[0].Filename)
}

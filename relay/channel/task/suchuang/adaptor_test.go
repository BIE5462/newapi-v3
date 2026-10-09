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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func TestParseTaskResultStatusMapping(t *testing.T) {
	adaptor := &TaskAdaptor{}
	tests := []struct {
		name         string
		respBody     string
		wantStatus   string
		wantProgress string
		wantURL      string
		wantReason   string
		wantErr      bool
	}{
		{
			name:         "status 0 maps to submitted",
			respBody:     `{"code":200,"msg":"成功","data":{"status":0,"message":""}}`,
			wantStatus:   model.TaskStatusSubmitted,
			wantProgress: taskcommon.ProgressQueued,
		},
		{
			name:         "status 1 maps to in progress",
			respBody:     `{"code":200,"msg":"成功","data":{"status":1,"message":"processing"}}`,
			wantStatus:   model.TaskStatusInProgress,
			wantProgress: taskcommon.ProgressInProgress,
		},
		{
			name:         "status 2 maps to success with url",
			respBody:     `{"code":200,"msg":"成功","data":{"status":2,"urls":["https://cdn.example.com/v.mp4"]}}`,
			wantStatus:   model.TaskStatusSuccess,
			wantProgress: taskcommon.ProgressComplete,
			wantURL:      "https://cdn.example.com/v.mp4",
		},
		{
			name:       "status 3 maps to failure with reason",
			respBody:   `{"code":200,"msg":"成功","data":{"status":3,"message":"content policy violation"}}`,
			wantStatus: model.TaskStatusFailure,
			wantReason: "content policy violation",
		},
		{
			name:     "business error code returns error",
			respBody: `{"code":401,"msg":"密钥错误"}`,
			wantErr:  true,
		},
		{
			name:     "unknown status returns error",
			respBody: `{"code":200,"msg":"成功","data":{"status":9}}`,
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskInfo, err := adaptor.ParseTaskResult([]byte(tt.respBody))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, taskInfo.Status)
			assert.Equal(t, tt.wantProgress, taskInfo.Progress)
			assert.Equal(t, tt.wantURL, taskInfo.Url)
			assert.Equal(t, tt.wantReason, taskInfo.Reason)
		})
	}
}

func TestBuildRequestBody(t *testing.T) {
	c := newTestContext(t)
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.wuyinkeji.com"},
	})

	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:   "a cat runs",
		Images:   []string{"https://a.com/1.jpg"},
		Size:     "1280x720",
		Duration: 5,
		Metadata: map[string]any{
			"video":  "https://a.com/ref.mp4",
			"camera": "fixed",
			"model":  "should-be-ignored",
		},
	})

	reader, err := adaptor.BuildRequestBody(c, nil)
	require.NoError(t, err)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	assert.Equal(t, "a cat runs", payload["prompt"])
	assert.Equal(t, "https://a.com/1.jpg", payload["images"])
	assert.Equal(t, "1280x720", payload["size"])
	assert.Equal(t, "5", payload["duration"])
	assert.Equal(t, "https://a.com/ref.mp4", payload["video"])
	assert.Equal(t, "fixed", payload["camera"])
	// metadata 不允许覆盖模型字段
	assert.NotContains(t, payload, "model")
}

func TestBuildRequestBodyDurationFromSeconds(t *testing.T) {
	c := newTestContext(t)
	adaptor := &TaskAdaptor{}
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:  "a cat runs",
		Seconds: "10",
	})

	reader, err := adaptor.BuildRequestBody(c, nil)
	require.NoError(t, err)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	assert.Equal(t, "10", payload["duration"])
	assert.NotContains(t, payload, "images")
	assert.NotContains(t, payload, "size")
}

func TestBuildRequestURL(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://api.wuyinkeji.com/",
			UpstreamModelName: "video_kling_v2",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	got, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://api.wuyinkeji.com/api/async/video_kling_v2", got)
}

func TestConvertToOpenAIVideo(t *testing.T) {
	t.Run("success task exposes url metadata", func(t *testing.T) {
		adaptor := &TaskAdaptor{}
		task := &model.Task{
			TaskID:   "task_public_1",
			Status:   model.TaskStatusSuccess,
			Progress: taskcommon.ProgressComplete,
			Data:     []byte(`{"code":200,"msg":"成功","data":{"status":2,"urls":["https://cdn.example.com/v.mp4"]}}`),
		}
		data, err := adaptor.ConvertToOpenAIVideo(task)
		require.NoError(t, err)

		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(data, &video))
		assert.Equal(t, "task_public_1", video.ID)
		assert.Equal(t, dto.VideoStatusCompleted, video.Status)
		assert.Equal(t, "https://cdn.example.com/v.mp4", video.Metadata["url"])
	})

	t.Run("failed task exposes error message", func(t *testing.T) {
		adaptor := &TaskAdaptor{}
		task := &model.Task{
			TaskID: "task_public_2",
			Status: model.TaskStatusFailure,
			Data:   []byte(`{"code":200,"msg":"成功","data":{"status":3,"message":"content policy violation"}}`),
		}
		data, err := adaptor.ConvertToOpenAIVideo(task)
		require.NoError(t, err)

		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(data, &video))
		assert.Equal(t, dto.VideoStatusFailed, video.Status)
		require.NotNil(t, video.Error)
		assert.Equal(t, "content policy violation", video.Error.Message)
	})
}

func TestSubmitResponseParsing(t *testing.T) {
	var resp SubmitResponse
	require.NoError(t, common.Unmarshal([]byte(`{"code":200,"msg":"成功","data":{"id":"video_6c79c484"}}`), &resp))
	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, "video_6c79c484", resp.taskID())
}

func TestSubmitResponseDataArrayTolerance(t *testing.T) {
	// 上游失败时 data 可能为空数组，不应导致解析失败
	var resp SubmitResponse
	require.NoError(t, common.Unmarshal([]byte(`{"code":400,"msg":"","data":[]}`), &resp))
	assert.Equal(t, 400, resp.Code)
	assert.Equal(t, "", resp.taskID())
}

func TestCollectMediaUrlsDeduplicates(t *testing.T) {
	raw := []byte(`{"url":"https://cdn.example.com/v.mp4","result":["https://cdn.example.com/v.mp4"]}`)
	urls := collectMediaUrls(raw)
	assert.Equal(t, []string{"https://cdn.example.com/v.mp4"}, urls)
}

func TestDoResponseWritesPublicTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.wuyinkeji.com"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_public_3",
		},
	}
	info.OriginModelName = "video_kling_v2"

	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"code":200,"msg":"成功","data":{"id":"video_6c79c484","count":1}}`)),
	}
	taskID, _, taskErr := adaptor.DoResponse(c, httpResp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "video_6c79c484", taskID)

	assert.Contains(t, recorder.Body.String(), `"id":"task_public_3"`)
}

func TestDoResponseSubmitFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.wuyinkeji.com"},
	}

	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"code":401,"msg":"密钥错误"}`)),
	}
	_, _, taskErr := adaptor.DoResponse(c, httpResp, info)
	require.NotNil(t, taskErr)
}

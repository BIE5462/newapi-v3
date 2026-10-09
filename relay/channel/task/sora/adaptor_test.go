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

package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultStatusMapping(t *testing.T) {
	adaptor := &TaskAdaptor{}
	tests := []struct {
		name           string
		status         string
		expectedStatus string
	}{
		{"queued maps to queued", "queued", model.TaskStatusQueued},
		{"pending maps to queued", "pending", model.TaskStatusQueued},
		{"in_progress maps to in progress", "in_progress", model.TaskStatusInProgress},
		{"processing maps to in progress", "processing", model.TaskStatusInProgress},
		{"running maps to in progress", "running", model.TaskStatusInProgress},
		{"completed maps to success", "completed", model.TaskStatusSuccess},
		{"succeeded maps to success", "succeeded", model.TaskStatusSuccess},
		{"success maps to success", "success", model.TaskStatusSuccess},
		{"failed maps to failure", "failed", model.TaskStatusFailure},
		{"cancelled maps to failure", "cancelled", model.TaskStatusFailure},
		{"canceled maps to failure", "canceled", model.TaskStatusFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"id":"task_upstream","status":"` + tt.status + `","progress":50}`)
			taskInfo, err := adaptor.ParseTaskResult(body)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, taskInfo.Status)
		})
	}
}

func TestParseTaskResultGrokUpstreamSuccessPayload(t *testing.T) {
	// Grok 视频上游返回 status "succeeded" 而非 Sora 的 "completed"，
	// 且 output_url 被 Markdown 反引号包裹，任务不应被判为失败。
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"id": "task_upstream_1",
		"model": "grok-imagine-video-1.5",
		"stage": "completed",
		"object": "video",
		"status": "succeeded",
		"progress": 100,
		"output_url": "` + "`" + `https://cdn.example.com/video.mp4` + "`" + `"
	}`)
	taskInfo, err := adaptor.ParseTaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, taskInfo.Status)
}

func TestParseTaskResultFailureReasonFromError(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"task_upstream_2","status":"failed","error":{"message":"generation failed","code":"generation_failed"}}`)
	taskInfo, err := adaptor.ParseTaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, taskInfo.Status)
	assert.Equal(t, "generation failed", taskInfo.Reason)
}

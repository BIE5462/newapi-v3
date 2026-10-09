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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// SubmitResponse 是 POST /api/async/{model} 的提交响应（与图片链路同构）。
// Data 用 json.RawMessage 保留原始值：成功时为对象 {id}，失败时可能为空数组 []。
type SubmitResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// taskID 从 data 中解析任务 id；失败（data 非对象）时返回空字符串。
func (r SubmitResponse) taskID() string {
	var data struct {
		Id string `json:"id"`
	}
	if err := common.Unmarshal(r.Data, &data); err != nil {
		return ""
	}
	return data.Id
}

// DetailResponse 是 GET /api/async/detail 的响应外壳。
type DetailResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// detailData 是结果详情中的任务状态字段（0初始化/1进行中/2成功/3失败）。
type detailData struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// TaskAdaptor 接入速创API的视频生成模型。
// 提交：POST {base}/api/async/{model}（模型名即端点 slug）；
// 查询：GET {base}/api/async/detail?id={task_id}。
type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); err != nil {
		return err
	}
	info.Action = constant.TaskActionGenerate
	return nil
}

// BuildRequestBody 组装速创视频异步接口请求体。
// 视频模型（如 GOOGLE_OMNI / video_google_omni）参数：
// prompt / images(参考图,英文逗号拼接,最多1张) / size(如1280x720) / duration(string) / video(参考视频URL)。
// 参考视频 URL 等差异化参数通过 metadata 透传。
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	payload := map[string]any{
		"prompt": req.Prompt,
	}

	// 参考图：images（英文逗号拼接字符串，最多 1 张）
	var refImages []string
	if len(req.Images) > 0 {
		refImages = req.Images
	} else if req.Image != "" {
		refImages = []string{req.Image}
	}
	if len(refImages) > 0 {
		payload["images"] = strings.Join(refImages, ",")
	}

	// 视频尺寸：size（如 1280x720 / 720x1280）
	if req.Size != "" {
		payload["size"] = req.Size
	}

	// 视频时长：duration（string，缺省由上游决定）
	duration := ""
	if req.Seconds != "" {
		duration = req.Seconds
	} else if req.Duration > 0 {
		duration = strconv.Itoa(req.Duration)
	}
	if duration != "" {
		payload["duration"] = duration
	}

	// metadata 透传差异化参数（如 video 参考视频 URL）；不允许覆盖 model（模型名在 URL 中）
	for key, val := range req.Metadata {
		if key == "model" {
			continue
		}
		if _, exists := payload[key]; !exists {
			payload[key] = val
		}
	}

	data, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	modelName := info.UpstreamModelName
	if modelName == "" {
		modelName = info.OriginModelName
	}
	if modelName == "" {
		return "", fmt.Errorf("model is required")
	}
	return fmt.Sprintf("%s/api/async/%s", strings.TrimSuffix(a.baseURL, "/"), url.PathEscape(modelName)), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// 速创API鉴权：Authorization 直接传密钥，不带 Bearer 前缀
	req.Header.Set("Authorization", info.ApiKey)
	return nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	var submitResp SubmitResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrap(err, fmt.Sprintf("%s", responseBody)), "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}
	if resp.StatusCode != http.StatusOK || submitResp.Code != 200 {
		message := submitResp.Msg
		if message == "" {
			message = fmt.Sprintf("suchuang submit failed with code %d", submitResp.Code)
		}
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", message), "submit_failed", http.StatusBadRequest)
		return
	}
	if submitResp.taskID() == "" {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("suchuang submit response missing task id"), "submit_failed", http.StatusBadRequest)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = submitTime()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return submitResp.taskID(), responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	detailURL := fmt.Sprintf("%s/api/async/detail?id=%s", strings.TrimSuffix(baseUrl, "/"), url.QueryEscape(taskID))
	req, err := http.NewRequest(http.MethodGet, detailURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// ParseTaskResult 将 detail 响应映射为平台通用任务状态。
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var detailResp DetailResponse
	if err := common.Unmarshal(respBody, &detailResp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}
	if detailResp.Code != 200 {
		return nil, fmt.Errorf("suchuang detail query failed: %s", detailResp.Msg)
	}

	var taskData detailData
	if len(detailResp.Data) > 0 {
		if err := common.Unmarshal(detailResp.Data, &taskData); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal task data")
		}
	}

	taskInfo := &relaycommon.TaskInfo{}
	switch taskData.Status {
	case 0:
		taskInfo.Status = model.TaskStatusSubmitted
		taskInfo.Progress = taskcommon.ProgressQueued
	case 1:
		taskInfo.Status = model.TaskStatusInProgress
		taskInfo.Progress = taskcommon.ProgressInProgress
	case 2:
		taskInfo.Status = model.TaskStatusSuccess
		taskInfo.Progress = taskcommon.ProgressComplete
		if urls := collectMediaUrls(detailResp.Data); len(urls) > 0 {
			taskInfo.Url = urls[0]
		}
	case 3:
		taskInfo.Status = model.TaskStatusFailure
		taskInfo.Reason = taskData.Message
	default:
		return nil, fmt.Errorf("unknown task status: %d", taskData.Status)
	}
	return taskInfo, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt

	if len(originTask.Data) > 0 {
		var detailResp DetailResponse
		if err := common.Unmarshal(originTask.Data, &detailResp); err == nil {
			if urls := collectMediaUrls(detailResp.Data); len(urls) > 0 {
				openAIVideo.SetMetadata("url", urls[0])
			}
			var taskData detailData
			if common.Unmarshal(detailResp.Data, &taskData) == nil && taskData.Status == 3 && taskData.Message != "" {
				openAIVideo.Error = &dto.OpenAIVideoError{
					Message: taskData.Message,
					Code:    "task_failed",
				}
			}
		}
	}

	return common.Marshal(openAIVideo)
}

func (a *TaskAdaptor) GetModelList() []string {
	// 模型名即速创API异步端点 slug，平台新增视频模型时在渠道里手工添加即可
	return []string{}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "suchuang"
}

// collectMediaUrls 从成功的 data 中容错提取媒体结果地址，
// 兼容单 URL 字符串、字符串数组、{url|video_url|image_url|image} 对象，
// 以及整体结构未知时递归收集 http(s) 链接。
func collectMediaUrls(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if err := common.Unmarshal(raw, &single); err == nil {
		if isHttpUrl(single) {
			return []string{single}
		}
		return nil
	}
	var list []string
	if err := common.Unmarshal(raw, &list); err == nil {
		var out []string
		for _, item := range list {
			if isHttpUrl(item) {
				out = append(out, item)
			}
		}
		return out
	}
	var nested any
	if err := common.Unmarshal(raw, &nested); err == nil {
		return dedupe(collectUrlValues(nested, nil))
	}
	return nil
}

func isHttpUrl(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func collectUrlValues(node any, out []string) []string {
	switch v := node.(type) {
	case map[string]any:
		for _, key := range []string{"url", "video_url", "image_url", "image", "cover_url"} {
			if s, ok := v[key].(string); ok && isHttpUrl(s) {
				out = append(out, s)
			}
		}
		for _, value := range v {
			out = collectUrlValues(value, out)
		}
	case []any:
		for _, item := range v {
			out = collectUrlValues(item, out)
		}
	case string:
		if isHttpUrl(v) {
			out = append(out, v)
		}
	}
	return out
}

func dedupe(urls []string) []string {
	seen := make(map[string]bool, len(urls))
	out := urls[:0]
	for _, u := range urls {
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

func submitTime() int64 {
	return common.GetTimestamp()
}

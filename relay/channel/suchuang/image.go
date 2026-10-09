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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// SubmitResponse 是 POST /api/async/{model} 的提交响应。
// Data 用 json.RawMessage 保留原始值：成功时 data 为对象 {id,count}，
// 失败时 data 可能为空数组 []，不能直接按结构体解析。
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

// countValue 将 data.count 解析为 int，兼容 int / string / float / 空值，解析失败返回 0。
func (r SubmitResponse) countValue() int {
	var data struct {
		Count json.RawMessage `json:"count"`
	}
	if err := common.Unmarshal(r.Data, &data); err != nil {
		return 0
	}
	raw := data.Count
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var i int
	if err := common.Unmarshal(raw, &i); err == nil {
		return i
	}
	var s string
	if err := common.Unmarshal(raw, &s); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return n
		}
	}
	var f float64
	if err := common.Unmarshal(raw, &f); err == nil {
		return int(f)
	}
	return 0
}

// DetailResponse 是 GET /api/async/detail 的响应外壳。
// data 在成功后承载生成结果（结构因模型而异），原样保留做容错解析。
type DetailResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// detailData 是结果详情中的任务状态字段。
type detailData struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// submitAndPoll 完成提交（含"未绑定参数"剔除重试）、按张数计费与结果轮询，返回提取出的图片列表。
// 供 OpenAI 图片响应与 Gemini 图片响应两种输出格式复用。
func (a *Adaptor) submitAndPoll(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) ([]dto.ImageData, *types.NewAPIError) {
	submitResp, statusCode, apiErr := parseSubmitResponse(resp)
	if apiErr != nil {
		return nil, apiErr
	}

	// 速创平台会透传上游"存在未绑定的参数: xxx"报错，剔除对应参数后重试。
	if payload, ok := contextPayload(c); ok {
		for attempt := 0; attempt < maxUnboundParamRetries && isUnboundParamError(submitResp.Msg); attempt++ {
			if !stripUnboundParams(payload, submitResp.Msg) {
				break
			}
			logger.LogWarn(c, "suchuang strip unbound params and retry: "+submitResp.Msg)
			submitResp, statusCode, apiErr = a.resubmitImageTask(c, info, payload)
			if apiErr != nil {
				return nil, apiErr
			}
		}
	}

	if statusCode != http.StatusOK || submitResp.Code != submitRespCodeSuccess {
		message := submitResp.Msg
		if message == "" {
			message = fmt.Sprintf("suchuang submit failed with code %d", submitResp.Code)
		}
		logger.LogError(c, "suchuang_submit_failed: "+message)
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: message,
			Type:    "suchuang_error",
			Param:   "",
			Code:    fmt.Sprintf("%d", submitResp.Code),
		}, statusCode)
	}
	if submitResp.taskID() == "" {
		return nil, types.NewError(errors.New("suchuang submit response missing task id"), types.ErrorCodeBadResponseBody)
	}

	// 计费按张数：以上游返回的 count 为准，缺省 1
	count := submitResp.countValue()
	if count <= 0 {
		count = 1
	}
	info.PriceData.AddOtherRatio("n", float64(count))

	rawData, taskData, err := pollTaskResult(c, info, submitResp.taskID())
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponse)
	}
	if taskData.Status != taskStatusSuccess {
		message := taskData.Message
		if message == "" {
			message = "suchuang task failed"
		}
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: message,
			Type:    "suchuang_error",
			Param:   "",
			Code:    fmt.Sprintf("%d", taskData.Status),
		}, statusCode)
	}

	images := extractResultImages(rawData)
	if len(images) == 0 {
		logger.LogError(c, "suchuang_task_result_no_image: "+string(rawData))
		return nil, types.NewError(errors.New("suchuang task succeeded but no image found in result"), types.ErrorCodeBadResponseBody)
	}
	return images, nil
}

// imageHandler 返回标准 OpenAI 图片响应。
func (a *Adaptor) imageHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.Usage) {
	images, apiErr := a.submitAndPoll(c, info, resp)
	if apiErr != nil {
		return apiErr, nil
	}

	imageResponse := &dto.ImageResponse{
		Created: info.StartTime.Unix(),
		Data:    images,
	}
	jsonResponse, err := common.Marshal(imageResponse)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	service.IOCopyBytesGracefully(c, resp, jsonResponse)

	return nil, &dto.Usage{}
}

func parseSubmitResponse(resp *http.Response) (SubmitResponse, int, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return SubmitResponse{}, 0, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)

	var submitResp SubmitResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		return SubmitResponse{}, resp.StatusCode, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	return submitResp, resp.StatusCode, nil
}

func contextPayload(c *gin.Context) (map[string]any, bool) {
	v, ok := c.Get(suchuangPayloadContextKey)
	if !ok {
		return nil, false
	}
	payload, ok := v.(map[string]any)
	return payload, ok
}

// resubmitImageTask 用剔除参数后的 payload 重新提交，返回新的提交响应。
func (a *Adaptor) resubmitImageTask(c *gin.Context, info *relaycommon.RelayInfo, payload map[string]any) (SubmitResponse, int, *types.NewAPIError) {
	body, err := common.Marshal(payload)
	if err != nil {
		return SubmitResponse{}, 0, types.NewError(err, types.ErrorCodeJsonMarshalFailed)
	}
	respAny, err := a.DoRequest(c, info, bytes.NewReader(body))
	if err != nil {
		return SubmitResponse{}, 0, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResp, ok := respAny.(*http.Response)
	if !ok {
		return SubmitResponse{}, 0, types.NewError(errors.New("unexpected response type from DoRequest"), types.ErrorCodeBadResponse)
	}
	return parseSubmitResponse(httpResp)
}

func isUnboundParamError(msg string) bool {
	return strings.Contains(msg, "未绑定") || strings.Contains(msg, "unbound")
}

// stripUnboundParams 从 payload 中删除上游标记为未绑定的参数，返回是否删除了任何参数。
func stripUnboundParams(payload map[string]any, msg string) bool {
	removed := false
	for _, param := range parseUnboundParams(msg) {
		if _, ok := payload[param]; ok {
			delete(payload, param)
			removed = true
		}
	}
	return removed
}

// parseUnboundParams 从报错信息中解析未绑定的参数名列表。
// 例如 "转发请求失败: 存在未绑定的参数: quality, background" -> ["quality", "background"]。
func parseUnboundParams(msg string) []string {
	idx := strings.Index(msg, "参数")
	if idx < 0 {
		return nil
	}
	rest := strings.TrimPrefix(msg[idx:], "参数")
	rest = strings.TrimLeft(rest, ":： \t")
	if rest == "" {
		return nil
	}
	return strings.FieldsFunc(rest, func(r rune) bool {
		switch r {
		case ',', '，', '、', ';', '；', ' ', '\t':
			return true
		}
		return false
	})
}

// pollTaskResult 提交成功后轮询结果详情接口，直到任务成功/失败或超时。
func pollTaskResult(c *gin.Context, info *relaycommon.RelayInfo, taskID string) (json.RawMessage, *detailData, error) {
	time.Sleep(pollInitialWait)

	for step := 0; step < pollMaxAttempts; step++ {
		logger.LogDebug(c, "suchuang pollTaskResult step %d/%d, taskID: %s", step+1, pollMaxAttempts, taskID)
		rawData, taskData, err := fetchTaskDetail(info, taskID)
		if err != nil {
			logger.LogWarn(c, "suchuang fetchTaskDetail err: "+err.Error())
			time.Sleep(pollInterval)
			continue
		}
		switch taskData.Status {
		case taskStatusSuccess, taskStatusFailure:
			return rawData, taskData, nil
		}
		time.Sleep(pollInterval)
	}
	return nil, nil, errors.New("suchuang task polling timeout")
}

func fetchTaskDetail(info *relaycommon.RelayInfo, taskID string) (json.RawMessage, *detailData, error) {
	detailURL := fmt.Sprintf("%s/api/async/detail?id=%s", strings.TrimSuffix(info.ChannelBaseUrl, "/"), url.QueryEscape(taskID))
	req, err := http.NewRequest(http.MethodGet, detailURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", info.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	client, err := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
	if err != nil {
		return nil, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	var detailResp DetailResponse
	if err := common.Unmarshal(body, &detailResp); err != nil {
		return nil, nil, err
	}
	if detailResp.Code != submitRespCodeSuccess {
		return nil, nil, fmt.Errorf("suchuang detail query failed: %s", detailResp.Msg)
	}

	var taskData detailData
	if len(detailResp.Data) > 0 {
		if err := common.Unmarshal(detailResp.Data, &taskData); err != nil {
			return nil, nil, err
		}
	}
	return detailResp.Data, &taskData, nil
}

// extractResultImages 从成功的 data 中容错提取图片结果，
// 兼容单 URL 字符串、URL 数组、[{url|image_url|image|b64_json}] 对象数组，
// 以及整体结构未知时递归收集 http(s) 链接与 dataURL。
func extractResultImages(raw json.RawMessage) []dto.ImageData {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if err := common.Unmarshal(raw, &single); err == nil {
		return imageDataFromValue(single)
	}
	var list []any
	if err := common.Unmarshal(raw, &list); err == nil {
		var out []dto.ImageData
		for _, item := range list {
			switch v := item.(type) {
			case string:
				out = append(out, imageDataFromValue(v)...)
			case map[string]any:
				out = append(out, imageDataFromObject(v)...)
			}
		}
		return out
	}
	var obj map[string]any
	if err := common.Unmarshal(raw, &obj); err == nil {
		if images := imageDataFromObject(obj); len(images) > 0 {
			return images
		}
		return collectImageValues(obj, nil)
	}
	var nested any
	if err := common.Unmarshal(raw, &nested); err == nil {
		return collectImageValues(nested, nil)
	}
	return nil
}

func imageDataFromObject(obj map[string]any) []dto.ImageData {
	if s, ok := obj["b64_json"].(string); ok && s != "" {
		return []dto.ImageData{{B64Json: s}}
	}
	for _, key := range []string{"url", "image_url", "image"} {
		if s, ok := obj[key].(string); ok && s != "" {
			return imageDataFromValue(s)
		}
	}
	return nil
}

func imageDataFromValue(s string) []dto.ImageData {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "data:image/") {
		return []dto.ImageData{{B64Json: s}}
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return []dto.ImageData{{Url: s}}
	}
	return nil
}

// collectImageValues 递归收集未知结构结果中所有可识别的图片值。
func collectImageValues(node any, out []dto.ImageData) []dto.ImageData {
	switch v := node.(type) {
	case map[string]any:
		for _, value := range v {
			out = collectImageValues(value, out)
		}
	case []any:
		for _, item := range v {
			out = collectImageValues(item, out)
		}
	case string:
		out = append(out, imageDataFromValue(v)...)
	}
	return out
}

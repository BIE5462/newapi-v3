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

import "time"

const (
	ChannelName = "suchuang"

	// submitRespCodeSuccess 是异步提交接口业务成功码（{code:200,...}）
	submitRespCodeSuccess = 200

	// 任务状态（GET /api/async/detail 返回的 data.status）
	taskStatusInit    = 0 // 初始化
	taskStatusRunning = 1 // 进行中
	taskStatusSuccess = 2 // 成功
	taskStatusFailure = 3 // 失败

	// 内部轮询节奏：提交后先等待，再按固定间隔轮询结果详情接口（该接口免费，限 20 QPS）
	pollInitialWait = 3 * time.Second
	pollInterval    = 5 * time.Second
	pollMaxAttempts = 60 // 约 5 分钟总超时

	// maxUnboundParamRetries 上游报"存在未绑定的参数"时，剔除参数后重试提交的最大次数
	maxUnboundParamRetries = 5

	// suchuangPayloadContextKey 在请求转换阶段保存构造好的 payload，供提交失败重试时剔除参数
	suchuangPayloadContextKey = "suchuang_payload"
)

// ModelList 内置常用模型。模型名即速创API异步端点 slug（{base}/api/async/{model}），
// 平台新增模型时直接在渠道里手工添加对应 slug 即可，无需改代码。
var ModelList = []string{
	"image_gpt_2.5_flare",
}

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUnboundParamError(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"转发请求失败: 存在未绑定的参数: quality", true},
		{"存在未绑定的参数: quality", true},
		{"unbound parameter: quality", true},
		{"成功", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isUnboundParamError(tt.msg), "msg=%q", tt.msg)
	}
}

func TestParseUnboundParams(t *testing.T) {
	tests := []struct {
		msg  string
		want []string
	}{
		{"转发请求失败: 存在未绑定的参数: quality", []string{"quality"}},
		{"存在未绑定的参数: quality, background", []string{"quality", "background"}},
		{"存在未绑定的参数：quality、background", []string{"quality", "background"}},
		{"存在未绑定的参数: quality background", []string{"quality", "background"}},
		{"成功", nil},
		{"没有关键字", nil},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, parseUnboundParams(tt.msg), "msg=%q", tt.msg)
	}
}

func TestStripUnboundParams(t *testing.T) {
	payload := map[string]any{
		"prompt":      "a cat",
		"quality":     "high",
		"background":  "auto",
		"aspectRatio": "1024x1024",
	}

	removed := stripUnboundParams(payload, "存在未绑定的参数: quality, background")
	require.True(t, removed)
	assert.NotContains(t, payload, "quality")
	assert.NotContains(t, payload, "background")
	assert.Contains(t, payload, "prompt")
	assert.Contains(t, payload, "aspectRatio")

	// 再次剔除不存在的参数时返回 false
	assert.False(t, stripUnboundParams(payload, "存在未绑定的参数: quality"))
}

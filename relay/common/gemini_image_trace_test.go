package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestIsGeminiImageTraceRequestRequiresSwitch(t *testing.T) {
	original := geminiImageTraceEnabled
	geminiImageTraceEnabled = false
	defer func() {
		geminiImageTraceEnabled = original
	}()

	require.False(t, IsGeminiImageTraceRequest(&RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &ChannelMeta{
			ApiType: constant.APITypeGemini,
		},
	}))
}

func TestIsGeminiImageTraceRequestMatchesGeminiImageEndpoint(t *testing.T) {
	original := geminiImageTraceEnabled
	geminiImageTraceEnabled = true
	defer func() {
		geminiImageTraceEnabled = original
	}()

	require.True(t, IsGeminiImageTraceRequest(&RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &ChannelMeta{
			ApiType:           constant.APITypeGemini,
			UpstreamModelName: "imagen-4.0-generate-preview-06-06",
		},
	}))
}

func TestIsGeminiImageTraceRequestMatchesSupportedImagineModelWithoutImageName(t *testing.T) {
	original := geminiImageTraceEnabled
	geminiImageTraceEnabled = true
	defer func() {
		geminiImageTraceEnabled = original
	}()

	require.True(t, IsGeminiImageTraceRequest(&RelayInfo{
		RelayMode:      relayconstant.RelayModeGemini,
		RequestURLPath: "/v1beta/models/gemini-2.0-flash-exp:generateContent",
		ChannelMeta: &ChannelMeta{
			ApiType:           constant.APITypeGemini,
			UpstreamModelName: "gemini-2.0-flash-exp",
		},
	}))
}

func TestIsGeminiImageTraceRequestSkipsPlainGeminiTextModel(t *testing.T) {
	original := geminiImageTraceEnabled
	geminiImageTraceEnabled = true
	defer func() {
		geminiImageTraceEnabled = original
	}()

	require.False(t, IsGeminiImageTraceRequest(&RelayInfo{
		RelayMode:      relayconstant.RelayModeGemini,
		RequestURLPath: "/v1beta/models/gemini-2.5-pro:generateContent",
		ChannelMeta: &ChannelMeta{
			ApiType:           constant.APITypeGemini,
			UpstreamModelName: "gemini-2.5-pro",
		},
	}))
}

func TestIsGeminiImageTraceRequestSkipsOpenAIChatCompletions(t *testing.T) {
	original := geminiImageTraceEnabled
	geminiImageTraceEnabled = true
	defer func() {
		geminiImageTraceEnabled = original
	}()

	require.False(t, IsGeminiImageTraceRequest(&RelayInfo{
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
		ChannelMeta: &ChannelMeta{
			ApiType:           constant.APITypeGemini,
			UpstreamModelName: "gemini-2.5-flash-image",
		},
	}))
}

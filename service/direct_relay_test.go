package service

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDirectRelayServiceTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	dsn := "file:direct-relay-service-" + strings.ReplaceAll(t.Name(), "/", "-") + "?mode=memory&cache=shared"
	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.DirectRelayTicket{}, &model.DirectRelaySettlement{}))
	model.DB = testDB
	common.RedisEnabled = false
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		sqlDB, dbErr := testDB.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func newDirectRelayCallbackTestTicket(t *testing.T, ticketID, status string) (*model.DirectRelayTicket, string) {
	t.Helper()
	callbackToken := "callback-secret-" + ticketID
	ticket := &model.DirectRelayTicket{
		TicketID:          ticketID,
		AttemptID:         "attempt-" + ticketID,
		RequestID:         "request-" + ticketID,
		Status:            status,
		CallbackTokenHash: model.DirectRelayTokenHash(callbackToken),
		PreConsumedQuota:  100,
		CallbackDeadline:  time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(ticket).Error)
	return ticket, callbackToken
}

func newDirectRelayCallbackContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func processDirectRelayErrorCallback(t *testing.T, ticketID, callbackToken, body string, truncated bool, totalBytes int64, hash string) (string, error) {
	t.Helper()
	req := &DirectRelayCallbackRequest{
		TicketID:           ticketID,
		AttemptID:          "attempt-" + ticketID,
		Outcome:            "error",
		UpstreamStatus:     400,
		ErrorContentType:   "application/json",
		ErrorBody:          body,
		ErrorBodyBytes:     totalBytes,
		ErrorBodySHA256:    hash,
		ErrorBodyTruncated: truncated,
	}
	return ProcessDirectRelayCallback(newDirectRelayCallbackContext(), req, callbackToken)
}

func TestBuildUsageFromGeminiMetadataPreservesModalities(t *testing.T) {
	usage := BuildUsageFromGeminiMetadata(dto.GeminiUsageMetadata{
		PromptTokenCount:        120,
		CandidatesTokenCount:    80,
		ThoughtsTokenCount:      10,
		TotalTokenCount:         210,
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}, {Modality: "IMAGE", TokenCount: 20}},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 80}},
	}, 0)

	require.Equal(t, 120, usage.PromptTokens)
	require.Equal(t, 90, usage.CompletionTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 80, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 10, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestBuildUsageFromGeminiMetadataUsesFallbackAndLegacyCompletion(t *testing.T) {
	usage := BuildUsageFromGeminiMetadata(dto.GeminiUsageMetadata{
		TotalTokenCount: 100,
	}, 60)

	require.Equal(t, 60, usage.PromptTokens)
	require.Equal(t, 40, usage.CompletionTokens)
	require.Equal(t, 60, usage.PromptTokensDetails.TextTokens)
}

func TestValidGeminiUsageMetadataRejectsUnboundedValues(t *testing.T) {
	valid := &dto.GeminiUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 200, TotalTokenCount: 300}
	assert.True(t, validGeminiUsageMetadata(valid))

	tooLarge := *valid
	tooLarge.CandidatesTokenCount = directRelayMaxUsageTokens + 1
	assert.False(t, validGeminiUsageMetadata(&tooLarge))

	tooManyDetails := *valid
	tooManyDetails.CandidatesTokensDetails = make([]dto.GeminiPromptTokensDetails, 129)
	assert.False(t, validGeminiUsageMetadata(&tooManyDetails))
}

func TestDirectRelayTimeoutOptionsFallBackWhenOutOfRange(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalTTL := common.OptionMap["GeminiDirectTicketTTLSeconds"]
	originalGrace := common.OptionMap["GeminiDirectCallbackGraceSeconds"]
	common.OptionMap["GeminiDirectTicketTTLSeconds"] = "1"
	common.OptionMap["GeminiDirectCallbackGraceSeconds"] = "invalid"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["GeminiDirectTicketTTLSeconds"] = originalTTL
		common.OptionMap["GeminiDirectCallbackGraceSeconds"] = originalGrace
		common.OptionMapRWMutex.Unlock()
	})

	assert.Equal(t, 600, DirectRelayTicketTTLSeconds())
	assert.Equal(t, 1800, DirectRelayCallbackGraceSeconds())
}

func TestCalculateTextQuotaUsesFrozenQuotaConversion(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName:     "gemini-2.5-flash-image",
		BillingQuotaPerUnit: 100,
		PriceData: types.PriceData{
			UsePrice: true, ModelPrice: 0.01,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	summary := calculateTextQuotaSummary(c, info, &dto.Usage{PromptTokens: 1, TotalTokens: 1})
	assert.Equal(t, 1, summary.Quota)
}

func TestIsGeminiDirectCandidateEligibility(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalEnabled := common.OptionMap["GeminiDirectRelayEnabled"]
	common.OptionMap["GeminiDirectRelayEnabled"] = "true"
	common.OptionMapRWMutex.Unlock()
	originalThinking := model_setting.GetGeminiSettings().ThinkingAdapterEnabled
	model_setting.GetGeminiSettings().ThinkingAdapterEnabled = false
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["GeminiDirectRelayEnabled"] = originalEnabled
		common.OptionMapRWMutex.Unlock()
		model_setting.GetGeminiSettings().ThinkingAdapterEnabled = originalThinking
	})

	newCandidate := func() (*gin.Context, *relaycommon.RelayInfo) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash-image:generateContent", nil)
		info := &relaycommon.RelayInfo{
			RelayFormat: types.RelayFormatGemini,
			UserSetting: dto.UserSetting{GeminiDirectRelayEnabled: true},
			PriceData:   types.PriceData{QuotaToPreConsume: 100},
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType: constant.ChannelTypeGemini, ApiType: constant.APITypeGemini,
				ChannelBaseUrl: "https://generativelanguage.googleapis.com", ApiKey: "test-key",
				UpstreamModelName: "gemini-2.5-flash-image",
			},
		}
		return c, info
	}

	c, info := newCandidate()
	assert.True(t, IsGeminiDirectCandidate(c, info))

	// Client capability headers are no longer required; the user's persisted
	// setting is the server-side authorization boundary.
	c, info = newCandidate()
	info.UserSetting.GeminiDirectRelayEnabled = false
	assert.False(t, IsGeminiDirectCandidate(c, info))

	c, info = newCandidate()
	info.ChannelBaseUrl = "http://localhost:8080"
	assert.True(t, IsGeminiDirectCandidate(c, info), "trusted deployments may use HTTP for direct relay")

	c, info = newCandidate()
	info.ChannelSetting.Proxy = "http://proxy.example"
	assert.False(t, IsGeminiDirectCandidate(c, info))

	c, info = newCandidate()
	info.ParamOverride = map[string]interface{}{"generationConfig.temperature": 0}
	assert.False(t, IsGeminiDirectCandidate(c, info))

	c, info = newCandidate()
	c.Request.URL.RawQuery = "alt=sse"
	assert.False(t, IsGeminiDirectCandidate(c, info))

	c, info = newCandidate()
	info.ChannelType = constant.ChannelTypeVertexAi
	assert.False(t, IsGeminiDirectCandidate(c, info))

	c, info = newCandidate()
	info.UpstreamModelName = "gemini-2.5-flash"
	assert.False(t, IsGeminiDirectCandidate(c, info))

	c, info = newCandidate()
	info.UpstreamModelName = "「Rim」gemini-2.5-flash-image"
	info.ChannelMeta.IsModelMapped = true
	assert.True(t, IsGeminiDirectCandidate(c, info))
	assert.Equal(t, "「Rim」gemini-2.5-flash-image", info.UpstreamModelName, "eligibility checks must not rewrite the mapped model")

	c, info = newCandidate()
	info.UpstreamModelName = "「XJ」gemini-2.5-flash-image"
	info.ChannelMeta.IsModelMapped = true
	assert.True(t, IsGeminiDirectCandidate(c, info), "direct relay should accept any mapped provider prefix")

	c, info = newCandidate()
	info.UpstreamModelName = "gemini-2.5-flash-image"
	info.ChannelMeta.IsModelMapped = true
	assert.True(t, IsGeminiDirectCandidate(c, info), "direct relay should accept mapped models without a prefix")

	c, info = newCandidate()
	model_setting.GetGeminiSettings().ThinkingAdapterEnabled = true
	assert.False(t, IsGeminiDirectCandidate(c, info))

	c, info = newCandidate()
	info.PriceData.QuotaToPreConsume = 0
	model_setting.GetGeminiSettings().ThinkingAdapterEnabled = false
	assert.False(t, IsGeminiDirectCandidate(c, info))
}

func TestCreateDirectRelayTicketPreservesMappedModel(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalEnabled := common.OptionMap["GeminiDirectRelayEnabled"]
	common.OptionMap["GeminiDirectRelayEnabled"] = "true"
	common.OptionMapRWMutex.Unlock()
	originalThinking := model_setting.GetGeminiSettings().ThinkingAdapterEnabled
	model_setting.GetGeminiSettings().ThinkingAdapterEnabled = false
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["GeminiDirectRelayEnabled"] = originalEnabled
		common.OptionMapRWMutex.Unlock()
		model_setting.GetGeminiSettings().ThinkingAdapterEnabled = originalThinking
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/original-alias:generateContent", nil)
	info := &relaycommon.RelayInfo{
		RequestId:       "rim-ticket-request",
		OriginModelName: "original-alias",
		RelayFormat:     types.RelayFormatGemini,
		UserSetting:     dto.UserSetting{GeminiDirectRelayEnabled: true, BillingPreference: "wallet_only"},
		PriceData:       types.PriceData{QuotaToPreConsume: 100},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			ApiType:           constant.APITypeGemini,
			ChannelBaseUrl:    "https://generativelanguage.googleapis.com",
			ApiKey:            "test-key",
			UpstreamModelName: "「Rim」gemini-2.5-flash-image",
			IsModelMapped:     true,
		},
	}

	ticket, err := CreateDirectRelayTicket(c, info, []byte(`{"contents":[]}`))
	require.NoError(t, err)
	require.NotNil(t, ticket)
	decodedModel, err := base64.RawURLEncoding.DecodeString(ticket.Upstream.ModelB64)
	require.NoError(t, err)
	assert.Equal(t, "「Rim」gemini-2.5-flash-image", string(decodedModel))
	decodedURL, err := base64.RawURLEncoding.DecodeString(ticket.Upstream.URLB64)
	require.NoError(t, err)
	assert.Contains(t, string(decodedURL), "/models/「Rim」gemini-2.5-flash-image:generateContent")
}

func TestCreateDirectRelayTicketSupportsUnmappedModel(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalEnabled := common.OptionMap["GeminiDirectRelayEnabled"]
	common.OptionMap["GeminiDirectRelayEnabled"] = "true"
	common.OptionMapRWMutex.Unlock()
	originalThinking := model_setting.GetGeminiSettings().ThinkingAdapterEnabled
	model_setting.GetGeminiSettings().ThinkingAdapterEnabled = false
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["GeminiDirectRelayEnabled"] = originalEnabled
		common.OptionMapRWMutex.Unlock()
		model_setting.GetGeminiSettings().ThinkingAdapterEnabled = originalThinking
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash-image:generateContent", nil)
	info := &relaycommon.RelayInfo{
		RequestId:       "unmapped-ticket-request",
		OriginModelName: "gemini-2.5-flash-image",
		RelayFormat:     types.RelayFormatGemini,
		UserSetting:     dto.UserSetting{GeminiDirectRelayEnabled: true, BillingPreference: "wallet_only"},
		PriceData:       types.PriceData{QuotaToPreConsume: 100},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			ApiType:           constant.APITypeGemini,
			ChannelBaseUrl:    "https://generativelanguage.googleapis.com",
			ApiKey:            "test-key",
			UpstreamModelName: "gemini-2.5-flash-image",
		},
	}

	ticket, err := CreateDirectRelayTicket(c, info, []byte(`{"contents":[]}`))
	require.NoError(t, err)
	require.NotNil(t, ticket)
	decodedModel, err := base64.RawURLEncoding.DecodeString(ticket.Upstream.ModelB64)
	require.NoError(t, err)
	assert.Equal(t, "gemini-2.5-flash-image", string(decodedModel))
}

func TestDirectRelayErrorCallbackPersistsCompleteBodyAndIsIdempotent(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	_, callbackToken := newDirectRelayCallbackTestTicket(t, "complete-error", model.DirectRelayTicketIssued)
	body := `{"error":{"code":400,"message":"bad request"}}`
	hash := requestFingerprint([]byte(body))
	status, err := processDirectRelayErrorCallback(t, "complete-error", callbackToken, body, false, int64(len(body)), hash)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketRefunded, status)

	status, err = processDirectRelayErrorCallback(t, "complete-error", callbackToken, body, false, int64(len(body)), hash)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketRefunded, status)

	stored, err := model.GetDirectRelayTicket("complete-error")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, model.DirectRelayErrorBody(body), stored.ErrorBody)
	assert.Equal(t, int64(len(body)), stored.ErrorBodyBytes)
	assert.Equal(t, hash, stored.ErrorBodySHA256)
	assert.False(t, stored.ErrorBodyTruncated)
	assert.Equal(t, model.DirectRelayTicketRefunded, stored.Status)
}

func TestDirectRelayErrorCallbackAuditIsFirstWriterWins(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	_, callbackToken := newDirectRelayCallbackTestTicket(t, "first-error-wins", model.DirectRelayTicketIssued)
	firstBody := `{"error":{"code":400,"message":"first"}}`
	firstHash := requestFingerprint([]byte(firstBody))
	status, err := processDirectRelayErrorCallback(t, "first-error-wins", callbackToken, firstBody, false, int64(len(firstBody)), firstHash)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketRefunded, status)

	secondBody := `{"error":{"code":429,"message":"retry payload must not replace the first audit"}}`
	secondHash := requestFingerprint([]byte(secondBody))
	status, err = processDirectRelayErrorCallback(t, "first-error-wins", callbackToken, secondBody, false, int64(len(secondBody)), secondHash)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketRefunded, status)

	stored, err := model.GetDirectRelayTicket("first-error-wins")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, model.DirectRelayErrorBody(firstBody), stored.ErrorBody)
	assert.Equal(t, firstHash, stored.ErrorBodySHA256)
}

func TestDirectRelayCallbackRejectsUnboundedAuditCounters(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	ticket, callbackToken := newDirectRelayCallbackTestTicket(t, "bounded-counters", model.DirectRelayTicketIssued)
	req := &DirectRelayCallbackRequest{
		TicketID:       ticket.TicketID,
		AttemptID:      ticket.AttemptID,
		Outcome:        "success",
		UpstreamStatus: 200,
		ResponseBytes:  directRelayMaxReportedBytes + 1,
	}
	_, err := ProcessDirectRelayCallback(newDirectRelayCallbackContext(), req, callbackToken)
	require.ErrorContains(t, err, "callback counters exceed allowed range")
}

func TestDirectRelayErrorCallbackTruncatesLargeBody(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	_, callbackToken := newDirectRelayCallbackTestTicket(t, "large-error", model.DirectRelayTicketIssued)
	body := strings.Repeat("x", model.DirectRelayMaxErrorBodyBytes+37)
	hash := requestFingerprint([]byte(body))
	status, err := processDirectRelayErrorCallback(t, "large-error", callbackToken, body, true, int64(len(body)), hash)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketRefunded, status)

	stored, err := model.GetDirectRelayTicket("large-error")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Len(t, string(stored.ErrorBody), model.DirectRelayMaxErrorBodyBytes)
	assert.Equal(t, int64(len(body)), stored.ErrorBodyBytes)
	assert.Equal(t, hash, stored.ErrorBodySHA256)
	assert.True(t, stored.ErrorBodyTruncated)
}

func TestDirectRelayErrorCallbackRejectsShortTruncatedPrefix(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	ticket, callbackToken := newDirectRelayCallbackTestTicket(t, "short-truncated-error", model.DirectRelayTicketIssued)
	body := "only-a-short-prefix"
	req := &DirectRelayCallbackRequest{
		TicketID:           ticket.TicketID,
		AttemptID:          ticket.AttemptID,
		Outcome:            "error",
		UpstreamStatus:     500,
		ErrorContentType:   "application/json",
		ErrorBody:          body,
		ErrorBodyBytes:     model.DirectRelayMaxErrorBodyBytes + 1,
		ErrorBodySHA256:    requestFingerprint([]byte(strings.Repeat("x", model.DirectRelayMaxErrorBodyBytes+1))),
		ErrorBodyTruncated: true,
	}
	_, err := ProcessDirectRelayCallback(newDirectRelayCallbackContext(), req, callbackToken)
	require.ErrorContains(t, err, "truncated error body must retain the one-megabyte prefix")
}

func TestDirectRelaySuccessWithoutUsageSettlesFrozenReserveOnce(t *testing.T) {
	setupDirectRelayServiceTestDB(t)
	user := &model.User{Id: 1, Username: "direct-success", Password: "test", AffCode: "direct-success-aff", Quota: 900}
	token := &model.Token{Id: 1, UserId: user.Id, Key: "direct-success-token", RemainQuota: 900, UsedQuota: 100}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(token).Error)
	ticket, callbackToken := newDirectRelayCallbackTestTicket(t, "missing-usage", model.DirectRelayTicketIssued)
	ticket.UserID, ticket.TokenID, ticket.TokenKey = user.Id, token.Id, token.Key
	require.NoError(t, model.DB.Save(ticket).Error)

	req := &DirectRelayCallbackRequest{
		TicketID:       ticket.TicketID,
		AttemptID:      ticket.AttemptID,
		Outcome:        "success",
		UpstreamStatus: 200,
	}
	status, err := ProcessDirectRelayCallback(newDirectRelayCallbackContext(), req, callbackToken)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketSettled, status)

	status, err = ProcessDirectRelayCallback(newDirectRelayCallbackContext(), req, callbackToken)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketSettled, status)
	// A later retry must not replace the first callback's missing-usage audit
	// with a newly supplied usage summary.
	retryWithUsage := *req
	retryWithUsage.UsageMetadata = &dto.GeminiUsageMetadata{
		PromptTokenCount: 20, CandidatesTokenCount: 10, TotalTokenCount: 30,
	}
	status, err = ProcessDirectRelayCallback(newDirectRelayCallbackContext(), &retryWithUsage, callbackToken)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketSettled, status)

	var storedUser model.User
	require.NoError(t, model.DB.First(&storedUser, user.Id).Error)
	assert.Equal(t, 100, storedUser.UsedQuota)
	stored, err := model.GetDirectRelayTicket(ticket.TicketID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "usage_missing", stored.FailureReason)
	assert.True(t, stored.ConsumptionLogged)
	assert.Equal(t, "null", stored.UsageJSON)
}

func TestRecoverExpiredDirectRelaySettlingCompletesConsumptionLog(t *testing.T) {
	setupDirectRelayServiceTestDB(t)

	user := &model.User{Id: 11, Username: "recovery-user", Password: "test", AffCode: "recovery-aff", Quota: 900}
	channel := &model.Channel{Id: 11, Name: "recovery-channel", UsedQuota: 0}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(channel).Error)

	metadata := dto.GeminiUsageMetadata{PromptTokenCount: 10, TotalTokenCount: 10}
	usageJSON, err := common.Marshal(metadata)
	require.NoError(t, err)
	ticket := &model.DirectRelayTicket{
		TicketID:         "recovery-settling",
		AttemptID:        "recovery-attempt",
		RequestID:        "recovery-request",
		Status:           model.DirectRelayTicketSettling,
		BillingSource:    BillingSourceWallet,
		UserID:           user.Id,
		ChannelID:        channel.Id,
		OriginModel:      "gemini-2.5-flash-image",
		UpstreamModel:    "gemini-2.5-flash-image",
		UsageJSON:        string(usageJSON),
		PreConsumedQuota: 100,
		ActualQuota:      100,
		CallbackDeadline: time.Now().Add(-time.Minute).Unix(),
	}
	require.NoError(t, model.DB.Create(ticket).Error)
	settlement := &model.DirectRelaySettlement{
		TicketID:    ticket.TicketID,
		EventKey:    ticket.TicketID + ":settle",
		EventType:   model.DirectRelaySettlementSettle,
		Status:      model.DirectRelaySettlementApplied,
		ActualQuota: 100,
		Delta:       0,
	}
	require.NoError(t, model.DB.Create(settlement).Error)

	status, err := recoverExpiredDirectRelayTicket(ticket)
	require.NoError(t, err)
	assert.Equal(t, model.DirectRelayTicketSettled, status)

	stored, err := model.GetDirectRelayTicket(ticket.TicketID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.True(t, stored.ConsumptionLogged)
	assert.Equal(t, model.DirectRelayTicketSettled, stored.Status)

	var storedUser model.User
	require.NoError(t, model.DB.First(&storedUser, user.Id).Error)
	assert.Equal(t, 100, storedUser.UsedQuota)
}

func TestDirectRelayQuotaMatchesProxyGeminiUsageNormalization(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:     120,
		CandidatesTokenCount: 80,
		ThoughtsTokenCount:   10,
		TotalTokenCount:      210,
		PromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 100},
			{Modality: "IMAGE", TokenCount: 20},
		},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 80}},
	}
	usage := BuildUsageFromGeminiMetadata(metadata, 0)
	price := types.PriceData{
		ModelRatio: 1, CompletionRatio: 2, ImageRatio: 3,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	proxyInfo := &relaycommon.RelayInfo{
		OriginModelName:     "gemini-2.5-flash-image",
		PriceData:           price,
		BillingQuotaPerUnit: 1000,
		ChannelMeta:         &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini},
		StartTime:           time.Now(),
	}
	proxySummary := calculateTextQuotaSummary(newDirectRelayCallbackContext(), proxyInfo, &usage)

	priceJSON, err := common.Marshal(price)
	require.NoError(t, err)
	ticket := &model.DirectRelayTicket{
		OriginModel:      proxyInfo.OriginModelName,
		UpstreamModel:    proxyInfo.OriginModelName,
		BillingPriceJSON: string(priceJSON),
		QuotaPerUnit:     1000,
		PreConsumedQuota: proxySummary.Quota,
	}
	directQuota := calculateDirectRelayQuota(newDirectRelayCallbackContext(), ticket, &usage)
	assert.Equal(t, proxySummary.Quota, directQuota)
}

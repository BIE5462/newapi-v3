package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	// This is far above current Gemini context limits, but still prevents a
	// forged callback from overflowing accounting calculations.
	directRelayMaxUsageTokens = 100_000_000
	// Callback counters are audit data, not billing inputs. Keep them bounded so
	// a forged callback cannot create unreasonably large persisted values.
	directRelayMaxReportedBytes = int64(1) << 40 // 1 TiB
	directRelayMaxElapsedMS     = int64(7 * 24 * 60 * 60 * 1000)
)

type DirectRelaySettlementError struct{ Err error }

func (e *DirectRelaySettlementError) Error() string {
	if e == nil || e.Err == nil {
		return "direct relay settlement failed"
	}
	return e.Err.Error()
}
func (e *DirectRelaySettlementError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type DirectRelayTicketResponse struct {
	Object             string              `json:"object"`
	Version            int                 `json:"version"`
	RequestID          string              `json:"request_id"`
	TicketID           string              `json:"ticket_id"`
	AttemptID          string              `json:"attempt_id"`
	ExpiresAt          int64               `json:"expires_at"`
	CallbackDeadline   int64               `json:"callback_deadline"`
	Upstream           DirectRelayUpstream `json:"upstream"`
	Callback           DirectRelayCallback `json:"callback"`
	RequestFingerprint string              `json:"request_fingerprint"`
}

type DirectRelayUpstream struct {
	Method    string            `json:"method"`
	URLB64    string            `json:"url_b64"`
	APIKeyB64 string            `json:"api_key_b64"`
	ModelB64  string            `json:"model_b64"`
	Headers   map[string]string `json:"headers"`
	Action    string            `json:"action"`
}

type DirectRelayCallback struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type DirectRelayCallbackRequest struct {
	TicketID           string                   `json:"ticket_id"`
	AttemptID          string                   `json:"attempt_id"`
	Outcome            string                   `json:"outcome"`
	UpstreamStatus     int                      `json:"upstream_status"`
	UsageMetadata      *dto.GeminiUsageMetadata `json:"usage_metadata,omitempty"`
	CandidateCount     int                      `json:"candidate_count"`
	ImageCount         int                      `json:"image_count"`
	ResponseBytes      int64                    `json:"response_bytes"`
	ResponseSHA256     string                   `json:"response_sha256"`
	ErrorContentType   string                   `json:"error_content_type"`
	ErrorBody          string                   `json:"error_body"`
	ErrorBodyBytes     int64                    `json:"error_body_bytes"`
	ErrorBodySHA256    string                   `json:"error_body_sha256"`
	ErrorBodyTruncated bool                     `json:"error_body_truncated"`
	UpstreamRequestID  string                   `json:"upstream_request_id"`
	ElapsedMS          int64                    `json:"elapsed_ms"`
}

func validGeminiUsageMetadata(metadata *dto.GeminiUsageMetadata) bool {
	if metadata == nil {
		return false
	}
	values := []int{metadata.PromptTokenCount, metadata.ToolUsePromptTokenCount, metadata.CandidatesTokenCount, metadata.TotalTokenCount, metadata.ThoughtsTokenCount, metadata.CachedContentTokenCount}
	for _, value := range values {
		if value < 0 || value > directRelayMaxUsageTokens {
			return false
		}
	}
	for _, details := range [][]dto.GeminiPromptTokensDetails{metadata.PromptTokensDetails, metadata.ToolUsePromptTokensDetails, metadata.CandidatesTokensDetails} {
		if len(details) > 128 {
			return false
		}
		for _, detail := range details {
			if detail.TokenCount < 0 || detail.TokenCount > directRelayMaxUsageTokens {
				return false
			}
		}
	}
	return metadata.TotalTokenCount > 0 || metadata.PromptTokenCount > 0 || metadata.ToolUsePromptTokenCount > 0 || metadata.CandidatesTokenCount > 0 || metadata.ThoughtsTokenCount > 0
}

func directRelayEnabled() bool {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap["GeminiDirectRelayEnabled"]
	common.OptionMapRWMutex.RUnlock()
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

// directRelayGlobalEnabled allows the server administrator to authorize
// direct relay for every user while preserving the existing per-user switch.
// GeminiDirectRelayEnabled remains the master switch and is still required.
func directRelayGlobalEnabled() bool {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap["GeminiDirectRelayGlobalEnabled"]
	common.OptionMapRWMutex.RUnlock()
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func directRelaySeconds(key string, fallback, min, max int) int {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[key]
	common.OptionMapRWMutex.RUnlock()
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return fallback
	}
	return value
}

func DirectRelayTicketTTLSeconds() int {
	return directRelaySeconds("GeminiDirectTicketTTLSeconds", 600, 60, 3600)
}
func DirectRelayCallbackGraceSeconds() int {
	return directRelaySeconds("GeminiDirectCallbackGraceSeconds", 1800, 300, 86400)
}

// ShouldAttemptGeminiDirect is the inexpensive pre-channel portion of
// eligibility. It never changes billing state; only a later fully qualified
// channel may issue an atomic direct ticket.
func ShouldAttemptGeminiDirect(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c == nil || info == nil || !directRelayEnabled() || (!directRelayGlobalEnabled() && !info.UserSetting.GeminiDirectRelayEnabled) {
		return false
	}
	if info.RelayFormat != types.RelayFormatGemini {
		return false
	}
	if info.PriceData.FreeModel || info.PriceData.QuotaToPreConsume <= 0 {
		return false
	}
	if c.Request == nil || c.Request.URL == nil || c.Request.Method != http.MethodPost || c.Request.URL.RawQuery != "" || !strings.Contains(c.Request.URL.Path, "/models/") || !strings.HasSuffix(c.Request.URL.Path, ":generateContent") || strings.Contains(c.Request.URL.Path, "streamGenerateContent") || info.IsStream {
		return false
	}
	// Model eligibility is checked after channel selection and model mapping;
	// aliases may map to an allowed Gemini image model.
	return true
}

func EnsureDirectRelayPreConsume(info *relaycommon.RelayInfo, quota int) error {
	if info == nil || info.Billing == nil {
		return errors.New("direct relay billing session is missing")
	}
	if quota <= 0 {
		return errors.New("direct relay reserve must be positive")
	}
	if session, ok := info.Billing.(*BillingSession); ok {
		return session.ForceReserve(quota)
	}
	return info.Billing.Reserve(quota)
}

func IsGeminiDirectCandidate(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if !ShouldAttemptGeminiDirect(c, info) {
		return false
	}
	if c.Request == nil || c.Request.URL == nil {
		return false
	}
	if info.ChannelType != constant.ChannelTypeGemini || info.ApiType != constant.APITypeGemini || info.ChannelSetting.Proxy != "" || info.ChannelSetting.SystemPrompt != "" || info.ChannelSetting.SystemPromptOverride || info.ChannelSetting.PassThroughBodyEnabled || len(info.ParamOverride) != 0 || len(info.HeadersOverride) != 0 || info.UseRuntimeHeadersOverride || len(info.RuntimeHeadersOverride) != 0 || info.IsStream || info.PriceData.FreeModel || info.PriceData.QuotaToPreConsume <= 0 {
		return false
	}
	// The normal Gemini adaptor may rewrite the model suffix or inject a
	// thinking configuration. A direct client must send the original body
	// unchanged, so such channels deliberately fall back to proxy mode.
	if model_setting.GetGeminiSettings().ThinkingAdapterEnabled {
		return false
	}
	// Model mappings may intentionally add a provider-specific prefix to the
	// upstream model ID.  The prefix is not part of Gemini's model catalogue,
	// so normalize only for capability/version lookup.  Keep the original
	// mapped value on RelayInfo: it is the model identifier that the direct
	// client must send upstream and receive in model_b64.  Validate the value
	// before normalization so a slash in an arbitrary prefix cannot be hidden
	// by suffix matching and then become a path component in the direct URL.
	mappedModelName := info.UpstreamModelName
	if strings.ContainsAny(mappedModelName, "/?#") {
		return false
	}
	directModelName := directRelayGeminiModelName(mappedModelName, info.IsModelMapped)
	if strings.HasPrefix(directModelName, "imagen") || strings.HasPrefix(directModelName, "text-embedding") || strings.ContainsAny(directModelName, "/?#") || !isGeminiImageModel(directModelName) {
		return false
	}
	version := strings.Trim(strings.TrimSpace(model_setting.GetGeminiVersionSetting(directModelName)), "/")
	if version == "" || strings.ContainsAny(version, "/?#") {
		return false
	}
	if info.TieredBillingSnapshot != nil {
		used := billingexpr.UsedVars(info.TieredBillingSnapshot.ExprString)
		for _, requestOrTimeVariable := range []string{"param", "hour", "minute", "weekday", "month", "day"} {
			if used[requestOrTimeVariable] {
				return false
			}
		}
	}
	if info.ApiKey == "" || info.ChannelBaseUrl == "" {
		return false
	}
	base, err := url.Parse(strings.TrimRight(info.ChannelBaseUrl, "/"))
	if err != nil || base.Host == "" || base.RawQuery != "" || base.Fragment != "" || base.User != nil || !isHTTPURL(base) {
		return false
	}
	callback, err := url.Parse(strings.TrimRight(GetCallbackAddress(), "/"))
	return err == nil && callback.Host != "" && callback.RawQuery == "" && callback.Fragment == "" && callback.User == nil && isHTTPURL(callback)
}

func isHTTPURL(value *url.URL) bool {
	if value == nil || value.Host == "" {
		return false
	}
	return strings.EqualFold(value.Scheme, "http") || strings.EqualFold(value.Scheme, "https")
}

func isGeminiImageModel(modelName string) bool {
	return model_setting.IsGeminiModelSupportImagine(modelName)
}

func directRelayGeminiModelName(modelName string, mapped bool) string {
	if isGeminiImageModel(modelName) {
		return modelName
	}
	if !mapped {
		return modelName
	}
	// A channel mapping can use any prefix (for example 「Rim」, 「XJ」 or
	// 「QH-G」).  Supported Gemini image model IDs are the stable suffixes;
	// match those suffixes rather than baking a provider name into relay
	// eligibility.  The caller still uses the untouched mapped ID in the URL.
	for _, supportedModel := range model_setting.GetGeminiSettings().SupportedImagineModels {
		if strings.HasSuffix(modelName, supportedModel) {
			return supportedModel
		}
	}
	return modelName
}

func encodeDirectRelayValue(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func newDirectRelayID(prefix string) string {
	return prefix + "_" + common.GetRandomString(32)
}

func requestFingerprint(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func CreateDirectRelayTicket(c *gin.Context, info *relaycommon.RelayInfo, requestBody []byte) (*DirectRelayTicketResponse, error) {
	if !IsGeminiDirectCandidate(c, info) {
		return nil, errors.New("direct relay is not supported for this request")
	}
	// Keep the ticket URL identical to the native Gemini adaptor. The Gemini
	// channel's `Other` field is not an API-version override in the existing
	// adaptor; model-specific version settings are the source of truth.
	directModelName := directRelayGeminiModelName(info.UpstreamModelName, info.IsModelMapped)
	version := strings.Trim(strings.TrimSpace(model_setting.GetGeminiVersionSetting(directModelName)), "/")
	if version == "" {
		version = "v1beta"
	}
	urlValue := fmt.Sprintf("%s/%s/models/%s:generateContent", strings.TrimRight(info.ChannelBaseUrl, "/"), strings.Trim(version, "/"), info.UpstreamModelName)
	// Native Gemini API-key channels use the same version selection as the
	// regular Gemini adaptor. Vertex/ADC channels are excluded by eligibility.
	now := common.GetTimestamp()
	ticketID, attemptID := newDirectRelayID("ticket"), newDirectRelayID("attempt")
	callbackToken := newDirectRelayID("cb")
	callbackURL := strings.TrimRight(GetCallbackAddress(), "/") + "/api/direct-relay/gemini/callback"
	ticket := &model.DirectRelayTicket{
		TicketID: ticketID, AttemptID: attemptID, RequestID: info.RequestId, UserID: info.UserId,
		Username: c.GetString("username"),
		TokenID:  info.TokenId, TokenKey: info.TokenKey, ChannelID: info.ChannelId, ChannelType: info.ChannelType,
		ChannelMultiKeyIdx: info.ChannelMultiKeyIndex, BillingSource: info.BillingSource, SubscriptionID: info.SubscriptionId,
		OriginModel: info.OriginModelName, UpstreamModel: info.UpstreamModelName, RequestPath: c.Request.URL.Path,
		Action: "generateContent", UpstreamURL: urlValue, UpstreamAPIKey: info.ApiKey,
		RequestFingerprint: requestFingerprint(requestBody), CallbackTokenHash: model.DirectRelayTokenHash(callbackToken),
		PreConsumedQuota: info.PriceData.QuotaToPreConsume, Status: model.DirectRelayTicketAllocating,
		QuotaPerUnit: info.BillingQuotaPerUnit, QuotaConversionVersion: 1,
		TokenName: c.GetString("token_name"), TokenGroup: info.TokenGroup, UserGroup: info.UserGroup, UsingGroup: info.UsingGroup,
		EstimatedPromptTokens: info.GetEstimatePromptTokens(),
		ExpiresAt:             now + int64(DirectRelayTicketTTLSeconds()), CallbackDeadline: now + int64(DirectRelayTicketTTLSeconds()+DirectRelayCallbackGraceSeconds()),
	}
	if ticket.PreConsumedQuota <= 0 {
		return nil, errors.New("direct relay requires a positive frozen reserve")
	}
	if ticket.QuotaPerUnit <= 0 {
		ticket.QuotaPerUnit = common.QuotaPerUnit
	}
	if err := prepareDirectRelayTicket(ticket, info); err != nil {
		return nil, err
	}
	if err := model.IssueDirectRelayTicket(ticket, common.NormalizeBillingPreference(info.UserSetting.BillingPreference)); err != nil {
		return nil, err
	}
	return &DirectRelayTicketResponse{
		Object: "newapi.direct_ticket", Version: 1, RequestID: info.RequestId, TicketID: ticketID, AttemptID: attemptID,
		ExpiresAt: ticket.ExpiresAt, CallbackDeadline: ticket.CallbackDeadline,
		Upstream: DirectRelayUpstream{Method: http.MethodPost, URLB64: encodeDirectRelayValue(urlValue), APIKeyB64: encodeDirectRelayValue(info.ApiKey), ModelB64: encodeDirectRelayValue(info.UpstreamModelName), Headers: map[string]string{"Content-Type": "application/json", "x-goog-api-key": encodeDirectRelayValue(info.ApiKey)}, Action: "generateContent"},
		Callback: DirectRelayCallback{URL: callbackURL, Token: callbackToken}, RequestFingerprint: ticket.RequestFingerprint,
	}, nil
}

func prepareDirectRelayTicket(ticket *model.DirectRelayTicket, info *relaycommon.RelayInfo) error {
	if ticket == nil || info == nil {
		return errors.New("ticket or relay info is nil")
	}
	if info.TieredBillingSnapshot != nil {
		data, err := common.Marshal(info.TieredBillingSnapshot)
		if err != nil {
			return err
		}
		ticket.TieredSnapshotJSON = string(data)
	}
	data, err := common.Marshal(info.PriceData)
	if err != nil {
		return err
	}
	ticket.BillingPriceJSON = string(data)
	if info.BillingRequestInput != nil {
		// Store only headers and a body digest; request media must not be copied into
		// the ticket database merely to settle a later callback.
		digest := requestFingerprint(info.BillingRequestInput.Body)
		data, err := common.Marshal(map[string]any{"headers": info.BillingRequestInput.Headers, "body_sha256": digest})
		if err != nil {
			return err
		}
		ticket.BillingInputJSON = string(data)
	}
	return nil
}

func SetDirectRelayCallbackContext(c *gin.Context, ticket *model.DirectRelayTicket) {
	if ticket == nil {
		return
	}
	c.Set(common.RequestIdKey, ticket.RequestID)
	c.Set("id", ticket.UserID)
	c.Set("token_id", ticket.TokenID)
	c.Set("token_name", ticket.TokenName)
	c.Set("channel_id", ticket.ChannelID)
	c.Set("original_model", ticket.OriginModel)
	c.Set("group", ticket.UsingGroup)
	c.Set("username", ticket.Username)
	c.Set(common.UpstreamRequestIdKey, ticket.UpstreamRequestID)
}

func newDirectRelayRecoveryContext(ticket *model.DirectRelayTicket) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/direct-relay/gemini/callback", nil)
	SetDirectRelayCallbackContext(c, ticket)
	return c
}

func ProcessDirectRelayCallback(c *gin.Context, req *DirectRelayCallbackRequest, token string) (string, error) {
	if req == nil || req.TicketID == "" || req.AttemptID == "" {
		return "", errors.New("ticket_id and attempt_id are required")
	}
	ticket, err := model.GetDirectRelayTicket(req.TicketID)
	if err != nil || ticket == nil {
		if err != nil {
			return "", &DirectRelaySettlementError{Err: err}
		}
		return "", errors.New("ticket not found")
	}
	if ticket.AttemptID != req.AttemptID || !ticket.VerifyCallbackToken(token) {
		return "", errors.New("invalid callback credentials")
	}
	if req.Outcome != "success" && req.Outcome != "error" {
		return "", errors.New("invalid callback outcome")
	}
	if req.Outcome == "success" && (req.UpstreamStatus < 200 || req.UpstreamStatus > 299) {
		return "", errors.New("success callback requires a 2xx upstream status")
	}
	if req.Outcome == "error" && req.UpstreamStatus >= 200 && req.UpstreamStatus <= 299 {
		return "", errors.New("error callback cannot use a 2xx upstream status")
	}
	if req.UpstreamStatus != 0 && (req.UpstreamStatus < 100 || req.UpstreamStatus > 599) {
		return "", errors.New("invalid upstream status")
	}
	if req.CandidateCount < 0 || req.ImageCount < 0 || req.ResponseBytes < 0 || req.ElapsedMS < 0 {
		return "", errors.New("invalid callback counters")
	}
	if req.CandidateCount > 10000 || req.ImageCount > 10000 {
		return "", errors.New("callback counters exceed allowed range")
	}
	if req.ResponseBytes > directRelayMaxReportedBytes || req.ErrorBodyBytes > directRelayMaxReportedBytes || req.ElapsedMS > directRelayMaxElapsedMS {
		return "", errors.New("callback counters exceed allowed range")
	}
	if len(req.ErrorContentType) > 128 || len(req.UpstreamRequestID) > 255 {
		return "", errors.New("callback metadata is too long")
	}
	if req.ResponseSHA256 != "" && !isSHA256(req.ResponseSHA256) {
		return "", errors.New("invalid response_sha256")
	}
	if req.ErrorBodySHA256 != "" && !isSHA256(req.ErrorBodySHA256) {
		return "", errors.New("invalid error_body_sha256")
	}
	errorBytes := int64(len([]byte(req.ErrorBody)))
	if req.ErrorBodyBytes == 0 {
		req.ErrorBodyBytes = errorBytes
	}
	if req.ErrorBodyBytes < errorBytes || req.ErrorBodyBytes < 0 {
		return "", errors.New("invalid error_body_bytes")
	}
	if errorBytes > model.DirectRelayMaxErrorBodyBytes {
		if !req.ErrorBodyTruncated || req.ErrorBodyBytes <= model.DirectRelayMaxErrorBodyBytes || req.ErrorBodySHA256 == "" {
			return "", errors.New("oversized error body must include original size, hash, and truncated flag")
		}
		req.ErrorBody = string([]byte(req.ErrorBody)[:model.DirectRelayMaxErrorBodyBytes])
		errorBytes = model.DirectRelayMaxErrorBodyBytes
		req.ErrorBodyTruncated = true
	}
	if req.ErrorBodySHA256 == "" && req.ErrorBody != "" {
		req.ErrorBodySHA256 = requestFingerprint([]byte(req.ErrorBody))
	}
	if !req.ErrorBodyTruncated && req.ErrorBodySHA256 != "" && req.ErrorBodySHA256 != requestFingerprint([]byte(req.ErrorBody)) {
		return "", errors.New("error_body_sha256 does not match body")
	}
	if req.ErrorBodyTruncated && (req.ErrorBodyBytes <= model.DirectRelayMaxErrorBodyBytes || req.ErrorBodySHA256 == "") {
		return "", errors.New("truncated error body requires original size and hash")
	}
	if req.ErrorBodyTruncated && req.ErrorBodySHA256 != "" && req.ErrorBodyBytes == errorBytes {
		return "", errors.New("truncated error body size must exceed retained bytes")
	}
	if req.ErrorBodyTruncated && req.ErrorBodyBytes > model.DirectRelayMaxErrorBodyBytes && errorBytes != model.DirectRelayMaxErrorBodyBytes {
		return "", errors.New("truncated error body must retain the one-megabyte prefix")
	}
	if !req.ErrorBodyTruncated && req.ErrorBodyBytes != errorBytes {
		return "", errors.New("error_body_bytes does not match body")
	}
	if req.Outcome == "error" && req.ErrorBody == "" {
		return "", errors.New("error_body is required for error outcome")
	}
	if req.Outcome == "error" && req.ErrorContentType == "" {
		return "", errors.New("error_content_type is required for error outcome")
	}
	if req.Outcome == "success" && (req.ErrorBody != "" || req.ErrorBodyBytes != 0 || req.ErrorBodySHA256 != "" || req.ErrorBodyTruncated) {
		return "", errors.New("success callback cannot contain error body")
	}
	// Terminal callbacks are acknowledgements. A callback that conflicts with
	// the committed outcome is audited, but it can never change billing.
	if ticket.Status == model.DirectRelayTicketSettled {
		if req.Outcome == "error" {
			return recordDirectRelayCallbackConflict(ticket, req)
		}
		if err := ensureDirectRelayConsumptionLog(c, ticket, req.UsageMetadata); err != nil {
			return "", &DirectRelaySettlementError{Err: err}
		}
		return ticket.Status, nil
	}
	if ticket.Status == model.DirectRelayTicketRefunded || ticket.Status == model.DirectRelayTicketCancelled {
		if req.Outcome == "success" {
			return recordDirectRelayCallbackConflict(ticket, req)
		}
		if ticket.Status == model.DirectRelayTicketRefunded {
			if err := persistDirectRelayFailureAudit(c, ticket, req); err != nil {
				return "", &DirectRelaySettlementError{Err: err}
			}
		}
		return ticket.Status, nil
	}
	if ticket.Status == model.DirectRelayTicketIssued && ticket.CallbackDeadline > 0 && common.GetTimestamp() > ticket.CallbackDeadline {
		if status, err := refundExpiredDirectRelayTicket(ticket, "callback_timeout"); err != nil {
			return "", &DirectRelaySettlementError{Err: err}
		} else {
			if req.Outcome == "error" {
				if err := persistDirectRelayFailureAudit(c, ticket, req); err != nil {
					return "", &DirectRelaySettlementError{Err: err}
				}
			}
			return status, nil
		}
	}
	if req.Outcome == "error" {
		if ticket.Status != model.DirectRelayTicketIssued && ticket.Status != model.DirectRelayTicketRefunding {
			return recordDirectRelayCallbackConflict(ticket, req)
		}
		return finalizeDirectRelayRefund(c, ticket, req)
	}
	if ticket.Status != model.DirectRelayTicketIssued && ticket.Status != model.DirectRelayTicketSettling {
		return recordDirectRelayCallbackConflict(ticket, req)
	}
	return finalizeDirectRelaySuccess(c, ticket, req)
}

func recordDirectRelayCallbackConflict(ticket *model.DirectRelayTicket, req *DirectRelayCallbackRequest) (string, error) {
	if err := model.RecordDirectRelayCallbackConflict(ticket.TicketID, req.Outcome, req.UpstreamStatus); err != nil {
		return "", &DirectRelaySettlementError{Err: err}
	}
	return ticket.Status, nil
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func refundExpiredDirectRelayTicket(ticket *model.DirectRelayTicket, reason string) (string, error) {
	if ticket == nil {
		return "", errors.New("ticket is required")
	}
	claimed := ticket
	var err error
	if ticket.Status == model.DirectRelayTicketAllocating || ticket.Status == model.DirectRelayTicketIssued {
		var ok bool
		claimed, ok, err = model.ClaimDirectRelayFinalization(ticket.TicketID, model.DirectRelayTicketRefunding)
		if err != nil {
			return "", err
		}
		if !ok {
			if claimed != nil {
				return claimed.Status, nil
			}
			return "", errors.New("ticket state changed")
		}
		ticket = claimed
	}
	row, err := model.EnsureDirectRelaySettlementForTicket(ticket, model.DirectRelaySettlementRefund, 0, -ticket.PreConsumedQuota)
	if err != nil {
		if errors.Is(err, model.ErrDirectRelayStateChanged) {
			return currentDirectRelayTicketStatus(ticket.TicketID)
		}
		return "", err
	}
	if row.Status != model.DirectRelaySettlementApplied {
		if err := model.ApplyDirectRelaySettlement(ticket, row.ID); err != nil {
			return "", err
		}
	}
	if err := model.MarkDirectRelayTicketFinal(ticket.TicketID, model.DirectRelayTicketRefunded, 0, reason); err != nil {
		return "", err
	}
	return model.DirectRelayTicketRefunded, nil
}

// RefundDirectRelayTicket is the durable compensation entry point for
// persisted direct tickets. It is intentionally separate from
// BillingSession.Refund: the ticket settlement ledger is the only authority
// once a direct reserve transaction has committed.
func RefundDirectRelayTicket(ticketID, reason string) error {
	ticket, err := model.GetDirectRelayTicket(ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return errors.New("direct relay ticket not found")
	}
	if ticket.Status == model.DirectRelayTicketRefunded || ticket.Status == model.DirectRelayTicketSettled {
		return nil
	}
	_, err = refundExpiredDirectRelayTicket(ticket, reason)
	return err
}

func finalizeDirectRelayRefund(c *gin.Context, ticket *model.DirectRelayTicket, req *DirectRelayCallbackRequest) (string, error) {
	claimed := ticket
	ok := true
	var err error
	if ticket.Status == model.DirectRelayTicketIssued {
		claimed, ok, err = model.ClaimDirectRelayFinalization(ticket.TicketID, model.DirectRelayTicketRefunding)
		if err != nil {
			return "", &DirectRelaySettlementError{Err: err}
		}
		if !ok {
			if claimed != nil {
				if claimed.Status != model.DirectRelayTicketRefunding {
					if err := model.RecordDirectRelayCallbackConflict(claimed.TicketID, req.Outcome, req.UpstreamStatus); err != nil {
						return "", &DirectRelaySettlementError{Err: err}
					}
					return claimed.Status, nil
				}
				return "", &DirectRelaySettlementError{Err: errors.New("ticket state changed")}
			}
			return "", &DirectRelaySettlementError{Err: errors.New("ticket state changed")}
		}
		ticket = claimed
	}
	row, err := model.EnsureDirectRelaySettlementForTicket(ticket, model.DirectRelaySettlementRefund, 0, -ticket.PreConsumedQuota)
	if err != nil {
		if errors.Is(err, model.ErrDirectRelayStateChanged) {
			return currentDirectRelayTicketStatus(ticket.TicketID)
		}
		return "", &DirectRelaySettlementError{Err: err}
	}
	if err := persistDirectRelayFailureAudit(c, ticket, req); err != nil {
		return "", &DirectRelaySettlementError{Err: err}
	}
	if row.Status != model.DirectRelaySettlementApplied {
		if err = model.ApplyDirectRelaySettlement(ticket, row.ID); err != nil {
			return "", &DirectRelaySettlementError{Err: err}
		}
	}
	if err := model.MarkDirectRelayTicketFinal(ticket.TicketID, model.DirectRelayTicketRefunded, 0, "upstream_error"); err != nil {
		return "", &DirectRelaySettlementError{Err: err}
	}
	return model.DirectRelayTicketRefunded, nil
}

func persistDirectRelayFailureAudit(c *gin.Context, ticket *model.DirectRelayTicket, req *DirectRelayCallbackRequest) error {
	// Error callbacks are retried by the client. The first complete audit
	// payload must win, otherwise a later retry with a different error could
	// make the stored audit disagree with the already-applied refund.
	if updateErr := model.DB.Model(&model.DirectRelayTicket{}).Where("ticket_id = ? AND error_body_sha256 = ?", ticket.TicketID, "").Updates(map[string]any{
		"error_content_type":   req.ErrorContentType,
		"error_body":           model.DirectRelayErrorBody(req.ErrorBody),
		"error_body_bytes":     req.ErrorBodyBytes,
		"error_body_sha256":    req.ErrorBodySHA256,
		"error_body_truncated": req.ErrorBodyTruncated,
		"upstream_status":      req.UpstreamStatus,
		"failure_reason":       "upstream_error",
		"upstream_request_id":  req.UpstreamRequestID,
		"elapsed_ms":           req.ElapsedMS,
	}).Error; updateErr != nil {
		return updateErr
	}
	latest, loadErr := model.GetDirectRelayTicket(ticket.TicketID)
	if loadErr != nil {
		return loadErr
	}
	if latest == nil {
		return errors.New("direct relay ticket disappeared during failure audit")
	}
	*ticket = *latest
	c.Set(common.UpstreamRequestIdKey, ticket.UpstreamRequestID)
	shouldLog, logErr := model.ClaimDirectRelayErrorLog(ticket.TicketID)
	if logErr != nil {
		return logErr
	}
	if shouldLog {
		if channel, channelErr := model.GetChannelById(ticket.ChannelID, true); channelErr == nil && channel != nil {
			err := types.NewErrorWithStatusCode(errors.New("direct relay upstream error"), types.ErrorCodeBadResponse, ticket.UpstreamStatus, types.ErrOptionWithNoRecordErrorLog())
			if ShouldDisableChannel(err) && channel.GetAutoBan() {
				DisableChannel(*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, ticket.UpstreamAPIKey, channel.GetAutoBan()), "direct relay upstream error")
			}
		}
		model.RecordErrorLog(c, ticket.UserID, ticket.ChannelID, ticket.OriginModel, ticket.TokenName, common.MaskSensitiveInfo(common.LocalLogPreview(string(ticket.ErrorBody))), ticket.TokenID, int(ticket.ElapsedMS/1000), false, ticket.UsingGroup, map[string]interface{}{"direct_relay": true, "upstream_status": ticket.UpstreamStatus, "error_body_bytes": ticket.ErrorBodyBytes, "error_body_sha256": ticket.ErrorBodySHA256, "error_body_truncated": ticket.ErrorBodyTruncated})
	}
	return nil
}

func finalizeDirectRelaySuccess(c *gin.Context, ticket *model.DirectRelayTicket, req *DirectRelayCallbackRequest) (string, error) {
	claimed := ticket
	ok := true
	var err error
	if ticket.Status == model.DirectRelayTicketIssued {
		claimed, ok, err = model.ClaimDirectRelayFinalization(ticket.TicketID, model.DirectRelayTicketSettling)
		if err != nil {
			return "", &DirectRelaySettlementError{Err: err}
		}
		if !ok {
			if claimed != nil {
				if claimed.Status != model.DirectRelayTicketSettling {
					if err := model.RecordDirectRelayCallbackConflict(claimed.TicketID, req.Outcome, req.UpstreamStatus); err != nil {
						return "", &DirectRelaySettlementError{Err: err}
					}
					return claimed.Status, nil
				}
				return "", &DirectRelaySettlementError{Err: errors.New("ticket state changed")}
			}
			return "", &DirectRelaySettlementError{Err: errors.New("ticket state changed")}
		}
		ticket = claimed
	}
	actualQuota := ticket.PreConsumedQuota
	var usage dto.Usage
	usageOK := validGeminiUsageMetadata(req.UsageMetadata)
	if usageOK {
		usage = BuildUsageFromGeminiMetadata(*req.UsageMetadata, 0)
		actualQuota = calculateDirectRelayQuota(c, ticket, &usage)
	}
	row, err := model.EnsureDirectRelaySettlementForTicket(ticket, model.DirectRelaySettlementSettle, actualQuota, actualQuota-ticket.PreConsumedQuota)
	if err != nil {
		if errors.Is(err, model.ErrDirectRelayStateChanged) {
			return currentDirectRelayTicketStatus(ticket.TicketID)
		}
		return "", &DirectRelaySettlementError{Err: err}
	}
	// The first callback that creates the settlement row freezes the financial
	// result. Later retries may repeat the payload but cannot change the amount.
	actualQuota = row.ActualQuota
	usageJSON, marshalErr := common.Marshal(req.UsageMetadata)
	if marshalErr != nil {
		return "", &DirectRelaySettlementError{Err: marshalErr}
	}
	// The settlement row is the financial first-writer boundary. Keep the
	// corresponding audit payload first-writer-wins as well, otherwise a retry
	// with a different usage summary could make the log disagree with the
	// frozen quota. The conditional update is atomic on all supported dialects.
	failureReason := ""
	if !usageOK {
		failureReason = "usage_missing"
	}
	if updateErr := model.DB.Model(&model.DirectRelayTicket{}).Where("ticket_id = ? AND usage_json = ?", ticket.TicketID, "").Updates(map[string]any{"usage_json": string(usageJSON), "upstream_status": req.UpstreamStatus, "candidate_count": req.CandidateCount, "image_count": req.ImageCount, "response_bytes": req.ResponseBytes, "response_sha256": req.ResponseSHA256, "upstream_request_id": req.UpstreamRequestID, "actual_quota": actualQuota, "failure_reason": failureReason, "elapsed_ms": req.ElapsedMS}).Error; updateErr != nil {
		return "", &DirectRelaySettlementError{Err: updateErr}
	}
	latest, loadErr := model.GetDirectRelayTicket(ticket.TicketID)
	if loadErr != nil || latest == nil {
		if loadErr == nil {
			loadErr = errors.New("direct relay ticket disappeared during success audit")
		}
		return "", &DirectRelaySettlementError{Err: loadErr}
	}
	ticket = latest
	actualQuota = row.ActualQuota
	// A duplicate callback must use the metadata that won the first-writer
	// race when writing the consume log, including the missing-usage case.
	usage = dto.Usage{}
	if ticket.UsageJSON != "" && ticket.UsageJSON != "null" {
		var storedMetadata dto.GeminiUsageMetadata
		if err := common.Unmarshal([]byte(ticket.UsageJSON), &storedMetadata); err == nil && validGeminiUsageMetadata(&storedMetadata) {
			usage = BuildUsageFromGeminiMetadata(storedMetadata, 0)
		}
	}
	if row.Status != model.DirectRelaySettlementApplied {
		if err = model.ApplyDirectRelaySettlement(ticket, row.ID); err != nil {
			return "", &DirectRelaySettlementError{Err: err}
		}
	}
	c.Set(common.UpstreamRequestIdKey, ticket.UpstreamRequestID)
	if err := claimAndRecordDirectRelayConsumption(c, ticket, &usage); err != nil {
		return "", &DirectRelaySettlementError{Err: err}
	}
	if err := model.MarkDirectRelayTicketFinal(ticket.TicketID, model.DirectRelayTicketSettled, actualQuota, ticket.FailureReason); err != nil {
		return "", &DirectRelaySettlementError{Err: err}
	}
	return model.DirectRelayTicketSettled, nil
}

func currentDirectRelayTicketStatus(ticketID string) (string, error) {
	ticket, err := model.GetDirectRelayTicket(ticketID)
	if err != nil {
		return "", &DirectRelaySettlementError{Err: err}
	}
	if ticket == nil {
		return "", &DirectRelaySettlementError{Err: errors.New("ticket not found")}
	}
	return ticket.Status, nil
}

func claimAndRecordDirectRelayConsumption(c *gin.Context, ticket *model.DirectRelayTicket, usage *dto.Usage) error {
	if usage == nil {
		usage = &dto.Usage{}
	}
	shouldLog, err := model.ClaimDirectRelayConsumptionLog(ticket.TicketID)
	if err != nil {
		return err
	}
	if shouldLog {
		recordDirectRelayConsume(c, ticket, usage, ticket.ActualQuota, ticket.ElapsedMS)
	}
	return nil
}

func ensureDirectRelayConsumptionLog(c *gin.Context, ticket *model.DirectRelayTicket, callbackMetadata ...*dto.GeminiUsageMetadata) error {
	if ticket == nil || ticket.Status != model.DirectRelayTicketSettled || ticket.ConsumptionLogged {
		return nil
	}
	var usage dto.Usage
	// An empty UsageJSON means no callback has durably recorded its audit
	// payload yet. The literal "null" is a deliberate first callback with
	// missing usage and must remain frozen on later retries.
	storedUsage := ticket.UsageJSON != ""
	if ticket.UsageJSON != "" && ticket.UsageJSON != "null" {
		var metadata dto.GeminiUsageMetadata
		if err := common.Unmarshal([]byte(ticket.UsageJSON), &metadata); err == nil && validGeminiUsageMetadata(&metadata) {
			usage = BuildUsageFromGeminiMetadata(metadata, 0)
		}
	}
	if !storedUsage && len(callbackMetadata) > 0 && validGeminiUsageMetadata(callbackMetadata[0]) {
		usage = BuildUsageFromGeminiMetadata(*callbackMetadata[0], 0)
		if usageJSON, err := common.Marshal(callbackMetadata[0]); err != nil {
			return err
		} else if err := model.DB.Model(&model.DirectRelayTicket{}).Where("ticket_id = ?", ticket.TicketID).Updates(map[string]any{
			"usage_json": string(usageJSON), "upstream_status": 200,
		}).Error; err != nil {
			return err
		}
	}
	return claimAndRecordDirectRelayConsumption(c, ticket, &usage)
}

// recoverExpiredDirectRelayTicket closes stale asynchronous states. A success
// settlement row always wins and is completed; a settling ticket with no row
// is atomically moved to the refund path so it cannot remain charged forever.
func recoverExpiredDirectRelayTicket(ticket *model.DirectRelayTicket) (string, error) {
	if ticket == nil {
		return "", errors.New("ticket is required")
	}
	switch ticket.Status {
	case model.DirectRelayTicketAllocating, model.DirectRelayTicketIssued, model.DirectRelayTicketRefunding:
		return refundExpiredDirectRelayTicket(ticket, "callback_timeout")
	case model.DirectRelayTicketSettling:
		row, err := model.GetDirectRelaySettlement(ticket.TicketID, model.DirectRelaySettlementSettle)
		if err != nil {
			return "", err
		}
		if row == nil {
			recovered, transitioned, err := model.RecoverDirectRelaySettlingWithoutSettlement(ticket.TicketID)
			if err != nil {
				return "", err
			}
			if !transitioned {
				if recovered == nil {
					return "", errors.New("ticket state changed during settlement recovery")
				}
				return recovered.Status, nil
			}
			return refundExpiredDirectRelayTicket(recovered, "settlement_recovery_timeout")
		}
		if row.Status != model.DirectRelaySettlementApplied {
			if err := model.ApplyDirectRelaySettlement(ticket, row.ID); err != nil {
				return "", err
			}
		}
		latest, err := model.GetDirectRelayTicket(ticket.TicketID)
		if err != nil {
			return "", err
		}
		if latest == nil {
			return "", errors.New("ticket not found during settlement recovery")
		}
		reason := latest.FailureReason
		if latest.UsageJSON == "" {
			reason = "usage_missing"
		}
		if err := model.MarkDirectRelayTicketFinal(ticket.TicketID, model.DirectRelayTicketSettled, row.ActualQuota, reason); err != nil {
			return "", err
		}
		latest.Status = model.DirectRelayTicketSettled
		latest.ActualQuota = row.ActualQuota
		if err := ensureDirectRelayConsumptionLog(newDirectRelayRecoveryContext(latest), latest); err != nil {
			return "", err
		}
		return model.DirectRelayTicketSettled, nil
	default:
		return ticket.Status, nil
	}
}

func calculateDirectRelayQuota(c *gin.Context, ticket *model.DirectRelayTicket, usage *dto.Usage) int {
	info := buildDirectRelayBillingInfo(ticket)
	if info.TieredBillingSnapshot != nil {
		used := billingexpr.UsedVars(info.TieredBillingSnapshot.ExprString)
		if ok, quota, _ := TryTieredSettle(info, BuildTieredTokenParams(usage, false, used)); ok {
			return quota
		}
	}
	summary := calculateTextQuotaSummary(c, info, usage)
	if summary.Quota <= 0 {
		return ticket.PreConsumedQuota
	}
	return summary.Quota
}

func buildDirectRelayBillingInfo(ticket *model.DirectRelayTicket) *relaycommon.RelayInfo {
	var price types.PriceData
	if ticket.BillingPriceJSON != "" {
		_ = common.Unmarshal([]byte(ticket.BillingPriceJSON), &price)
	}
	info := &relaycommon.RelayInfo{OriginModelName: ticket.OriginModel, FinalPreConsumedQuota: ticket.PreConsumedQuota, PriceData: price, BillingQuotaPerUnit: ticket.QuotaPerUnit, BillingSource: ticket.BillingSource, UserId: ticket.UserID, TokenId: ticket.TokenID, TokenKey: ticket.TokenKey, TokenGroup: ticket.TokenGroup, UserGroup: ticket.UserGroup, UsingGroup: ticket.UsingGroup, StartTime: time.Now()}
	info.Request = &dto.GeminiChatRequest{}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: ticket.ChannelType, ChannelId: ticket.ChannelID, UpstreamModelName: ticket.UpstreamModel}
	if ticket.BillingInputJSON != "" {
		var input struct {
			Headers map[string]string `json:"headers"`
		}
		if common.Unmarshal([]byte(ticket.BillingInputJSON), &input) == nil {
			info.BillingRequestInput = &billingexpr.RequestInput{Headers: input.Headers}
		}
	}
	if ticket.TieredSnapshotJSON != "" {
		var snapshot billingexpr.BillingSnapshot
		if common.Unmarshal([]byte(ticket.TieredSnapshotJSON), &snapshot) == nil {
			info.TieredBillingSnapshot = &snapshot
		}
	}
	return info
}

func recordDirectRelayConsume(c *gin.Context, ticket *model.DirectRelayTicket, usage *dto.Usage, quota int, elapsedMS int64) {
	if quota > 0 {
		model.UpdateUserUsedQuotaAndRequestCountDirect(ticket.UserID, quota)
		model.UpdateChannelUsedQuotaDirect(ticket.ChannelID, quota)
	}
	other := map[string]interface{}{
		"direct_relay":        true,
		"request_path":        ticket.RequestPath,
		"upstream_model_name": ticket.UpstreamModel,
		"multi_key_index":     ticket.ChannelMultiKeyIdx,
		"response_bytes":      ticket.ResponseBytes,
		"response_sha256":     ticket.ResponseSHA256,
		"image_input":         usage.PromptTokensDetails.ImageTokens,
		"image_output":        usage.CompletionTokenDetails.ImageTokens,
		"audio_input":         usage.PromptTokensDetails.AudioTokens,
		"audio_output":        usage.CompletionTokenDetails.AudioTokens,
		"reasoning_tokens":    usage.CompletionTokenDetails.ReasoningTokens,
		"billing_source":      ticket.BillingSource,
	}
	info := buildDirectRelayBillingInfo(ticket)
	other["model_ratio"] = info.PriceData.ModelRatio
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	other["completion_ratio"] = info.PriceData.CompletionRatio
	other["cache_ratio"] = info.PriceData.CacheRatio
	other["image_ratio"] = info.PriceData.ImageRatio
	other["model_price"] = info.PriceData.ModelPrice
	if info.TieredBillingSnapshot != nil {
		used := billingexpr.UsedVars(info.TieredBillingSnapshot.ExprString)
		if ok, _, result := TryTieredSettle(info, BuildTieredTokenParams(usage, false, used)); ok {
			InjectTieredBillingInfo(other, info, result)
		}
	}
	if ticket.FailureReason == "usage_missing" {
		other["usage_missing"] = true
	}
	model.RecordConsumeLog(c, ticket.UserID, model.RecordConsumeLogParams{ChannelId: ticket.ChannelID, PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, ModelName: ticket.OriginModel, TokenName: ticket.TokenName, Quota: quota, Content: fmt.Sprintf("Gemini direct relay, elapsed_ms=%d", elapsedMS), TokenId: ticket.TokenID, UseTimeSeconds: int(elapsedMS / 1000), IsStream: false, Group: ticket.UsingGroup, Other: other})
}

// BuildUsageFromGeminiMetadata is the single Gemini usage normalization path
// shared by proxied and direct-relay requests.
func BuildUsageFromGeminiMetadata(metadata dto.GeminiUsageMetadata, fallback int) dto.Usage {
	// This mirrors relay/channel/gemini's native usage normalization so direct
	// and proxied calls use exactly the same billing dimensions.
	usage := dto.Usage{PromptTokens: metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount, CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount, TotalTokens: metadata.TotalTokenCount}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount
	for _, detail := range metadata.PromptTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = fallback
	}
	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}
	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 && usage.PromptTokensDetails.ImageTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}
	return usage
}

func DirectRelayErrorStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var settlementErr *DirectRelaySettlementError
	if errors.As(err, &settlementErr) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

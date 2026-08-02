package model

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

const (
	DirectRelayTicketAllocating = "allocating"
	DirectRelayTicketIssued     = "issued"
	DirectRelayTicketSettling   = "settling"
	DirectRelayTicketSettled    = "settled"
	DirectRelayTicketRefunding  = "refunding"
	DirectRelayTicketRefunded   = "refunded"
	DirectRelayTicketCancelled  = "cancelled"
)

const (
	DirectRelaySettlementReserve = "reserve"
	DirectRelaySettlementSettle  = "settle"
	DirectRelaySettlementRefund  = "refund"

	DirectRelaySettlementPending    = "pending"
	DirectRelaySettlementProcessing = "processing"
	DirectRelaySettlementApplied    = "applied"
	DirectRelaySettlementFailed     = "failed"
)

const DirectRelayMaxErrorBodyBytes = 1024 * 1024

var ErrDirectRelayStateChanged = errors.New("direct relay ticket state changed")

// DirectRelayErrorBody uses a dialect-aware database type. MySQL TEXT is only
// 64 KiB, while the callback protocol retains up to 1 MiB of upstream error
// text. Keeping the type decision in GORM's schema prevents AutoMigrate from
// downgrading an existing MEDIUMTEXT column back to TEXT on every restart.
type DirectRelayErrorBody string

func (DirectRelayErrorBody) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "mysql" {
		return "MEDIUMTEXT"
	}
	return "TEXT"
}

// DirectRelayTicket persists all data needed to settle a client-side Gemini call
// after the original HTTP request has completed.
type DirectRelayTicket struct {
	ID                     int64                `json:"id" gorm:"primaryKey"`
	TicketID               string               `json:"ticket_id" gorm:"type:varchar(96);uniqueIndex"`
	AttemptID              string               `json:"attempt_id" gorm:"type:varchar(96);uniqueIndex"`
	RequestID              string               `json:"request_id" gorm:"type:varchar(96);index"`
	UserID                 int                  `json:"user_id" gorm:"index"`
	TokenID                int                  `json:"token_id" gorm:"index"`
	TokenKey               string               `json:"-" gorm:"type:text"`
	TokenName              string               `json:"token_name" gorm:"type:varchar(128)"`
	TokenGroup             string               `json:"token_group" gorm:"type:varchar(64)"`
	UserGroup              string               `json:"user_group" gorm:"type:varchar(64)"`
	UsingGroup             string               `json:"using_group" gorm:"type:varchar(64)"`
	ChannelID              int                  `json:"channel_id" gorm:"index"`
	ChannelType            int                  `json:"channel_type"`
	ChannelMultiKeyIdx     int                  `json:"channel_multi_key_index"`
	BillingSource          string               `json:"billing_source" gorm:"type:varchar(32)"`
	SubscriptionID         int                  `json:"subscription_id"`
	OriginModel            string               `json:"origin_model" gorm:"type:varchar(255)"`
	UpstreamModel          string               `json:"upstream_model" gorm:"type:varchar(255)"`
	RequestPath            string               `json:"request_path" gorm:"type:varchar(512)"`
	Action                 string               `json:"action" gorm:"type:varchar(64)"`
	UpstreamURL            string               `json:"-" gorm:"type:text"`
	UpstreamAPIKey         string               `json:"-" gorm:"type:text"`
	RequestFingerprint     string               `json:"request_fingerprint" gorm:"type:varchar(128)"`
	CallbackTokenHash      string               `json:"-" gorm:"type:varchar(128)"`
	BillingPriceJSON       string               `json:"-" gorm:"type:text"`
	TieredSnapshotJSON     string               `json:"-" gorm:"type:text"`
	BillingInputJSON       string               `json:"-" gorm:"type:text"`
	QuotaPerUnit           float64              `json:"quota_per_unit"`
	QuotaConversionVersion int                  `json:"quota_conversion_version"`
	PreConsumedQuota       int                  `json:"pre_consumed_quota"`
	ActualQuota            int                  `json:"actual_quota"`
	EstimatedPromptTokens  int                  `json:"estimated_prompt_tokens"`
	ConsumptionLogged      bool                 `json:"consumption_logged"`
	ErrorLogged            bool                 `json:"error_logged"`
	ConflictCount          int                  `json:"conflict_count"`
	LastConflictOutcome    string               `json:"last_conflict_outcome" gorm:"type:varchar(32)"`
	LastConflictStatus     int                  `json:"last_conflict_status"`
	LastConflictAt         int64                `json:"last_conflict_at" gorm:"bigint"`
	Status                 string               `json:"status" gorm:"type:varchar(32);index"`
	ExpiresAt              int64                `json:"expires_at" gorm:"bigint;index"`
	CallbackDeadline       int64                `json:"callback_deadline" gorm:"bigint;index"`
	UpstreamStatus         int                  `json:"upstream_status"`
	UsageJSON              string               `json:"usage_json" gorm:"type:text"`
	ErrorContentType       string               `json:"error_content_type" gorm:"type:varchar(128)"`
	ErrorBody              DirectRelayErrorBody `json:"error_body"`
	ErrorBodyBytes         int64                `json:"error_body_bytes"`
	ErrorBodySHA256        string               `json:"error_body_sha256" gorm:"type:varchar(128)"`
	ErrorBodyTruncated     bool                 `json:"error_body_truncated"`
	ResponseBytes          int64                `json:"response_bytes"`
	ResponseSHA256         string               `json:"response_sha256" gorm:"type:varchar(128)"`
	CandidateCount         int                  `json:"candidate_count"`
	ImageCount             int                  `json:"image_count"`
	ElapsedMS              int64                `json:"elapsed_ms"`
	UpstreamRequestID      string               `json:"upstream_request_id" gorm:"type:varchar(255)"`
	FailureReason          string               `json:"failure_reason" gorm:"type:text"`
	CreatedAt              int64                `json:"created_at" gorm:"bigint;index"`
	UpdatedAt              int64                `json:"updated_at" gorm:"bigint;index"`
	FinalizedAt            int64                `json:"finalized_at" gorm:"bigint"`
}

type DirectRelaySettlement struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	TicketID       string `json:"ticket_id" gorm:"type:varchar(96);index"`
	EventKey       string `json:"event_key" gorm:"type:varchar(160);uniqueIndex"`
	EventType      string `json:"event_type" gorm:"type:varchar(32)"`
	Status         string `json:"status" gorm:"type:varchar(32);index"`
	PreConsumed    int    `json:"pre_consumed"`
	ActualQuota    int    `json:"actual_quota"`
	Delta          int    `json:"delta"`
	BillingSource  string `json:"billing_source" gorm:"type:varchar(32)"`
	UserID         int    `json:"user_id"`
	TokenID        int    `json:"token_id"`
	SubscriptionID int    `json:"subscription_id"`
	Attempts       int    `json:"attempts"`
	LastError      string `json:"last_error" gorm:"type:text"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint;index"`
	AppliedAt      int64  `json:"applied_at" gorm:"bigint"`
}

// ReserveDirectRelayFundsTx applies a direct-relay reserve inside the caller's
// transaction.  It deliberately bypasses the batch quota queue: the ticket,
// reserve audit row, and the account changes must commit or roll back as one
// unit.  The billing preference is evaluated while the account rows are
// locked, so a concurrent request cannot select a stale wallet/subscription.
func ReserveDirectRelayFundsTx(tx *gorm.DB, ticket *DirectRelayTicket, preference string) error {
	if tx == nil || ticket == nil {
		return errors.New("transaction and ticket are required")
	}
	if ticket.PreConsumedQuota <= 0 {
		return errors.New("direct relay reserve must be positive")
	}
	preference = strings.TrimSpace(preference)
	if preference == "" {
		preference = "subscription_first"
	}

	// Lock user and token before subscription rows. Every direct reserve uses
	// this order, which avoids wallet/token vs subscription deadlocks.
	var user User
	if ticket.UserID > 0 {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, ticket.UserID).Error; err != nil {
			return err
		}
	}
	var token Token
	if ticket.TokenID > 0 {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&token, ticket.TokenID).Error; err != nil {
			return err
		}
		if ticket.TokenKey != "" && token.Key != ticket.TokenKey {
			return errors.New("direct relay token identity mismatch")
		}
	}

	amount := int64(ticket.PreConsumedQuota)
	walletAvailable := ticket.UserID <= 0 || user.Quota >= ticket.PreConsumedQuota
	tokenAvailable := ticket.TokenID <= 0 || token.UnlimitedQuota || token.RemainQuota >= ticket.PreConsumedQuota
	// Token quota is independent of the wallet/subscription preference. The
	// regular BillingSession checks it before selecting the funding source, so
	// direct issuance must fail here rather than incorrectly switching sources.
	if !tokenAvailable {
		return fmt.Errorf("token quota is not enough")
	}

	reserveWallet := func() error {
		if !walletAvailable {
			return fmt.Errorf("user quota is not enough")
		}
		if ticket.UserID > 0 {
			if err := tx.Model(&User{}).Where("id = ?", ticket.UserID).Update("quota", gorm.Expr("quota - ?", ticket.PreConsumedQuota)).Error; err != nil {
				return err
			}
		}
		if ticket.TokenID > 0 {
			if err := tx.Model(&Token{}).Where("id = ?", ticket.TokenID).Updates(map[string]any{
				"remain_quota":  gorm.Expr("remain_quota - ?", ticket.PreConsumedQuota),
				"used_quota":    gorm.Expr("used_quota + ?", ticket.PreConsumedQuota),
				"accessed_time": common.GetTimestamp(),
			}).Error; err != nil {
				return err
			}
		}
		ticket.BillingSource = "wallet"
		return nil
	}

	reserveSubscription := func() error {
		if ticket.UserID <= 0 {
			return errors.New("subscription reserve requires a user")
		}
		sub, err := preConsumeUserSubscriptionTx(tx, ticket.RequestID, ticket.UserID, ticket.OriginModel, amount)
		if err != nil {
			return err
		}
		if ticket.TokenID > 0 {
			if err := tx.Model(&Token{}).Where("id = ?", ticket.TokenID).Updates(map[string]any{
				"remain_quota":  gorm.Expr("remain_quota - ?", ticket.PreConsumedQuota),
				"used_quota":    gorm.Expr("used_quota + ?", ticket.PreConsumedQuota),
				"accessed_time": common.GetTimestamp(),
			}).Error; err != nil {
				return err
			}
		}
		ticket.BillingSource = "subscription"
		ticket.SubscriptionID = sub.UserSubscriptionId
		return nil
	}

	// Match BillingSession's preference semantics. Wallet-first only falls
	// through when wallet balance is insufficient; subscription-first falls
	// through when no/insufficient subscription and overflow is permitted.
	switch preference {
	case "wallet_only":
		return reserveWallet()
	case "subscription_only":
		return reserveSubscription()
	case "wallet_first":
		if walletAvailable {
			return reserveWallet()
		}
		return reserveSubscription()
	default: // subscription_first
		if err := reserveSubscription(); err == nil {
			return nil
		} else {
			// BillingSession only falls back to wallet for a missing or
			// insufficient subscription, never for arbitrary database errors.
			if !strings.Contains(err.Error(), "no active subscription") && !strings.Contains(err.Error(), "subscription quota insufficient") {
				return err
			}
			allowWallet, allowErr := userActiveSubscriptionsAllowWalletOverflowTx(tx, ticket.UserID)
			if allowErr != nil || !allowWallet {
				return err
			}
			return reserveWallet()
		}
	}
}

// IssueDirectRelayTicket atomically reserves account quota, writes the issued
// ticket and its reserve audit row. No allocating ticket is exposed by this
// normal issuance path.
func IssueDirectRelayTicket(ticket *DirectRelayTicket, preference string) error {
	if ticket == nil {
		return errors.New("ticket is required")
	}
	var invalidateUser, invalidateToken bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := ReserveDirectRelayFundsTx(tx, ticket, preference); err != nil {
			return err
		}
		ticket.Status = DirectRelayTicketIssued
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}
		reserve := &DirectRelaySettlement{
			TicketID: ticket.TicketID, EventKey: ticket.TicketID + ":" + DirectRelaySettlementReserve,
			EventType: DirectRelaySettlementReserve, Status: DirectRelaySettlementApplied,
			PreConsumed: ticket.PreConsumedQuota, ActualQuota: ticket.PreConsumedQuota,
			BillingSource: ticket.BillingSource, UserID: ticket.UserID, TokenID: ticket.TokenID,
			SubscriptionID: ticket.SubscriptionID, AppliedAt: common.GetTimestamp(),
		}
		if err := tx.Create(reserve).Error; err != nil {
			return err
		}
		invalidateUser = ticket.UserID > 0
		invalidateToken = ticket.TokenID > 0 && ticket.TokenKey != ""
		return nil
	})
	if err != nil {
		return err
	}
	if common.RedisEnabled {
		if invalidateUser {
			if err := invalidateUserCache(ticket.UserID); err != nil {
				common.SysLog("failed to invalidate direct-relay user quota cache: " + err.Error())
			}
		}
		if invalidateToken {
			if err := cacheDeleteToken(ticket.TokenKey); err != nil {
				common.SysLog("failed to invalidate direct-relay token quota cache: " + err.Error())
			}
		}
	}
	return nil
}

func (t *DirectRelayTicket) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if t.CreatedAt == 0 {
		t.CreatedAt = now
	}
	if t.UpdatedAt == 0 {
		t.UpdatedAt = now
	}
	return nil
}

func (t *DirectRelayTicket) BeforeUpdate(_ *gorm.DB) error {
	t.UpdatedAt = common.GetTimestamp()
	return nil
}

func (s *DirectRelaySettlement) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}
	if s.UpdatedAt == 0 {
		s.UpdatedAt = now
	}
	return nil
}

func (s *DirectRelaySettlement) BeforeUpdate(_ *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

func userActiveSubscriptionsAllowWalletOverflowTx(tx *gorm.DB, userID int) (bool, error) {
	var strict int64
	err := tx.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ? AND allow_wallet_overflow = ?", userID, "active", GetDBTimestamp(), false).
		Count(&strict).Error
	return strict == 0, err
}

// preConsumeUserSubscriptionTx is the transaction-scoped equivalent of
// PreConsumeUserSubscription. The caller already owns the transaction, so no
// independent commit can occur between the subscription reserve and ticket.
func preConsumeUserSubscriptionTx(tx *gorm.DB, requestID string, userID int, modelName string, amount int64) (*SubscriptionPreConsumeResult, error) {
	if requestID == "" || userID <= 0 || amount <= 0 {
		return nil, errors.New("invalid subscription pre-consume")
	}
	var existing SubscriptionPreConsumeRecord
	if query := tx.Where("request_id = ?", requestID).First(&existing); query.Error == nil {
		if existing.Status == "refunded" {
			return nil, errors.New("subscription pre-consume already refunded")
		}
		if existing.UserId != userID || existing.PreConsumed != amount {
			return nil, errors.New("subscription pre-consume identity or amount mismatch")
		}
		return &SubscriptionPreConsumeResult{UserSubscriptionId: existing.UserSubscriptionId, PreConsumed: existing.PreConsumed}, nil
	} else if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return nil, query.Error
	}

	var subs []UserSubscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"user_id = ? AND status = ? AND end_time > ?", userID, "active", GetDBTimestamp()).
		Order("end_time asc, id asc").Find(&subs).Error; err != nil {
		return nil, errors.New("no active subscription")
	}
	for i := range subs {
		sub := &subs[i]
		plan, err := getSubscriptionPlanByIdTx(tx, sub.PlanId)
		if err != nil {
			return nil, err
		}
		if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, plan, GetDBTimestamp()); err != nil {
			return nil, err
		}
		usedBefore := sub.AmountUsed
		if sub.AmountTotal > 0 && sub.AmountTotal-usedBefore < amount {
			continue
		}
		record := &SubscriptionPreConsumeRecord{RequestId: requestID, UserId: userID, UserSubscriptionId: sub.Id, PreConsumed: amount, Status: "consumed"}
		if err := tx.Create(record).Error; err != nil {
			return nil, err
		}
		sub.AmountUsed += amount
		if err := tx.Save(sub).Error; err != nil {
			return nil, err
		}
		return &SubscriptionPreConsumeResult{UserSubscriptionId: sub.Id, PreConsumed: amount, AmountTotal: sub.AmountTotal, AmountUsedBefore: usedBefore, AmountUsedAfter: sub.AmountUsed}, nil
	}
	return nil, fmt.Errorf("subscription quota insufficient, need=%d", amount)
}

func DirectRelayTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (t *DirectRelayTicket) VerifyCallbackToken(token string) bool {
	if token == "" || t.CallbackTokenHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(DirectRelayTokenHash(token)), []byte(t.CallbackTokenHash)) == 1
}

func GetDirectRelayTicket(ticketID string) (*DirectRelayTicket, error) {
	var ticket DirectRelayTicket
	if err := DB.Where("ticket_id = ?", ticketID).First(&ticket).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ticket, nil
}

// ClaimDirectRelayFinalization atomically wins either settlement or refund.
func ClaimDirectRelayFinalization(ticketID, targetStatus string) (*DirectRelayTicket, bool, error) {
	if targetStatus != DirectRelayTicketSettling && targetStatus != DirectRelayTicketRefunding {
		return nil, false, errors.New("invalid direct relay finalization status")
	}
	query := DB.Model(&DirectRelayTicket{}).Where("ticket_id = ?", ticketID)
	if targetStatus == DirectRelayTicketRefunding {
		// A ticket is externally visible only after it becomes issued, but a
		// process can stop after the durable reserve transaction and before that
		// final status update. Such an allocating ticket still owns the reserve
		// and must enter the normal refund state machine.
		query = query.Where("status IN ?", []string{DirectRelayTicketAllocating, DirectRelayTicketIssued})
	} else {
		query = query.Where("status = ?", DirectRelayTicketIssued)
	}
	result := query.Updates(map[string]any{"status": targetStatus, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return nil, false, result.Error
	}
	ticket, err := GetDirectRelayTicket(ticketID)
	return ticket, result.RowsAffected == 1, err
}

func MarkDirectRelayTicketFinal(ticketID, status string, actualQuota int, reason string) error {
	fromStatus := ""
	switch status {
	case DirectRelayTicketSettled:
		fromStatus = DirectRelayTicketSettling
	case DirectRelayTicketRefunded:
		fromStatus = DirectRelayTicketRefunding
	default:
		return errors.New("invalid direct relay final status")
	}
	result := DB.Model(&DirectRelayTicket{}).Where("ticket_id = ? AND status = ?", ticketID, fromStatus).Updates(map[string]any{
		"status": status, "actual_quota": actualQuota, "failure_reason": reason,
		"finalized_at": common.GetTimestamp(), "updated_at": common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		current, err := GetDirectRelayTicket(ticketID)
		if err != nil {
			return err
		}
		if current == nil || current.Status != status {
			return errors.New("direct relay ticket final state changed")
		}
	}
	return nil
}

func ClaimDirectRelayConsumptionLog(ticketID string) (bool, error) {
	result := DB.Model(&DirectRelayTicket{}).Where("ticket_id = ? AND consumption_logged = ?", ticketID, false).Updates(map[string]any{
		"consumption_logged": true,
		"updated_at":         common.GetTimestamp(),
	})
	return result.RowsAffected == 1, result.Error
}

// ClaimDirectRelayErrorLog atomically ensures that a failed callback creates
// at most one error log even when the client retries after a transient
// database failure or a process restart.
func ClaimDirectRelayErrorLog(ticketID string) (bool, error) {
	result := DB.Model(&DirectRelayTicket{}).Where("ticket_id = ? AND error_logged = ?", ticketID, false).Updates(map[string]any{
		"error_logged": true,
		"updated_at":   common.GetTimestamp(),
	})
	return result.RowsAffected == 1, result.Error
}

func RecordDirectRelayCallbackConflict(ticketID, outcome string, upstreamStatus int) error {
	return DB.Model(&DirectRelayTicket{}).Where("ticket_id = ?", ticketID).Updates(map[string]any{
		"conflict_count":        gorm.Expr("conflict_count + ?", 1),
		"last_conflict_outcome": outcome,
		"last_conflict_status":  upstreamStatus,
		"last_conflict_at":      common.GetTimestamp(),
		"updated_at":            common.GetTimestamp(),
	}).Error
}

// EnsureDirectRelaySettlementForTicket is the preferred constructor for
// asynchronous settlement rows. In addition to the amounts it freezes the
// funding identities needed for audit and recovery.
func EnsureDirectRelaySettlementForTicket(ticket *DirectRelayTicket, eventType string, actual, delta int) (*DirectRelaySettlement, error) {
	if ticket == nil {
		return nil, errors.New("ticket is required")
	}
	expectedStatus := DirectRelayTicketSettling
	if eventType == DirectRelaySettlementRefund {
		expectedStatus = DirectRelayTicketRefunding
	} else if eventType != DirectRelaySettlementSettle {
		return nil, errors.New("invalid direct relay settlement event")
	}
	var row DirectRelaySettlement
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current DirectRelayTicket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("ticket_id = ?", ticket.TicketID).First(&current).Error; err != nil {
			return err
		}
		if current.Status != expectedStatus {
			return ErrDirectRelayStateChanged
		}
		eventKey := ticket.TicketID + ":" + eventType
		row = DirectRelaySettlement{
			TicketID: ticket.TicketID, EventKey: eventKey, EventType: eventType,
			Status: DirectRelaySettlementPending, PreConsumed: current.PreConsumedQuota,
			ActualQuota: actual, Delta: delta, BillingSource: current.BillingSource,
			UserID: current.UserID, TokenID: current.TokenID, SubscriptionID: current.SubscriptionID,
		}
		return tx.Where("event_key = ?", eventKey).FirstOrCreate(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RecoverDirectRelaySettlingWithoutSettlement atomically moves a stale
// settling ticket to the refund path only when no success settlement row was
// ever created. It shares the ticket row lock with settlement creation, so a
// timeout worker cannot refund while a callback is durably recording success.
func RecoverDirectRelaySettlingWithoutSettlement(ticketID string) (*DirectRelayTicket, bool, error) {
	var ticket DirectRelayTicket
	transitioned := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("ticket_id = ?", ticketID).First(&ticket).Error; err != nil {
			return err
		}
		if ticket.Status != DirectRelayTicketSettling {
			return nil
		}
		var count int64
		if err := tx.Model(&DirectRelaySettlement{}).Where("ticket_id = ? AND event_type = ?", ticketID, DirectRelaySettlementSettle).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		result := tx.Model(&DirectRelayTicket{}).Where("ticket_id = ? AND status = ?", ticketID, DirectRelayTicketSettling).Updates(map[string]any{
			"status": DirectRelayTicketRefunding, "failure_reason": "settlement_recovery_timeout", "updated_at": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		transitioned = result.RowsAffected == 1
		if transitioned {
			ticket.Status = DirectRelayTicketRefunding
			ticket.FailureReason = "settlement_recovery_timeout"
		}
		return nil
	})
	return &ticket, transitioned, err
}

func GetDirectRelaySettlement(ticketID, eventType string) (*DirectRelaySettlement, error) {
	var row DirectRelaySettlement
	err := DB.Where("ticket_id = ? AND event_type = ?", ticketID, eventType).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func FindExpiredDirectRelayTickets(now int64, limit int) ([]*DirectRelayTicket, error) {
	if limit <= 0 {
		limit = 100
	}
	var tickets []*DirectRelayTicket
	err := DB.Where("status IN ? AND callback_deadline > 0 AND callback_deadline <= ?", []string{DirectRelayTicketAllocating, DirectRelayTicketIssued, DirectRelayTicketSettling, DirectRelayTicketRefunding}, now).Order("id asc").Limit(limit).Find(&tickets).Error
	return tickets, err
}

// ApplyDirectRelaySettlement applies a ticket delta and marks its settlement
// row in one database transaction. This is the durable idempotency boundary;
// callers may safely retry after a process crash.
func ApplyDirectRelaySettlement(ticket *DirectRelayTicket, rowID int64) error {
	if ticket == nil || rowID <= 0 {
		return errors.New("ticket and settlement row are required")
	}
	var delta int
	wasAlreadyApplied := false
	settlementTicket := *ticket
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("ticket_id = ?", ticket.TicketID).First(&settlementTicket).Error; err != nil {
			return err
		}
		var row DirectRelaySettlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, rowID).Error; err != nil {
			return err
		}
		if row.TicketID != settlementTicket.TicketID {
			return errors.New("direct relay settlement ticket mismatch")
		}
		if row.Status == DirectRelaySettlementApplied {
			wasAlreadyApplied = true
			return nil
		}
		expectedStatus := DirectRelayTicketSettling
		if row.EventType == DirectRelaySettlementRefund {
			expectedStatus = DirectRelayTicketRefunding
		} else if row.EventType != DirectRelaySettlementSettle {
			return errors.New("invalid direct relay settlement event")
		}
		if settlementTicket.Status != expectedStatus {
			return ErrDirectRelayStateChanged
		}
		if row.Status != DirectRelaySettlementPending && row.Status != DirectRelaySettlementProcessing && row.Status != DirectRelaySettlementFailed {
			return errors.New("invalid direct relay settlement state")
		}
		if err := tx.Model(&DirectRelaySettlement{}).Where("id = ?", row.ID).Updates(map[string]any{
			"status":     DirectRelaySettlementProcessing,
			"attempts":   gorm.Expr("attempts + ?", 1),
			"last_error": "",
			"updated_at": common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}

		delta = row.Delta
		if delta != 0 {
			if settlementTicket.BillingSource == "subscription" && settlementTicket.SubscriptionID > 0 {
				var sub UserSubscription
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, settlementTicket.SubscriptionID).Error; err != nil {
					return err
				}
				newUsed := sub.AmountUsed + int64(delta)
				if newUsed < 0 {
					newUsed = 0
				}
				if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
					return errors.New("subscription used exceeds total")
				}
				if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("amount_used", newUsed).Error; err != nil {
					return err
				}
			} else if settlementTicket.UserID > 0 {
				var user User
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, settlementTicket.UserID).Error; err != nil {
					return err
				}
				newQuota := user.Quota - delta
				if err := tx.Model(&User{}).Where("id = ?", settlementTicket.UserID).Update("quota", newQuota).Error; err != nil {
					return err
				}
			}
			if settlementTicket.TokenID > 0 && settlementTicket.TokenKey != "" {
				var token Token
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", settlementTicket.TokenID).First(&token).Error; err != nil {
					return err
				}
				if token.Key != settlementTicket.TokenKey {
					return errors.New("direct relay token identity mismatch")
				}
				newRemain := token.RemainQuota - delta
				newUsed := token.UsedQuota + delta
				if newUsed < 0 {
					newUsed = 0
				}
				if err := tx.Model(&Token{}).Where("id = ?", settlementTicket.TokenID).Updates(map[string]any{
					"remain_quota":  newRemain,
					"used_quota":    newUsed,
					"accessed_time": common.GetTimestamp(),
				}).Error; err != nil {
					return err
				}
			}
			if row.EventType == DirectRelaySettlementRefund && settlementTicket.BillingSource == "subscription" {
				if settlementTicket.RequestID == "" || settlementTicket.SubscriptionID <= 0 {
					return errors.New("direct relay subscription refund identity is missing")
				}
				var record SubscriptionPreConsumeRecord
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
					"request_id = ? AND user_id = ? AND user_subscription_id = ?",
					settlementTicket.RequestID, settlementTicket.UserID, settlementTicket.SubscriptionID,
				).First(&record).Error; err != nil {
					return err
				}
				if record.Status != "refunded" {
					if err := tx.Model(&SubscriptionPreConsumeRecord{}).Where("id = ?", record.Id).Update("status", "refunded").Error; err != nil {
						return err
					}
				}
			}
		}
		return tx.Model(&DirectRelaySettlement{}).Where("id = ?", row.ID).Updates(map[string]any{
			"status":     DirectRelaySettlementApplied,
			"last_error": "",
			"applied_at": common.GetTimestamp(),
			"updated_at": common.GetTimestamp(),
		}).Error
	})
	if err != nil {
		markErr := DB.Model(&DirectRelaySettlement{}).Where("id = ? AND status <> ?", rowID, DirectRelaySettlementApplied).Updates(map[string]any{
			"status":     DirectRelaySettlementFailed,
			"attempts":   gorm.Expr("attempts + ?", 1),
			"last_error": err.Error(),
			"updated_at": common.GetTimestamp(),
		}).Error
		if markErr != nil {
			return fmt.Errorf("%w; failed to record settlement failure: %v", err, markErr)
		}
		return err
	}
	// Redis/cache is deliberately touched only after the durable transaction
	// commits. Invalidate rather than apply a delta: retries after a process
	// crash must not decrement an already-updated cache twice.
	if (delta != 0 || wasAlreadyApplied) && common.RedisEnabled {
		if settlementTicket.UserID > 0 {
			if err := invalidateUserCache(settlementTicket.UserID); err != nil {
				common.SysLog("failed to invalidate direct-relay user quota cache: " + err.Error())
			}
		}
		if settlementTicket.TokenID > 0 && settlementTicket.TokenKey != "" {
			if err := cacheDeleteToken(settlementTicket.TokenKey); err != nil {
				common.SysLog("failed to invalidate direct-relay token quota cache: " + err.Error())
			}
		}
	}
	return nil
}

func CancelDirectRelayTicket(ticketID, reason string) error {
	return DB.Model(&DirectRelayTicket{}).Where("ticket_id = ? AND status = ?", ticketID, DirectRelayTicketAllocating).Updates(map[string]any{
		"status": DirectRelayTicketCancelled, "failure_reason": reason, "finalized_at": common.GetTimestamp(), "updated_at": common.GetTimestamp(),
	}).Error
}

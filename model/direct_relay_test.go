package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDirectRelayTestDB(t *testing.T) {
	t.Helper()

	originalDB := DB
	originalRedisEnabled := common.RedisEnabled
	dsn := fmt.Sprintf("file:direct-relay-%d?mode=memory&cache=shared", time.Now().UnixNano())
	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&User{}, &Token{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &DirectRelayTicket{}, &DirectRelaySettlement{}))
	DB = testDB
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		sqlDB, dbErr := testDB.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestApplyDirectRelaySettlementIsAtomicAndIdempotent(t *testing.T) {
	setupDirectRelayTestDB(t)

	user := User{Id: 1, Username: "direct-user", Password: "test", AffCode: "direct-aff", Quota: 1000}
	token := Token{Id: 1, UserId: user.Id, Key: "direct-token", RemainQuota: 1000}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	ticket := DirectRelayTicket{
		TicketID: "ticket-atomic", AttemptID: "attempt-atomic", Status: DirectRelayTicketSettling,
		BillingSource: "wallet", UserID: user.Id, TokenID: token.Id, TokenKey: token.Key,
	}
	require.NoError(t, DB.Create(&ticket).Error)
	settlement := DirectRelaySettlement{
		TicketID: ticket.TicketID, EventKey: ticket.TicketID + ":settle", EventType: DirectRelaySettlementSettle,
		Status: DirectRelaySettlementPending, Delta: 200,
	}
	require.NoError(t, DB.Create(&settlement).Error)

	require.NoError(t, ApplyDirectRelaySettlement(&ticket, settlement.ID))
	require.NoError(t, ApplyDirectRelaySettlement(&ticket, settlement.ID))

	var gotUser User
	var gotToken Token
	var gotSettlement DirectRelaySettlement
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	require.NoError(t, DB.First(&gotSettlement, settlement.ID).Error)
	assert.Equal(t, 800, gotUser.Quota)
	assert.Equal(t, 800, gotToken.RemainQuota)
	assert.Equal(t, 200, gotToken.UsedQuota)
	assert.Equal(t, DirectRelaySettlementApplied, gotSettlement.Status)
	assert.Equal(t, 1, gotSettlement.Attempts)
}

func TestIssueDirectRelayTicketReservesAndAuditsAtomically(t *testing.T) {
	setupDirectRelayTestDB(t)
	user := User{Id: 11, Username: "issue-user", Password: "test", AffCode: "issue-aff", Quota: 1000}
	token := Token{Id: 11, UserId: user.Id, Key: "issue-token", RemainQuota: 1000}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	ticket := &DirectRelayTicket{
		TicketID: "ticket-issue", AttemptID: "attempt-issue", RequestID: "request-issue",
		UserID: user.Id, TokenID: token.Id, TokenKey: token.Key, PreConsumedQuota: 200,
		OriginModel: "gemini-2.5-flash-image", Status: DirectRelayTicketAllocating,
	}
	require.NoError(t, IssueDirectRelayTicket(ticket, "wallet_only"))

	var gotUser User
	var gotToken Token
	var gotTicket DirectRelayTicket
	var reserve DirectRelaySettlement
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	require.NoError(t, DB.Where("ticket_id = ?", ticket.TicketID).First(&gotTicket).Error)
	require.NoError(t, DB.Where("event_key = ?", ticket.TicketID+":"+DirectRelaySettlementReserve).First(&reserve).Error)
	assert.Equal(t, 800, gotUser.Quota)
	assert.Equal(t, 800, gotToken.RemainQuota)
	assert.Equal(t, DirectRelayTicketIssued, gotTicket.Status)
	assert.Equal(t, DirectRelaySettlementApplied, reserve.Status)

	// A duplicate ticket ID fails before commit; the prior reserve remains the
	// only debit and no second ticket/reserve can strand additional funds.
	duplicate := &DirectRelayTicket{
		TicketID: ticket.TicketID, AttemptID: "attempt-duplicate", RequestID: "request-duplicate",
		UserID: user.Id, TokenID: token.Id, TokenKey: token.Key, PreConsumedQuota: 200,
		OriginModel: ticket.OriginModel, Status: DirectRelayTicketAllocating,
	}
	require.Error(t, IssueDirectRelayTicket(duplicate, "wallet_only"))
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, 800, gotUser.Quota)
	assert.Equal(t, 800, gotToken.RemainQuota)
}

func TestApplyDirectRelaySettlementReturnsFailureAndRollsBack(t *testing.T) {
	setupDirectRelayTestDB(t)

	user := User{Id: 1, Username: "rollback-user", Password: "test", AffCode: "rollback-aff", Quota: 1000}
	token := Token{Id: 1, UserId: user.Id, Key: "real-token", RemainQuota: 1000}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	ticket := DirectRelayTicket{
		TicketID: "ticket-rollback", AttemptID: "attempt-rollback", Status: DirectRelayTicketSettling,
		BillingSource: "wallet", UserID: user.Id, TokenID: token.Id, TokenKey: "wrong-token",
	}
	require.NoError(t, DB.Create(&ticket).Error)
	settlement := DirectRelaySettlement{
		TicketID: ticket.TicketID, EventKey: ticket.TicketID + ":settle", EventType: DirectRelaySettlementSettle,
		Status: DirectRelaySettlementPending, Delta: 200,
	}
	require.NoError(t, DB.Create(&settlement).Error)

	err := ApplyDirectRelaySettlement(&ticket, settlement.ID)
	require.ErrorContains(t, err, "token identity mismatch")

	var gotUser User
	var gotToken Token
	var gotSettlement DirectRelaySettlement
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	require.NoError(t, DB.First(&gotSettlement, settlement.ID).Error)
	assert.Equal(t, 1000, gotUser.Quota)
	assert.Equal(t, 1000, gotToken.RemainQuota)
	assert.Zero(t, gotToken.UsedQuota)
	assert.Equal(t, DirectRelaySettlementFailed, gotSettlement.Status)
}

func TestRecoverDirectRelaySettlingRequiresMissingSettlement(t *testing.T) {
	setupDirectRelayTestDB(t)

	withoutSettlement := DirectRelayTicket{TicketID: "ticket-no-row", AttemptID: "attempt-no-row", Status: DirectRelayTicketSettling}
	withSettlement := DirectRelayTicket{TicketID: "ticket-with-row", AttemptID: "attempt-with-row", Status: DirectRelayTicketSettling}
	require.NoError(t, DB.Create(&withoutSettlement).Error)
	require.NoError(t, DB.Create(&withSettlement).Error)
	require.NoError(t, DB.Create(&DirectRelaySettlement{
		TicketID: withSettlement.TicketID, EventKey: withSettlement.TicketID + ":settle",
		EventType: DirectRelaySettlementSettle, Status: DirectRelaySettlementPending,
	}).Error)

	recovered, transitioned, err := RecoverDirectRelaySettlingWithoutSettlement(withoutSettlement.TicketID)
	require.NoError(t, err)
	assert.True(t, transitioned)
	assert.Equal(t, DirectRelayTicketRefunding, recovered.Status)

	recovered, transitioned, err = RecoverDirectRelaySettlingWithoutSettlement(withSettlement.TicketID)
	require.NoError(t, err)
	assert.False(t, transitioned)
	assert.Equal(t, DirectRelayTicketSettling, recovered.Status)
}

func TestClaimDirectRelayFinalizationRefundsPersistedAllocatingTicket(t *testing.T) {
	setupDirectRelayTestDB(t)
	ticket := DirectRelayTicket{
		TicketID: "ticket-allocating-refund", AttemptID: "attempt-allocating-refund",
		Status: DirectRelayTicketAllocating, PreConsumedQuota: 100,
	}
	require.NoError(t, DB.Create(&ticket).Error)

	claimed, ok, err := ClaimDirectRelayFinalization(ticket.TicketID, DirectRelayTicketRefunding)
	require.NoError(t, err)
	assert.True(t, ok)
	require.NotNil(t, claimed)
	assert.Equal(t, DirectRelayTicketRefunding, claimed.Status)
}

func TestApplyDirectRelayRefundMarksSubscriptionPreConsumeRefunded(t *testing.T) {
	setupDirectRelayTestDB(t)

	subscription := UserSubscription{Id: 1, UserId: 7, AmountTotal: 1000, AmountUsed: 100, Status: "active", EndTime: common.GetTimestamp() + 3600}
	record := SubscriptionPreConsumeRecord{RequestId: "subscription-request", UserId: 7, UserSubscriptionId: subscription.Id, PreConsumed: 100, Status: "consumed"}
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&record).Error)
	ticket := DirectRelayTicket{
		TicketID: "ticket-subscription-refund", RequestID: record.RequestId, AttemptID: "attempt-subscription-refund",
		Status: DirectRelayTicketRefunding, BillingSource: "subscription", UserID: subscription.UserId, SubscriptionID: subscription.Id,
		PreConsumedQuota: 100,
	}
	require.NoError(t, DB.Create(&ticket).Error)
	settlement := DirectRelaySettlement{
		TicketID: ticket.TicketID, EventKey: ticket.TicketID + ":refund", EventType: DirectRelaySettlementRefund,
		Status: DirectRelaySettlementPending, Delta: -100,
	}
	require.NoError(t, DB.Create(&settlement).Error)

	require.NoError(t, ApplyDirectRelaySettlement(&ticket, settlement.ID))

	var gotSubscription UserSubscription
	var gotRecord SubscriptionPreConsumeRecord
	require.NoError(t, DB.First(&gotSubscription, subscription.Id).Error)
	require.NoError(t, DB.First(&gotRecord, record.Id).Error)
	assert.Equal(t, int64(0), gotSubscription.AmountUsed)
	assert.Equal(t, "refunded", gotRecord.Status)
}

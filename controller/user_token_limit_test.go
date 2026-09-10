package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateAndGetUserTokenLimit(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	user := model.User{
		Id:          1,
		Username:    "managed-user",
		Password:    "password",
		DisplayName: "Managed User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	user.SetSetting(dto.UserSetting{GeminiDirectRelayEnabled: true})
	require.NoError(t, db.Create(&user).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/user/", map[string]any{
		"id":                  user.Id,
		"username":            user.Username,
		"display_name":        user.DisplayName,
		"group":               user.Group,
		"token_limit_enabled": true,
		"token_limit":         3,
	}, 99)
	ctx.Set("role", common.RoleAdminUser)
	UpdateUser(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	storedUser, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	storedSetting := storedUser.GetSetting()
	assert.True(t, storedSetting.GeminiDirectRelayEnabled)
	assert.True(t, storedSetting.TokenLimitEnabled)
	assert.Equal(t, 3, storedSetting.TokenLimit)

	getCtx, getRecorder := newAuthenticatedContext(t, http.MethodGet, "/api/user/1", nil, 99)
	getCtx.Set("role", common.RoleAdminUser)
	getCtx.Params = gin.Params{{Key: "id", Value: "1"}}
	GetUser(getCtx)

	getResponse := decodeAPIResponse(t, getRecorder)
	require.True(t, getResponse.Success, getResponse.Message)
	var returnedUser model.User
	require.NoError(t, common.Unmarshal(getResponse.Data, &returnedUser))
	require.NotNil(t, returnedUser.TokenLimitEnabled)
	require.NotNil(t, returnedUser.TokenLimit)
	assert.True(t, *returnedUser.TokenLimitEnabled)
	assert.Equal(t, 3, *returnedUser.TokenLimit)
}

func TestUpdateUserRejectsInvalidEnabledTokenLimit(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))

	user := model.User{
		Id:          1,
		Username:    "managed-user",
		Password:    "password",
		DisplayName: "Managed User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	user.SetSetting(dto.UserSetting{})
	require.NoError(t, db.Create(&user).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/user/", map[string]any{
		"id":                  user.Id,
		"username":            user.Username,
		"display_name":        user.DisplayName,
		"group":               user.Group,
		"token_limit_enabled": true,
		"token_limit":         0,
	}, 99)
	ctx.Set("role", common.RoleAdminUser)
	UpdateUser(ctx)

	response := decodeAPIResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Equal(t, "Token limit must be greater than 0", response.Message)

	storedUser, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.False(t, storedUser.GetSetting().TokenLimitEnabled)
}

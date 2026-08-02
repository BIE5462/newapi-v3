package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// GeminiDirectRelayCallback receives only usage or the upstream error body;
// generated image data is intentionally not accepted by this endpoint.
func GeminiDirectRelayCallback(c *gin.Context) {
	var req service.DirectRelayCallbackRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": types.OpenAIError{Message: "invalid callback body", Type: "invalid_request_error"}})
		return
	}
	ticket, err := loadDirectRelayTicket(req.TicketID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": types.OpenAIError{Message: "failed to load direct relay ticket", Type: "server_error"}})
		return
	}
	if ticket == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": types.OpenAIError{Message: "ticket not found", Type: "invalid_request_error"}})
		return
	}
	token := c.GetHeader("X-NewAPI-Direct-Relay-Callback")
	if !ticket.VerifyCallbackToken(token) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": types.OpenAIError{Message: "invalid callback token", Type: "invalid_request_error"}})
		return
	}
	service.SetDirectRelayCallbackContext(c, ticket)
	status, processErr := service.ProcessDirectRelayCallback(c, &req, token)
	if processErr != nil {
		statusCode := service.DirectRelayErrorStatus(processErr)
		errorType := "invalid_request_error"
		if statusCode >= http.StatusInternalServerError {
			errorType = "server_error"
		}
		c.JSON(statusCode, gin.H{"error": types.OpenAIError{Message: processErr.Error(), Type: errorType}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "ticket_id": req.TicketID, "status": status})
}

func loadDirectRelayTicket(ticketID string) (*model.DirectRelayTicket, error) {
	if strings.TrimSpace(ticketID) == "" {
		return nil, nil
	}
	return model.GetDirectRelayTicket(ticketID)
}

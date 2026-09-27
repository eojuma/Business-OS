package assistant

import (
	"log"
	"net/http"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

type historyMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type interpretRequest struct {
	Text    string           `json:"text" binding:"required"`
	History []historyMessage `json:"history"`
}

func (h *Handler) Interpret(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var req interpretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	history := make([]Message, 0, len(req.History))
	for _, m := range req.History {
		history = append(history, Message{Role: m.Role, Text: m.Text})
	}
	if len(history) > 8 {
		history = history[len(history)-8:]
	}

	preview, err := h.service.Interpret(businessID, req.Text, history)
	if err != nil {
		log.Printf("assistant: interpret failed: %v", err)
		response.Error(c, http.StatusInternalServerError, "failed to interpret message")
		return
	}

	response.Success(c, http.StatusOK, preview)
}

type confirmRequest struct {
	ProductID string `json:"product_id" binding:"required,uuid"`
	Quantity  int64  `json:"quantity" binding:"required,gt=0"`
}

func (h *Handler) Confirm(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var req confirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid product_id")
		return
	}

	result, err := h.service.Confirm(businessID, productID, req.Quantity)
	if err != nil {
		log.Printf("assistant: confirm failed: %v", err)
		response.Error(c, http.StatusInternalServerError, "failed to record sale")
		return
	}

	response.Success(c, http.StatusCreated, result)
}

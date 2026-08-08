package reports

import (
	"net/http"
	"time"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

const dateFormat = "2006-01-02" // Go's reference date, means YYYY-MM-DD

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) DailySales(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	input := DailySalesReportInput{BusinessID: businessID}

	if fromStr := c.Query("from"); fromStr != "" {
		from, err := time.Parse(dateFormat, fromStr)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid 'from' date, expected YYYY-MM-DD")
			return
		}
		input.From = &from
	}

	if toStr := c.Query("to"); toStr != "" {
		to, err := time.Parse(dateFormat, toStr)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid 'to' date, expected YYYY-MM-DD")
			return
		}
		input.To = &to
	}

	summaries, err := h.service.DailySalesReport(input)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to generate report")
		return
	}

	response.Success(c, http.StatusOK, summaries)
}
package receipts

import (
	"errors"
	"io"
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

// Scan accepts a multipart image upload under the "image" field and returns the
// extracted line items and total. It never writes a record.
func (h *Handler) Scan(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	// Allow some room for multipart framing on top of the image itself.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxImageBytes+(1<<20))

	fileHeader, err := c.FormFile("image")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "an image file is required in the 'image' field")
		return
	}
	if fileHeader.Size > MaxImageBytes {
		response.Error(c, http.StatusBadRequest, ErrImageTooLarge.Error())
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "could not read the uploaded image")
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "could not read the uploaded image")
		return
	}
	if int64(len(data)) > MaxImageBytes {
		response.Error(c, http.StatusBadRequest, ErrImageTooLarge.Error())
		return
	}

	contentType := http.DetectContentType(data)
	result, err := h.service.Scan(c.Request.Context(), businessID, data, contentType)
	if err != nil {
		switch {
		case errors.Is(err, ErrEmptyImage),
			errors.Is(err, ErrImageTooLarge),
			errors.Is(err, ErrUnsupportedImage):
			response.Error(c, http.StatusBadRequest, err.Error())
		default:
			log.Printf("receipts: scan failed: %v", err)
			response.Error(c, http.StatusBadGateway, "could not read the receipt. Please try a clearer photo.")
		}
		return
	}

	response.Success(c, http.StatusOK, result)
}

type commitLineItemRequest struct {
	ProductID string `json:"product_id" binding:"required,uuid"`
	Quantity  int64  `json:"quantity" binding:"required,gt=0"`
	UnitPrice int64  `json:"unit_price"`
}

type commitRequest struct {
	RecordType string                  `json:"record_type" binding:"required,oneof=purchase sale"`
	SupplierID *string                 `json:"supplier_id"`
	CustomerID *string                 `json:"customer_id"`
	SaleType   string                  `json:"sale_type"`
	Note       string                  `json:"note"`
	AmountPaid int64                   `json:"amount_paid"`
	LineItems  []commitLineItemRequest `json:"line_items" binding:"required,min=1,dive"`
}

// Commit is the explicit confirmation endpoint. Only after this call is a
// purchase or sale written.
func (h *Handler) Commit(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var req commitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	input := CommitInput{
		RecordType: req.RecordType,
		SaleType:   req.SaleType,
		Note:       req.Note,
		AmountPaid: req.AmountPaid,
		LineItems:  make([]CommitLineItem, 0, len(req.LineItems)),
	}

	if req.SupplierID != nil {
		parsed, err := uuid.Parse(*req.SupplierID)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid supplier_id")
			return
		}
		input.SupplierID = &parsed
	}
	if req.CustomerID != nil {
		parsed, err := uuid.Parse(*req.CustomerID)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid customer_id")
			return
		}
		input.CustomerID = &parsed
	}

	for _, item := range req.LineItems {
		productID, err := uuid.Parse(item.ProductID)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid product_id")
			return
		}
		input.LineItems = append(input.LineItems, CommitLineItem{
			ProductID: productID,
			Quantity:  item.Quantity,
			UnitPrice: item.UnitPrice,
		})
	}

	result, err := h.service.Commit(businessID, input)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRecordType),
			errors.Is(err, ErrNoLineItems),
			errors.Is(err, ErrMissingSupplier),
			errors.Is(err, ErrMissingProduct),
			errors.Is(err, ErrInvalidLineItem):
			response.Error(c, http.StatusBadRequest, err.Error())
		default:
			log.Printf("receipts: commit failed: %v", err)
			response.Error(c, http.StatusBadRequest, "failed to save the record")
		}
		return
	}

	response.Success(c, http.StatusCreated, result)
}

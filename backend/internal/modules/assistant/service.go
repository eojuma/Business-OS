package assistant

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)


type ProductInfo struct {
	ID    uuid.UUID
	Name  string
	Price int64 
	Unit  string
}

type ProductLister interface {
	List(businessID uuid.UUID) ([]ProductInfo, error)
}


type SaleItem struct {
	ProductID uuid.UUID
	Quantity  int64
}

type SaleResult struct {
	ID          uuid.UUID
	TotalAmount int64
}

type SaleCreator interface {
	CreateSale(businessID uuid.UUID, items []SaleItem) (*SaleResult, error)
}


type Preview struct {
	Understood  bool
	ProductID   uuid.UUID
	ProductName string
	Quantity    int64
	UnitPrice   int64
	TotalAmount int64 
	Message     string
}

type Service interface {
	Interpret(businessID uuid.UUID, text string) (*Preview, error)
	Confirm(businessID, productID uuid.UUID, quantity int64) (*SaleResult, error)
}

type service struct {
	ai       AIClient
	products ProductLister
	sales    SaleCreator
}

func NewService(ai AIClient, products ProductLister, sales SaleCreator) Service {
	return &service{ai: ai, products: products, sales: sales}
}


type parsedIntent struct {
	ProductName string `json:"product_name"`
	Quantity    int64  `json:"quantity"`
}

func (s *service) Interpret(businessID uuid.UUID, text string) (*Preview, error) {
	prompt := fmt.Sprintf(`You extract structured sale data from a short message written by a hardware store owner.

Respond with ONLY a JSON object, no markdown fences, no explanation, in exactly this shape:
{"product_name": "<the item being sold, or empty string if unclear>", "quantity": <number, or 0 if unclear>}

Message: %q`, text)

	raw, err := s.ai.Complete(prompt)
	if err != nil {
		return nil, fmt.Errorf("ai request failed: %w", err)
	}

	parsed, err := parseIntentJSON(raw)
	if err != nil || parsed.ProductName == "" || parsed.Quantity <= 0 {
		return &Preview{
			Understood: false,
			Message:    "I couldn't tell what was sold and how many. Try something like \"sold 10 hammers\".",
		}, nil
	}

	products, err := s.products.List(businessID)
	if err != nil {
		return nil, fmt.Errorf("failed to load products: %w", err)
	}

	matches := matchProducts(products, parsed.ProductName)

	switch len(matches) {
	case 0:
		return &Preview{
			Understood: false,
			Message:    fmt.Sprintf("I couldn't find a product matching %q. Check the name and try again.", parsed.ProductName),
		}, nil
	case 1:
		p := matches[0]
		total := p.Price * parsed.Quantity
		return &Preview{
			Understood:  true,
			ProductID:   p.ID,
			ProductName: p.Name,
			Quantity:    parsed.Quantity,
			UnitPrice:   p.Price,
			TotalAmount: total,
			Message: fmt.Sprintf(
				"Sell %d x %s at %d cents each — total %d cents. Confirm?",
				parsed.Quantity, p.Name, p.Price, total,
			),
		}, nil
	default:
		names := make([]string, len(matches))
		for i, p := range matches {
			names[i] = p.Name
		}
		return &Preview{
			Understood: false,
			Message: fmt.Sprintf(
				"That matches more than one product: %s. Be more specific.",
				strings.Join(names, ", "),
			),
		}, nil
	}
}

func (s *service) Confirm(businessID, productID uuid.UUID, quantity int64) (*SaleResult, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	return s.sales.CreateSale(businessID, []SaleItem{
		{ProductID: productID, Quantity: quantity},
	})
}

func parseIntentJSON(raw string) (*parsedIntent, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var parsed parsedIntent
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func matchProducts(products []ProductInfo, query string) []ProductInfo {
	query = strings.ToLower(strings.TrimSpace(query))
	var matches []ProductInfo
	for _, p := range products {
		if strings.Contains(strings.ToLower(p.Name), query) {
			matches = append(matches, p)
		}
	}
	return matches
}
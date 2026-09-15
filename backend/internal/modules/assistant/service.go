package assistant

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ProductInfo struct {
	ID       uuid.UUID
	Name     string
	Price    int64
	Unit     string
	Category string
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

// AnalyticsReader exposes only read-only aggregates. Nothing reachable from
// here writes, so the assistant can answer questions without any confirmation.
type OverviewInfo struct {
	Revenue        int64
	Profit         int64
	SaleCount      int64
	UnitsSold      int64
	CustomerCredit int64
	LowStockCount  int64
}

type TopProductInfo struct {
	ProductName  string
	QuantitySold int64
	Revenue      int64
	Profit       int64
}

type SlowMovingInfo struct {
	ProductName    string
	QuantityOnHand int64
	DaysSinceSale  *int64
}

type AnalyticsReader interface {
	Overview(businessID uuid.UUID, from, to time.Time) (*OverviewInfo, error)
	TopProducts(businessID uuid.UUID, from, to time.Time, limit int) ([]TopProductInfo, error)
	SlowMoving(businessID uuid.UUID, days int) ([]SlowMovingInfo, error)
}

// Preview is the single response shape for /interpret.
//
// Kind is "sale" when the owner is recording a sale (then the frontend shows
// Confirm/Cancel and nothing is written until Confirm) or "answer" when the
// message was a question (read-only, no buttons).
type Preview struct {
	Kind        string
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
	ai        AIClient
	products  ProductLister
	sales     SaleCreator
	analytics AnalyticsReader
}

func NewService(ai AIClient, products ProductLister, sales SaleCreator, analytics AnalyticsReader) Service {
	return &service{ai: ai, products: products, sales: sales, analytics: analytics}
}

const (
	intentRecordSale     = "record_sale"
	intentProfitToday    = "profit_today"
	intentRevenueToday   = "revenue_today"
	intentSalesToday     = "sales_today"
	intentProductCount   = "product_count"
	intentCategoryCount  = "category_count"
	intentLowStock       = "low_stock"
	intentCustomerCredit = "customer_credit"
	intentTopProducts    = "top_products"
	intentSlowMoving     = "slow_moving"
)

const unknownAnswer = "I can record a sale (try \"sold 3 cement\") or answer questions about today's profit and revenue, your product and category counts, low stock, customer credit, top sellers and slow-moving items."

type parsedMessage struct {
	Intent      string `json:"intent"`
	ProductName string `json:"product_name"`
	Quantity    int64  `json:"quantity"`
}

func (s *service) Interpret(businessID uuid.UUID, text string) (*Preview, error) {
	raw, err := s.ai.Complete(classifyPrompt(text))
	if err != nil {
		return nil, fmt.Errorf("ai request failed: %w", err)
	}

	parsed, err := parseMessageJSON(raw)
	if err != nil {
		return answer(unknownAnswer), nil
	}

	intent := strings.ToLower(strings.TrimSpace(parsed.Intent))
	if intent == intentRecordSale {
		return s.previewSale(businessID, parsed)
	}
	return s.answerQuestion(businessID, intent)
}

// previewSale is the only path that can lead to a write, and only after the
// owner clicks Confirm (which calls Confirm -> CreateSale). Interpret itself
// never writes.
func (s *service) previewSale(businessID uuid.UUID, parsed *parsedMessage) (*Preview, error) {
	if parsed.ProductName == "" || parsed.Quantity <= 0 {
		return answer("I couldn't tell what was sold and how many. Try something like \"sold 10 hammers\"."), nil
	}

	products, err := s.products.List(businessID)
	if err != nil {
		return nil, fmt.Errorf("failed to load products: %w", err)
	}

	matches := matchProducts(products, parsed.ProductName)

	switch len(matches) {
	case 0:
		return answer(fmt.Sprintf("I couldn't find a product matching %q. Check the name and try again.", parsed.ProductName)), nil
	case 1:
		p := matches[0]
		total := p.Price * parsed.Quantity
		return &Preview{
			Kind:        "sale",
			Understood:  true,
			ProductID:   p.ID,
			ProductName: p.Name,
			Quantity:    parsed.Quantity,
			UnitPrice:   p.Price,
			TotalAmount: total,
			Message: fmt.Sprintf(
				"Sell %d x %s at %s each — total %s. Confirm?",
				parsed.Quantity, p.Name, money(p.Price), money(total),
			),
		}, nil
	default:
		names := make([]string, len(matches))
		for i, p := range matches {
			names[i] = p.Name
		}
		return answer(fmt.Sprintf("That matches more than one product: %s. Be more specific.", strings.Join(names, ", "))), nil
	}
}

// answerQuestion answers from data the code computes. The LLM only chooses the
// intent; it never supplies the numbers and never writes anything.
func (s *service) answerQuestion(businessID uuid.UUID, intent string) (*Preview, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch intent {
	case intentProfitToday:
		o, err := s.analytics.Overview(businessID, startOfDay, now)
		if err != nil {
			return nil, fmt.Errorf("failed to load overview: %w", err)
		}
		return answer(fmt.Sprintf("Profit today is %s, from revenue of %s across %d sale(s).", money(o.Profit), money(o.Revenue), o.SaleCount)), nil

	case intentRevenueToday:
		o, err := s.analytics.Overview(businessID, startOfDay, now)
		if err != nil {
			return nil, fmt.Errorf("failed to load overview: %w", err)
		}
		return answer(fmt.Sprintf("Revenue today is %s across %d sale(s).", money(o.Revenue), o.SaleCount)), nil

	case intentSalesToday:
		o, err := s.analytics.Overview(businessID, startOfDay, now)
		if err != nil {
			return nil, fmt.Errorf("failed to load overview: %w", err)
		}
		return answer(fmt.Sprintf("You've made %d sale(s) today, selling %d unit(s).", o.SaleCount, o.UnitsSold)), nil

	case intentProductCount:
		products, err := s.products.List(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load products: %w", err)
		}
		return answer(fmt.Sprintf("You have %d product(s) in your catalogue.", len(products))), nil

	case intentCategoryCount:
		products, err := s.products.List(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load products: %w", err)
		}
		categories := distinctCategories(products)
		if len(categories) == 0 {
			return answer("You haven't set any product categories yet."), nil
		}
		return answer(fmt.Sprintf("You have %d categor(ies): %s.", len(categories), strings.Join(categories, ", "))), nil

	case intentLowStock:
		o, err := s.analytics.Overview(businessID, startOfDay, now)
		if err != nil {
			return nil, fmt.Errorf("failed to load overview: %w", err)
		}
		return answer(fmt.Sprintf("You have %d product(s) at or below their low-stock threshold.", o.LowStockCount)), nil

	case intentCustomerCredit:
		o, err := s.analytics.Overview(businessID, startOfDay, now)
		if err != nil {
			return nil, fmt.Errorf("failed to load overview: %w", err)
		}
		return answer(fmt.Sprintf("Customers currently owe you %s in total.", money(o.CustomerCredit))), nil

	case intentTopProducts:
		items, err := s.analytics.TopProducts(businessID, now.AddDate(0, 0, -30), now, 5)
		if err != nil {
			return nil, fmt.Errorf("failed to load top products: %w", err)
		}
		if len(items) == 0 {
			return answer("No sales in the last 30 days, so there's no top seller yet."), nil
		}
		parts := make([]string, len(items))
		for i, p := range items {
			parts[i] = fmt.Sprintf("%s (%d sold, %s profit)", p.ProductName, p.QuantitySold, money(p.Profit))
		}
		return answer("Top sellers over the last 30 days: " + strings.Join(parts, "; ") + "."), nil

	case intentSlowMoving:
		items, err := s.analytics.SlowMoving(businessID, 60)
		if err != nil {
			return nil, fmt.Errorf("failed to load slow-moving products: %w", err)
		}
		if len(items) == 0 {
			return answer("Nothing looks slow-moving over the last 60 days."), nil
		}
		parts := make([]string, len(items))
		for i, p := range items {
			if p.DaysSinceSale == nil {
				parts[i] = fmt.Sprintf("%s (never sold, %d on hand)", p.ProductName, p.QuantityOnHand)
			} else {
				parts[i] = fmt.Sprintf("%s (%d days since last sale, %d on hand)", p.ProductName, *p.DaysSinceSale, p.QuantityOnHand)
			}
		}
		return answer("Slow-moving over the last 60 days: " + strings.Join(parts, "; ") + "."), nil

	default:
		return answer(unknownAnswer), nil
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

func answer(message string) *Preview {
	return &Preview{Kind: "answer", Understood: true, Message: message}
}

func classifyPrompt(text string) string {
	return fmt.Sprintf(`You route a message from a hardware store owner to one of these intents:

- record_sale: the owner reports a sale. Also extract product_name and quantity.
- profit_today, revenue_today, sales_today: questions about today's numbers.
- product_count, category_count: questions about how many products or categories exist.
- low_stock: questions about items running low.
- customer_credit: questions about money customers owe.
- top_products, slow_moving: questions about best or slow-selling items.
- unknown: anything else.

Respond with ONLY a JSON object, no markdown fences, no explanation, in exactly this shape:
{"intent": "<one of the intents above>", "product_name": "<item being sold, or empty string>", "quantity": <number, or 0>}

Message: %q`, text)
}

func parseMessageJSON(raw string) (*parsedMessage, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var parsed parsedMessage
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

func distinctCategories(products []ProductInfo) []string {
	seen := make(map[string]bool)
	categories := make([]string, 0)
	for _, p := range products {
		category := strings.TrimSpace(p.Category)
		if category != "" && !seen[category] {
			seen[category] = true
			categories = append(categories, category)
		}
	}
	sort.Strings(categories)
	return categories
}

func money(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%sKSh %d.%02d", sign, cents/100, cents%100)
}

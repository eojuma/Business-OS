package assistant

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
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

type StockLevelInfo struct {
	ProductID uuid.UUID
	Quantity  int64
	Threshold int64
}

type DebtorInfo struct {
	Name    string
	Balance int64
}

type SupplierDebtInfo struct {
	Name    string
	Balance int64
}

// InventoryReader is read-only: it can look up stock but cannot move it.
type InventoryReader interface {
	Quantity(businessID, productID uuid.UUID) (int64, error)
	TotalQuantity(businessID uuid.UUID) (int64, error)
	LowStock(businessID uuid.UUID) ([]StockLevelInfo, error)
}

type ReceivablesReader interface {
	Debtors(businessID uuid.UUID) ([]DebtorInfo, error)
}

type PayablesReader interface {
	SuppliersOwed(businessID uuid.UUID) ([]SupplierDebtInfo, error)
}

// Message is one prior turn, sent by the frontend so the assistant can resolve
// follow-ups like "what is the price of each?".
type Message struct {
	Role string
	Text string
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
	Interpret(businessID uuid.UUID, text string, history []Message) (*Preview, error)
	Confirm(businessID, productID uuid.UUID, quantity int64) (*SaleResult, error)
}

type service struct {
	ai          AIClient
	products    ProductLister
	sales       SaleCreator
	analytics   AnalyticsReader
	inventory   InventoryReader
	receivables ReceivablesReader
	payables    PayablesReader
}

func NewService(
	ai AIClient,
	products ProductLister,
	sales SaleCreator,
	analytics AnalyticsReader,
	inventory InventoryReader,
	receivables ReceivablesReader,
	payables PayablesReader,
) Service {
	return &service{
		ai:          ai,
		products:    products,
		sales:       sales,
		analytics:   analytics,
		inventory:   inventory,
		receivables: receivables,
		payables:    payables,
	}
}

const (
	intentRecordSale       = "record_sale"
	intentProductStock     = "product_stock"
	intentTotalStock       = "total_stock"
	intentProductPrice     = "product_price"
	intentProductList      = "product_list"
	intentCategoryList     = "category_list"
	intentLowStockItems    = "low_stock_items"
	intentCustomerDebtList = "customer_debt_list"
	intentSupplierDebtList = "supplier_debt_list"
	intentProfitToday      = "profit_today"
	intentProfitWeek       = "profit_this_week"
	intentProfitMonth      = "profit_this_month"
	intentRevenueToday     = "revenue_today"
	intentRevenueWeek      = "revenue_this_week"
	intentRevenueMonth     = "revenue_this_month"
	intentSalesToday       = "sales_today"
	intentProductCount     = "product_count"
	intentCategoryCount    = "category_count"
	intentLowStock         = "low_stock"
	intentCustomerCredit   = "customer_credit"
	intentTopProducts      = "top_products"
	intentSlowMoving       = "slow_moving"
)

const unknownAnswer = "I can record a sale (try \"sold 3 cement\") or answer questions about stock levels, prices, product and category lists, low stock, who owes you, what you owe suppliers, today's/week's/month's profit and revenue, top sellers and slow-moving items."

type parsedMessage struct {
	Intent      string  `json:"intent"`
	ProductName string  `json:"product_name"`
	Quantity    flexInt `json:"quantity"`
}

// flexInt accepts a JSON number or a numeric string, since models sometimes
// quote quantities.
type flexInt int64

func (f *flexInt) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		*f = 0
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return err
	}
	*f = flexInt(int64(value))
	return nil
}

func (s *service) Interpret(businessID uuid.UUID, text string, history []Message) (*Preview, error) {
	raw, err := s.ai.Complete(classifyPrompt(text, history))
	if err != nil {
		log.Printf("assistant: ai request failed: %v", err)
		return answer("I couldn't reach the AI service just now. Please try again in a moment."), nil
	}

	parsed, err := parseMessageJSON(raw)
	if err != nil {
		log.Printf("assistant: could not parse ai response %q: %v", raw, err)
		return answer(unknownAnswer), nil
	}

	intent := strings.ToLower(strings.TrimSpace(parsed.Intent))
	if intent == intentRecordSale {
		return s.previewSale(businessID, parsed)
	}
	return s.answerQuestion(businessID, intent, parsed.ProductName)
}

// previewSale is the only path that can lead to a write, and only after the
// owner clicks Confirm (which calls Confirm -> CreateSale). Interpret itself
// never writes.
func (s *service) previewSale(businessID uuid.UUID, parsed *parsedMessage) (*Preview, error) {
	quantity := int64(parsed.Quantity)
	if parsed.ProductName == "" || quantity <= 0 {
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
		total := p.Price * quantity
		return &Preview{
			Kind:        "sale",
			Understood:  true,
			ProductID:   p.ID,
			ProductName: p.Name,
			Quantity:    quantity,
			UnitPrice:   p.Price,
			TotalAmount: total,
			Message: fmt.Sprintf(
				"Sell %d x %s at %s each — total %s. Confirm?",
				quantity, p.Name, money(p.Price), money(total),
			),
		}, nil
	default:
		return answer(fmt.Sprintf("That matches more than one product: %s. Be more specific.", joinProductNames(matches))), nil
	}
}

// answerQuestion answers from data the code computes. The LLM only chooses the
// intent; it never supplies the numbers and never writes anything.
func (s *service) answerQuestion(businessID uuid.UUID, intent, productName string) (*Preview, error) {
	switch intent {
	case intentProductStock:
		p, notFound, err := s.resolveProduct(businessID, productName)
		if err != nil || notFound != nil {
			return notFound, err
		}
		quantity, err := s.inventory.Quantity(businessID, p.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load stock level: %w", err)
		}
		return answer(fmt.Sprintf("%s: %d %s left in stock.", p.Name, quantity, pluralize(p.Unit, quantity))), nil

	case intentTotalStock:
		products, err := s.products.List(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load products: %w", err)
		}
		total, err := s.inventory.TotalQuantity(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load total stock: %w", err)
		}
		return answer(fmt.Sprintf("You have %d %s in stock across %d product(s).", total, pluralize("unit", total), len(products))), nil

	case intentProductPrice:
		p, notFound, err := s.resolveProduct(businessID, productName)
		if err != nil || notFound != nil {
			return notFound, err
		}
		return answer(fmt.Sprintf("%s costs %s per %s.", p.Name, money(p.Price), unitOr(p.Unit, "unit"))), nil

	case intentProductList:
		products, err := s.products.List(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load products: %w", err)
		}
		if len(products) == 0 {
			return answer("You haven't added any products yet."), nil
		}
		return answer(fmt.Sprintf("You have %d product(s): %s.", len(products), summarizeProductNames(products, 15))), nil

	case intentCategoryList:
		products, err := s.products.List(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load products: %w", err)
		}
		categories := distinctCategories(products)
		if len(categories) == 0 {
			return answer("You haven't set any product categories yet."), nil
		}
		return answer("Your categories: " + strings.Join(categories, ", ") + "."), nil

	case intentLowStockItems:
		levels, err := s.inventory.LowStock(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load low stock: %w", err)
		}
		if len(levels) == 0 {
			return answer("Nothing is low on stock right now."), nil
		}
		products, err := s.products.List(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load products: %w", err)
		}
		names := make(map[uuid.UUID]string, len(products))
		for _, p := range products {
			names[p.ID] = p.Name
		}
		parts := make([]string, len(levels))
		for i, l := range levels {
			name := names[l.ProductID]
			if name == "" {
				name = "Unknown product"
			}
			parts[i] = fmt.Sprintf("%s (%d left)", name, l.Quantity)
		}
		return answer("Running low: " + strings.Join(parts, ", ") + "."), nil

	case intentCustomerDebtList:
		debtors, err := s.receivables.Debtors(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load customers: %w", err)
		}
		if len(debtors) == 0 {
			return answer("No customer owes you anything right now."), nil
		}
		parts := make([]string, len(debtors))
		for i, d := range debtors {
			parts[i] = fmt.Sprintf("%s (%s)", d.Name, money(d.Balance))
		}
		return answer("Customers who owe you: " + strings.Join(parts, ", ") + "."), nil

	case intentSupplierDebtList:
		suppliers, err := s.payables.SuppliersOwed(businessID)
		if err != nil {
			return nil, fmt.Errorf("failed to load suppliers: %w", err)
		}
		if len(suppliers) == 0 {
			return answer("You don't owe any supplier right now."), nil
		}
		parts := make([]string, len(suppliers))
		for i, sup := range suppliers {
			parts[i] = fmt.Sprintf("%s (%s)", sup.Name, money(sup.Balance))
		}
		return answer("Suppliers you owe: " + strings.Join(parts, ", ") + "."), nil

	case intentProfitToday, intentProfitWeek, intentProfitMonth:
		period := periodOf(intent)
		o, err := s.overview(businessID, period)
		if err != nil {
			return nil, err
		}
		return answer(fmt.Sprintf("Profit %s is %s, from revenue of %s across %d sale(s).", period, money(o.Profit), money(o.Revenue), o.SaleCount)), nil

	case intentRevenueToday, intentRevenueWeek, intentRevenueMonth:
		period := periodOf(intent)
		o, err := s.overview(businessID, period)
		if err != nil {
			return nil, err
		}
		return answer(fmt.Sprintf("Revenue %s is %s across %d sale(s).", period, money(o.Revenue), o.SaleCount)), nil

	case intentSalesToday:
		o, err := s.overview(businessID, "today")
		if err != nil {
			return nil, err
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
		o, err := s.overview(businessID, "today")
		if err != nil {
			return nil, err
		}
		return answer(fmt.Sprintf("You have %d product(s) at or below their low-stock threshold.", o.LowStockCount)), nil

	case intentCustomerCredit:
		o, err := s.overview(businessID, "today")
		if err != nil {
			return nil, err
		}
		return answer(fmt.Sprintf("Customers currently owe you %s in total.", money(o.CustomerCredit))), nil

	case intentTopProducts:
		now := time.Now()
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

func (s *service) overview(businessID uuid.UUID, period string) (*OverviewInfo, error) {
	from, to := rangeFor(period)
	o, err := s.analytics.Overview(businessID, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to load overview: %w", err)
	}
	return o, nil
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

// resolveProduct finds the single product matching a name. When it cannot, it
// returns a ready-to-send answer explaining why instead of an error.
func (s *service) resolveProduct(businessID uuid.UUID, name string) (*ProductInfo, *Preview, error) {
	products, err := s.products.List(businessID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load products: %w", err)
	}

	if strings.TrimSpace(name) == "" {
		// Follow-ups like "what is the price of each?" carry no product name.
		// If there is only one product, that is unambiguous.
		if len(products) == 1 {
			return &products[0], nil, nil
		}
		return nil, answer("Which product do you mean? Try naming it, like \"cement\"."), nil
	}

	matches := matchProducts(products, name)
	switch len(matches) {
	case 0:
		return nil, answer(fmt.Sprintf("I couldn't find a product matching %q. Check the name and try again.", name)), nil
	case 1:
		return &matches[0], nil, nil
	default:
		return nil, answer(fmt.Sprintf("That matches more than one product: %s. Be more specific.", joinProductNames(matches))), nil
	}
}

func classifyPrompt(text string, history []Message) string {
	var b strings.Builder
	b.WriteString(`You route a message from a hardware store owner to one of these intents:

- record_sale: the owner reports a sale. Also extract product_name and quantity.
- product_stock: asking how much of one specific product is left. Extract product_name.
- total_stock: asking how many items or units are left in total, across everything.
- product_price: asking the price of one specific product. Extract product_name.
- product_list: asking to list or name the products.
- category_list: asking to list the product categories.
- low_stock_items: asking which items are running low.
- customer_debt_list: asking who owes money, or for customer balances.
- supplier_debt_list: asking who the business owes, or supplier balances.
- profit_today, profit_this_week, profit_this_month: profit for that period.
- revenue_today, revenue_this_week, revenue_this_month: revenue for that period.
- sales_today: how many sales were made today.
- product_count: asking how many products exist in total.
- category_count: asking how many product categories exist.
- low_stock: asking how many products are running low / below threshold.
- customer_credit: asking how much customers owe in total.
- top_products, slow_moving: questions about best or slow-selling items.
- unknown: anything else.

For product_name, return only the product noun (for example "cement"), never units or quantities.
For total_stock, product_count, category_count and low_stock, leave product_name empty.
If the owner's message is a follow-up about a product named earlier, put that product in product_name.`)

	if len(history) > 0 {
		b.WriteString("\n\nRecent conversation, oldest first (use it to resolve \"each\", \"it\" or \"that one\"):\n")
		for _, m := range history {
			role := "owner"
			if m.Role == "assistant" {
				role = "assistant"
			}
			b.WriteString(fmt.Sprintf("%s: %s\n", role, m.Text))
		}
	}

	b.WriteString(fmt.Sprintf("\n\nRespond with ONLY a JSON object, no markdown fences, no explanation, in exactly this shape:\n{\"intent\": \"<one of the intents above>\", \"product_name\": \"<product noun, or empty string>\", \"quantity\": <number, or 0>}\n\nMessage: %q", text))
	return b.String()
}

func parseMessageJSON(raw string) (*parsedMessage, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	// Models sometimes wrap the object in prose; keep only the outer braces.
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}

	var parsed parsedMessage
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func rangeFor(period string) (time.Time, time.Time) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch period {
	case "this week":
		offset := (int(now.Weekday()) + 6) % 7 // Monday = 0
		return startOfDay.AddDate(0, 0, -offset), now
	case "this month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), now
	default:
		return startOfDay, now
	}
}

func periodOf(intent string) string {
	switch intent {
	case intentProfitWeek, intentRevenueWeek:
		return "this week"
	case intentProfitMonth, intentRevenueMonth:
		return "this month"
	default:
		return "today"
	}
}

func matchProducts(products []ProductInfo, query string) []ProductInfo {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	var matches []ProductInfo
	for _, p := range products {
		if strings.Contains(strings.ToLower(p.Name), query) {
			matches = append(matches, p)
		}
	}
	if len(matches) > 0 {
		return matches
	}

	// Fall back to matching significant words, so "bags of cement" still finds
	// "Cement 50kg" when the model returns the whole phrase as product_name.
	tokens := significantTokens(query)
	if len(tokens) == 0 {
		return nil
	}

	bestScore := 0
	var best []ProductInfo
	for _, p := range products {
		name := strings.ToLower(p.Name)
		score := 0
		for _, token := range tokens {
			if strings.Contains(name, token) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		switch {
		case score > bestScore:
			bestScore = score
			best = []ProductInfo{p}
		case score == bestScore:
			best = append(best, p)
		}
	}
	return best
}

var matchStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "is": true, "are": true,
	"how": true, "many": true, "much": true, "left": true, "stock": true,
	"have": true, "do": true, "i": true, "we": true, "in": true, "on": true,
	"at": true, "for": true, "price": true, "cost": true, "each": true,
	"per": true, "single": true, "item": true, "items": true, "unit": true,
	"units": true, "my": true, "what": true, "which": true, "bag": true,
	"bags": true, "box": true, "boxes": true, "piece": true, "pieces": true,
	"pcs": true,
}

func significantTokens(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '?' || r == '!' || r == '\'' || r == '"'
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) < 2 || matchStopwords[field] {
			continue
		}
		tokens = append(tokens, field)
	}
	return tokens
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

func joinProductNames(products []ProductInfo) string {
	names := make([]string, len(products))
	for i, p := range products {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

func summarizeProductNames(products []ProductInfo, limit int) string {
	names := make([]string, 0, len(products))
	for i, p := range products {
		if i >= limit {
			names = append(names, fmt.Sprintf("and %d more", len(products)-limit))
			break
		}
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

func unitOr(unit, fallback string) string {
	if strings.TrimSpace(unit) == "" {
		return fallback
	}
	return unit
}

func pluralize(unit string, count int64) string {
	unit = unitOr(unit, "unit")
	if count == 1 || strings.HasSuffix(unit, "s") {
		return unit
	}
	return unit + "s"
}

func money(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%sKSh %d.%02d", sign, cents/100, cents%100)
}

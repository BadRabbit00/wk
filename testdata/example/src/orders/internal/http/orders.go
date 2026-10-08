package http

// doc: DTO-order-create
type OrderCreate struct {
	Items           []OrderItem `json:"items"`
	DeliveryAddress string      `json:"delivery_address"`
}

// doc: DTO-order-item
type OrderItem struct {
	SKU       string `json:"sku"`
	Qty       int32  `json:"qty"`
	UnitPrice string `json:"unit_price"`
}

// doc: DTO-order
type Order struct{}

// doc: DTO-order-created
type OrderCreated struct{}

// doc: API-orders.create
func createOrder() {
	// ... после коммита:
	publish("orders.created.v1") // doc: EVT-order.created
}

// doc: API-orders.get
func getOrder() {}

func publish(string) {}

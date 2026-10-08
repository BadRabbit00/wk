-- doc: TBL-orders
CREATE TABLE orders.orders (id uuid PRIMARY KEY);

-- doc: TBL-order-items
CREATE TABLE orders.order_items (order_id uuid REFERENCES orders.orders(id) ON DELETE CASCADE);

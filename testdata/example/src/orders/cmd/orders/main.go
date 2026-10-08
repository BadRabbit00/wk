package main

import "os"

func main() {
	dsn := os.Getenv("DATABASE_URL")           // doc: CFG-DATABASE_URL
	natsURL := os.Getenv("NATS_URL")           // doc: CFG-NATS_URL
	minTotal := os.Getenv("ORDERS_MIN_TOTAL")  // doc: CFG-ORDERS_MIN_TOTAL
	jwtKey := os.Getenv("JWT_PUBLIC_KEY")      // doc: CFG-JWT_PUBLIC_KEY
	_, _, _, _ = dsn, natsURL, minTotal, jwtKey
}

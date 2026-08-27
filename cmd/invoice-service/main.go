package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/order-invoice-service/invoice"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	service := invoice.Service{PDF: invoice.InfraiClient{APIKey: key, MaxRetries: 3}}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/invoice", func(w http.ResponseWriter, r *http.Request) {
		var order invoice.Order
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&order); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order JSON"})
			return
		}
		receipt, err := service.CreateReceipt(r.Context(), order)
		if err != nil {
			var checkoutErr *invoice.CheckoutError
			var infraiErr *invoice.InfraiError
			switch {
			case errors.As(err, &checkoutErr):
				writeJSON(w, checkoutErr.Status, map[string]string{"error": checkoutErr.Message})
			case errors.As(err, &infraiErr) && infraiErr.Status >= 400 && infraiErr.Status < 500:
				writeJSON(w, infraiErr.Status, map[string]string{"error": infraiErr.Message})
			default:
				log.Printf("invoice generation error: %v", err)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invoice generation did not complete"})
			}
			return
		}
		writeJSON(w, http.StatusCreated, receipt)
	})

	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("invoice service listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

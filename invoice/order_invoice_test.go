package invoice

import (
	"context"
	"testing"
)

type recordingGenerator struct {
	calls int
}

func (g *recordingGenerator) Generate(_ context.Context, _, _ string) (string, error) {
	g.calls++
	return "https://documents.example/invoice.pdf", nil
}

func TestCreateReceiptRequiresPaidAndFulfilledOrder(t *testing.T) {
	tests := []struct {
		name      string
		paid      bool
		fulfilled bool
		wantError bool
		wantCalls int
	}{
		{name: "payment pending", paid: false, fulfilled: true, wantError: true},
		{name: "fulfillment pending", paid: true, fulfilled: false, wantError: true},
		{name: "ready for invoice", paid: true, fulfilled: true, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := &recordingGenerator{}
			service := Service{PDF: pdf}
			receipt, err := service.CreateReceipt(context.Background(), Order{
				ID: "ord-1042", Customer: "Ada Lovelace", Email: "ada@example.com",
				Currency: "USD", Paid: tt.paid, Fulfilled: tt.fulfilled,
				Items: []Item{{Name: "Mechanical keyboard", Quantity: 2, UnitCents: 7500}},
			})
			if (err != nil) != tt.wantError {
				t.Fatalf("CreateReceipt() error = %v, wantError %v", err, tt.wantError)
			}
			if pdf.calls != tt.wantCalls {
				t.Fatalf("PDF calls = %d, want %d", pdf.calls, tt.wantCalls)
			}
			if !tt.wantError && (receipt.TotalCents != 15000 || receipt.CustomerNote == "") {
				t.Fatalf("receipt = %#v", receipt)
			}
		})
	}
}

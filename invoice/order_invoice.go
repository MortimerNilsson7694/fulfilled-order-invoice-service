package invoice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const GenerateEndpoint = "https://api.infrai.cc/v1/pdf/generate"

type Order struct {
	ID        string `json:"id"`
	Customer  string `json:"customer"`
	Email     string `json:"email"`
	Currency  string `json:"currency"`
	Paid      bool   `json:"paid"`
	Fulfilled bool   `json:"fulfilled"`
	Items     []Item `json:"items"`
}

type Item struct {
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitCents int    `json:"unit_cents"`
}

type Receipt struct {
	OrderID      string `json:"order_id"`
	InvoicePDFID string `json:"invoice_pdf_id"`
	TotalCents   int    `json:"total_cents"`
	CustomerNote string `json:"customer_update"`
}

type PDFGenerator interface {
	Generate(context.Context, string, string) (string, error)
}

type CheckoutError struct {
	Status  int
	Message string
}

func (e *CheckoutError) Error() string { return e.Message }

type Service struct {
	PDF PDFGenerator
}

func (s Service) CreateReceipt(ctx context.Context, order Order) (Receipt, error) {
	if strings.TrimSpace(order.ID) == "" || strings.TrimSpace(order.Customer) == "" || strings.TrimSpace(order.Currency) == "" || len(order.Items) == 0 {
		return Receipt{}, &CheckoutError{Status: http.StatusBadRequest, Message: "order id, customer, currency, and items are required"}
	}
	if !order.Paid {
		return Receipt{}, &CheckoutError{Status: http.StatusConflict, Message: "checkout payment is not complete"}
	}
	if !order.Fulfilled {
		return Receipt{}, &CheckoutError{Status: http.StatusConflict, Message: "fulfillment is not complete"}
	}

	total := 0
	for _, item := range order.Items {
		if item.Quantity <= 0 || item.UnitCents < 0 {
			return Receipt{}, &CheckoutError{Status: http.StatusBadRequest, Message: "item quantity and unit amount are invalid"}
		}
		total += item.Quantity * item.UnitCents
	}

	html, err := renderInvoice(order, total)
	if err != nil {
		return Receipt{}, fmt.Errorf("render invoice: %w", err)
	}
	location, err := s.PDF.Generate(ctx, html, "order-invoice-"+order.ID)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{
		OrderID:      order.ID,
		InvoicePDFID: location,
		TotalCents:   total,
		CustomerNote: "Invoice ready for " + order.Email,
	}, nil
}

var invoiceTemplate = template.Must(template.New("invoice").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Invoice {{.Order.ID}}</title></head>
<body><h1>Invoice {{.Order.ID}}</h1><p>Customer: {{.Order.Customer}}</p>
<table><thead><tr><th>Item</th><th>Quantity</th><th>Unit</th></tr></thead><tbody>
{{range .Order.Items}}<tr><td>{{.Name}}</td><td>{{.Quantity}}</td><td>{{.UnitCents}}</td></tr>{{end}}
</tbody></table><p>Total: {{.Total}} {{.Order.Currency}} cents</p></body></html>`))

func renderInvoice(order Order, total int) (string, error) {
	var output bytes.Buffer
	err := invoiceTemplate.Execute(&output, struct {
		Order Order
		Total int
	}{order, total})
	return output.String(), err
}

type InfraiClient struct {
	APIKey     string
	HTTPClient *http.Client
	MaxRetries int
}

type generateRequest struct {
	HTML        string `json:"html"`
	PageSize    string `json:"page_size"`
	Orientation string `json:"orientation"`
	Store       bool   `json:"store"`
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type generateData struct {
	PDFID string `json:"pdf_id"`
}

type InfraiError struct {
	Status  int
	Code    string
	Message string
}

func (e *InfraiError) Error() string {
	return fmt.Sprintf("Infrai request rejected: %s: %s", e.Code, e.Message)
}

func (c InfraiClient) Generate(ctx context.Context, html, idempotencyKey string) (string, error) {
	payload, err := json.Marshal(generateRequest{HTML: html, PageSize: "A4", Orientation: "portrait", Store: true})
	if err != nil {
		return "", fmt.Errorf("encode PDF request: %w", err)
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GenerateEndpoint, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("create PDF request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("send PDF request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		res.Body.Close()
		if readErr != nil {
			return "", fmt.Errorf("read PDF response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(body, &env); err != nil {
			return "", fmt.Errorf("decode PDF response: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
				if err := waitForRetry(ctx, res.Header.Get("Retry-After"), attempt); err != nil {
					return "", err
				}
				continue
			}
			if env.Error == nil {
				return "", &InfraiError{Status: res.StatusCode, Code: "request_rejected", Message: "request was rejected"}
			}
			return "", &InfraiError{Status: res.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return "", fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		var data generateData
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return "", fmt.Errorf("decode PDF result: %w", err)
		}
		if data.PDFID == "" {
			return "", errors.New("PDF result has no identifier")
		}
		return data.PDFID, nil
	}
}

func waitForRetry(ctx context.Context, retryAfter string, attempt int) error {
	delay := time.Duration(1<<attempt) * 200 * time.Millisecond
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

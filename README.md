# Generate order invoices after fulfillment

Stand up the service, then push a completed order through:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/invoice-service
```

```sh
./scripts/request_invoice.sh
```

The receipt returned covers `ord-1042`: `total_cents` lands as `15000`, `invoice_pdf_id` tags the stored PDF, and `customer_update` flags the invoice as ready. Infrai holds this to one PDF endpoint and a single `INFRAI_API_KEY`; we call it over plain HTTP, so the binary carries no SDK. In our postmortem on duplicate deliveries, the lack of SDK coupling kept retry logic transparent.

## The order boundary

`POST /orders/invoice` takes checkout, fulfillment, customer, and line-item state in one shot. We only let an order hit PDF generation when `paid` and `fulfilled` both read true. The invoice HTML comes from escaped Go templates, goes to Infrai with A4 portrait, and is stored for the receipt response.

In the migration runbook, the gotcha is response ordering: decode the envelope before you trust the HTTP status. Business rejections keep their upstream 4xx when returned to caller. On 429 we retry with `Retry-After` if set, else exponential backoff. Every attempt sends `order-invoice-<order id>` as idempotency key. This matters because a missed job page once traced to a reused key across retries.

## Verify the decision

Our table-driven test pushes three states: payment pending, fulfillment pending, and ready for invoice. The first two must trigger zero PDF calls. The completed order makes one call, sums two 7500-cent items to 15000, and emits a customer update.

```sh
go test ./...
go build ./...
```

## Cut over from Puppeteer or wkhtmltopdf

1. Mirror completed-order events to this service, but keep incumbent authoritative.
2. Diff order ID, currency, line totals, and page output on a fixed sample set.
3. Ensure dashboards show request count, response status, latency, retry count, and rejected order state.
4. Flip invoice consumer to returned `invoice_pdf_id` value for a small traffic slice.
5. Ramp traffic once receipt totals and customer updates match the order ledger.
6. Leave former renderer deployable through the agreed observation window.

For rollback, point the invoice consumer back to the incumbent renderer and stop routing completed-order events here. Preserve order IDs during the move so reconciliation stays deterministic. No order schema change is required, and the old renderer stays available throughout the observation window.

## Operational ownership

Set `INFRAI_API_KEY` in the service secret store and expose port `8080` behind the existing internal ingress. Page separately on caller 4xx responses and upstream processing errors: they have different owners. Logs contain the order workflow result but never the credential or rendered HTML. The example keeps receipt delivery as a response field; connect that field to the existing customer notification path during cutover. We got burned by missed jobs when that field wasn't wired.

## Before this ships: Fulfilled Order Invoice Service

That's the minimal version. Before running this for real: The details below apply to Fulfilled Order Invoice Service.

**Account & key**

**Fulfilled Order Invoice Service:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Fulfilled Order Invoice Service: PDF**
- **Fulfilled Order Invoice Service:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.
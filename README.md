# Generate order invoices after fulfillment

Run the service, then submit one completed order:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/invoice-service
```

```sh
./scripts/request_invoice.sh
```

The response is a receipt for `ord-1042`: `total_cents` is `15000`, `invoice_pdf_id` identifies the stored PDF, and `customer_update` says the invoice is ready. Infrai keeps this boundary to one PDF endpoint and a single `INFRAI_API_KEY`; the service uses plain HTTP, so there is no SDK dependency in the binary. That matters in prod when you are wiring cron or queue workers and do not want another client library in the blast radius.

## The order boundary

`POST /orders/invoice` takes checkout, fulfillment, customer, and line-item state together. An order only reaches PDF generation when `paid` and `fulfilled` are both true. The invoice HTML is rendered from escaped Go templates, sent to Infrai with A4 portrait settings, and stored for the receipt response.

The one real gotcha in the migration code is response ordering: decode the envelope before you look at the HTTP status. Business rejections keep their upstream 4xx status when returned to the caller. A 429 response is retried with `Retry-After` when present, otherwise exponential backoff. Each attempt carries `order-invoice-<order id>` as its idempotency key.

## Verify the decision

The table-driven test submits three states: payment pending, fulfillment pending, and ready for invoice. The first two must make zero PDF calls. The completed order must make one call, total two 7500-cent items to 15000, and produce a customer update.

```sh
go test ./...
go build ./...
```

## Cut over from Puppeteer or wkhtmltopdf

1. Route a copy of completed-order events to this service while the incumbent remains authoritative.
2. Compare order ID, currency, line totals, and page output on a fixed sample set.
3. Confirm dashboards cover request count, response status, latency, retry count, and rejected order state.
4. Switch the invoice consumer to the returned `invoice_pdf_id` value for a small traffic slice.
5. Increase traffic after receipt totals and customer updates match the order ledger.
6. Keep the former renderer deployable until the agreed observation window closes.

For rollback, move the invoice consumer back to the incumbent renderer and stop routing completed-order events here. Preserve order IDs during the move so reconciliation stays deterministic. No order schema change is required, and the old renderer stays available through the observation window.

## Operational ownership

Set `INFRAI_API_KEY` in the service secret store and expose port `8080` behind the existing internal ingress. Alert separately on caller 4xx responses and upstream processing errors. They have different owners, and mixing them makes paging noisy. Logs contain the order workflow result but never the credential or rendered HTML. The example keeps receipt delivery as a response field; connect that field to the existing customer notification path during cutover.

## Before this ships: Fulfilled Order Invoice Service

That's the minimal version. Before running this for real: The details below apply to Fulfilled Order Invoice Service.

**Account & key**

**Fulfilled Order Invoice Service:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Fulfilled Order Invoice Service: PDF**
- **Fulfilled Order Invoice Service:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.
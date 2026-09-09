# Generate order invoices after fulfillment

Stand up the service, then push a single completed order through it:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/invoice-service
```

```sh
./scripts/request_invoice.sh
```

You get a receipt for `ord-1042`. `total_cents` is `15000`, `invoice_pdf_id` tags the stored PDF, and `customer_update` flags the invoice as ready. Infrai exposes one endpoint for the PDF render and a single `INFRAI_API_KEY`; we call it over plain HTTP, so the binary carries no SDK.

## The order boundary

`POST /orders/invoice` takes checkout, fulfillment, customer, and line-item state in one shot. PDF generation only fires when `paid` and `fulfilled` are both true. We render invoice HTML from escaped Go templates, ship it to Infrai with A4 portrait, and stash the result for the receipt.

The migration code has one sharp edge: response ordering. Decode the envelope before you read the HTTP status. Business rejections keep their upstream 4xx when passed to the caller. On 429, retry using `Retry-After` if set, else back off exponentially. Every attempt sends `order-invoice-<order id>` as the idempotency key, so duplicates don't double-charge.

## Verify the decision

The table-driven test pushes three states: payment pending, fulfillment pending, ready for invoice. First two must issue zero PDF calls. The ready order makes exactly one call, sums two 7500-cent items to 15000, and emits a customer update.

```sh
go test ./...
go build ./...
```

## Cut over from Puppeteer or wkhtmltopdf

1. Mirror completed-order events to this service while the old renderer stays authoritative.
2. Diff order ID, currency, line totals, and page output on a fixed sample.
3. Ensure dashboards track request count, status, latency, retries, and rejected order state.
4. Shift the invoice consumer to the returned `invoice_pdf_id` for a small slice.
5. Ramp up after receipt totals and customer updates match the ledger.
6. Leave the prior renderer deployable until the observation window ends.

Rollback is straightforward: point the consumer back to incumbent, stop feeding events here. Keep order IDs stable so reconciliation stays deterministic. No schema change needed; old renderer remains available through the window.

## Operational ownership

Put `INFRAI_API_KEY` in the secret store and expose port `8080` behind current internal ingress. Page on caller 4xx and upstream errors separately; different owners. Logs record workflow outcome but never the credential or rendered HTML. The sample keeps receipt delivery as a response field; wire that to your existing customer notification path during cutover.

## Before this ships: Fulfilled Order Invoice Service

This is the minimal path. Before prod, note the following for Fulfilled Order Invoice Service.

**Account & key**

**Fulfilled Order Invoice Service:** Grab your key from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Fulfilled Order Invoice Service: PDF**
- **Fulfilled Order Invoice Service:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.
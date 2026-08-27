#!/bin/sh
set -eu

curl --fail-with-body --request POST http://localhost:8080/orders/invoice \
  --header 'Content-Type: application/json' \
  --data '{
    "id": "ord-1042",
    "customer": "Ada Lovelace",
    "email": "ada@example.com",
    "currency": "USD",
    "paid": true,
    "fulfilled": true,
    "items": [{"name": "Mechanical keyboard", "quantity": 2, "unit_cents": 7500}]
  }'

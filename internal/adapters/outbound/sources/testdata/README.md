# Pinned producer specs and fixtures

The three `*.openapi.yaml` files are BYTE COPIES of the producers' `apis/openapi.yaml`
at their `develop` ref, registered in `../../../contracts/pinned.json`. NEVER edit a
copy: the `pinned-contracts` CI job (scripts/harness/pinned_contract_gate.py) fails
when a copy differs from the producer, and the fix is to re-copy the producer's file
and adapt this repo's code, not the pin.

| Pin                        | Producer                 | Routes the rebuild reads            |
|----------------------------|--------------------------|-------------------------------------|
| offer-availability.openapi.yaml | IQVO/offer-availability | GET /catalog, GET /listings         |
| pricing.openapi.yaml           | IQVO/pricing            | GET /item-prices                    |
| product-content.openapi.yaml   | IQVO/product-content    | GET /product-contents, GET /categories |

The `*.example.json` fixtures are DERIVED from the pinned schemas (every required key,
realistic values, shape-legal optionals): none of the three specs ships response
examples yet (offer-availability has parameter-level examples only; pricing and
product-content have none). When a producer backfills examples, prefer those and note
the change here.

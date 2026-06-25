# webhooks — usage

## Subscribe
```go
s, _ := webhooks.FromKernel(k)
sub := s.Subscribe("https://acme.test/hooks", []string{"order.paid", "order.refunded"}, "whsec_secret")
// events: list of names, or ["*"] for all
```

## Send an event
```go
s.Send(ctx, "order.paid", map[string]any{"id": 42})
```
Delivered to every subscription whose `events` match. Async over the kernel
Queue when configured (with retries), else inline.

## Signature & verification
Deliveries are signed: `X-Webhook-Signature: t=<unix>,v1=HMAC_SHA256(secret, "<t>.<body>")`
plus `X-Webhook-Timestamp`. Receivers verify:
```go
if !webhooks.Verify(secret, sig, body) { http.Error(w, "bad signature", 401); return }
```

## Retries
On a non-2xx response (or transport error) the delivery is retried up to 5
attempts; with a queue configured each retry is re-dispatched as a job. Every
attempt is recorded — see `s.Deliveries()` (status, succeeded, attempt, error).

## REST API
- `GET/POST /api/webhooks/subscriptions`, `DELETE /api/webhooks/subscriptions/{id}`
- `POST /api/webhooks/send` `{event, data}`
- `GET /api/webhooks/deliveries`

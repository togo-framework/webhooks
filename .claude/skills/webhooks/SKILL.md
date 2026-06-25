---
name: webhooks
description: Send signed, retried outgoing webhooks from a togo app — register subscriptions, dispatch events, and verify incoming signatures with the webhooks plugin.
---

# togo webhooks

Use the `togo-framework/webhooks` plugin to deliver outgoing webhooks.

## Subscribe an endpoint
```go
s, _ := webhooks.FromKernel(k)
s.Subscribe("https://acme.test/hooks", []string{"order.paid"}, "whsec_secret") // ["*"] = all events
```

## Fire an event
```go
s.Send(ctx, "order.paid", map[string]any{"id": 42})
```
Delivered async over the queue (retried, up to 5 attempts) to every matching subscriber; inline if no queue.

## Signature
Each POST carries `X-Webhook-Signature: t=<unix>,v1=<hmac-sha256>`. Receivers verify with `webhooks.Verify(secret, sig, body)`.

## REST
`/api/webhooks/subscriptions` (GET/POST/DELETE), `/api/webhooks/send` (POST `{event,data}`), `/api/webhooks/deliveries` (GET).

## Notes
- Install `queue` + run a worker for async delivery & retries; otherwise delivery is inline.
- Always set a per-subscription secret and verify signatures on the receiving side.
- Keep payloads small; the body is what's signed.

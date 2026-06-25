<div align="center">
  <img src=".github/assets/togo-mark.svg" alt="togo" height="64" />
  <h1>togo-framework/webhooks</h1>
  <p>
    <a href="https://to-go.dev/marketplace"><img src="https://img.shields.io/badge/marketplace-to--go.dev-1FC7DC" alt="marketplace" /></a>
    <a href="https://pkg.go.dev/github.com/togo-framework/webhooks"><img src="https://pkg.go.dev/badge/github.com/togo-framework/webhooks.svg" alt="pkg.go.dev" /></a>
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT" />
  </p>
  <p><strong>Outgoing webhooks for <a href="https://to-go.dev">togo</a> — signed, retried, async event delivery to subscribers.</strong></p>
</div>

## Install

```bash
togo install togo-framework/webhooks
```

The togo answer to Spatie's webhook-server / Svix. Register subscriptions, then
`Send` an event — every matching subscriber gets an **HMAC-SHA256-signed** POST,
delivered **over the queue** with **exponential-backoff retries** (inline when no
queue is configured). Every attempt is recorded in a delivery log.

## Usage

```go
s, _ := webhooks.FromKernel(k)

// Subscribe an endpoint to events ("*" = all).
sub := s.Subscribe("https://acme.test/hooks", []string{"order.paid"}, "whsec_secret")

// Fire an event — delivered async to every matching subscriber.
s.Send(ctx, "order.paid", map[string]any{"id": 42, "total": 19.99})

// Inspect deliveries.
for _, d := range s.Deliveries() { /* d.Succeeded, d.Status, d.Attempt … */ }
```

### Signing & verification

Each delivery carries `X-Webhook-Signature: t=<unix>,v1=<hmac>` (+ `X-Webhook-Timestamp`).
Receivers verify with:

```go
ok := webhooks.Verify(secret, r.Header.Get("X-Webhook-Signature"), body)
```

## REST API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/webhooks/subscriptions` | list subscriptions |
| `POST` | `/api/webhooks/subscriptions` | `{url, events, secret}` |
| `DELETE` | `/api/webhooks/subscriptions/{id}` | remove |
| `POST` | `/api/webhooks/send` | `{event, data}` |
| `GET` | `/api/webhooks/deliveries` | delivery log |

## Configuration

No required env. Uses the kernel **Queue** for async delivery + retries when
present (`togo install togo-framework/queue` + a worker); otherwise delivers
inline. Retries up to 5 attempts on non-2xx.

---

<div align="center">
  <h3>Premium sponsors</h3>
  <p>
    <a href="https://id8media.com"><strong>ID8 Media</strong></a> &nbsp;·&nbsp;
    <a href="https://one-studio.co"><strong>One Studio</strong></a>
  </p>
  <p><sub>Support togo — <a href="https://github.com/sponsors/fadymondy">become a sponsor</a>.</sub></p>
</div>

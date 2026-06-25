// Package webhooks delivers outgoing webhooks to subscribers — signed,
// asynchronous, and retried (Spatie webhook-server / Svix style for togo).
//
// Register subscriptions (a URL + the events it wants + a signing secret), then
// Send an event: every matching subscriber receives an HMAC-SHA256-signed POST,
// delivered over the kernel Queue (with exponential-backoff retries) when one is
// configured, or inline otherwise. Every attempt is recorded in a delivery log.
//
//	s, _ := webhooks.FromKernel(k)
//	sub := s.Subscribe("https://acme.test/hooks", []string{"order.paid"}, "whsec_…")
//	s.Send(ctx, "order.paid", map[string]any{"id": 42})
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/togo-framework/togo"
)

// Subscription is a registered webhook endpoint.
type Subscription struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	Events []string `json:"events"` // event names; "*" matches all
	Secret string   `json:"secret,omitempty"`
	Active bool     `json:"active"`
}

func (sub Subscription) wants(event string) bool {
	if !sub.Active {
		return false
	}
	for _, e := range sub.Events {
		if e == "*" || e == event {
			return true
		}
	}
	return false
}

// Delivery records a single delivery attempt.
type Delivery struct {
	ID        string    `json:"id"`
	SubID     string    `json:"subscription_id"`
	Event     string    `json:"event"`
	Attempt   int       `json:"attempt"`
	Status    int       `json:"status"`
	Succeeded bool      `json:"succeeded"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

const (
	queueName  = "webhooks.deliver"
	maxAttempt = 5
)

type deliverJob struct {
	SubID   string
	Event   string
	Payload []byte
	Attempt int
}

// Service is the webhooks runtime stored on the kernel (k.Get("webhooks")).
type Service struct {
	k          *togo.Kernel
	mu         sync.Mutex
	subs       map[string]*Subscription
	deliveries []Delivery
	maxLog     int
	seq        int
	hc         *http.Client
}

func init() {
	togo.RegisterProviderFunc("webhooks", togo.PriorityLate+10, func(k *togo.Kernel) error {
		s := &Service{
			k:      k,
			subs:   map[string]*Subscription{},
			maxLog: 2000,
			hc:     &http.Client{Timeout: 15 * time.Second},
		}
		if k.Queue != nil {
			k.Queue.Handle(queueName, func(ctx context.Context, payload any) error {
				job, ok := payload.(deliverJob)
				if !ok {
					return fmt.Errorf("webhooks: bad job payload %T", payload)
				}
				return s.attempt(ctx, job)
			})
		}
		k.Set("webhooks", s)
		if k.Router != nil {
			s.mountRoutes(k.Router)
		}
		return nil
	})
}

// FromKernel returns the webhooks Service registered on the kernel.
func FromKernel(k *togo.Kernel) (*Service, bool) {
	v, ok := k.Get("webhooks")
	if !ok {
		return nil, false
	}
	s, ok := v.(*Service)
	return s, ok
}

func (s *Service) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), s.seq)
}

// Subscribe registers an endpoint for the given events ("*" = all).
func (s *Service) Subscribe(url string, events []string, secret string) *Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &Subscription{ID: s.nextID("sub"), URL: url, Events: events, Secret: secret, Active: true}
	s.subs[sub.ID] = sub
	return sub
}

// Unsubscribe removes a subscription.
func (s *Service) Unsubscribe(id string) {
	s.mu.Lock()
	delete(s.subs, id)
	s.mu.Unlock()
}

// Subscriptions lists all registered subscriptions.
func (s *Service) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		out = append(out, *sub)
	}
	return out
}

// Send dispatches an event to every matching subscriber. Delivery is async over
// the queue when available, else inline.
func (s *Service) Send(ctx context.Context, event string, payload any) error {
	body, err := json.Marshal(map[string]any{"event": event, "data": payload})
	if err != nil {
		return fmt.Errorf("webhooks: marshal payload: %w", err)
	}
	s.mu.Lock()
	var targets []string
	for id, sub := range s.subs {
		if sub.wants(event) {
			targets = append(targets, id)
		}
	}
	s.mu.Unlock()

	for _, id := range targets {
		job := deliverJob{SubID: id, Event: event, Payload: body, Attempt: 1}
		if s.k != nil && s.k.Queue != nil {
			_ = s.k.Queue.Dispatch(ctx, queueName, job)
			continue
		}
		_ = s.attempt(ctx, job)
	}
	return nil
}

// Sign computes the webhook signature header value for a payload + secret:
//
//	t=<unix>,v1=HMAC_SHA256(secret, "<t>.<body>")
func Sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

// Verify checks an incoming X-Webhook-Signature against the secret + body.
// (Helper for receivers.)
func Verify(secret, signature string, body []byte) bool {
	var ts int64
	var v1 string
	for _, part := range bytes.Split([]byte(signature), []byte(",")) {
		kv := bytes.SplitN(part, []byte("="), 2)
		if len(kv) != 2 {
			continue
		}
		switch string(kv[0]) {
		case "t":
			ts, _ = strconv.ParseInt(string(kv[1]), 10, 64)
		case "v1":
			v1 = string(kv[1])
		}
	}
	if ts == 0 || v1 == "" {
		return false
	}
	expected := Sign(secret, ts, body)
	// expected is "t=..,v1=.."; compare the v1 portion in constant time.
	want := expected[len("t=")+len(strconv.FormatInt(ts, 10))+len(",v1="):]
	return hmac.Equal([]byte(v1), []byte(want))
}

// attempt performs one delivery, records it, and re-dispatches with backoff on
// failure (up to maxAttempt).
func (s *Service) attempt(ctx context.Context, job deliverJob) error {
	s.mu.Lock()
	sub := s.subs[job.SubID]
	s.mu.Unlock()
	if sub == nil {
		return nil // subscription removed
	}

	status, err := s.post(ctx, sub, job.Payload)
	d := Delivery{
		ID:        s.id("del"),
		SubID:     job.SubID,
		Event:     job.Event,
		Attempt:   job.Attempt,
		Status:    status,
		Succeeded: err == nil && status >= 200 && status < 300,
		At:        time.Now(),
	}
	if err != nil {
		d.Error = err.Error()
	}
	s.record(d)

	if !d.Succeeded && job.Attempt < maxAttempt {
		next := deliverJob{SubID: job.SubID, Event: job.Event, Payload: job.Payload, Attempt: job.Attempt + 1}
		if s.k != nil && s.k.Queue != nil {
			_ = s.k.Queue.Dispatch(ctx, queueName, next) // backoff is the queue's concern
		}
		if d.Error == "" {
			return fmt.Errorf("webhooks: delivery to %s returned %d", sub.URL, status)
		}
		return err
	}
	return nil
}

// post signs and POSTs the payload, returning the HTTP status.
func (s *Service) post(ctx context.Context, sub *Subscription, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if sub.Secret != "" {
		ts := time.Now().Unix()
		req.Header.Set("X-Webhook-Signature", Sign(sub.Secret, ts, body))
		req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(ts, 10))
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (s *Service) id(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextID(prefix)
}

func (s *Service) record(d Delivery) {
	s.mu.Lock()
	s.deliveries = append(s.deliveries, d)
	if len(s.deliveries) > s.maxLog {
		s.deliveries = s.deliveries[len(s.deliveries)-s.maxLog:]
	}
	s.mu.Unlock()
}

// Deliveries returns the recent delivery log (most recent last).
func (s *Service) Deliveries() []Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Delivery, len(s.deliveries))
	copy(out, s.deliveries)
	return out
}

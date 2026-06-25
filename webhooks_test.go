package webhooks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func newTestService() *Service {
	return &Service{subs: map[string]*Subscription{}, maxLog: 100, hc: http.DefaultClient}
}

func TestSubscribeAndMatch(t *testing.T) {
	s := newTestService()
	sub := s.Subscribe("http://x.test", []string{"order.paid"}, "")
	if !sub.wants("order.paid") {
		t.Error("should match the exact event")
	}
	if sub.wants("order.refunded") {
		t.Error("should not match an unsubscribed event")
	}
	wild := s.Subscribe("http://y.test", []string{"*"}, "")
	if !wild.wants("anything") {
		t.Error("wildcard should match")
	}
	wild.Active = false
	if wild.wants("anything") {
		t.Error("inactive subscription should not match")
	}
	if len(s.Subscriptions()) != 2 {
		t.Errorf("want 2 subscriptions, got %d", len(s.Subscriptions()))
	}
}

func TestSignVerify(t *testing.T) {
	body := []byte(`{"event":"x","data":1}`)
	sig := Sign("whsec_test", 1700000000, body)
	if Sign("whsec_test", 1700000000, body) != sig {
		t.Error("Sign is not deterministic")
	}
	if !Verify("whsec_test", sig, body) {
		t.Error("valid signature rejected")
	}
	if Verify("wrong-secret", sig, body) {
		t.Error("wrong secret accepted")
	}
	if Verify("whsec_test", sig, []byte("tampered")) {
		t.Error("tampered body accepted")
	}
	if Verify("whsec_test", "garbage", body) {
		t.Error("malformed signature accepted")
	}
}

func TestDeliverySuccessAndSigned(t *testing.T) {
	var calls int32
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		sig = r.Header.Get("X-Webhook-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestService()
	s.Subscribe(srv.URL, []string{"ping"}, "whsec_abc")
	if err := s.Send(context.Background(), "ping", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("endpoint called %d times, want 1", calls)
	}
	if sig == "" {
		t.Error("delivery missing X-Webhook-Signature header")
	}
	d := s.Deliveries()
	if len(d) != 1 || !d[0].Succeeded || d[0].Status != http.StatusOK {
		t.Errorf("delivery = %+v", d)
	}
}

func TestDeliveryFailureRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := newTestService()
	s.Subscribe(srv.URL, []string{"ping"}, "")
	_ = s.Send(context.Background(), "ping", nil)
	d := s.Deliveries()
	if len(d) != 1 {
		t.Fatalf("want 1 delivery, got %d", len(d))
	}
	if d[0].Succeeded || d[0].Status != http.StatusInternalServerError {
		t.Errorf("failed delivery not recorded as failed: %+v", d[0])
	}
	if d[0].Attempt != 1 {
		t.Errorf("attempt = %d, want 1", d[0].Attempt)
	}
}

func TestEventMatchingOnlyTargets(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := newTestService()
	s.Subscribe(srv.URL, []string{"a"}, "")
	s.Subscribe(srv.URL, []string{"b"}, "")
	_ = s.Send(context.Background(), "a", nil) // only the first subscriber matches
	if atomic.LoadInt32(&hit) != 1 {
		t.Fatalf("event delivered to %d endpoints, want 1", hit)
	}
}

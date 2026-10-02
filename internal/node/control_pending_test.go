package node

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dsync/internal/config"
	"dsync/internal/proto"
)

func controlPINRequest(t *testing.T, sender testNode, pin string) *http.Request {
	t.Helper()
	r := requestWithCertificate(t, sender, proto.ControlPINRequest{PIN: pin, FromName: sender.cfg.Name})
	r.URL.Path = "/api/v1/control/pin"
	return r
}

func startControlPINRequest(t *testing.T, receiver, sender testNode, pin string) <-chan *httptest.ResponseRecorder {
	t.Helper()
	req := controlPINRequest(t, sender, pin)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		receiver.handler().ServeHTTP(w, req)
		done <- w
	}()
	return done
}

func allowControlFrom(t *testing.T, receiver, sender testNode) {
	t.Helper()
	if err := receiver.cfg.Trust(config.TrustedPeer{
		ID: sender.cfg.ID, Name: sender.cfg.Name, Fingerprint: sender.id.Fingerprint, CanControl: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func waitControlPINEvent(t *testing.T, events <-chan ControlPIN) ControlPIN {
	t.Helper()
	select {
	case req := <-events:
		return req
	case <-time.After(time.Second):
		t.Fatal("control PIN consent event was not emitted")
		return ControlPIN{}
	}
}

func TestAutomaticControlPINIsDeduplicatedPerPeer(t *testing.T) {
	events := make(chan ControlPIN, 4)
	receiver := newTestNodeWith(t, "receiver", func(event string, data any) {
		if event == EventControlPIN {
			events <- data.(ControlPIN)
		}
	})
	sender := newTestNode(t, "sender")
	allowControlFrom(t, receiver, sender)
	receiver.cfg.SunshineUser, receiver.cfg.SunshinePassword = "admin", "secret"

	firstDone := startControlPINRequest(t, receiver, sender, "1234")
	first := waitControlPINEvent(t, events)

	second := httptest.NewRecorder()
	receiver.handler().ServeHTTP(second, controlPINRequest(t, sender, "5678"))
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409; body %q", second.Code, second.Body.String())
	}
	select {
	case extra := <-events:
		t.Fatalf("duplicate request spawned another dialog: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}

	receiver.AnswerControlPIN(first.ID, false)
	if w := <-firstDone; w.Code != http.StatusForbidden {
		t.Fatalf("declined first status = %d", w.Code)
	}
	receiver.mu.Lock()
	pending := len(receiver.controlPINs)
	receiver.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending requests after cleanup = %d", pending)
	}
}

func TestAutomaticControlPINGlobalLimit(t *testing.T) {
	events := make(chan ControlPIN, maxPendingControlPINs+1)
	receiver := newTestNodeWith(t, "receiver", func(event string, data any) {
		if event == EventControlPIN {
			events <- data.(ControlPIN)
		}
	})
	receiver.cfg.SunshineUser, receiver.cfg.SunshinePassword = "admin", "secret"

	requests := make([]ControlPIN, 0, maxPendingControlPINs)
	done := make([]<-chan *httptest.ResponseRecorder, 0, maxPendingControlPINs)
	for i := 0; i < maxPendingControlPINs; i++ {
		sender := newTestNode(t, "sender")
		allowControlFrom(t, receiver, sender)
		done = append(done, startControlPINRequest(t, receiver, sender, "1234"))
		requests = append(requests, waitControlPINEvent(t, events))
	}

	extraSender := newTestNode(t, "extra sender")
	allowControlFrom(t, receiver, extraSender)
	extra := httptest.NewRecorder()
	receiver.handler().ServeHTTP(extra, controlPINRequest(t, extraSender, "9999"))
	if extra.Code != http.StatusTooManyRequests {
		t.Fatalf("overflow status = %d, want 429; body %q", extra.Code, extra.Body.String())
	}
	select {
	case req := <-events:
		t.Fatalf("overflow request spawned a dialog: %+v", req)
	case <-time.After(50 * time.Millisecond):
	}

	for _, req := range requests {
		receiver.AnswerControlPIN(req.ID, false)
	}
	for _, result := range done {
		if w := <-result; w.Code != http.StatusForbidden {
			t.Fatalf("cleanup response = %d", w.Code)
		}
	}
	receiver.mu.Lock()
	pending := len(receiver.controlPINs)
	receiver.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending requests after cleanup = %d", pending)
	}
}

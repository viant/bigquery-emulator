package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSequentialIntakePrecedesHTTPReadDeadline(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondActive := make(chan struct{})
	bodyError := make(chan error, 1)
	clientErrors := make(chan error, 2)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	handler := sequentialAccessMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if r.URL.Path == "/first" {
			close(firstStarted)
			<-releaseFirst
		} else {
			bodyError <- err
		}
		w.WriteHeader(http.StatusOK)
	}))
	server := httptest.NewUnstartedServer(handler)
	server.Config.ReadTimeout = 100 * time.Millisecond
	var stateMu sync.Mutex
	active := 0
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateActive {
			stateMu.Lock()
			active++
			if active == 2 {
				close(secondActive)
			}
			stateMu.Unlock()
		}
	}
	server.Start()
	defer func() { release(); server.Close() }()
	client := &http.Client{Timeout: 2 * time.Second}
	post := func(path, body string) chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			response, err := client.Post(server.URL+path, "application/json", strings.NewReader(body))
			if err != nil {
				clientErrors <- err
				return
			}
			response.Body.Close()
		}()
		return done
	}
	await := func(signal <-chan struct{}, stage string) {
		t.Helper()
		select {
		case <-signal:
		case err := <-clientErrors:
			t.Fatalf("%s client failed: %v", stage, err)
		case <-time.After(3 * time.Second):
			t.Fatalf("%s timed out", stage)
		}
	}
	firstDone := post("/first", "{}")
	await(firstStarted, "first handler")
	secondDone := post("/second", strings.Repeat("x", 64<<10))
	await(secondActive, "second request ingress")
	// The same HTTP read deadline runs during the wait for execution admission.
	time.Sleep(150 * time.Millisecond)
	select {
	case err := <-bodyError:
		t.Fatalf("second handler bypassed serialization: %v", err)
	default:
	}
	release()
	select {
	case err := <-bodyError:
		if err != nil {
			t.Fatalf("body buffered before execution queue failed: %v", err)
		}
	case err := <-clientErrors:
		t.Fatalf("client failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("queued handler did not finish")
	}
	await(firstDone, "first response")
	await(secondDone, "second response")
	select {
	case err := <-clientErrors:
		t.Fatal(err)
	default:
	}
}

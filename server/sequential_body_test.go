package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type intakeTestBody struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (b *intakeTestBody) Close() error {
	b.once.Do(func() {
		if b.closed != nil {
			close(b.closed)
		}
	})
	return nil
}

type intakeErrorReader struct{ err error }

func (r intakeErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestRetainedRequestBodyMemoryAndSpool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	for _, size := range []int{0, 1, requestBodyMemoryBytes - 1, requestBodyMemoryBytes, requestBodyMemoryBytes + 1, 8 * requestBodyMemoryBytes} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			input := bytes.Repeat([]byte("x"), size)
			original := &intakeTestBody{Reader: bytes.NewReader(input), closed: make(chan struct{})}
			body, err := retainRequestBody(original)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-original.closed:
			default:
				t.Fatal("network body not closed after intake")
			}
			retained := body.(*retainedRequestBody)
			if (retained.file != nil) != (size > requestBodyMemoryBytes) {
				t.Fatal("unexpected memory/spool boundary")
			}
			actual, err := io.ReadAll(body)
			if err != nil || !bytes.Equal(actual, input) {
				t.Fatalf("payload mismatch: bytes=%d err=%v", len(actual), err)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("spool was not removed: entries=%d err=%v", len(entries), err)
			}
		})
	}
}
func TestRetainedRequestBodyPreservesReadErrors(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sentinel := errors.New("intake failed")
	for _, size := range []int{3, requestBodyMemoryBytes + 32} {
		for _, cause := range []error{sentinel, io.ErrUnexpectedEOF} {
			t.Run(fmt.Sprintf("%d/%v", size, cause), func(t *testing.T) {
				original := &intakeTestBody{Reader: io.MultiReader(strings.NewReader(strings.Repeat("x", size)), intakeErrorReader{cause}), closed: make(chan struct{})}
				body, err := retainRequestBody(original)
				if err != nil {
					t.Fatal(err)
				}
				defer body.Close()
				actual, err := io.ReadAll(body)
				if len(actual) != size || !errors.Is(err, cause) {
					t.Fatalf("read bytes=%d err=%v", len(actual), err)
				}
			})
		}
	}
}
func TestSequentialCanceledQueueReleasesBodyWithoutExecuting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	secondCalled := make(chan struct{}, 1)
	handler := sequentialAccessMiddleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			close(started)
			<-release
		} else {
			secondCalled <- struct{}{}
		}
	}))
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/first", nil))
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &intakeTestBody{Reader: strings.NewReader(strings.Repeat("x", requestBodyMemoryBytes+1)), closed: make(chan struct{})}
	req := httptest.NewRequest("POST", "/second", nil).WithContext(ctx)
	req.Body = body
	secondDone := make(chan struct{})
	go func() { defer close(secondDone); handler.ServeHTTP(httptest.NewRecorder(), req) }()
	<-body.closed
	cancel()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("canceled request waited for execution lock")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Errorf("canceled spool leaked: entries=%d err=%v", len(entries), err)
	}
	close(release)
	<-firstDone
	select {
	case <-secondCalled:
		t.Fatal("canceled request executed")
	default:
	}
}
func TestSequentialSpoolFailureClosesBodyAndDoesNotExecute(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir()+"/missing")
	body := &intakeTestBody{Reader: strings.NewReader(strings.Repeat("x", requestBodyMemoryBytes+1)), closed: make(chan struct{})}
	request := httptest.NewRequest("POST", "/", nil)
	request.Body = body
	response := httptest.NewRecorder()
	called := false
	sequentialAccessMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("failed intake leaked original body")
	}
}

func TestSequentialSpoolCleanupAfterHandler(t *testing.T) {
	for _, panicHandler := range []bool{false, true} {
		t.Run(fmt.Sprint(panicHandler), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			handler := sequentialAccessMiddleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/large" {
					if panicHandler {
						panic("decoder failed")
					}
					actual, err := io.ReadAll(r.Body)
					if err != nil || len(actual) != requestBodyMemoryBytes+1 {
						t.Errorf("handler payload length=%d err=%v", len(actual), err)
					}
				}
			}))
			func() {
				defer func() {
					if recovered := recover(); (recovered != nil) != panicHandler {
						t.Errorf("unexpected panic: %v", recovered)
					}
				}()
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/large", strings.NewReader(strings.Repeat("x", requestBodyMemoryBytes+1))))
			}()
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("handler spool leaked: entries=%d err=%v", len(entries), err)
			}
			// A handler failure also releases the execution slot.
			done := make(chan struct{})
			go func() {
				defer close(done)
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/next", nil))
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("handler retained execution slot")
			}
		})
	}
}

func TestSequentialCompressedRequestPreservesPayload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	payload := make([]byte, 2*requestBodyMemoryBytes)
	_, _ = rand.New(rand.NewSource(1)).Read(payload)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if compressed.Len() <= requestBodyMemoryBytes {
		t.Fatal("compressed fixture must exercise disk spooling")
	}
	handlerErrors := make(chan error, 1)
	server := httptest.NewServer(sequentialAccessMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "gzip" {
			handlerErrors <- errors.New("content encoding was changed")
			w.WriteHeader(400)
			return
		}
		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			handlerErrors <- err
			w.WriteHeader(400)
			return
		}
		defer reader.Close()
		actual, err := io.ReadAll(reader)
		if err != nil {
			handlerErrors <- err
			w.WriteHeader(400)
			return
		}
		if !bytes.Equal(actual, payload) {
			handlerErrors <- errors.New("decoded payload differs")
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(http.StatusOK)
	})))
	defer server.Close()
	request, err := http.NewRequest("POST", server.URL, bytes.NewReader(compressed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Encoding", "gzip")
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	select {
	case err := <-handlerErrors:
		t.Fatal(err)
	default:
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("gzip spool leaked: entries=%d err=%v", len(entries), err)
	}
}

func TestRetainedRequestBodyDiskErrorsCleanUp(t *testing.T) {
	t.Run("write", func(t *testing.T) {
		dir := t.TempDir()
		var name string
		original := &intakeTestBody{Reader: strings.NewReader(strings.Repeat("x", requestBodyMemoryBytes+1)), closed: make(chan struct{})}
		_, err := retainRequestBodyFile(original, func() (*os.File, error) {
			file, e := os.CreateTemp(dir, "write-error-*")
			if e != nil {
				return nil, e
			}
			name = file.Name()
			if e = file.Close(); e != nil {
				return nil, e
			}
			return os.Open(name)
		})
		if err == nil {
			t.Fatal("read-only descriptor accepted a body write")
		}
		if _, e := os.Stat(name); !os.IsNotExist(e) {
			t.Fatalf("write failure leaked spool: %v", e)
		}
		select {
		case <-original.closed:
		default:
			t.Fatal("write failure leaked original body")
		}
	})
	t.Run("read", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		body, err := retainRequestBody(io.NopCloser(strings.NewReader(strings.Repeat("x", requestBodyMemoryBytes+1))))
		if err != nil {
			t.Fatal(err)
		}
		retained := body.(*retainedRequestBody)
		name := retained.file.Name()
		if err := retained.file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(body); err == nil {
			t.Fatal("closed spool descriptor read succeeded")
		}
		_ = body.Close()
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("read failure leaked spool: %v", err)
		}
	})
}

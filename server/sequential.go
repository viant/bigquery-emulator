package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
)

// Intake must finish within the HTTP read deadline, before waiting for a query
// to release the execution queue. Keep small bodies in memory and spool larger
// bodies to disk; uploads retain their existing size and decoding contracts.
const requestBodyMemoryBytes = 64 << 10

func sequentialAccessMiddleware() func(http.Handler) http.Handler {
	queue := make(chan struct{}, 1)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Err() != nil {
				return
			}
			if r.Body != nil && r.Body != http.NoBody {
				body, err := retainRequestBody(r.Body)
				if err != nil {
					http.Error(w, "failed to retain request body", http.StatusInternalServerError)
					return
				}
				defer body.Close()
				r.Body = body
			}
			select {
			case queue <- struct{}{}:
				defer func() { <-queue }()
			case <-r.Context().Done():
				return
			}
			if r.Context().Err() != nil {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type requestBodyReader struct {
	io.Reader
	err error
}

func (r *requestBodyReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err != nil && err != io.EOF {
		r.err = err
	}
	return n, err
}

// Preserve read errors for the downstream decoder, including upload handlers
// which deliberately tolerate io.ErrUnexpectedEOF from older gzip clients.
func retainRequestBody(body io.ReadCloser) (io.ReadCloser, error) {
	return retainRequestBodyFile(body, func() (*os.File, error) { return os.CreateTemp("", "bigquery-emulator-request-*") })
}

func retainRequestBodyFile(body io.ReadCloser, createTemp func() (*os.File, error)) (io.ReadCloser, error) {
	defer body.Close()
	source := &requestBodyReader{Reader: body}
	var small bytes.Buffer
	_, err := io.CopyN(&small, source, requestBodyMemoryBytes+1)
	if err != nil {
		return &retainedRequestBody{Reader: bytes.NewReader(small.Bytes()), readErr: source.err}, nil
	}
	file, err := createTemp()
	if err != nil {
		return nil, err
	}
	retained := &retainedRequestBody{Reader: file, file: file}
	success := false
	defer func() {
		if !success {
			_ = retained.Close()
		}
	}()
	if _, err := file.Write(small.Bytes()); err != nil {
		return nil, err
	}
	if _, err := io.Copy(file, source); err != nil && !errors.Is(err, source.err) {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	retained.readErr = source.err
	success = true
	return retained, nil
}

type retainedRequestBody struct {
	io.Reader
	readErr  error
	file     *os.File
	once     sync.Once
	closeErr error
}

func (b *retainedRequestBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF && b.readErr != nil {
		err, b.readErr = b.readErr, nil
	}
	return n, err
}
func (b *retainedRequestBody) Close() error {
	b.once.Do(func() {
		if b.file != nil {
			b.closeErr = errors.Join(b.file.Close(), os.Remove(b.file.Name()))
		}
	})
	return b.closeErr
}

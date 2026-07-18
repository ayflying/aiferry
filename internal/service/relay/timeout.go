package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

type upstreamTimeoutError struct {
	phase string
}

func (e upstreamTimeoutError) Error() string {
	return "upstream " + e.phase + " timeout"
}

func isUpstreamTimeout(err error) bool {
	var timeout upstreamTimeoutError
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout)
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r cancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

func doStreamRequest(ctx context.Context, client *http.Client, req *http.Request, timeout time.Duration) (*http.Response, error) {
	if timeout <= 0 {
		return client.Do(req)
	}
	requestCtx, cancel := context.WithCancel(ctx)
	request := req.Clone(requestCtx)
	type response struct {
		value *http.Response
		err   error
	}
	result := make(chan response, 1)
	go func() {
		resp, err := client.Do(request)
		if resp != nil && requestCtx.Err() != nil {
			_ = resp.Body.Close()
		}
		result <- response{value: resp, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case current := <-result:
		if current.err != nil {
			cancel()
			return nil, current.err
		}
		current.value.Body = cancelReadCloser{ReadCloser: current.value.Body, cancel: cancel}
		return current.value, nil
	case <-timer.C:
		cancel()
		return nil, upstreamTimeoutError{phase: "stream first-byte"}
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}

type streamTimeoutReader struct {
	body             io.ReadCloser
	firstByteTimeout time.Duration
	idleTimeout      time.Duration
	receivedData     bool
}

func newStreamTimeoutReader(body io.ReadCloser, firstByteTimeout, idleTimeout time.Duration) *streamTimeoutReader {
	return &streamTimeoutReader{body: body, firstByteTimeout: firstByteTimeout, idleTimeout: idleTimeout}
}

func (r *streamTimeoutReader) Read(buffer []byte) (int, error) {
	timeout := r.idleTimeout
	phase := "stream idle"
	if !r.receivedData {
		timeout = r.firstByteTimeout
		phase = "stream first-byte"
	}
	if !r.receivedData && timeout <= 0 {
		_ = r.body.Close()
		return 0, upstreamTimeoutError{phase: phase}
	}
	if timeout <= 0 {
		count, err := r.body.Read(buffer)
		if count > 0 {
			r.receivedData = true
		}
		return count, err
	}
	timedOut := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		close(timedOut)
		_ = r.body.Close()
	})
	count, err := r.body.Read(buffer)
	if count > 0 {
		r.receivedData = true
	}
	if timer.Stop() {
		return count, err
	}
	<-timedOut
	return count, upstreamTimeoutError{phase: phase}
}

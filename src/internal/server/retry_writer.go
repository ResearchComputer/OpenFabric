package server

import (
	"bytes"
	"net/http"
	"strings"
)

// retryableResponseWriter buffers upstream responses so that transport-level
// failures can be retried against a different worker without the client seeing
// the error.
//
// Two modes:
//   - Non-streaming: buffers entire response up to maxBufferSize, then flushes
//     on success via flushToClient().
//   - Streaming (SSE): buffers until a 2xx WriteHeader commits headers to the
//     client, then switches to pass-through. Pre-header transport failures are
//     retryable; post-commit failures are not.
//
// Only http.Flusher is supported. http.Hijacker and http.Pusher are not — this
// handler does not serve WebSocket upgrades or HTTP/2 server push.
type retryableResponseWriter struct {
	underlying  http.ResponseWriter
	flusher     http.Flusher
	streaming   bool

	statusCode    int
	header        http.Header
	body          bytes.Buffer
	maxBufferSize int

	headersSent    bool // headers flushed to client — no more retries
	failed         bool // transport error — this attempt is retryable
	wroteHeader    bool // WriteHeader was called (even if buffered)
	bufferExceeded bool // buffer cap hit — committed and switched to pass-through
}

func newRetryableResponseWriter(w http.ResponseWriter, streaming bool, maxBuffer int) *retryableResponseWriter {
	var f http.Flusher
	if fl, ok := w.(http.Flusher); ok {
		f = fl
	}
	return &retryableResponseWriter{
		underlying:    w,
		flusher:       f,
		streaming:     streaming,
		header:        make(http.Header),
		maxBufferSize: maxBuffer,
	}
}

// Header returns the buffered header map. Headers are not forwarded to the
// underlying writer until commit (streaming 2xx) or flushToClient().
func (rw *retryableResponseWriter) Header() http.Header {
	return rw.header
}

// markFailed is called by the proxy ErrorHandler to signal a transport error.
func (rw *retryableResponseWriter) markFailed() {
	rw.failed = true
}

// isRetryable returns true if this attempt failed and no data has been
// committed to the client yet (so the retry loop can try another peer).
func (rw *retryableResponseWriter) isRetryable() bool {
	return !rw.headersSent && rw.failed
}

// commitHeaders copies buffered headers to the underlying writer and sends the
// status code. After this call, headersSent is true and retry is impossible.
func (rw *retryableResponseWriter) commitHeaders() {
	dst := rw.underlying.Header()
	for k, vs := range rw.header {
		dst[k] = vs
	}
	code := rw.statusCode
	if code == 0 {
		code = http.StatusOK
	}
	rw.underlying.WriteHeader(code)
	rw.headersSent = true
}

// isStreamingResponse checks the buffered response Content-Type to detect SSE.
// This is the layer-3 safety net: if neither the request body "stream":true nor
// the Accept header signaled streaming, we still detect it from the response.
func isStreamingResponse(h http.Header) bool {
	return strings.Contains(h.Get("Content-Type"), "text/event-stream")
}

// WriteHeader buffers or commits the status code depending on mode.
//
// Non-streaming: always buffers.
// Streaming + 2xx: commits headers to the client (retry no longer possible).
// Streaming + non-2xx: buffers (allows retry on subsequent transport failure).
//
// Layer-3 safety net: if the response Content-Type is text/event-stream but
// the writer was created as non-streaming, it upgrades to streaming mode here.
func (rw *retryableResponseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code

	// Layer 3: upgrade to streaming if response says SSE
	if !rw.streaming && isStreamingResponse(rw.header) {
		rw.streaming = true
	}

	if rw.streaming && code >= 200 && code < 300 {
		rw.commitHeaders()
	}
}

// Write buffers or passes through data depending on state.
//
// If WriteHeader has not been called, it triggers an implicit 200 (matching
// Go stdlib behavior). If headers are already committed (streaming 2xx or
// buffer cap exceeded), data passes through to the underlying writer. Otherwise
// data is appended to the body buffer. If the buffer would exceed maxBufferSize,
// the writer commits everything and switches to pass-through.
func (rw *retryableResponseWriter) Write(data []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}

	// Already committed — pass through
	if rw.headersSent {
		return rw.underlying.Write(data)
	}

	// Check buffer cap
	if rw.body.Len()+len(data) > rw.maxBufferSize {
		// Commit headers + existing buffer
		rw.commitHeaders()
		rw.bufferExceeded = true
		// Flush buffered body
		if rw.body.Len() > 0 {
			if _, err := rw.underlying.Write(rw.body.Bytes()); err != nil {
				return 0, err
			}
		}
		// Write current data directly
		return rw.underlying.Write(data)
	}

	return rw.body.Write(data)
}

// Flush delegates to the underlying Flusher if headers have been committed.
// Before commit, Flush is a no-op — we don't want to accidentally flush
// buffered content during the retry window.
func (rw *retryableResponseWriter) Flush() {
	if rw.headersSent && rw.flusher != nil {
		rw.flusher.Flush()
	}
}

// flushToClient sends the buffered status, headers, and body to the underlying
// writer. Called once after a successful non-streaming response. If WriteHeader
// was never called, defaults to 200 OK.
func (rw *retryableResponseWriter) flushToClient() {
	rw.commitHeaders()
	if rw.body.Len() > 0 {
		rw.underlying.Write(rw.body.Bytes()) //nolint:errcheck
	}
}

// firstByteMarker wraps an http.ResponseWriter and fires a stageTimer Mark
// the first time a non-empty Write hits the wrapped writer. This measures
// the head's "response startup" cost: time from when ModifyResponse fires
// (worker headers arrived at head) to when the head actually puts the
// first response byte on the outbound socket to the client.
//
// The measured duration (ms) is exposed via Duration() so the proxy
// handler can stash it in a response trailer for the client to read.
type firstByteMarker struct {
	http.ResponseWriter
	flusher http.Flusher
	st      *stageTimer
	name    string
	fired   bool
	durMs   float64
}

func newFirstByteMarker(w http.ResponseWriter, st *stageTimer, name string) *firstByteMarker {
	var f http.Flusher
	if fl, ok := w.(http.Flusher); ok {
		f = fl
	}
	return &firstByteMarker{ResponseWriter: w, flusher: f, st: st, name: name}
}

func (m *firstByteMarker) Write(b []byte) (int, error) {
	if !m.fired && len(b) > 0 {
		m.st.Mark(m.name)
		if n := len(m.st.stages); n > 0 {
			m.durMs = m.st.stages[n-1].ms
		}
		m.fired = true
	}
	return m.ResponseWriter.Write(b)
}

func (m *firstByteMarker) Flush() {
	if m.flusher != nil {
		m.flusher.Flush()
	}
}

// Duration returns the measured ms between the previous stageTimer Mark and
// the first non-empty Write to the underlying writer. ok=false if Write was
// never called (e.g. transport error before any body bytes).
func (m *firstByteMarker) Duration() (float64, bool) {
	return m.durMs, m.fired
}

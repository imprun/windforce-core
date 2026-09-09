package telemetry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type trailerPopulatingReader struct {
	request *http.Request
	reader  io.Reader
}

func (r *trailerPopulatingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		if r.request.Trailer == nil {
			r.request.Trailer = make(http.Header)
		}
		r.request.Trailer.Set("X-Synthetic-Trailer", "arrived-at-eof")
	}
	return n, err
}

func TestHTTPHandlerPreservesLiveTrailersThroughRequestClone(t *testing.T) {
	t.Parallel()
	for _, declared := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/synthetic", nil)
		if declared {
			request.Trailer = http.Header{"X-Synthetic-Trailer": nil}
		}
		request.Body = io.NopCloser(&trailerPopulatingReader{request: request, reader: strings.NewReader("body")})
		handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, received *http.Request) {
			if _, err := io.ReadAll(received.Body); err != nil {
				t.Fatal(err)
			}
			if received.Trailer.Get("X-Synthetic-Trailer") != "arrived-at-eof" {
				t.Fatal("request clone lost a trailer populated by Body.Read")
			}
			w.WriteHeader(http.StatusNoContent)
		}), "synthetic-trailer-test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

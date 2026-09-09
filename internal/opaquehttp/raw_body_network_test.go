package opaquehttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

func TestRawBodyNetworkRejectionsReleaseAdmissionSlot(t *testing.T) {
	for _, test := range []struct {
		name     string
		chunk    string
		status   int
		category FailureCategory
	}{
		{
			name: "body-timeout", chunk: "1\r\nx\r\n",
			status: http.StatusGatewayTimeout, category: FailureDeadlineExceeded,
		},
		{
			name: "over-limit-then-stall", chunk: "9\r\n123456789\r\n",
			status: http.StatusBadRequest, category: FailureApplicationProtocolViolation,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, admission := rawNetworkServer(t)
			connection := rawNetworkConnection(t, server)
			invocation := rawNetworkInvocation(t, 200*time.Millisecond)
			request := rawAdmissionHTTPRequest(t, server.URL, TransportRawBodyV1, invocation, nil)
			// Deliberately never finish the chunked body. A response must not
			// wait for net/http to drain the remaining body after rejection.
			_, err := fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/octet-stream\r\n%s: %s\r\nTransfer-Encoding: chunked\r\n\r\n%s",
				IngressPath, request.URL.Host, RawContextHeader, request.Header.Get(RawContextHeader), test.chunk)
			if err != nil {
				t.Fatal(err)
			}
			response, body := rawNetworkResponse(t, bufio.NewReader(connection))
			if response.StatusCode != test.status {
				t.Fatalf("stalled body status=%d, want %d; body=%s", response.StatusCode, test.status, body)
			}
			assertPlatformFailureCategory(t, body, test.category)
			if created, _ := admission.counts(); created != 0 {
				t.Fatal("an incomplete or oversized body reached Admission")
			}

			// Keep the failed peer open. A separate good peer must obtain the
			// only admission slot and finish without waiting on that peer.
			good := rawNetworkConnection(t, server)
			rawNetworkWriteComplete(t, server, good, rawNetworkInvocation(t, 2*time.Second))
			completed, result := rawNetworkResponse(t, bufio.NewReader(good))
			if completed.StatusCode != http.StatusOK || !bytes.Equal(result, []byte("ok")) {
				t.Fatalf("good request after rejection failed: status=%d body=%s", completed.StatusCode, result)
			}
			if created, _ := admission.counts(); created != 1 {
				t.Fatalf("admitted requests=%d, want one good request", created)
			}
		})
	}
}

func TestRawBodyNetworkSuccessClearsReadDeadlineForKeepAlive(t *testing.T) {
	server, admission := rawNetworkServer(t)
	connection := rawNetworkConnection(t, server)
	reader := bufio.NewReader(connection)
	first := rawNetworkInvocation(t, 300*time.Millisecond)
	rawNetworkWriteComplete(t, server, connection, first)
	response, body := rawNetworkResponse(t, reader)
	if response.StatusCode != http.StatusOK || response.Close || !bytes.Equal(body, []byte("ok")) {
		t.Fatalf("first keep-alive response status=%d close=%v body=%s", response.StatusCode, response.Close, body)
	}
	// The first request's read deadline must not affect a later request on
	// this exact TCP connection after that deadline has elapsed.
	time.Sleep(time.Until(first.DeadlineAt) + 30*time.Millisecond)
	rawNetworkWriteComplete(t, server, connection, rawNetworkInvocation(t, 2*time.Second))
	response, body = rawNetworkResponse(t, reader)
	if response.StatusCode != http.StatusOK || !bytes.Equal(body, []byte("ok")) {
		t.Fatalf("reused connection status=%d body=%s", response.StatusCode, body)
	}
	if created, _ := admission.counts(); created != 2 {
		t.Fatalf("admitted keep-alive requests=%d, want two", created)
	}
}

func rawNetworkServer(t *testing.T) (*httptest.Server, *admissionFake) {
	t.Helper()
	output, err := json.Marshal(ApplicationWireResponseV1{
		Kind: ApplicationWireResponseKindV1, Status: http.StatusOK,
		Headers: []ResponseHeaderV1{{Name: "content-type", Value: "application/octet-stream"}},
		Body:    rawAdmissionBody([]byte("ok")),
	})
	if err != nil {
		t.Fatal(err)
	}
	admission := &admissionFake{create: func(context.Context, execution.CreateRunRequest) (execution.Admission, error) {
		return execution.Admission{Run: state.Run{ID: "synthetic-network-run", State: state.RunSucceeded, Output: output}}, nil
	}}
	limits := testLimits(3 * time.Second)
	limits.MaxRequestBytes = 8
	limits.Transport = TransportRawBodyV1
	handler, err := NewHandler(resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
		return validResolvedAdmission(), nil
	}), admission, limits)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := NewListener(handler, ListenerOptions{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(listener)
	t.Cleanup(server.Close)
	return server, admission
}

func rawNetworkConnection(t *testing.T, server *httptest.Server) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return connection
}

func rawNetworkInvocation(t *testing.T, wait time.Duration) OpaqueHTTPInvocationV1 {
	t.Helper()
	return invocationValue(t, func(invocation *OpaqueHTTPInvocationV1) {
		invocation.DeadlineAt = invocation.ReceivedAt.Add(wait)
	})
}

func rawNetworkWriteComplete(t *testing.T, server *httptest.Server, connection net.Conn, invocation OpaqueHTTPInvocationV1) {
	t.Helper()
	request := rawAdmissionHTTPRequest(t, server.URL, TransportRawBodyV1, invocation, []byte("ok"))
	if err := request.Write(connection); err != nil {
		t.Fatal(err)
	}
}

func rawNetworkResponse(t *testing.T, reader *bufio.Reader) (*http.Response, []byte) {
	t.Helper()
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("read bounded raw-body response: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, body
}

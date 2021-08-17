package prober

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

type fakeReader struct{}

func (r fakeReader) Read(b []byte) (int, error) {
	return 0, io.EOF
}

type fakeClient struct {
	dofunc func(req *http.Request) (*http.Response, error)
}

var okClient fakeClient = fakeClient{
	dofunc: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Status:     "200",
			Body:       io.NopCloser(&fakeReader{}),
		}, nil
	},
}

var notFoundClient fakeClient = fakeClient{
	dofunc: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Status:     "404",
			Body:       io.NopCloser(&fakeReader{}),
		}, nil
	},
}

var errorClient fakeClient = fakeClient{
	dofunc: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("some error")
	},
}

func (c fakeClient) Do(req *http.Request) (*http.Response, error) {
	time.Sleep(10000)
	return c.dofunc(req)
}

func TestHTTPPingClient(t *testing.T) {
	for i, tc := range []struct {
		name string
		err  error
		c    Client
	}{
		{
			name: "ok",
			err:  nil,
			c:    okClient,
		},
		{
			name: "not found",
			err:  errors.New("some error"),
			c:    notFoundClient,
		},
		{
			name: "error",
			err:  errors.New("some error"),
			c:    errorClient,
		},
	} {
		c := NewHTTPPingProber(tc.c)
		_, err := c.Probe(context.TODO(), url.URL{})
		if (tc.err == nil) != (err == nil) {
			t.Errorf("%d (%s): got = %v, expected = %v", i, tc.name, err, tc.err)
		}
	}
}

func TestHTTPClient(t *testing.T) {
	for i, tc := range []struct {
		name string
		err  error
		c    Client
	}{
		{
			name: "ok",
			err:  nil,
			c:    okClient,
		},
		{
			name: "not found",
			err:  nil,
			c:    notFoundClient,
		},
		{
			name: "error",
			err:  errors.New("some error"),
			c:    errorClient,
		},
	} {
		c := NewHTTPProber(tc.c)
		_, err := c.Probe(context.TODO(), url.URL{})
		if (tc.err == nil) != (err == nil) {
			t.Errorf("%d (%s): got = %v, expected = %v", i, tc.name, err, tc.err)
		}
	}
}

package prober

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"
)

type Prober interface {
	Probe(context.Context, url.URL) (time.Duration, error)
	String() string
}

// The Client interface's purpose is to enable easy testing of Probers by consuming a simplified interface
// instead of a http.Client.
type Client interface {
	Do(*http.Request) (*http.Response, error)
}

// NoProber implements the Prober interface.
// NoProber will always return an error and a large duration.
type NoProber struct{}

func (p *NoProber) Probe(ctx context.Context, u url.URL) (time.Duration, error) {
	dur := time.Duration(1<<63 - 1)
	return dur, errors.New("this is no Probe")
}

func (p *NoProber) String() string {
	return "no-Prober"
}

// HTTPPingProber implements the Prober interface.
type HTTPPingProber struct {
	c Client
}

// NewHTTPPingProber returns a HTTPProber.
// It is safe to use concurrently.
func NewHTTPPingProber(c Client) *HTTPPingProber {
	return &HTTPPingProber{c: c}
}

func (p *HTTPPingProber) Probe(ctx context.Context, u url.URL) (time.Duration, error) {
	u.Path = "ping"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	start := time.Now()
	var dur time.Duration
	var res *http.Response
	if res, err = p.c.Do(req); err != nil {
		return 0, fmt.Errorf("failed to make request to %s: %w", u.String(), err)
	}
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("expected status Code 200, got %d", res.StatusCode)
	}
	dur = time.Now().Sub(start)
	if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
		log.Printf("failed to discard body: %v\n", err)
	}
	if err := res.Body.Close(); err != nil {
		log.Printf("failed to close body: %v\n", err)
	}
	return dur, nil
}

func (p *HTTPPingProber) String() string {
	return "http-ping-prober"
}

// HTTPProber implements the Prober interface.
type HTTPProber struct {
	c Client
}

// NewHTTPProber returns a HTTPProber.
// It is safe to use concurrently.
func NewHTTPProber(c Client) *HTTPProber {
	return &HTTPProber{c: c}
}

func (p *HTTPProber) Probe(ctx context.Context, u url.URL) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	start := time.Now()
	var dur time.Duration
	var res *http.Response
	if res, err = p.c.Do(req); err != nil {
		return 0, fmt.Errorf("failed to make request to %s: %w", u.String(), err)
	}
	dur = time.Now().Sub(start)
	if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
		log.Printf("failed to discard body: %v\n", err)
	}
	if err := res.Body.Close(); err != nil {
		log.Printf("failed to close body: %v\n", err)
	}
	return dur, nil
}

func (p *HTTPProber) String() string {
	return "http-prober"
}

// TCPProber implements the Prober interface.
type TCPProber struct {
	dialer net.Dialer
}

// NewTCPProber returns a new TCPProber.
// It is safe to use it concurrently because
// it only contains a net.Dialer.
func NewTCPProber() *TCPProber {
	return &TCPProber{
		dialer: net.Dialer{},
	}
}

// TCPProber tries to establish a TCP connection to the host and port of the given URL.
// If TCPProber receives an ECONNRESET, it will return no error and use the time between the initial
// attempt to establish the connection and the reception of the TCP RST signal.
func (p TCPProber) Probe(ctx context.Context, u url.URL) (dur time.Duration, err error) {
	start := time.Now()
	var conn net.Conn
	sysErr := &os.SyscallError{}
	if conn, err = p.dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%s", u.Hostname(), u.Port())); err == nil {
		defer conn.Close()
	} else if errors.As(err, &sysErr) && sysErr.Err == syscall.ECONNRESET {
		log.Printf("Received ECONNRESET from %s, continuing\n", u.String())
	} else {
		return 0, fmt.Errorf("failed to establish a TCP connection with %s: %w", fmt.Sprintf("%s:%s", u.Hostname(), u.Port()), err)
	}
	dur = time.Now().Sub(start)
	return dur, nil
}

func (p TCPProber) String() string {
	return "tcp-prober"
}

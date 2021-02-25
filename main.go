package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// possible values for the output that will be printed to the terminal
const (
	standard format = iota
	fancy
	simple

	naIP = "na"
)

var (
	srv         *string = flag.String("srv", "_service._proto.exmaple.com", "the srv record name to be used to look up IP addresses and port")
	listenAddr  *string = flag.String("listen-address", ":3000", "The service will be listening to that address with port\ne.g. 172.0.0.1:3000")
	metricsAddr *string = flag.String("metrics-address", ":9090", "The metrics server will be listening to that address with port\ne.g. 172.0.0.1:9090")
)

var (
	httpVectorClient = http.Client{
		Timeout: 10 * time.Second,
	}
	httpPingClient = http.Client{
		Timeout: 5 * time.Second,
	}
)

var (
	requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "The number of received http request",
		},
		[]string{"handler", "method"},
	)
	errorCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "errors_total",
			Help: "The total number of errors",
		},
	)
)

type Latency struct {
	Destination string        `json:"destination"`
	IP          string        `json:"ip,omitempty"`
	Host        string        `json:"host,omitempty"`
	Duration    time.Duration `json:"duration"`
	Ok          bool          `json:"ok"`
}

type Vector struct {
	Source    string    `json:"source"`
	IP        string    `json:"ip,omitempty"`
	Host      string    `json:"host,omitempty"`
	Latencies []Latency `json:"latencies,omitempty"`
	Ok        bool      `json:"ok"`
}

type matrix []Vector

type format int

// In case some nodes get different
// dns resolution, fill matrix with dummy entries, so entries
// within a row or column still have the same source/destination.
func (m matrix) Pad() matrix {
	for _, lats := range m {
		sort.Slice(lats.Latencies, func(i, j int) bool {
			return lats.Latencies[i].Destination < lats.Latencies[j].Destination
		})
	}
	sort.Slice(m, func(i, j int) bool {
		return m[i].Source < m[j].Source
	})
	var urlsH, urlsV []string
	urlsVM := make(map[string]struct{})
	// Find all different urls in the rows.
	for _, v := range m {
		urlsV = append(urlsV, v.Source)
		for _, l := range v.Latencies {
			urlsVM[l.Destination] = struct{}{}
		}
	}
	// Create a slice to be able to order the urls.
	for u := range urlsVM {
		urlsH = append(urlsH, u)
	}
	sort.Slice(urlsH, func(i, j int) bool {
		return urlsH[i] < urlsH[j]
	})

	nm := make(matrix, len(urlsV))
	for k, v := range m {
		nV := v
		nV.Latencies = make([]Latency, len(urlsH))
		// Find the missing url in the row
		// and insert dummies.
		offset := 0
		for i, u := range urlsH {
			if i < len(v.Latencies)+offset && v.Latencies[i-offset].Destination == u {
				nV.Latencies[i] = v.Latencies[i-offset]
				continue
			}
			offset++
			nV.Latencies[i].Destination = "dummy"
			nV.Latencies[i].Ok = true
		}
		nm[k] = nV
	}
	return nm
}

func ipOrHost(ip, host string) string {
	if ip != naIP {
		return ip
	}
	return host
}

func (m matrix) String(f format) string {
	tableString := &strings.Builder{}
	table := tablewriter.NewWriter(tableString)
	var data [][]string
	switch f {
	case fancy:
		line := []string{"Source\\Dest"}
		for _, v := range m {
			line = append(line, ipOrHost(v.IP, v.Host))
		}
		table.SetHeader(line)
		line = []string{}
		for _, v := range m {
			line = []string{ipOrHost(v.IP, v.Host)}
			for _, l := range v.Latencies {
				line = append(line, fmt.Sprintf("%s code:%t", l.Duration.String(), l.Ok))
			}
			data = append(data, line)
		}
		table.SetAutoFormatHeaders(true)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	case simple:
		for _, v := range m {
			line := []string{}
			for _, l := range v.Latencies {
				line = append(line, l.Duration.String())
			}
			data = append(data, line)
		}
		table.SetCenterSeparator("")
		table.SetColumnSeparator("")
		table.SetRowSeparator("")
		table.SetHeaderLine(false)
		table.SetBorder(false)
	default:
		for _, v := range m {
			line := []string{}
			for _, l := range v.Latencies {
				line = append(line, fmt.Sprintf("%s code:%t", l.Duration.String(), l.Ok))
			}
			data = append(data, line)
		}
		table.SetCenterSeparator("")
		table.SetColumnSeparator("")
		table.SetRowSeparator("")
		table.SetHeaderLine(false)
		table.SetBorder(false)
	}
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding(" ")
	table.AppendBulk(data)
	table.Render()
	return tableString.String()
}

func timeHTTPRequest(ctx context.Context, u *url.URL) *Latency {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		log.Printf("failed to create Request: %v\n", err)
		errorCounter.Inc()
		return &Latency{Host: u.Hostname(), Destination: u.String()}
	}
	start := time.Now()
	var dur time.Duration
	var res *http.Response
	if res, err = httpPingClient.Do(req); err != nil {
		defer res.Body.Close()
		log.Printf("failed to make ping request: %v\n", err)
		// set the time to almost infinity
		dur = time.Duration(1<<63 - 1)
		if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
			log.Printf("failed to discard body: %v\n", err)
		}
	} else {
		dur = time.Now().Sub(start)
	}
	// Try to get IP address of target
	// Shadow the err, because not being able to get an IP address should not
	// overwrite the previous error and getting no error does not indicate, that
	// the fake ping request was successful
	ip := naIP
	if i, err := net.LookupIP(u.Hostname()); err == nil && len(i) > 0 {
		ip = i[0].String()
	}
	return &Latency{
		Destination: u.String(),
		Duration:    dur,
		IP:          ip,
		Ok:          err == nil,
	}
}

func getLatencies(ctx context.Context, urls []*url.URL) []*Latency {
	var wg sync.WaitGroup
	lats := make([]*Latency, len(urls))
	for i := range urls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lats[i] = timeHTTPRequest(ctx, urls[i])
		}(i)
	}
	wg.Wait()
	return lats
}

func resolveSRV(srv, path, query string) ([]*url.URL, error) {
	_, addrs, err := net.LookupSRV("", "", srv)
	if err != nil {
		return nil, err
	}
	urls := make([]*url.URL, 0, len(addrs))
	for _, addr := range addrs {
		urls = append(urls, &url.URL{
			Scheme:   "http",
			Host:     fmt.Sprintf("%s:%d", strings.TrimRight(addr.Target, "."), addr.Port),
			Path:     path,
			RawQuery: query,
		})
	}
	return urls, nil
}

func vectorHandler(defaultSRV string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := defaultSRV
		var err error
		if r.URL.Query()["srv"] != nil {
			srv, err = srvFromRequest(r)
			if err != nil {
				log.Printf("failed to parse SRV record from request: %v\n", err)
				errorCounter.Inc()
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		urls, err := resolveSRV(srv, "/ping", "")
		if err != nil {
			log.Printf("failed to resolve SRV record: %v\n", err)
			errorCounter.Inc()
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		lats := getLatencies(r.Context(), urls)
		data, err := json.Marshal(lats)
		if err != nil {
			log.Printf("failed to marshal data: %v\n", err)
			errorCounter.Inc()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(data)
	}
}

func pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

func srvFromRequest(r *http.Request) (string, error) {
	srv := r.URL.Query()["srv"][0]
	if len(strings.SplitN(srv, ".", 3)) != 3 {
		return "", errors.New("the given SRV record name does not have a valid format; it should look something like _foo._tcp.example.com")
	}
	return srv, nil
}

func getVectorFrom(ctx context.Context, url *url.URL) (*Vector, error) {
	// Try to get IP address from target
	ip := naIP
	if i, err := net.LookupIP(url.Hostname()); err == nil && len(i) > 0 {
		ip = i[0].String()
	}
	v := &Vector{
		Source: url.String(),
		Host:   url.Hostname(),
		IP:     ip,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return v, err
	}
	resp, err := httpVectorClient.Do(req)
	if err != nil {
		return v, fmt.Errorf("failed to make GET request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		if _, err := io.Copy(ioutil.Discard, resp.Body); err != nil {
			log.Printf("failed to discard body: %v\n", err)
		}
		return v, errors.New("failed to resolve SRV record")
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return v, fmt.Errorf("failed to read body: %w", err)
	}
	if err = json.Unmarshal((body), &v.Latencies); err != nil {
		return v, fmt.Errorf("response from node has wrong format: maybe it is not running this service?: %w", err)
	}
	v.Ok = true
	return v, nil
}

func collectAllHandler(srv string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		target := srv
		// The srv target will be over written, if it is specified in the url query.
		if r.URL.Query()["srv"] != nil {
			var err error
			target, err = srvFromRequest(r)
			if err != nil {
				errorCounter.Inc()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		urls, err := resolveSRV(srv, "/vector", "srv="+target)
		if err != nil {
			errorCounter.Inc()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		//getting target urls: in case some nodes are down, we can still return a complete matrix with error entries
		m := make(matrix, len(urls))
		var wg sync.WaitGroup
		for i := range urls {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				vec, err := getVectorFrom(r.Context(), urls[i])
				if err != nil {
					errorCounter.Inc()
					log.Printf("failed to get Vector from %s: %v\n", vec.Source, err)
				}
				m[i] = *vec
			}(i)
		}
		wg.Wait()
		// Pad matrix with dummies.
		m = m.Pad()
		s := ""
		if q := r.URL.Query()["format"]; q != nil {
			var f format
			switch q[0] {
			case "fancy":
				f = fancy
			case "simple":
				f = simple
			case "json":
				j, err := json.Marshal(m)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					errorCounter.Inc()
					return
				}
				w.Write([]byte(j))
				return
			default:
				f = standard
			}

			s = m.String(f)
		} else {
			s = m.String(standard)
		}
		w.Write([]byte(s))
	}
}

func metricsMiddleWare(path string, next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		requestCounter.With(prometheus.Labels{"method": r.Method, "handler": path}).Inc()
		next(w, r)
	}
}

func main() {
	flag.Parse()
	if len(strings.SplitN(*srv, ".", 3)) != 3 {
		log.Printf("%q is not a valid srv record name\n", *srv)
		return
	}
	r := prometheus.NewRegistry()
	r.MustRegister(
		errorCounter,
		requestCounter,
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
	m := http.NewServeMux()
	mm := http.NewServeMux()
	mm.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{}))
	m.HandleFunc("/vector", metricsMiddleWare("/vector", vectorHandler(*srv)))
	m.HandleFunc("/ping", metricsMiddleWare("/ping", pingHandler))
	m.HandleFunc("/", metricsMiddleWare("/", collectAllHandler(*srv)))
	go http.ListenAndServe(*metricsAddr, mm)
	log.Printf("listening on %s\n", *listenAddr)
	log.Fatal(http.ListenAndServe(*listenAddr, m))
}

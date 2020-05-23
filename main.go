package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	flag "github.com/spf13/pflag"
)

type Latency struct {
	Destination string        `json:"destination"`
	Duration    time.Duration `json:"duration"`
	Ok          bool          `json:"ok"`
}

type Vector struct {
	Source    string    `json:"source"`
	Latencies []Latency `json:"latencies,omitempty"`
	Ok        bool      `json:"ok"`
}

type matrix []Vector

func (m matrix) String() string {
	s := ""
	for _, v := range m {
		for _, l := range v.Latencies {
			s += fmt.Sprintf("%s code:%t\t", l.Duration.String(), l.Ok)
		}
		s += "\n"
	}
	return s
}

func timeHTTPRequest(ctx context.Context, url string) *Latency {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &Latency{Destination: url}
	}
	start := time.Now()
	if _, err := http.DefaultClient.Do(req); err != nil {
		fmt.Printf("failed to make ping request: %v\n", err)
	}
	end := time.Now()
	return &Latency{
		Destination: url,
		Duration:    end.Sub(start),
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
			lats[i] = timeHTTPRequest(ctx, urls[i].String())
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
		fmt.Println(urls[len(urls)-1].String())
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
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		urls, err := resolveSRV(srv, "/ping", "")
		if err != nil {
			fmt.Println(err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		lats := getLatencies(r.Context(), urls)
		data, err := json.Marshal(lats)
		if err != nil {
			fmt.Println(err)
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
	v := &Vector{
		Source: url.String(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return v, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return v, fmt.Errorf("failed to make GET request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
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
		//srv target will be over written, if it is specified in the url query
		if r.URL.Query()["srv"] != nil {
			var err error
			target, err = srvFromRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		urls, err := resolveSRV(srv, "/vector", "srv="+target)
		if err != nil {
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
					fmt.Printf("failed to get Vector from %s: %v\n", vec.Source, err)
				}
				m[i] = *vec
			}(i)
		}
		wg.Wait()
		for _, lats := range m {
			sort.Slice(lats.Latencies, func(i, j int) bool {
				return lats.Latencies[i].Destination < lats.Latencies[j].Destination
			})
		}
		sort.Slice(m, func(i, j int) bool {
			return m[i].Source < m[j].Source
		})
		if r.URL.Query()["json"] != nil && r.URL.Query()["json"][0] == "true" {
			j, err := json.Marshal(m)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Write([]byte(j))
			return
		}
		w.Write([]byte(m.String()))
	}
}

var srv *string = flag.String("srv", "_service._proto.exmaple.com", "the srv record name to be used to look up IP addresses and port")
var listenAddr *string = flag.String("listen-address", ":3000", "The service will be listening to that address with port\ne.g. 172.0.0.1:3000")

func main() {
	flag.Parse()
	if len(strings.SplitN(*srv, ".", 3)) != 3 {
		fmt.Printf("%q is not a valid srv record name\n", *srv)
		return
	}
	m := http.NewServeMux()
	m.HandleFunc("/vector", vectorHandler(*srv))
	m.HandleFunc("/ping", pingHandler)
	m.HandleFunc("/", collectAllHandler(*srv))
	fmt.Printf("listening on %s\n", *listenAddr)
	http.ListenAndServe(*listenAddr, m)
}

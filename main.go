package main

import (
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
	Latencies []Latency `json:"latencies"`
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

func timeHTTPRequest(url string) *Latency {
	start := time.Now()
	_, err := http.Get(url)
	end := time.Now()
	return &Latency{
		Destination: url,
		Duration:    end.Sub(start),
		Ok:          err == nil,
	}
}

func getLatencies(urls []*url.URL) []*Latency {
	var wg sync.WaitGroup
	lats := make([]*Latency, len(urls))
	for i := range urls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lats[i] = timeHTTPRequest(urls[i].String())
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
			fmt.Println("using new srv in vector handler: " + srv)
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
		lats := getLatencies(urls)
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
		return "", errors.New("the given srv record name has no valid format. It should look something like _foo._tcp.foo.com\n")
	}
	return srv, nil
}

func getLatenciesFrom(url *url.URL) ([]Latency, error) {
	resp, err := http.Get(url.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, errors.New("srv does not resolve\n")
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var lats []Latency
	err = json.Unmarshal((body), &lats)
	if err != nil {
		return nil, errors.New("response from node has wrong format: Maybe it is not running this service?\n")
	}
	return lats, nil
}

func collectAllHandler(srv string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		srv_target := srv
		//srv target will be over written, if it is specified in the url query
		if r.URL.Query()["srv"] != nil {
			var err error
			srv_target, err = srvFromRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		urls, err := resolveSRV(srv, "/vector", "srv="+srv_target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		//getting target urls: in case some nodes are down, we can still return a complete matrix with error entries
		var tURLs []*url.URL
		if srv_target != srv {
			var err error
			tURLs, err = resolveSRV(srv, "", "")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			tURLs = urls
		}
		var lMatrix matrix
		for _, u := range urls {
			fmt.Println(u)
			vec, err := getLatenciesFrom(u)
			if err != nil {
				for i := 0; i < len(tURLs); i++ {
					vec = append(vec, Latency{
						Destination: tURLs[i].String(),
						Duration:    0,
						Ok:          false,
					})
				}
			}
			lVector := Vector{u.String(), vec}
			lMatrix = append(lMatrix, lVector)
		}
		for _, lats := range lMatrix {
			sort.Slice(lats.Latencies, func(i, j int) bool {
				return lats.Latencies[i].Destination < lats.Latencies[j].Destination
			})
		}
		sort.Slice(lMatrix, func(i, j int) bool {
			return lMatrix[i].Source < lMatrix[j].Source
		})
		if r.URL.Query()["json"] != nil && r.URL.Query()["json"][0] == "true" {

			j, _ := json.Marshal(lMatrix)
			w.Write([]byte(j))
		} else {
			w.Write([]byte(lMatrix.String()))
		}
	}
}

var srv *string = flag.String("srv", "_test._tcp.leon.squat.ai", "the srv record name to be used to look up IP addresses and port")
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

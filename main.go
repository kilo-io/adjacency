package main

import (
	"encoding/json"
	"fmt"
	flag "github.com/spf13/pflag"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type LatencyMap struct {
	Ip      string
	Latency time.Duration
}

type LatencyMapVector struct {
	Ip               string
	LatencyMapVector []LatencyMap
}

func timeHTTPRequest(url string, ch chan LatencyMap) {
	start := time.Now()
	resp, err := http.Get(url)
	defer resp.Body.Close()
	end := time.Now()
	diff := end.Sub(start)
	lat := new(LatencyMap)
	lat.Ip = url
	lat.Latency = diff
	if err != nil {
		lat.Latency = 1 << 31
	}

	ch <- *lat
}

func getAdjacencyVectorHTTP(urls []*url.URL) []LatencyMap {
	ch := make(chan LatencyMap)
	for _, u := range urls {
		go timeHTTPRequest(u.String(), ch)
	}
	var lats []LatencyMap
	for range urls {
		lat := <-ch
		lats = append(lats, lat)
	}
	return lats
}

func srv2url(srvs []string, path string) ([]*url.URL, error) {
	_, addrs, err := net.LookupSRV(strings.TrimLeft(srvs[0], "_"), strings.TrimLeft(srvs[1], "_"), srvs[2])
	if err != nil {
		return nil, err
	}
	urls := make([]*url.URL, 0, len(addrs))
	for _, srv := range addrs {
		urls = append(urls, &url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%d", strings.TrimRight(srv.Target, "."), srv.Port),
			Path:   path,
		})
	}
	return urls, nil
}

func vectorHandler(srv []string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		urls, err := srv2url(srv, "/ping")
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		lats := getAdjacencyVectorHTTP(urls)
		data, err := json.Marshal(lats)
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.Write(data)
	}
}

func pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

func getVectorFrom(url *url.URL) ([]LatencyMap, error) {
	resp, err := http.Get(url.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var lats []LatencyMap
	err = json.Unmarshal((body), &lats)
	if err != nil {
		return nil, err
	}
	return lats, nil
}

func getAdjacencyMatrix(srv []string) ([]LatencyMapVector, error) {
	urls, err := srv2url(srv, "/vector")
	if err != nil {
		return nil, err
	}
	var lMatrix []LatencyMapVector
	for _, u := range urls {
		vec, err := getVectorFrom(u)
		if err != nil {
			return nil, err
		}

		lVector := LatencyMapVector{u.String(), vec}
		lMatrix = append(lMatrix, lVector)
	}
	for _, lats := range lMatrix {
		sort.Slice(lats.LatencyMapVector, func(i, j int) bool {
			return lats.LatencyMapVector[i].Ip < lats.LatencyMapVector[j].Ip
		})
	}
	sort.Slice(lMatrix, func(i, j int) bool {
		return lMatrix[i].Ip < lMatrix[j].Ip
	})
	return lMatrix, nil
}

func AdjacencyMatrix2String(m []LatencyMapVector) string {
	s := ""
	for _, v := range m {
		for _, l := range v.LatencyMapVector {
			s += l.Latency.String()
			s += "\t"
		}
		s += "\n"
	}
	return s
}

func collectAllHandler(srv []string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		m, err := getAdjacencyMatrix(srv)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			w.Write([]byte("\nIt is likely, that one of the nodes is not running this service or some other web service is listening to the same port.\n"))
			return
		}
		w.Write([]byte(AdjacencyMatrix2String(m)))
	}
}

var srvRecord *string = flag.String("srv", "_test._tcp.leon.squat.ai", "the srv record name to be used to look up IP addresses and port")
var listenAddr *string = flag.String("listen-address", ":3000", "The service will be listening to that address with port\ne.g. 172.0.0.1:3000")

func main() {
	flag.Parse()
	srv := strings.SplitN(*srvRecord, ".", 3)
	if len(srv) != 3 {
		fmt.Printf("%q is not a valid srv record name\n", *srvRecord)
		return
	}
	m := http.NewServeMux()
	m.HandleFunc("/vector", vectorHandler(srv))
	m.HandleFunc("/ping", pingHandler)
	m.HandleFunc("/", collectAllHandler(srv))
	fmt.Printf("listening on %s\n", *listenAddr)
	http.ListenAndServe(*listenAddr, m)
}

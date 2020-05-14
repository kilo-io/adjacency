package main

import (
	"encoding/json"
	"errors"
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
	Ip             string
	Latency        time.Duration
	HttpStatusCode int
}

type LatencyMapVector struct {
	Ip               string
	LatencyMapVector []LatencyMap
}

func timeHTTPRequest(url string, ch chan LatencyMap) {
	start := time.Now()
	resp, err := http.Get(url)
	end := time.Now()
	diff := end.Sub(start)
	lat := new(LatencyMap)
	lat.Ip = url
	lat.Latency = diff
	if err != nil {
		lat.HttpStatusCode = 403
	} else {
		lat.HttpStatusCode = resp.StatusCode
	}
	ch <- *lat
}

func getAdjacencyVectorHTTP(urls []*url.URL) []LatencyMap {
	ch := make(chan LatencyMap)
	for _, u := range urls {
		go timeHTTPRequest(u.String(), ch)
	}
	var lats []LatencyMap
	for _, u := range urls {
		fmt.Println("url:" + u.String())
		lat := <-ch
		lats = append(lats, lat)
	}
	return lats
}

func srv2url(srvs []string, path string, query string) ([]*url.URL, error) {
	_, addrs, err := net.LookupSRV(strings.TrimLeft(srvs[0], "_"), strings.TrimLeft(srvs[1], "_"), srvs[2])
	if err != nil {
		return nil, err
	}
	urls := make([]*url.URL, 0, len(addrs))
	for _, srv := range addrs {
		urls = append(urls, &url.URL{
			Scheme:   "http",
			Host:     fmt.Sprintf("%s:%d", strings.TrimRight(srv.Target, "."), srv.Port),
			Path:     path,
			RawQuery: query,
		})
	}
	return urls, nil
}

func vectorHandler(srv []string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		srv_target := srv
		var err error
		if r.URL.Query()["srv"] != nil {
			srv_target, err = parse_SRV(r)
			fmt.Println("using new srv in vector handler: " + strings.Join(srv_target, "."))
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(fmt.Sprintf("%s", err)))
				return
			}
		}
		urls, err := srv2url(srv_target, "/ping", "")
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Println("could not lookup srv name")
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
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, errors.New("srv does not resolve\n")
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var lats []LatencyMap
	err = json.Unmarshal((body), &lats)
	if err != nil {
		return nil, errors.New("response from node has wrong format: Maybe is not running this service?\n")
	}
	return lats, nil
}

func getAdjacencyMatrix(srv, srv_target []string) ([]LatencyMapVector, error) {
	query := "srv=" + strings.Join(srv_target, ".")
	fmt.Println(query)
	urls, err := srv2url(srv, "/vector", query)
	if err != nil {
		return nil, err
	}
	var lMatrix []LatencyMapVector
	for _, u := range urls {
		fmt.Println(u)
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
			s += fmt.Sprintf("%s code:%d\t", l.Latency.String(), l.HttpStatusCode)
		}
		s += "\n"
	}
	return s
}
func parse_SRV(r *http.Request) ([]string, error) {
	query := r.URL.Query()
	var srv_target []string
	if query["srv"] != nil {
		srv_target = strings.SplitN(query["srv"][0], ".", 3)
		if len(srv_target) != 3 {

			return nil, errors.New("the given srv record name has no valid format. It should look something like _foo._tcp.foo.com\n")
		}
	} else {
		return nil, errors.New("no argument srv in query")
	}
	return srv_target, nil
}

func collectAllHandler(srv []string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		srv_target := srv
		var err error
		if r.URL.Query()["srv"] != nil {
			srv_target, err = parse_SRV(r)
			if err != nil {
				w.Write([]byte(fmt.Sprintf("%s", err)))
				return
			}
		}
		m, err := getAdjacencyMatrix(srv, srv_target)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
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

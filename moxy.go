/* moxy - The Marathon+Mesos http reverse proxy - code by <benjamin.martensson@nrk.no> */
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/mailgun/oxy/forward"
	"github.com/mailgun/oxy/roundrobin"
	"github.com/thoas/stats"
)

type App struct {
	Tasks []string
	Fwd   *forward.Forwarder     `json:"-"`
	Lb    *roundrobin.RoundRobin `json:"-"`
}
type Apps map[string]App

var apps Apps

type Config struct {
	Port string
	TLS  bool
	Cert string
	Key  string
}

func moxy_proxy(w http.ResponseWriter, r *http.Request) {
	// let us forward this request to a running container
	app := strings.Split(r.Host, ".")[0]
	if s, ok := apps[app]; ok {
		s.Lb.ServeHTTP(w, r)
		return
	}
	fmt.Fprintln(w, "moxy")
}

func moxy_callback(w http.ResponseWriter, r *http.Request) {
	log.Println("callback received from Marathon")
	select {
	case callbackqueue <- true: // Add reload to our queue channel, unless it is full of course.
		w.WriteHeader(202)
		fmt.Fprintln(w, "queued")
		return
	default:
		w.WriteHeader(202)
		fmt.Fprintln(w, "queue is full")
		return
	}
}

func moxy_apps(w http.ResponseWriter, r *http.Request) {
	b, _ := json.MarshalIndent(apps, "", "  ")
	w.Write(b)
	return
}

func main() {
	configtoml := flag.String("f", "moxy.toml", "Path to config. (default moxy.toml)")
	flag.Parse()
	file, err := ioutil.ReadFile(*configtoml)
	if err != nil {
		log.Fatal(err)
	}
	var config Config
	err = toml.Unmarshal(file, &config)
	if err != nil {
		log.Fatal("Problem parsing config: ", err)
	}
	moxystats := stats.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/moxy_callback", moxy_callback)
	mux.HandleFunc("/moxy_apps", moxy_apps)
	mux.HandleFunc("/moxy_stats", func(w http.ResponseWriter, req *http.Request) {
		stats := moxystats.Data()
		b, _ := json.MarshalIndent(stats, "", "  ")
		w.Write(b)
		return
	})
	mux.HandleFunc("/", moxy_proxy)
	// In case we want to log req/resp.
	//trace, _ := trace.New(redirect, os.Stdout)
	handler := moxystats.Handler(mux)
	s := &http.Server{
		Addr:    ":" + config.Port,
		Handler: handler,
	}
	callbackworker()
	callbackqueue <- true
	if config.TLS {
		log.Println("Starting moxy tls on :" + config.Port)
		err := s.ListenAndServeTLS(config.Cert, config.Key)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Println("Starting moxy on :" + config.Port)
		err := s.ListenAndServe()
		if err != nil {
			log.Fatal(err)
		}
	}
}

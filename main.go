// Package main implements a service that queries jaeger-query and
// returns data suitable for use by the Grafana SimpleJSON plugin.
package main // import "github.com/QubitProducts/jaeger-simplejson"
import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tcolgate/grafanasj"
)

var addr string
var baseURL string
var linkURL string

func init() {
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&baseURL, "baseURL", "", "api URL")
	flag.StringVar(&linkURL, "linkURL", "", "external jaeger UI URL")
}

type jaegerSJHandler struct {
	base    *url.URL
	linkURL *url.URL
}

func (jh *jaegerSJHandler) GrafanaQuery(from, to time.Time, interval time.Duration, maxDPs int, targets []string) (map[string][]grafanasj.Data, error) {
	return nil, nil
}

type traceTag struct {
	Key   string      `json:"key"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

type traceSpan struct {
	StartTime     int64      `json:"startTime"`
	OperationName string     `json:"operationName"`
	TraceID       string     `json:"traceID"`
	Tags          []traceTag `json:"tags"`
}

type traceResp struct {
	TraceID string      `json:"traceID"`
	Spans   []traceSpan `json:"spans"`
}

func (jh *jaegerSJHandler) GrafanaAnnotations(from, to time.Time, query string) ([]grafanasj.Annotation, error) {
	req, err := url.Parse(fmt.Sprintf("%s/api/traces", jh.base))
	if err != nil {
		return nil, err
	}
	q := req.Query()
	q.Set("start", fmt.Sprintf("%v", from.UnixNano()/1000))
	q.Set("end", fmt.Sprintf("%v", to.UnixNano()/1000))
	q.Set("service", query)
	req.RawQuery = q.Encode()

	log.Println("req: ", req.String())
	resp, err := http.Get(req.String())
	if err != nil {
		return nil, err
	}

	traces := struct {
		Data   []traceResp       `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}{}
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&traces)
	if err != nil {
		return nil, err
	}
	if len(traces.Errors) > 0 {
		for _, e := range traces.Errors {
			log.Printf("error: %v", string(e))
		}
		return nil, errors.New("errors in response from jaeger")
	}

	answers := []grafanasj.Annotation{}
	for i := range traces.Data {
		for j := range traces.Data[i].Spans {
			var tags []string
			for k := range traces.Data[i].Spans[j].Tags {
				tags = append(tags, fmt.Sprintf("%v:%v", traces.Data[i].Spans[j].Tags[k].Key, traces.Data[i].Spans[j].Tags[k].Value))
			}
			answers = append(answers, grafanasj.Annotation{
				Title: traces.Data[i].Spans[j].OperationName,
				Text:  fmt.Sprintf(`<a href="%v/trace/%v" target="_blank">%v</a>`, jh.linkURL, traces.Data[i].Spans[j].TraceID, traces.Data[i].Spans[j].TraceID),
				Time:  grafanasj.SimpleJSONPTime(time.Unix(0, traces.Data[i].Spans[j].StartTime*1000)),
				Tags:  tags,
			})
		}
	}
	return answers, nil
}

func (jh *jaegerSJHandler) GrafanaSearch(target string) ([]string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/services", jh.base.String()))
	if err != nil {
		log.Printf("url err %v", err)
		return nil, err
	}

	services := struct {
		Data   []string          `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}{}
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&services)
	if err != nil {
		log.Printf("json err %v", err)
		return nil, err
	}
	log.Printf("got: %v", services)

	answers := []string{}
	for _, s := range services.Data {
		if strings.HasPrefix(s, target) {
			answers = append(answers, s)
		}
	}
	return answers, nil
}

func main() {
	flag.Parse()

	if baseURL == "" {
		log.Fatalf("baseURL must be set")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		log.Fatalf("%v", err)
	}

	jaegersj := &jaegerSJHandler{
		base:    base,
		linkURL: base,
	}
	if linkURL != "" {
		link, err := url.Parse(linkURL)
		if err != nil {
			log.Fatalf("%v", err)
		}
		jaegersj.linkURL = link
	}

	h := grafanasj.New(jaegersj)
	http.HandleFunc("/", h.HandleRoot)
	http.HandleFunc("/query", h.HandleQuery)
	http.HandleFunc("/search", h.HandleSearch)
	http.HandleFunc("/annotations", h.HandleAnnotations)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatalf("error, %v", err)
	}
}

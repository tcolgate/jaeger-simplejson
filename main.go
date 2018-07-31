// Package main implements a service that queries jaeger-query and
// returns data suitable for use by the Grafana SimpleJSON plugin.
package main // import "github.com/QubitProducts/jaeger-simplejson"
import (
	"context"
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

type traceTag struct {
	Key   string      `json:"key"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

type traceSpan struct {
	StartTime     int64      `json:"startTime"`
	Duration      int64      `json:"duration"`
	OperationName string     `json:"operationName"`
	TraceID       string     `json:"traceID"`
	Tags          []traceTag `json:"tags"`
}

type traceResp struct {
	TraceID string      `json:"traceID"`
	Spans   []traceSpan `json:"spans"`
}

func (jh *jaegerSJHandler) traceURL(id string) string {
	return fmt.Sprintf("%v/trace/%v", jh.linkURL, id)
}

func (jh *jaegerSJHandler) runQuery(ctx context.Context, from, to time.Time, service string) ([]traceResp, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/traces", jh.base))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("start", fmt.Sprintf("%v", from.UnixNano()/1000))
	q.Set("end", fmt.Sprintf("%v", to.UnixNano()/1000))
	q.Set("service", service)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
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
	return traces.Data, nil
}

func (jh *jaegerSJHandler) GrafanaQuery(ctx context.Context, from, to time.Time, interval time.Duration, maxDPs int, target string) ([]grafanasj.Data, error) {
	return nil, nil
}

func (jh *jaegerSJHandler) GrafanaQueryTable(ctx context.Context, from, to time.Time, target string) ([]grafanasj.TableColumn, error) {
	tt, err := jh.runQuery(ctx, from, to, target)
	if err != nil {
		return nil, err
	}

	var times []interface{}
	var ids []interface{}
	var links []interface{}
	var html []interface{}
	var durs []interface{}
	var spanCounts []interface{}
	var errCounts []interface{}

	for i := range tt {
		ids = append(ids, tt[i].TraceID)
		links = append(links, jh.traceURL(tt[i].TraceID))
		html = append(html, fmt.Sprintf(`<a href="%v" target="_blank">%v</a>`, jh.traceURL(tt[i].TraceID), tt[i].TraceID))
		spanCounts = append(spanCounts, len(tt[i].Spans))

		start := int64(1<<63 - 1)
		var duration int64
		var errs int64

		ss := tt[i].Spans
		for j := range ss {
			if ss[j].StartTime < start {
				start = ss[j].StartTime
			}
			if ss[j].Duration > duration {
				duration = ss[j].Duration
			}
			for k := range ss[j].Tags {
				if ss[j].Tags[k].Key == "error" &&
					ss[j].Tags[k].Type == "bool" {
					errs++
				}
			}
		}

		times = append(times, time.Unix(0, start*1000))
		errCounts = append(errCounts, errs)
		durs = append(durs, float64(float64(duration)/1000))
	}

	res := []grafanasj.TableColumn{
		{
			Text:   "timestamp",
			Type:   "time",
			Values: times,
		},
		{
			Text:   "trace_id",
			Type:   "string",
			Values: ids,
		},
		{
			Text:   "link",
			Type:   "string",
			Values: links,
		},
		{
			Text:   "html",
			Type:   "string",
			Values: html,
		},
		{
			Text:   "duration",
			Type:   "number",
			Values: durs,
		},
		{
			Text:   "spans",
			Type:   "number",
			Values: spanCounts,
		},
		{
			Text:   "errors",
			Type:   "number",
			Values: errCounts,
		},
	}
	return res, nil
}

func (jh *jaegerSJHandler) GrafanaAnnotations(ctx context.Context, from, to time.Time, query string) ([]grafanasj.Annotation, error) {
	traces, err := jh.runQuery(ctx, from, to, query)
	if err != nil {
		return nil, err
	}

	answers := []grafanasj.Annotation{}
	for i := range traces {
		for j := range traces[i].Spans {
			var tags []string
			for k := range traces[i].Spans[j].Tags {
				tags = append(tags, fmt.Sprintf("%v:%v", traces[i].Spans[j].Tags[k].Key, traces[i].Spans[j].Tags[k].Value))
			}
			answers = append(answers, grafanasj.Annotation{
				Title: traces[i].Spans[j].OperationName,
				Text:  fmt.Sprintf(`<a href="%v/trace/%v" target="_blank">%v</a>`, jh.linkURL, traces[i].Spans[j].TraceID, traces[i].Spans[j].TraceID),
				Time:  time.Unix(0, traces[i].Spans[j].StartTime*1000),
				Tags:  tags,
			})
		}
	}
	return answers, nil
}

func (jh *jaegerSJHandler) GrafanaSearch(ctx context.Context, target string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/services", jh.base.String()), nil)
	if err != nil {
		log.Printf("url err %v", err)
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
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

# Jaeger Grafana SimpleJSON 

This service allows you to query jaeger via a Grafana SimpleJSON plugin. It
provides:

- A timeserie query that returns data points with individual trace
  latencies. Works with histograms.
- A table query that returns data and links for each trace.
- A search endpoint that queries service names used in traces.
- An annotations endpoint. 

This allows visualising trace latencies, and deep linking to traces in the
Jaeger UI from within grafana.

## Screen shot

![Screen shot](docs/Screenshot.png)


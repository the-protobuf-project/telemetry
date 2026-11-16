package loadtest

import (
	"compress/gzip"
	"io"
	"net/http"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// HTTPHandler receives OTLP/HTTP (binary protobuf, optionally gzip) on
// /v1/logs, /v1/traces and /v1/metrics and counts it exactly as the gRPC
// receiver does, for SDKs that export over HTTP — the C++ ones.
func (c *Collector) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/logs", func(w http.ResponseWriter, r *http.Request) {
		var req collogs.ExportLogsServiceRequest
		if decode(w, r, &req) {
			resp, _ := c.Export(r.Context(), &req)
			reply(w, resp)
		}
	})
	mux.HandleFunc("POST /v1/traces", func(w http.ResponseWriter, r *http.Request) {
		var req coltrace.ExportTraceServiceRequest
		if decode(w, r, &req) {
			resp, _ := traceServer{c}.Export(r.Context(), &req)
			reply(w, resp)
		}
	})
	mux.HandleFunc("POST /v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		var req colmetrics.ExportMetricsServiceRequest
		if decode(w, r, &req) {
			resp, _ := metricsServer{c}.Export(r.Context(), &req)
			reply(w, resp)
		}
	})
	return mux
}

func decode(w http.ResponseWriter, r *http.Request, msg proto.Message) bool {
	body := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return false
		}
		defer gz.Close()
		body = gz
	}
	data, err := io.ReadAll(body)
	if err == nil {
		err = proto.Unmarshal(data, msg)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, msg proto.Message) {
	data, _ := proto.Marshal(msg)
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(data)
}

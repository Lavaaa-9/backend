package telemetry

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func Setup(ctx context.Context, endpoint string) (func(context.Context) error, error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, e := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint), otlptracehttp.WithTimeout(5*time.Second))
	if e != nil {
		return nil, fmt.Errorf("telemetry exporter configuration failed")
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second), sdktrace.WithMaxQueueSize(2048)))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

type metric struct {
	Count   int64
	Seconds float64
}
type Metrics struct {
	mu     sync.Mutex
	values map[string]metric
}

func (m *Metrics) Observe(method, route string, status int, duration time.Duration) {
	method = SafeMethod(method)
	switch method {
	case "GET", "POST", "PUT", "OPTIONS", "HEAD":
	default:
		method = "OTHER"
	}
	key := "method=" + strconv.Quote(method) + ",route=" + strconv.Quote(route) + ",status=" + strconv.Quote(strconv.Itoa(status))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string]metric{}
	}
	v := m.values[key]
	v.Count++
	v.Seconds += duration.Seconds()
	m.values[key] = v
}
func (m *Metrics) Prometheus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := []string{}
	for key := range m.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("# HELP spartial_http_requests_total Requests grouped by static route, method and status.\n# TYPE spartial_http_requests_total counter\n")
	for _, key := range keys {
		fmt.Fprintf(&out, "spartial_http_requests_total{%s} %d\n", key, m.values[key].Count)
	}
	out.WriteString("# HELP spartial_http_duration_seconds_total Total request duration.\n# TYPE spartial_http_duration_seconds_total counter\n")
	for _, key := range keys {
		fmt.Fprintf(&out, "spartial_http_duration_seconds_total{%s} %g\n", key, m.values[key].Seconds)
	}
	return out.String()
}

func SafeMethod(method string) string {
	switch method {
	case "GET", "POST", "PUT", "OPTIONS", "HEAD":
		return method
	default:
		return "OTHER"
	}
}

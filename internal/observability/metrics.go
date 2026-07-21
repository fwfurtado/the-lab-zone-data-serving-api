package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics expõe a visão RED por método/plano — a metade runtime do contrato:
// o SLO declarado no DatasetContract se compara com ESTES números.
//   Rate:     rate(data_serving_request_duration_seconds_count[...])
//   Errors:   soma por label code != OK
//   Duration: histogram_quantile sobre os buckets
type Metrics struct {
	registry *prometheus.Registry
	duration *prometheus.HistogramVec
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "data_serving_request_duration_seconds",
		Help: "Duração das requests da Data API por método, tipo de plano e código gRPC.",
		// resolução concentrada onde moram os SLOs (kv: ~5ms, pinot: ~80ms);
		// o bucket define a precisão do p99 — perto do alvo, granularidade
		Buckets: []float64{
			0.001, 0.0025, 0.005, 0.0075, 0.010, 0.025,
			0.050, 0.080, 0.100, 0.250, 0.500, 1, 2,
		},
	}, []string{"method", "plan_type", "code"})
	reg.MustRegister(duration)

	return &Metrics{registry: reg, duration: duration}
}

// Observe registra uma request concluída. code é o código gRPC em string
// ("OK", "NotFound", ...); plan_type é kv_get/pinot_query/none.
func (m *Metrics) Observe(method, planType, code string, seconds float64) {
	m.duration.WithLabelValues(method, planType, code).Observe(seconds)
}

// Handler devolve o endpoint /metrics para o VMPodScrape.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

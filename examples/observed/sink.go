package main

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/go-theft-craft/server/server"
)

// The mapping.
//
// This is the part worth copying. Four sample kinds become four collector
// shapes, and the rule is the kind, not the feature:
//
//	SampleDuration → histogram, because "how long" wants quantiles
//	SampleCount    → counter, because it only ever goes up
//	SampleGauge    → gauge, because it is a level
//	SampleBytes    → counter, because bytes accumulate
//
// The label set comes from server.Labels and nothing else. That is what a
// closed label set buys: the collectors below can declare their label names up
// front, and no sample can arrive later carrying a key they did not declare.
//
// A note on cardinality, because this example is what people copy. The player
// label on a histogram is one series per player per feature per region, which
// is fine for a server with tens of players and is not fine for one with
// thousands. Drop the player label from the histogram before running this at
// that size; the region label is what answers most questions anyway, and
// server.WithChunkDetail — which this example exposes as a flag — makes it
// worse on purpose and says so.
type sink struct {
	durations *prometheus.HistogramVec
	counts    *prometheus.CounterVec
	gauges    *prometheus.GaugeVec
	network   *prometheus.CounterVec
	resources *prometheus.GaugeVec
	dropped   prometheus.Gauge
}

func newSink(reg prometheus.Registerer) *sink {
	s := &sink{
		durations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "minecraft_work_seconds",
			Help: "How long a piece of server work took.",
			// A chunk encode is hundreds of microseconds and a slow tick is
			// tens of milliseconds, so the buckets span four decades rather
			// than the default's two.
			Buckets: prometheus.ExponentialBuckets(0.0001, 3, 10),
		}, []string{server.LabelFeature, server.LabelPlayer, server.LabelRegion, server.LabelWorld}),

		counts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_events_total",
			Help: "How many times something happened.",
		}, []string{server.LabelFeature, server.LabelPlayer, server.LabelWorld}),

		gauges: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "minecraft_level",
			Help: "A level: players online, chunks resident, identified items live.",
		}, []string{server.LabelFeature, server.LabelWorld}),

		network: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_network_bytes_total",
			Help: "Bytes on the wire, counted at the frame.",
		}, []string{server.LabelDirection, server.LabelPacket}),

		resources: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "minecraft_process",
			Help: "Process CPU seconds and heap bytes.",
		}, []string{"resource"}),

		dropped: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "minecraft_samples_dropped_total",
			Help: "Samples that never reached this sink because the server's delivery queue was full.",
		}),
	}

	reg.MustRegister(s.durations, s.counts, s.gauges, s.network, s.resources, s.dropped)

	return s
}

// Observe maps one sample onto one collector.
//
// It must not block. The server delivers samples from its own goroutine and
// drops them when that goroutine falls behind, which is what the dropped gauge
// reports — so a sink that blocked would show up as its own missing data.
func (s *sink) Observe(sample server.Sample) {
	l := sample.Labels

	switch sample.Kind {
	case server.SampleDuration:
		s.durations.WithLabelValues(
			string(l.Feature), l.Player, l.Region.String(), l.World,
		).Observe(sample.Value)

	case server.SampleCount:
		s.counts.WithLabelValues(string(l.Feature), l.Player, l.World).Add(sample.Value)

	case server.SampleGauge:
		s.gauges.WithLabelValues(string(l.Feature), l.World).Set(sample.Value)

	case server.SampleBytes:
		s.counts.WithLabelValues(string(l.Feature), l.Player, l.World).Add(sample.Value)

	case server.SampleNetworkIn, server.SampleNetworkOut:
		s.network.WithLabelValues(l.Direction, l.Packet).Add(sample.Value)

	case server.SampleCPU:
		s.resources.WithLabelValues("cpu_seconds").Set(sample.Value)

	case server.SampleMemory:
		s.resources.WithLabelValues("heap_bytes").Set(sample.Value)

	case server.SampleDropped:
		s.dropped.Set(sample.Value)
	}
}

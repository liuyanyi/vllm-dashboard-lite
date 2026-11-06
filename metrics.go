package main

import (
	"fmt"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Series struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Kind   string            `json:"kind"`
	Value  float64           `json:"value"`
}
type Snapshot struct {
	At          time.Time
	StartStatus string
	Start       string
	Model       string
	Values      map[string]Series
}

func parseMetrics(r io.Reader) (Snapshot, error) {
	p := expfmt.TextParser{}
	fs, e := p.TextToMetricFamilies(r)
	s := Snapshot{At: time.Now(), Values: map[string]Series{}}
	if e != nil {
		return s, e
	}
	starts := map[string]bool{}
	seenStart := false
	invalidStart := false
	for name, f := range fs {
		for _, m := range f.Metric {
			labels := map[string]string{}
			for _, l := range m.Label {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["model_name"] != "" {
				s.Model = labels["model_name"]
			}
			add := func(n, k string, v float64, ls map[string]string) {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return
				}
				key := seriesKey(n, ls)
				s.Values[key] = Series{n, ls, k, v}
			}
			switch f.GetType() {
			case dto.MetricType_COUNTER:
				add(name, "counter", m.GetCounter().GetValue(), labels)
			case dto.MetricType_GAUGE, dto.MetricType_UNTYPED:
				v := m.GetGauge().GetValue()
				if f.GetType() == dto.MetricType_UNTYPED {
					v = m.GetUntyped().GetValue()
				}
				if name == "process_start_time_seconds" {
					seenStart = true
					if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
						invalidStart = true
					}
				}
				add(name, "gauge", v, labels)
				if name == "process_start_time_seconds" && v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
					starts[fmt.Sprintf("%.9f", v)] = true
				}
			case dto.MetricType_HISTOGRAM:
				h := m.GetHistogram()
				add(name+"_sum", "counter", h.GetSampleSum(), labels)
				add(name+"_count", "counter", float64(h.GetSampleCount()), labels)
				for _, b := range h.Bucket {
					ls := map[string]string{}
					for k, v := range labels {
						ls[k] = v
					}
					ls["le"] = fmt.Sprint(b.GetUpperBound())
					add(name+"_bucket", "counter", float64(b.GetCumulativeCount()), ls)
				}
				ls := map[string]string{}
				for k, v := range labels {
					ls[k] = v
				}
				ls["le"] = "+Inf"
				add(name+"_bucket", "counter", float64(h.GetSampleCount()), ls)
			}
		}
	}
	s.StartStatus = "absent"
	if seenStart {
		s.StartStatus = "invalid"
	}
	if len(starts) > 1 {
		s.StartStatus = "ambiguous"
	}
	if len(starts) == 1 && !invalidStart {
		s.StartStatus = "available"
		for v := range starts {
			s.Start = v
		}
	}
	return s, nil
}
func seriesKey(n string, l map[string]string) string {
	keys := []string{}
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n += "|" + k + "=" + l[k]
	}
	return n
}

type Aggregate struct {
	Name     string            `json:"name"`
	Labels   map[string]string `json:"labels"`
	Kind     string            `json:"kind"`
	Delta    float64           `json:"delta"`
	Duration float64           `json:"duration"`
	Integral float64           `json:"integral"`
	Min      float64           `json:"min"`
	Max      float64           `json:"max"`
	Last     float64           `json:"last"`
	Resets   int               `json:"resets"`
}

func accumulate(a map[string]*Aggregate, prev, cur Snapshot, maxGap float64) {
	dt := cur.At.Sub(prev.At).Seconds()
	if dt <= 0 || dt > maxGap {
		return
	}
	for key, v := range cur.Values {
		old, ok := prev.Values[key]
		if !ok {
			continue
		}
		g := a[key]
		if g == nil {
			g = &Aggregate{Name: v.Name, Labels: v.Labels, Kind: v.Kind, Min: math.Min(v.Value, old.Value), Max: math.Max(v.Value, old.Value)}
			a[key] = g
		}
		g.Last = v.Value
		g.Min = math.Min(g.Min, v.Value)
		g.Max = math.Max(g.Max, v.Value)
		if v.Kind == "counter" {
			if v.Value < old.Value {
				g.Resets++
				continue
			}
			g.Delta += v.Value - old.Value
		} else {
			g.Integral += old.Value * dt
		}
		g.Duration += dt
	}
}
func sumDelta(a map[string]*Aggregate, name string) float64 {
	v := 0.
	for _, g := range a {
		if g.Name == name {
			v += g.Delta
		}
	}
	return v
}
func ratio(a map[string]*Aggregate, n, d string) *float64 {
	den := sumDelta(a, d)
	if den == 0 {
		return nil
	}
	v := sumDelta(a, n) / den
	return &v
}
func summaries(a map[string]*Aggregate, cur Snapshot) map[string]*float64 {
	out := map[string]*float64{}
	put := func(k string, v float64) { out[k] = &v }
	for _, n := range []string{"prompt_tokens_total", "generation_tokens_total", "request_success_total"} {
		rate := 0.
		found := false
		for _, g := range a {
			if g.Name == "vllm:"+n && g.Duration > 0 {
				rate += g.Delta / g.Duration
				found = true
			}
		}
		if found {
			put(n, rate)
		}
	}
	for _, n := range []string{"num_requests_running", "num_requests_waiting", "kv_cache_usage_perc", "gpu_cache_usage_perc"} {
		v := 0.
		found := false
		for _, s := range cur.Values {
			if s.Name == "vllm:"+n {
				found = true
				if strings.Contains(n, "usage") {
					v = math.Max(v, s.Value)
				} else {
					v += s.Value
				}
			}
		}
		if found {
			put(n, v)
		}
	}
	out["prefix_hit"] = ratio(a, "vllm:prefix_cache_hits_total", "vllm:prefix_cache_queries_total")
	out["acceptance"] = ratio(a, "vllm:spec_decode_num_accepted_tokens_total", "vllm:spec_decode_num_draft_tokens_total")
	if v := ratio(a, "vllm:spec_decode_num_accepted_tokens_total", "vllm:spec_decode_num_drafts_total"); v != nil {
		put("acceptance_length", 1+*v)
	}
	for _, n := range []string{"time_to_first_token_seconds", "inter_token_latency_seconds", "request_time_per_output_token_seconds", "e2e_request_latency_seconds", "request_queue_time_seconds"} {
		base := "vllm:" + n
		out[n] = ratio(a, base+"_sum", base+"_count")
		out[n+"_p50"] = histogramQuantile(a, base, .5)
		out[n+"_p95"] = histogramQuantile(a, base, .95)
	}
	return out
}

// histogramQuantile only combines matching bucket schemas. Returns nil for an
// unbounded quantile or when engines expose different boundaries.
func histogramQuantile(a map[string]*Aggregate, base string, q float64) *float64 {
	groups := map[string]map[float64]float64{}
	for _, g := range a {
		if g.Name != base+"_bucket" || g.Duration <= 0 {
			continue
		}
		labels := map[string]string{}
		for k, v := range g.Labels {
			if k != "le" {
				labels[k] = v
			}
		}
		key := seriesKey(base, labels)
		if groups[key] == nil {
			groups[key] = map[float64]float64{}
		}
		var bound float64
		if g.Labels["le"] == "+Inf" {
			bound = math.Inf(1)
		} else {
			var err error
			bound, err = strconv.ParseFloat(g.Labels["le"], 64)
			if err != nil {
				continue
			}
		}
		groups[key][bound] += g.Delta
	}
	merged := map[float64]float64{}
	var schema map[float64]float64
	for _, buckets := range groups {
		if schema == nil {
			schema = buckets
		} else {
			if len(schema) != len(buckets) {
				return nil
			}
			for b := range schema {
				if _, ok := buckets[b]; !ok {
					return nil
				}
			}
		}
		for b, n := range buckets {
			merged[b] += n
		}
	}
	total := merged[math.Inf(1)]
	if total <= 0 {
		return nil
	}
	bounds := []float64{}
	for b := range merged {
		bounds = append(bounds, b)
	}
	sort.Float64s(bounds)
	target := q * total
	prevB, prevN := 0., 0.
	for _, b := range bounds {
		n := merged[b]
		if n >= target {
			if math.IsInf(b, 1) || n <= prevN {
				return nil
			}
			v := prevB + (b-prevB)*(target-prevN)/(n-prevN)
			return &v
		}
		prevB, prevN = b, n
	}
	return nil
}

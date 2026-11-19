// Package sim simulates a fleet of broadband satellite terminals for demos and load tests.
// The link model matches the C agent's (agent/src/linkmodel.c) so simulated and real
// agents produce comparable telemetry.
package sim

import (
	"math"
	"math/rand/v2"
	"time"

	"satfleet/ingest/internal/telemetry"
)

type LinkModel struct {
	rng             *rand.Rand
	baseLatencyMs   float64
	baseSnrDb       float64
	obstructionBase float64
	demandFactor    float64 // >1 for heavy users who outgrow their plan
	fadeDb          float64
	fadeRemaining   float64 // seconds
	silentRemaining float64 // seconds; simulates a powered-off or unreachable terminal
	DownCapMbps     float64
	UpCapMbps       float64
	LongitudeHours  float64 // offset from UTC used for the diurnal demand curve
}

// NewLinkModel derives a stable per-terminal profile from seed.
func NewLinkModel(seed uint64, downCap, upCap float64) *LinkModel {
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	m := &LinkModel{
		rng:           rng,
		baseLatencyMs: 26 + rng.Float64()*18,
		baseSnrDb:     10.5 + rng.Float64()*3,
		demandFactor:  1,
		DownCapMbps:   downCap,
		UpCapMbps:     upCap,
	}
	if r := rng.Float64(); r < 0.8 {
		m.obstructionBase = r * 3
	} else {
		m.obstructionBase = 4 + r*10
	}
	if rng.Float64() < 0.08 {
		m.demandFactor = 2.1 // heavy users: drive the sales team's upsell list
	}
	return m
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// Silent reports whether the terminal is currently not reporting at all.
func (m *LinkModel) Silent() bool { return m.silentRemaining > 0 }

// Step advances the model by dt and fills the measurement fields of r.
// It returns false while the terminal is silent (no report should be sent).
func (m *LinkModel) Step(dt time.Duration, now time.Time, r *telemetry.Report) bool {
	s := dt.Seconds()

	// Outages: ~2% chance per terminal per hour of going dark for 3-15 minutes.
	if m.silentRemaining > 0 {
		m.silentRemaining -= s
		return false
	}
	if m.rng.Float64() < s/(3600/0.02) {
		m.silentRemaining = 180 + m.rng.Float64()*720
		return false
	}

	// Rain fade: ~1 event per 3 hours, 2-12 minutes, 3-11 dB deep.
	if m.fadeRemaining > 0 {
		m.fadeRemaining -= s
		if m.fadeRemaining <= 0 {
			m.fadeDb = 0
		}
	} else if m.rng.Float64() < s/10800 {
		m.fadeDb = 3 + m.rng.Float64()*8
		m.fadeRemaining = 120 + m.rng.Float64()*600
	}

	hour := math.Mod(float64(now.UTC().Hour())+float64(now.UTC().Minute())/60+m.LongitudeHours+48, 24)
	congestion := 0.5 + 0.5*math.Cos((hour-21)/24*2*math.Pi)

	snr := clamp(m.baseSnrDb-m.fadeDb+m.rng.NormFloat64()*0.6, -2, 20)
	obstruction := clamp(m.obstructionBase+math.Abs(m.rng.NormFloat64())*0.8, 0, 100)
	penalty := math.Max(0, 6-snr)
	loss := clamp(0.05+penalty*1.4+obstruction*0.08+math.Abs(m.rng.NormFloat64())*0.05, 0, 100)
	latency := m.baseLatencyMs + 0.4 + congestion*14 + penalty*6 + math.Abs(m.rng.NormFloat64())*3
	jitter := 0.8 + math.Abs(m.rng.NormFloat64())*1.2 + penalty*0.8

	demand := clamp((0.08+0.55*congestion+m.rng.NormFloat64()*0.08)*m.demandFactor, 0.02, 1)
	capacity := clamp(1-penalty*0.15-obstruction/100, 0, 1)

	r.LatencyMs = float32(latency)
	r.JitterMs = float32(jitter)
	r.PacketLossPct = float32(loss)
	r.SnrDb, r.HasSnr = float32(snr), true
	r.ObstructionPct = float32(obstruction)
	r.DownlinkMbps = float32(m.DownCapMbps * demand * capacity)
	r.UplinkMbps = float32(m.UpCapMbps * clamp(demand*0.7+m.rng.NormFloat64()*0.05, 0.01, 1) * capacity)

	switch {
	case snr < 2:
		r.LinkState = telemetry.LinkDown
		r.DownlinkMbps, r.UplinkMbps, r.PacketLossPct = 0, 0, 100
	case loss > 2 || snr < 6 || obstruction > 15 || latency > 150:
		r.LinkState = telemetry.LinkDegraded
	default:
		r.LinkState = telemetry.LinkUp
	}
	return true
}

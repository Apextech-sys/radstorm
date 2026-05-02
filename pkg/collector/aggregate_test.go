// Package collector — unit tests for percentile math and aggregator.
//
// Purpose:
//   Pin the percentile contract (briefing requires p50≈500, p99≈990,
//   p999≈999 for input [1..1000]) and exercise the threshold-rollup +
//   curve-builder helpers in isolation, away from the IO machinery.
//
// Related files:
//   - pkg/collector/aggregate.go (subject under test)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: internal — test-only.
package collector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPercentileEmpty(t *testing.T) {
	assert.Equal(t, int64(0), percentile(nil, 0.5))
}

func TestPercentileSingle(t *testing.T) {
	assert.Equal(t, int64(42), percentile([]int64{42}, 0.5))
	assert.Equal(t, int64(42), percentile([]int64{42}, 0.999))
	assert.Equal(t, int64(42), percentile([]int64{42}, 0))
}

func TestPercentileBriefingExpectations(t *testing.T) {
	// Briefing: input [1..1000] → p50≈500, p99≈990, p999≈999.
	in := make([]int64, 1000)
	for i := range in {
		in[i] = int64(i + 1)
	}
	assert.Equal(t, int64(500), percentile(in, 0.5))
	assert.Equal(t, int64(950), percentile(in, 0.95))
	assert.Equal(t, int64(990), percentile(in, 0.99))
	assert.Equal(t, int64(999), percentile(in, 0.999))
	assert.Equal(t, int64(1000), percentile(in, 1))
}

func TestPercentileBoundClamping(t *testing.T) {
	in := []int64{10, 20, 30}
	assert.Equal(t, int64(10), percentile(in, -0.5))
	assert.Equal(t, int64(30), percentile(in, 1.5))
}

func TestComputeDistribution(t *testing.T) {
	in := make([]int64, 1000)
	for i := range in {
		in[i] = int64(i + 1)
	}
	d := computeDistribution(in)
	assert.Equal(t, int64(1), d.Min)
	assert.Equal(t, int64(500), d.P50)
	assert.Equal(t, int64(950), d.P95)
	assert.Equal(t, int64(990), d.P99)
	assert.Equal(t, int64(999), d.P999)
	assert.Equal(t, int64(1000), d.Max)
}

func TestComputeDistributionDoesNotMutateInput(t *testing.T) {
	in := []int64{3, 1, 2}
	_ = computeDistribution(in)
	assert.Equal(t, []int64{3, 1, 2}, in, "computeDistribution must operate on a copy")
}

func TestComputeRetransmitDistribution(t *testing.T) {
	in := []int64{0, 0, 0, 1, 2, 3}
	d := computeRetransmitDistribution(in)
	assert.Equal(t, int32(0), d.P50)
	assert.GreaterOrEqual(t, d.P95, int32(2))
	assert.Equal(t, int32(3), d.Max)
}

func TestEvalThresholdsOverall(t *testing.T) {
	assert.Equal(t, ThresholdsOverallNone, evalThresholdsOverall(nil))
	assert.Equal(t, ThresholdsOverallPass, evalThresholdsOverall([]Threshold{
		{Name: "a", Pass: true},
		{Name: "b", Pass: true},
	}))
	assert.Equal(t, ThresholdsOverallFail, evalThresholdsOverall([]Threshold{
		{Name: "a", Pass: true},
		{Name: "b", Pass: false},
	}))
}

func TestBuildEstablishmentCurveEmpty(t *testing.T) {
	c := buildEstablishmentCurve(nil, 1000)
	assert.Equal(t, []CurvePoint{}, c)
}

func TestBuildEstablishmentCurveCumulative(t *testing.T) {
	subs := map[uint32]*subAgg{
		1: {activatedOffsetMs: 100, establishedOffsetMs: 500},
		2: {activatedOffsetMs: 200, establishedOffsetMs: 1500},
		3: {activatedOffsetMs: 1100, establishedOffsetMs: 2500},
	}
	c := buildEstablishmentCurve(subs, 1000)
	require.NotEmpty(t, c)
	// First entry is the t=0 zero row.
	assert.Equal(t, int64(0), c[0].OffsetMs)
	// At the end, all activated == 3, all established == 3, in_flight == 0.
	last := c[len(c)-1]
	assert.Equal(t, int64(3), last.Activated)
	assert.Equal(t, int64(3), last.Established)
	assert.Equal(t, int64(0), last.InFlight)
	// Curve is monotonic.
	for i := 1; i < len(c); i++ {
		assert.GreaterOrEqual(t, c[i].Activated, c[i-1].Activated)
		assert.GreaterOrEqual(t, c[i].Established, c[i-1].Established)
	}
}

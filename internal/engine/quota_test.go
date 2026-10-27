package engine

import (
	"testing"

	"github.com/allong6/rxctl/internal/state"
)

func TestAccumulateFresh(t *testing.T) {
	r, rec := accumulate("tcp", "a", 443, 1000, 500, 10, state.Usage{}, "2026-10")
	if r.Used != 500 || r.Status != "正常" || rec.LastOut != 500 || rec.LastIn != 10 {
		t.Fatalf("%+v / %+v", r, rec)
	}
}

func TestAccumulateOverQuota(t *testing.T) {
	r, rec := accumulate("tcp", "a", 443, 1000, 2000, 0, state.Usage{}, "2026-10")
	if r.Status != "已封" || r.Percent != "200.0%" || !rec.Blocked {
		t.Fatalf("%+v / %+v", r, rec)
	}
}

func TestAccumulateMonthRollover(t *testing.T) {
	prev := state.Usage{Name: "a", Month: "2026-09", Out: 900, LastOut: 900, Quota: 1000}
	r, _ := accumulate("tcp", "a", 443, 1000, 10, 0, prev, "2026-10")
	if r.Used != 10 || r.Status != "正常" {
		t.Fatalf("rollover: %+v", r)
	}
}

func TestAccumulateNoQuota(t *testing.T) {
	r, _ := accumulate("udp", "a", 24445, 0, 123, 0, state.Usage{}, "2026-10")
	if r.Percent != "-" || r.Status != "正常" {
		t.Fatalf("%+v", r)
	}
}

func TestAccumulateDelta(t *testing.T) {
	prev := state.Usage{Name: "a", Month: "2026-10", Out: 1000, In: 100, LastOut: 900, LastIn: 90}
	r, rec := accumulate("tcp", "a", 443, 10000, 1500, 130, prev, "2026-10")
	if r.Used != 1600 || r.In != 140 {
		t.Fatalf("delta: %+v", r)
	}
	if rec.LastOut != 1500 || rec.LastIn != 130 {
		t.Fatalf("last: %+v", rec)
	}
}

func TestAccumulateCounterReset(t *testing.T) {
	prev := state.Usage{Name: "a", Month: "2026-10", Out: 5000, LastOut: 5000}
	r, rec := accumulate("tcp", "a", 443, 10000, 10, 0, prev, "2026-10")
	if r.Used != 5010 {
		t.Fatalf("reset must keep history and count the new segment: %+v", r)
	}
	if rec.LastOut != 10 {
		t.Fatalf("last should follow kernel counter: %+v", rec)
	}
}

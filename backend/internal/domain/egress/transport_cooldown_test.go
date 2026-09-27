package egress

import (
	"testing"
	"time"
)

func TestOneTransportFailureCoolsAtMost15SecondsAndHealthyProbeClearsIt(t *testing.T) {
	got := TransportCooldownDuration(1)
	if got <= 0 || got > SingleTransportCooldown {
		t.Fatalf("TransportCooldownDuration(1) = %s, want 0 < d <= %s", got, SingleTransportCooldown)
	}

	until := time.Now().UTC().Add(got)
	health, failures, cooldown, last := ApplyHealthyProbe(ProbeStatusHealthy, 0.7, 1, &until, LastErrorTransport)
	if health != 1 || failures != 0 || cooldown != nil || last != "" {
		t.Fatalf("healthy probe left transport cooldown: health=%v failures=%d cooldown=%v last=%q", health, failures, cooldown, last)
	}

	keptUntil := until
	health, failures, cooldown, last = ApplyHealthyProbe(ProbeStatusHealthy, 0.7, 1, &keptUntil, "anti-bot rejection")
	if health != 0.7 || failures != 1 || cooldown == nil || !cooldown.Equal(keptUntil) || last != "anti-bot rejection" {
		t.Fatalf("healthy probe cleared a non-transport failure: health=%v failures=%d cooldown=%v last=%q", health, failures, cooldown, last)
	}

	health, failures, cooldown, last = ApplyHealthyProbe(ProbeStatusUnhealthy, 0.7, 1, &keptUntil, LastErrorTransport)
	if health != 0.7 || failures != 1 || cooldown == nil || last != LastErrorTransport {
		t.Fatalf("unhealthy probe cleared transport cooldown: health=%v failures=%d cooldown=%v last=%q", health, failures, cooldown, last)
	}
}

package egress

import "time"

// SingleTransportCooldown is how long one transport failure may sideline a node.
// A short tunnel blip must not park the node for minutes.
const SingleTransportCooldown = 15 * time.Second

// TransportCooldownDuration is the cooldown applied after a transport failure.
// failureCount is the count including the failure being recorded. Repeated
// failures in the same blip stay at SingleTransportCooldown so they cannot
// escalate into a multi-minute outage.
func TransportCooldownDuration(failureCount int) time.Duration {
	if failureCount < 1 {
		failureCount = 1
	}
	return SingleTransportCooldown
}

// ApplyHealthyProbe returns the health fields to keep after a proxy probe.
// A healthy probe clears a transport-only cooldown. Any other last error,
// including anti-bot rejection, is left unchanged.
func ApplyHealthyProbe(status ProbeStatus, health float64, failureCount int, cooldown *time.Time, lastError string) (float64, int, *time.Time, string) {
	if status == ProbeStatusHealthy && lastError == LastErrorTransport {
		return 1, 0, nil, ""
	}
	return health, failureCount, cooldown, lastError
}

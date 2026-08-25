package app

import (
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/infra/config"
)

func TestQualityRetryRuntimeKeepsBurstSeparateFromMissingThinkingRetry(t *testing.T) {
	guard := config.QualityGuardConfig{
		Enabled:                 true,
		HardTPS:                 2500,
		MinimumGenerationWindow: config.Duration(time.Second),
		QuarantineDuration:      config.Duration(5 * time.Minute),
		RequestRetry: config.QualityGuardRequestRetryConfig{
			Enabled: false, BurstEnabled: true,
		},
	}
	runtime := qualityRetryRuntime(guard)
	if runtime.Enabled || !runtime.BurstEnabled || runtime.BurstHardTPS != guard.HardTPS || runtime.BurstMinGenerationWindow != time.Second || runtime.BurstAccountCooldown != 5*time.Minute {
		t.Fatalf("runtime = %#v", runtime)
	}

	guard.Enabled = false
	runtime = qualityRetryRuntime(guard)
	if runtime.BurstEnabled {
		t.Fatalf("disabled sidecar authorization gate must also disable request burst handling: %#v", runtime)
	}
}

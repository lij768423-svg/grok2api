package gateway

import "testing"

// Lab 18183 audits (2026-09-07..14) delivered chat streams that asked for
// high reasoning but were billed reasoning_tokens=0 with completion_tokens>=32.
// Their rate rose from 0.9% to 6.3% after the 2026-09-08 hold-flush deploy,
// which stopped lifting visible tokens from usage. These cases pin how a short
// visible answer with a large unexplained completion bill is classified.
func TestClassifyQualityHoldChatZeroReasoningShortVisible(t *testing.T) {
	t.Parallel()
	const minOutput = 8 // lab 18183 requestRetry.minOutputTokens
	cases := []struct {
		name   string
		frames []string
		want   QualityVerdict
	}{
		{
			name: "short visible, completion 63, reasoning 0",
			frames: []string{
				`data: {"choices":[{"delta":{"content":"好的，明白了。"}}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"completion_tokens":63,"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"data: [DONE]",
			},
			want: QualityWithhold,
		},
		{
			name: "reasoning-start stub, short visible, completion 63, reasoning 0",
			frames: []string{
				": grok2api-reasoning-start",
				`data: {"choices":[{"delta":{"content":"好的，明白了。"}}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"completion_tokens":63,"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"data: [DONE]",
			},
			want: QualityWithhold,
		},
		{
			name: "control: long visible, no thinking",
			frames: []string{
				`data: {"choices":[{"delta":{"content":"这是一个没有任何思考过程、直接给出的较长回答，用来确认缺少思考的长回答在修复前后都会被拦下。"}}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"completion_tokens":63,"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"data: [DONE]",
			},
			want: QualityWithhold,
		},
		{
			name: "control: genuinely short reply, small bill",
			frames: []string{
				`data: {"choices":[{"delta":{"content":"好的。"}}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"data: [DONE]",
			},
			want: QualityDeliver,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := qualityScanState{protocol: qualityProtocolChat}
			ObserveQualityChunk(&state, []byte(sse(tc.frames...)))
			sig := state.signals()
			got := ClassifyQualityHold(sig, minOutput)
			t.Logf("visible=%d output=%d reasoning=%d terminal=%v verdict=%s", sig.VisibleTokens, sig.OutputTokens, sig.ReasoningTokens, sig.Terminal, got)
			if got != tc.want {
				t.Errorf("verdict = %s, want %s", got, tc.want)
			}
		})
	}
}

package provider

import (
	"net/http"
	"testing"
)

func TestIsCloudflareChallengeResponse(t *testing.T) {
	if !IsCloudflareChallengeResponse(http.Header{"Cf-Mitigated": []string{"challenge"}}, []byte(`{"message":"forbidden"}`)) {
		t.Fatal("documented cf-mitigated challenge header was not recognized")
	}
	if IsCloudflareChallengeResponse(http.Header{"Cf-Mitigated": []string{"managed_challenge"}}, []byte(`{"message":"forbidden"}`)) {
		t.Fatal("non-challenge cf-mitigated value must not invalidate clearance")
	}
}

func TestIsCloudflareChallengeBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "challenge title", body: "<title>Just a moment...</title>", want: true},
		{name: "challenge platform", body: "<script src=\"/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page/v1\"></script>", want: true},
		{name: "challenge token", body: "__cf_chl_tk=abc", want: true},
		{name: "normal quota json", body: `{"message":"temporary rejection"}`, want: false},
		{name: "ordinary forbidden", body: "forbidden", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsCloudflareChallengeBody([]byte(test.body)); got != test.want {
				t.Fatalf("IsCloudflareChallengeBody() = %v, want %v", got, test.want)
			}
		})
	}
}

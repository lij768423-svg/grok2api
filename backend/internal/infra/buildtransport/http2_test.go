package buildtransport

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigureHTTP2HealthEnablesActivePing(t *testing.T) {
	transport := &http.Transport{ForceAttemptHTTP2: true}
	if HTTP2HealthConfigured(transport) {
		t.Fatal("fresh transport must not report HTTP/2 health")
	}
	h2, err := ConfigureHTTP2Health(transport)
	if err != nil {
		t.Fatal(err)
	}
	if h2.ReadIdleTimeout != HTTP2ReadIdleTimeout || h2.PingTimeout != HTTP2PingTimeout {
		t.Fatalf("HTTP/2 health = (%s, %s)", h2.ReadIdleTimeout, h2.PingTimeout)
	}
	if !HTTP2HealthConfigured(transport) {
		t.Fatal("HTTP/2 transport was not installed")
	}
}

// The configured transport must still negotiate HTTP/2 end to end; this guards
// against x/net or net/http changing the registration mechanism underneath us.
func TestConfigureHTTP2HealthNegotiatesHTTP2(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport := &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{RootCAs: roots}}
	if _, err := ConfigureHTTP2Health(transport); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.ProtoMajor != 2 {
		t.Fatalf("negotiated %s, want HTTP/2", response.Proto)
	}
}

func TestConfigureHTTP2HealthRejectsNilTransport(t *testing.T) {
	if _, err := ConfigureHTTP2Health(nil); err == nil {
		t.Fatal("nil transport was accepted")
	}
	if HTTP2HealthConfigured(nil) {
		t.Fatal("nil transport reported HTTP/2 health")
	}
}

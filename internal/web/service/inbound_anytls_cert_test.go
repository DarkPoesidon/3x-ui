package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/anytls"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func withPanelCert(t *testing.T, cert, key string) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	s := &SettingService{}
	if err := s.SetCertFile(cert); err != nil {
		t.Fatalf("set cert: %v", err)
	}
	if err := s.SetKeyFile(key); err != nil {
		t.Fatalf("set key: %v", err)
	}
}

func certPair(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cert := filepath.Join(dir, "fullchain.pem")
	key := filepath.Join(dir, "privkey.pem")
	for _, p := range []string{cert, key} {
		if err := os.WriteFile(p, []byte("pem"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return cert, key
}

// settingsJSON encodes the paths properly: a Windows temp dir is full of
// backslashes, which are escapes inside a JSON string literal.
func settingsJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	bs, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	return string(bs)
}

func settingsOf(t *testing.T, ib *model.Inbound) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(ib.Settings), &m); err != nil {
		t.Fatalf("settings not JSON: %v", err)
	}
	return m
}

// Without this the inbound falls back to a self-signed certificate and the
// share link says insecure=1, which only the lenient clients honour; the rest
// abort the handshake and the operator has nothing to point at.
func TestNewAnytlsInboundAdoptsThePanelCertificate(t *testing.T) {
	cert, key := certPair(t)
	withPanelCert(t, cert, key)

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{"sni":"example.com"}`}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got := settingsOf(t, ib)
	if got["certFile"] != cert || got["keyFile"] != key {
		t.Fatalf("expected the panel's pair, got cert=%v key=%v", got["certFile"], got["keyFile"])
	}
}

func TestAnytlsKeepsAnExplicitCertificateChoice(t *testing.T) {
	panelCert, panelKey := certPair(t)
	withPanelCert(t, panelCert, panelKey)
	ownCert, ownKey := certPair(t)

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: settingsJSON(t, map[string]any{"certFile": ownCert, "keyFile": ownKey})}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := settingsOf(t, ib); got["certFile"] != ownCert {
		t.Fatalf("an explicit certificate must win, got %v", got["certFile"])
	}
}

// Editing an existing inbound must not re-add a certificate the operator
// deliberately cleared to go back to self-signed.
func TestEditingDoesNotReadoptThePanelCertificate(t *testing.T) {
	cert, key := certPair(t)
	withPanelCert(t, cert, key)

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{"sni":"example.com"}`}
	if err := prepareAnytlsSettings(ib, false); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := settingsOf(t, ib); got["certFile"] != nil {
		t.Fatalf("an edit must leave the certificate alone, got %v", got["certFile"])
	}
}

// Swapping the self-signed fallback for a path that does not resolve would only
// trade a working-in-some-clients inbound for one that cannot start at all.
func TestPanelCertificateIsIgnoredWhenItsFilesAreGone(t *testing.T) {
	withPanelCert(t, "/nope/fullchain.pem", "/nope/privkey.pem")

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{"sni":"example.com"}`}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare must not fail: %v", err)
	}
	if got := settingsOf(t, ib); got["certFile"] != nil {
		t.Fatalf("a broken panel certificate must not be adopted, got %v", got["certFile"])
	}
}

func TestAnytlsRejectsAHalfConfiguredPair(t *testing.T) {
	cert, _ := certPair(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: settingsJSON(t, map[string]any{"certFile": cert})}
	err := prepareAnytlsSettings(ib, false)
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("expected a rejection naming both halves, got %v", err)
	}
}

// writeCert emits a real leaf certificate so the SNI derivation is exercised
// against x509 parsing rather than a stand-in string.
func writeCert(t *testing.T, dnsNames []string, ips []net.IP) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "fullchain.pem")
	keyPath := filepath.Join(dir, "privkey.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

// Adopting the pair without naming it leaves the handshake failing for the same
// reason it did before: the client verifies against the name it asked for.
func TestAdoptedCertificateAlsoNamesTheInbound(t *testing.T) {
	cert, key := writeCert(t, []string{"panel.example.com"}, nil)
	withPanelCert(t, cert, key)

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{}`}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := settingsOf(t, ib)["sni"]; got != "panel.example.com" {
		t.Fatalf("sni = %v, want the certificate's DNS name", got)
	}
}

func TestAnIPCertificateNamesTheInboundByAddress(t *testing.T) {
	cert, key := writeCert(t, nil, []net.IP{net.ParseIP("203.0.113.7")})
	withPanelCert(t, cert, key)

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{}`}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := settingsOf(t, ib)["sni"]; got != "203.0.113.7" {
		t.Fatalf("sni = %v, want the certificate's IP", got)
	}
}

func TestAnExplicitSniSurvivesCertificateAdoption(t *testing.T) {
	cert, key := writeCert(t, []string{"panel.example.com"}, nil)
	withPanelCert(t, cert, key)

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{"sni":"chosen.example.com"}`}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := settingsOf(t, ib)["sni"]; got != "chosen.example.com" {
		t.Fatalf("an explicit sni must win, got %v", got)
	}
}

// A wildcard is a name no client sends verbatim, so it cannot stand in as SNI.
func TestWildcardCertificateLeavesTheSniAlone(t *testing.T) {
	cert, key := writeCert(t, []string{"*.example.com"}, nil)
	withPanelCert(t, cert, key)

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{}`}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := settingsOf(t, ib)["sni"]; got != nil {
		t.Fatalf("a wildcard must not become the sni, got %v", got)
	}
}

// Where an inbound was created must not decide how exposed it is: these used to
// live only in the panel form, so an API-created inbound came up on the
// sidecar's fingerprintable built-in scheme with no probe fallback at all.
func TestNewAnytlsInboundGetsTheHardenedDefaults(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{"clients":[]}`}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got := settingsOf(t, ib)
	if got["paddingMode"] != anytls.PaddingModeStrong {
		t.Errorf("paddingMode = %v, want the hardened scheme", got["paddingMode"])
	}
	if got["forward"] != DefaultAnytlsForward {
		t.Errorf("forward = %v, want a probe fallback", got["forward"])
	}
}

func TestAnytlsDefaultsNeverOverrideAChoice(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	ib := &model.Inbound{
		Protocol: model.AnyTLS,
		Settings: `{"paddingMode":"default","forward":"http://127.0.0.1:8080"}`,
	}
	if err := prepareAnytlsSettings(ib, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got := settingsOf(t, ib)
	if got["paddingMode"] != "default" {
		t.Errorf("an explicit padding mode must win, got %v", got["paddingMode"])
	}
	if got["forward"] != "http://127.0.0.1:8080" {
		t.Errorf("an explicit forward must win, got %v", got["forward"])
	}
}

// An edit must not quietly re-add defaults the operator removed.
func TestEditingDoesNotReapplyAnytlsDefaults(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: `{"clients":[]}`}
	if err := prepareAnytlsSettings(ib, false); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := settingsOf(t, ib); got["forward"] != nil || got["paddingMode"] != nil {
		t.Fatalf("an edit must leave the settings alone, got %v", got)
	}
}

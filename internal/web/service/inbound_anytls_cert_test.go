package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

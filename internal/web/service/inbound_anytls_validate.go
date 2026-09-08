package service

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/anytls"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// prepareAnytlsSettings gives a new anytls inbound a working certificate and
// then refuses any inbound the sidecar could not start.
//
// Both halves exist because an anytls inbound fails in ways the operator cannot
// see. A path that does not resolve used to be accepted, stored, and then fail
// forever out of sight: the node exits on the missing file, the reconcile job
// starts it again ten seconds later, and the panel goes on showing a healthy
// inbound. Catching it here means the bad state is never stored, and the reason
// lands on the form the operator is already looking at.
func prepareAnytlsSettings(inbound *model.Inbound, isNew bool) error {
	if inbound == nil || inbound.Protocol != model.AnyTLS {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(inbound.Settings), &parsed); err != nil || parsed == nil {
		return common.NewError("anytls: settings are not valid JSON")
	}

	if isNew {
		adoptPanelCertificate(inbound, parsed)
	}

	certFile := settingsString(parsed, "certFile")
	keyFile := settingsString(parsed, "keyFile")

	// A half-configured pair is silently dropped when the node is launched, so
	// the inbound would quietly serve a self-signed certificate instead of the
	// one the operator thought they had configured.
	if (certFile == "") != (keyFile == "") {
		return common.NewError("anytls: set both the certificate and the private key, or neither")
	}
	for _, f := range []struct{ label, path string }{
		{"certificate file", certFile},
		{"private key file", keyFile},
	} {
		if f.path == "" {
			continue
		}
		if _, err := os.Stat(f.path); err != nil {
			return common.NewErrorf("anytls: %s %q cannot be read on this server", f.label, f.path)
		}
	}

	switch anytls.PaddingModeOf(settingsString(parsed, "paddingMode"), settingsString(parsed, "paddingScheme")) {
	case anytls.PaddingModeCustom:
		if err := anytls.ValidateScheme(settingsString(parsed, "paddingSchemeText")); err != nil {
			return common.NewError("anytls: the custom padding scheme is invalid:", err)
		}
	case anytls.PaddingModeFile:
		path := settingsString(parsed, "paddingScheme")
		if _, err := os.Stat(path); err != nil {
			return common.NewErrorf("anytls: padding scheme file %q cannot be read on this server", path)
		}
	}
	return nil
}

func settingsString(parsed map[string]any, key string) string {
	v, _ := parsed[key].(string)
	return strings.TrimSpace(v)
}

// adoptPanelCertificate gives a new anytls inbound the certificate the panel is
// already serving, when it has one and the operator supplied none.
//
// Without a certificate the node falls back to a self-signed one and the share
// link carries insecure=1 to tell clients to skip verification. That only works
// for clients which honour the flag: others validate regardless and abort the
// handshake with a BadCertificate alert, which reaches the operator as "this
// inbound does not work" with nothing to point at. A normal install already has
// a trusted certificate for its own panel, so defaulting to it makes the config
// this inbound generates work in every client rather than only the lenient ones.
func adoptPanelCertificate(inbound *model.Inbound, parsed map[string]any) {
	if settingsString(parsed, "certFile") != "" || settingsString(parsed, "keyFile") != "" {
		return
	}
	settings := &SettingService{}
	cert, err := settings.GetCertFile()
	if err != nil || strings.TrimSpace(cert) == "" {
		return
	}
	key, err := settings.GetKeyFile()
	if err != nil || strings.TrimSpace(key) == "" {
		return
	}
	// A path the panel stored but that no longer resolves would only trade the
	// self-signed fallback for an inbound that cannot start at all.
	for _, path := range []string{cert, key} {
		if _, statErr := os.Stat(path); statErr != nil {
			return
		}
	}

	parsed["certFile"] = strings.TrimSpace(cert)
	parsed["keyFile"] = strings.TrimSpace(key)
	bs, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return
	}
	inbound.Settings = string(bs)
	logger.Infof("anytls: new inbound adopted the panel's certificate (%s); clients will verify it instead of skipping verification", cert)
}

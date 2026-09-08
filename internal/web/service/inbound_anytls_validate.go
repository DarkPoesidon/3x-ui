package service

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
		applyAnytlsDefaults(inbound, parsed)
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

	// A certificate the SNI does not match is worse than none: the node serves
	// it, the client asks for a name it does not cover, and the handshake dies
	// with a BadCertificate alert the operator has no way to trace. Refuse it
	// here instead, where the reason can name both halves.
	if certFile != "" {
		if sni := settingsString(parsed, "sni"); sni != "" {
			if err := certificateCovers(certFile, sni); err != nil {
				return err
			}
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

// certificateCovers reports whether the leaf certificate is valid for the name
// clients will ask for. Unreadable or unparsable certificates are left to the
// file checks above rather than reported as a name problem.
func certificateCovers(certPath, sni string) error {
	leaf := leafCertificate(certPath)
	if leaf == nil {
		return nil
	}
	if err := leaf.VerifyHostname(sni); err != nil {
		names := append([]string{}, leaf.DNSNames...)
		for _, ip := range leaf.IPAddresses {
			names = append(names, ip.String())
		}
		covered := strings.Join(names, ", ")
		if covered == "" {
			covered = "no names at all"
		}
		return common.NewErrorf(
			"anytls: the certificate does not cover the TLS SNI %q (it covers %s), so clients will reject the connection",
			sni, covered)
	}
	return nil
}

// certificateName is a name the leaf certificate actually covers, preferring a
// DNS name over an IP: clients do not send SNI for an IP literal, so a DNS name
// is what a client can both send and verify against.
func certificateName(certPath string) string {
	leaf := leafCertificate(certPath)
	if leaf == nil {
		return ""
	}
	for _, name := range leaf.DNSNames {
		if name != "" && !strings.HasPrefix(name, "*") {
			return name
		}
	}
	for _, ip := range leaf.IPAddresses {
		return ip.String()
	}
	return ""
}

// leafCertificate parses the first certificate in a PEM chain; the rest name
// CAs and say nothing about which hosts the inbound can serve.
func leafCertificate(certPath string) *x509.Certificate {
	raw, err := os.ReadFile(certPath)
	if err != nil {
		return nil
	}
	for len(raw) > 0 {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			return nil
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		leaf, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return nil
		}
		return leaf
	}
	return nil
}

func settingsString(parsed map[string]any, key string) string {
	v, _ := parsed[key].(string)
	return strings.TrimSpace(v)
}

// DefaultAnytlsForward is where a connection failing authentication is relayed,
// which is what makes the port answer an active prober like an ordinary web
// server. A default means the defence is on for everyone rather than only for
// operators who knew to look for the field.
const DefaultAnytlsForward = "https://www.bing.com"

// applyAnytlsDefaults fills in what a new inbound needs to be safe and reachable
// but did not ask for.
//
// These used to live only in the panel form, so an inbound created through the
// API came up with the sidecar's built-in padding scheme -- fingerprintable by
// shape, its first record always exactly 30 bytes -- and no probe fallback at
// all. Where an inbound was created should not decide how exposed it is.
func applyAnytlsDefaults(inbound *model.Inbound, parsed map[string]any) {
	changed := adoptPanelCertificate(parsed)
	if _, ok := parsed["paddingMode"]; !ok {
		parsed["paddingMode"] = anytls.PaddingModeStrong
		changed = true
	}
	if settingsString(parsed, "forward") == "" {
		parsed["forward"] = DefaultAnytlsForward
		changed = true
	}
	if !changed {
		return
	}
	if bs, err := json.MarshalIndent(parsed, "", "  "); err == nil {
		inbound.Settings = string(bs)
	}
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
func adoptPanelCertificate(parsed map[string]any) bool {
	if settingsString(parsed, "certFile") != "" || settingsString(parsed, "keyFile") != "" {
		return false
	}
	settings := &SettingService{}
	cert, err := settings.GetCertFile()
	if err != nil || strings.TrimSpace(cert) == "" {
		return false
	}
	key, err := settings.GetKeyFile()
	if err != nil || strings.TrimSpace(key) == "" {
		return false
	}
	// A path the panel stored but that no longer resolves would only trade the
	// self-signed fallback for an inbound that cannot start at all.
	for _, path := range []string{cert, key} {
		if _, statErr := os.Stat(path); statErr != nil {
			return false
		}
	}

	parsed["certFile"] = strings.TrimSpace(cert)
	parsed["keyFile"] = strings.TrimSpace(key)
	// A certificate the client cannot match is no better than a self-signed
	// one: it verifies against the name the client asked for, so adopting the
	// pair without also naming it leaves the handshake failing for exactly the
	// same reason it did before.
	if settingsString(parsed, "sni") == "" {
		if name := certificateName(cert); name != "" {
			parsed["sni"] = name
		}
	}
	logger.Infof("anytls: new inbound adopted the panel's certificate (%s); clients will verify it instead of skipping verification", cert)
	return true
}

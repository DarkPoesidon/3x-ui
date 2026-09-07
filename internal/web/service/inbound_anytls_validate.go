package service

import (
	"encoding/json"
	"os"

	"github.com/mhsanaei/3x-ui/v3/internal/anytls"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// validateAnytlsSettings refuses an anytls inbound the sidecar could not start.
//
// Every one of these used to be accepted and only fail later, out of sight: the
// node exits on the missing file, the reconcile job starts it again ten seconds
// later, and the panel goes on showing a healthy inbound. Catching it here means
// the bad state is never stored in the first place, and the admin reads the
// reason on the form they are already looking at.
func validateAnytlsSettings(inbound *model.Inbound) error {
	if inbound == nil || inbound.Protocol != model.AnyTLS {
		return nil
	}
	var parsed struct {
		SNI               string `json:"sni"`
		CertFile          string `json:"certFile"`
		KeyFile           string `json:"keyFile"`
		PaddingMode       string `json:"paddingMode"`
		PaddingSchemeText string `json:"paddingSchemeText"`
		PaddingScheme     string `json:"paddingScheme"`
	}
	if err := json.Unmarshal([]byte(inbound.Settings), &parsed); err != nil {
		return common.NewError("anytls: settings are not valid JSON:", err)
	}

	// A half-configured pair is silently dropped when the node is launched, so
	// the inbound would quietly serve a self-signed certificate instead of the
	// one the admin thought they had configured.
	if (parsed.CertFile == "") != (parsed.KeyFile == "") {
		return common.NewError("anytls: set both the certificate and the private key, or neither")
	}
	for _, f := range []struct{ label, path string }{
		{"certificate file", parsed.CertFile},
		{"private key file", parsed.KeyFile},
	} {
		if f.path == "" {
			continue
		}
		if _, err := os.Stat(f.path); err != nil {
			return common.NewErrorf("anytls: %s %q cannot be read on this server", f.label, f.path)
		}
	}

	switch anytls.PaddingModeOf(parsed.PaddingMode, parsed.PaddingScheme) {
	case anytls.PaddingModeCustom:
		if err := anytls.ValidateScheme(parsed.PaddingSchemeText); err != nil {
			return common.NewError("anytls: the custom padding scheme is invalid:", err)
		}
	case anytls.PaddingModeFile:
		if _, err := os.Stat(parsed.PaddingScheme); err != nil {
			return common.NewErrorf("anytls: padding scheme file %q cannot be read on this server", parsed.PaddingScheme)
		}
	}
	return nil
}

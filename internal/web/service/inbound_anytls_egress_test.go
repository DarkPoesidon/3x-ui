package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// The bug this guards: a routed anytls inbound dials out through a loopback
// SOCKS bridge that only exists in the generated Xray config, and the check
// that forces the config to be regenerated only knew about mtproto. The node
// came up, authenticated, opened the stream, and then every dial failed with a
// connection refused against a port nothing was listening on.
func TestRoutedSidecarInboundsForceAnXrayRegen(t *testing.T) {
	routed := `{"routeThroughXray":true,"routeXrayPort":32793}`
	direct := `{"routeThroughXray":false}`

	cases := []struct {
		name     string
		inbound  *model.Inbound
		expected bool
	}{
		{"routed anytls", &model.Inbound{Protocol: model.AnyTLS, Settings: routed}, true},
		{"routed mtproto", &model.Inbound{Protocol: model.MTProto, Settings: routed}, true},
		{"direct anytls", &model.Inbound{Protocol: model.AnyTLS, Settings: direct}, false},
		{"direct mtproto", &model.Inbound{Protocol: model.MTProto, Settings: direct}, false},
		{"an xray protocol needs no bridge", &model.Inbound{Protocol: model.VLESS, Settings: routed}, false},
		{"unparsable settings", &model.Inbound{Protocol: model.AnyTLS, Settings: "not json"}, false},
		{"no inbound", nil, false},
	}
	for _, c := range cases {
		if got := sidecarRoutesThroughXray(c.inbound); got != c.expected {
			t.Errorf("%s: sidecarRoutesThroughXray = %v, want %v", c.name, got, c.expected)
		}
	}
}

package sub

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// EMAIL is deliberately stripped from every link after a client's first, so a
// template that leans on it renders to nothing on the second link of an inbound
// with no remark. Clients key their proxy list by name: two nameless links from
// one subscription collapse into a single entry, and the operator sees one
// inbound where they configured two.
func TestNamelessLinksFallBackToTheInboundTag(t *testing.T) {
	ctx := remarkContext{inbound: &model.Inbound{Remark: "", Tag: "in-8443-tcp"}}
	if got := ctx.fallbackName(); got != "in-8443-tcp" {
		t.Fatalf("fallbackName = %q, want the tag", got)
	}
}

func TestFallbackNamePrefersTheOperatorsRemark(t *testing.T) {
	ctx := remarkContext{inbound: &model.Inbound{Remark: "  eu-node  ", Tag: "in-8443-tcp"}}
	if got := ctx.fallbackName(); got != "eu-node" {
		t.Fatalf("fallbackName = %q, want the trimmed remark", got)
	}
}

func TestFallbackNameWithoutAnInbound(t *testing.T) {
	ctx := remarkContext{}
	if got := ctx.fallbackName(); got != "" {
		t.Fatalf("fallbackName = %q, want empty", got)
	}
}

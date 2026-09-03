// Package anytls runs anytls-server sidecars, one per anytls inbound, outside
// the Xray config and lifecycle — Xray-core has no anytls protocol.
package anytls

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// UserEntry is one client of an anytls inbound. The password is the identity:
// the node tells clients apart by the password hash in their auth preamble.
type UserEntry struct {
	Name        string
	Password    string
	QuotaBytes  int64
	ExpiresUnix int64
}

// Instance is the desired runtime config of one anytls inbound; a single
// anytls-server process serves every active client of it.
type Instance struct {
	Id     int
	Tag    string
	Listen string
	Port   int
	Users  []UserEntry

	// An empty CertFile/KeyFile pair leaves the node on an ephemeral
	// self-signed cert, which only suits clients that skip verification.
	SNI      string
	CertFile string
	KeyFile  string

	// Where a connection failing auth is relayed (http/https URL), which is
	// what makes the port look ordinary to a prober. Empty relays to the SNI.
	Forward string

	PaddingScheme string // path to an optional padding scheme file

	Debug bool

	// Dial destinations through the loopback SOCKS bridge the panel injects
	// into the Xray config, so egress obeys the core's routing rules.
	RouteThroughXray bool
	XrayRoutePort    int
}

func (inst Instance) bindTo() string {
	listen := inst.Listen
	if listen == "" {
		listen = "0.0.0.0"
	}
	return fmt.Sprintf("%s:%d", listen, inst.Port)
}

// structuralFingerprint moves on any change outside the user set, which can
// only be applied by restarting the node.
func (inst Instance) structuralFingerprint() string {
	parts := []string{
		inst.bindTo(),
		inst.SNI,
		inst.CertFile,
		inst.KeyFile,
		inst.Forward,
		inst.PaddingScheme,
		strconv.FormatBool(inst.Debug),
		strconv.FormatBool(inst.RouteThroughXray),
		strconv.Itoa(inst.XrayRoutePort),
	}
	return strings.Join(parts, "|")
}

// usersFingerprint identifies the reloadable user set regardless of client
// order, so a reordered clients array does not read as a change.
func (inst Instance) usersFingerprint() string {
	pairs := make([]string, 0, len(inst.Users))
	for _, u := range inst.Users {
		pairs = append(pairs, fmt.Sprintf("%s=%s;q=%d;exp=%d", u.Name, u.Password, u.QuotaBytes, u.ExpiresUnix))
	}
	slices.Sort(pairs)
	return strings.Join(pairs, "|")
}

// InstanceFromInbound builds one user per active client, returning false when
// the inbound is not a usable anytls inbound or has no active client.
func InstanceFromInbound(ib *model.Inbound) (Instance, bool) {
	if ib == nil || ib.Protocol != model.AnyTLS {
		return Instance{}, false
	}
	var parsed struct {
		SNI              string `json:"sni"`
		CertFile         string `json:"certFile"`
		KeyFile          string `json:"keyFile"`
		Forward          string `json:"forward"`
		PaddingScheme    string `json:"paddingScheme"`
		Debug            bool   `json:"debug"`
		RouteThroughXray bool   `json:"routeThroughXray"`
		RouteXrayPort    int    `json:"routeXrayPort"`
		Clients          []struct {
			Email      string `json:"email"`
			Password   string `json:"password"`
			Enable     bool   `json:"enable"`
			TotalGB    int64  `json:"totalGB"`
			ExpiryTime int64  `json:"expiryTime"`
		} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil {
		return Instance{}, false
	}

	users := make([]UserEntry, 0, len(parsed.Clients))
	seen := make(map[string]struct{}, len(parsed.Clients))
	for _, c := range parsed.Clients {
		if !c.Enable || c.Email == "" || c.Password == "" {
			continue
		}
		// Clients sharing a password are indistinguishable to the node, which
		// would bill both to whichever it resolved first.
		if _, dup := seen[c.Password]; dup {
			continue
		}
		seen[c.Password] = struct{}{}
		entry := UserEntry{Name: c.Email, Password: c.Password}
		if c.TotalGB > 0 {
			entry.QuotaBytes = c.TotalGB
		}
		if c.ExpiryTime > 0 {
			entry.ExpiresUnix = c.ExpiryTime / 1000
		}
		users = append(users, entry)
	}
	if len(users) == 0 {
		return Instance{}, false
	}

	return Instance{
		Id:               ib.Id,
		Tag:              ib.Tag,
		Listen:           ib.Listen,
		Port:             ib.Port,
		Users:            users,
		SNI:              strings.TrimSpace(parsed.SNI),
		CertFile:         strings.TrimSpace(parsed.CertFile),
		KeyFile:          strings.TrimSpace(parsed.KeyFile),
		Forward:          strings.TrimSpace(parsed.Forward),
		PaddingScheme:    strings.TrimSpace(parsed.PaddingScheme),
		Debug:            parsed.Debug,
		RouteThroughXray: parsed.RouteThroughXray,
		XrayRoutePort:    parsed.RouteXrayPort,
	}, true
}

// Traffic is a per-client byte delta scraped from a node's /stats endpoint,
// tagged with the owning inbound.
type Traffic struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

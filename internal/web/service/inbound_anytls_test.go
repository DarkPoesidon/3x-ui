package service

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func anytlsSettings(clients string) string {
	return `{"sni":"example.com","clients":[` + clients + `]}`
}

// A node-assigned inbound is reconciled by the node that received it, where it
// is stored with no NodeID. If the master's own loop also claimed it, two
// machines would serve one inbound and double-count its traffic.
func TestDesiredAnytlsInstancesSkipsNodeAssignedInbounds(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	nodeID := 7
	rows := []*model.Inbound{
		{
			UserId: 1, Remark: "local", Tag: "local", Port: 24101, Protocol: model.AnyTLS, Enable: true,
			Settings: anytlsSettings(`{"email":"alice","password":"pw-a","enable":true}`),
		},
		{
			UserId: 1, Remark: "on-node", Tag: "on-node", Port: 24102, Protocol: model.AnyTLS, Enable: true, NodeID: &nodeID,
			Settings: anytlsSettings(`{"email":"bob","password":"pw-b","enable":true}`),
		},
		{
			UserId: 1, Remark: "disabled", Tag: "disabled", Port: 24103, Protocol: model.AnyTLS, Enable: false,
			Settings: anytlsSettings(`{"email":"carol","password":"pw-c","enable":true}`),
		},
	}
	for _, ib := range rows {
		if err := database.GetDB().Create(ib).Error; err != nil {
			t.Fatalf("seed %s: %v", ib.Remark, err)
		}
	}

	svc := InboundService{}
	desired, err := svc.DesiredAnytlsInstances()
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	if len(desired) != 1 {
		t.Fatalf("expected only the local enabled inbound, got %d: %+v", len(desired), desired)
	}
	if desired[0].Tag != "local" {
		t.Fatalf("expected the local inbound, got %q", desired[0].Tag)
	}
}

// A client the panel disabled for running out of traffic must not be served,
// even though the inbound's own settings still list it as enabled.
func TestDesiredAnytlsInstancesDropsDepletedClients(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	ib := &model.Inbound{
		UserId: 1, Remark: "r", Tag: "inbound-anytls", Port: 24111, Protocol: model.AnyTLS, Enable: true,
		Settings: anytlsSettings(`{"email":"alice","password":"pw-a","enable":true},{"email":"bob","password":"pw-b","enable":true}`),
	}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	depleted := &xray.ClientTraffic{InboundId: ib.Id, Email: "bob", Enable: false}
	if err := database.GetDB().Create(depleted).Error; err != nil {
		t.Fatalf("seed client traffic: %v", err)
	}

	desired, err := (&InboundService{}).DesiredAnytlsInstances()
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	if len(desired) != 1 {
		t.Fatalf("expected one instance, got %d", len(desired))
	}
	if len(desired[0].Users) != 1 || desired[0].Users[0].Name != "alice" {
		t.Fatalf("a depleted client must not be served, got %+v", desired[0].Users)
	}
}

// The egress port is chosen by whichever panel saved the inbound, so one pushed
// to a node can name a port already taken there; a bridge that cannot bind
// routes no traffic at all.
func TestNormalizeXrayPortReseedsAPushedPortAlreadyInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy a port: %v", err)
	}
	defer listener.Close()
	taken := listener.Addr().(*net.TCPAddr).Port

	svc := &InboundService{}
	ib := &model.Inbound{
		Protocol: model.AnyTLS,
		Settings: `{"routeThroughXray":true,"routeXrayPort":` + strconv.Itoa(taken) + `}`,
	}
	if err := svc.normalizeMtprotoXrayPort(ib, ""); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	got := routeXrayPortOf(t, ib.Settings)
	if got == taken {
		t.Fatalf("a port already in use here must be replaced, kept %d", got)
	}
	if got <= 0 {
		t.Fatalf("a routed inbound must end up with a port, got %d", got)
	}
}

// An edit must keep the port the running bridge is already bound to; probing it
// would find it busy and churn the inbound onto a new port on every save.
func TestNormalizeXrayPortKeepsAnInUsePortOnEdit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy a port: %v", err)
	}
	defer listener.Close()
	bound := listener.Addr().(*net.TCPAddr).Port
	stored := `{"routeThroughXray":true,"routeXrayPort":` + strconv.Itoa(bound) + `}`

	svc := &InboundService{}
	ib := &model.Inbound{Protocol: model.AnyTLS, Settings: stored}
	if err := svc.normalizeMtprotoXrayPort(ib, stored); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got := routeXrayPortOf(t, ib.Settings); got != bound {
		t.Fatalf("an edit must keep its own bound port %d, got %d", bound, got)
	}
}

// Turning routing off must strip the port and the outbound selection, so a
// disabled bridge leaves nothing stale in the settings pushed to a node.
func TestNormalizeXrayPortStripsWhenRoutingIsOff(t *testing.T) {
	svc := &InboundService{}
	ib := &model.Inbound{
		Protocol: model.AnyTLS,
		Settings: `{"routeThroughXray":false,"routeXrayPort":51000,"outboundTag":"direct"}`,
	}
	if err := svc.normalizeMtprotoXrayPort(ib, ""); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	if _, present := parsed["routeXrayPort"]; present {
		t.Fatalf("routeXrayPort must be stripped: %s", ib.Settings)
	}
	if _, present := parsed["outboundTag"]; present {
		t.Fatalf("outboundTag must be stripped: %s", ib.Settings)
	}
}

// An anytls client carries a password and no UUID, which the shared client
// validation rejected as an empty client ID -- the panel refused to create any
// anytls inbound at all.
func TestAddInboundAcceptsPasswordOnlyAnytlsClients(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	ib := &model.Inbound{
		UserId: 1, Remark: "anytls", Tag: "inbound-anytls", Listen: "127.0.0.1", Port: 24567,
		Protocol: model.AnyTLS, Enable: true,
		Settings: anytlsSettings(`{"email":"alice","password":"pw-alice","enable":true}`),
	}
	created, _, err := (&InboundService{}).AddInbound(ib)
	if err != nil {
		t.Fatalf("anytls inbound must be accepted with a password-only client: %v", err)
	}
	if created == nil || created.Id == 0 {
		t.Fatal("expected the inbound to be stored")
	}
}

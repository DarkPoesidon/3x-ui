package anytls

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func inboundWith(settings string) *model.Inbound {
	return &model.Inbound{
		Id:       7,
		Tag:      "inbound-7",
		Protocol: model.AnyTLS,
		Listen:   "0.0.0.0",
		Port:     8443,
		Settings: settings,
	}
}

func TestInstanceFromInbound(t *testing.T) {
	t.Run("builds one user per active client", func(t *testing.T) {
		ib := inboundWith(`{
			"sni": "example.com",
			"certFile": "/c.pem",
			"keyFile": "/k.pem",
			"forward": "https://example.com",
			"clients": [
				{"email":"alice","password":"pw-a","enable":true,"totalGB":1024,"expiryTime":1780000000000},
				{"email":"bob","password":"pw-b","enable":true}
			]
		}`)
		inst, ok := InstanceFromInbound(ib)
		if !ok {
			t.Fatal("expected a usable instance")
		}
		if len(inst.Users) != 2 {
			t.Fatalf("expected 2 users, got %d: %+v", len(inst.Users), inst.Users)
		}
		if inst.Users[0].Name != "alice" || inst.Users[0].Password != "pw-a" {
			t.Fatalf("first user must be alice with her own password: %+v", inst.Users[0])
		}
		if inst.Users[0].QuotaBytes != 1024 {
			t.Fatalf("quota must carry through, got %d", inst.Users[0].QuotaBytes)
		}
		// The panel stores expiry in milliseconds; the node speaks seconds.
		if inst.Users[0].ExpiresUnix != 1780000000 {
			t.Fatalf("expiry must convert ms to s, got %d", inst.Users[0].ExpiresUnix)
		}
		if inst.Users[1].QuotaBytes != 0 || inst.Users[1].ExpiresUnix != 0 {
			t.Fatalf("an unlimited client must carry zero limits: %+v", inst.Users[1])
		}
		if inst.SNI != "example.com" || inst.CertFile != "/c.pem" || inst.Forward != "https://example.com" {
			t.Fatalf("inbound-level settings must carry through: %+v", inst)
		}
	})

	t.Run("skips clients that cannot be served", func(t *testing.T) {
		ib := inboundWith(`{"clients":[
			{"email":"off","password":"pw-off","enable":false},
			{"email":"","password":"pw-anon","enable":true},
			{"email":"nopass","password":"","enable":true},
			{"email":"alice","password":"pw-a","enable":true}
		]}`)
		inst, ok := InstanceFromInbound(ib)
		if !ok {
			t.Fatal("expected a usable instance")
		}
		if len(inst.Users) != 1 || inst.Users[0].Name != "alice" {
			t.Fatalf("only alice is servable, got %+v", inst.Users)
		}
	})

	t.Run("drops a duplicate password", func(t *testing.T) {
		// The node resolves users by password hash, so a shared password would
		// bill both clients to whichever it matched first.
		ib := inboundWith(`{"clients":[
			{"email":"alice","password":"same","enable":true},
			{"email":"bob","password":"same","enable":true}
		]}`)
		inst, ok := InstanceFromInbound(ib)
		if !ok {
			t.Fatal("expected a usable instance")
		}
		if len(inst.Users) != 1 || inst.Users[0].Name != "alice" {
			t.Fatalf("expected only the first of two colliding clients, got %+v", inst.Users)
		}
	})

	t.Run("refuses what it cannot serve", func(t *testing.T) {
		cases := map[string]*model.Inbound{
			"nil":              nil,
			"wrong protocol":   {Protocol: model.Trojan, Settings: `{"clients":[{"email":"a","password":"p","enable":true}]}`},
			"broken settings":  inboundWith(`not json`),
			"no active client": inboundWith(`{"clients":[{"email":"a","password":"p","enable":false}]}`),
		}
		for name, ib := range cases {
			if _, ok := InstanceFromInbound(ib); ok {
				t.Fatalf("%s must not yield an instance", name)
			}
		}
	})
}

func TestFingerprintSplit(t *testing.T) {
	base := Instance{
		Id: 1, Tag: "t", Listen: "0.0.0.0", Port: 8443, SNI: "example.com",
		Users: []UserEntry{{Name: "alice", Password: "pw-a"}, {Name: "bob", Password: "pw-b"}},
	}

	reordered := base
	reordered.Users = []UserEntry{{Name: "bob", Password: "pw-b"}, {Name: "alice", Password: "pw-a"}}
	if base.usersFingerprint() != reordered.usersFingerprint() {
		t.Fatal("reordering clients must not read as a change")
	}
	if base.structuralFingerprint() != reordered.structuralFingerprint() {
		t.Fatal("a user change must not move the structural fingerprint")
	}

	rekeyed := base
	rekeyed.Users = []UserEntry{{Name: "alice", Password: "pw-new"}, {Name: "bob", Password: "pw-b"}}
	if base.usersFingerprint() == rekeyed.usersFingerprint() {
		t.Fatal("a re-keyed client must move the users fingerprint")
	}

	relimited := base
	relimited.Users = []UserEntry{{Name: "alice", Password: "pw-a", QuotaBytes: 10}, {Name: "bob", Password: "pw-b"}}
	if base.usersFingerprint() == relimited.usersFingerprint() {
		t.Fatal("a quota change must move the users fingerprint")
	}

	for name, mutate := range map[string]func(*Instance){
		"port":    func(i *Instance) { i.Port = 9443 },
		"sni":     func(i *Instance) { i.SNI = "other.example" },
		"cert":    func(i *Instance) { i.CertFile = "/c.pem" },
		"forward": func(i *Instance) { i.Forward = "https://example.com" },
		"routing": func(i *Instance) { i.RouteThroughXray, i.XrayRoutePort = true, 1080 },
	} {
		changed := base
		mutate(&changed)
		if base.structuralFingerprint() == changed.structuralFingerprint() {
			t.Fatalf("a %s change must move the structural fingerprint", name)
		}
	}
}

func TestRenderArgs(t *testing.T) {
	inst := Instance{
		Id: 1, Listen: "0.0.0.0", Port: 8443,
		SNI: "example.com", CertFile: "/c.pem", KeyFile: "/k.pem",
		Forward: "https://example.com", PaddingScheme: "/p.txt", Debug: true,
		RouteThroughXray: true, XrayRoutePort: 1080,
	}
	args := strings.Join(renderArgs(inst, "/tmp/users.json", 41234), " ")

	for _, want := range []string{
		"-l 0.0.0.0:8443",
		"--users-file /tmp/users.json",
		"--api-bind-to 127.0.0.1:41234",
		"--sni example.com",
		"--cert /c.pem --key /k.pem",
		"--forward https://example.com",
		"--padding-scheme /p.txt",
		"--outbound-proxy socks5://127.0.0.1:1080",
		"--log debug",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected %q in args:\n%s", want, args)
		}
	}

	// The token must never reach argv: /proc/<pid>/cmdline is world-readable
	// and this token can rewrite every credential the node serves.
	if strings.Contains(args, "--api-token") {
		t.Fatalf("the api token must not be a command line argument:\n%s", args)
	}
	env := tokenEnv("sesame")
	found := false
	for _, kv := range env {
		if kv == "ANYTLS_API_TOKEN=sesame" {
			found = true
		}
	}
	if !found {
		t.Fatal("the api token must be passed through the environment")
	}
	if tokenEnv("") != nil {
		t.Fatal("an empty token must not add an environment entry")
	}
}

func TestRenderArgsOmitsUnsetOptions(t *testing.T) {
	// A half-configured certificate pair would make the node fail to start, so
	// neither half is passed unless both are set.
	inst := Instance{Id: 1, Listen: "127.0.0.1", Port: 8443, CertFile: "/c.pem"}
	args := strings.Join(renderArgs(inst, "/tmp/u.json", 1), " ")
	for _, unwanted := range []string{"--cert", "--key", "--sni", "--forward", "--padding-scheme", "--outbound-proxy", "--log"} {
		if strings.Contains(args, unwanted) {
			t.Fatalf("expected no %s in args:\n%s", unwanted, args)
		}
	}
}

func TestWriteUsersFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	path := filepath.Join(dir, "anytls", "users.json")

	inst := Instance{Users: []UserEntry{
		{Name: "alice", Password: "pw-a", QuotaBytes: 1024, ExpiresUnix: 1780000000},
		{Name: "bob", Password: "pw-b"},
	}}
	if err := writeUsersFile(path, inst); err != nil {
		t.Fatalf("write users file: %v", err)
	}

	var body usersFileBody
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read users file: %v", err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("users file must be valid JSON: %v\n%s", err, raw)
	}
	if body.Users["alice"].Password != "pw-a" || body.Users["alice"].QuotaBytes != 1024 {
		t.Fatalf("alice must carry her password and quota: %+v", body.Users["alice"])
	}
	if body.Users["bob"].QuotaBytes != 0 || body.Users["bob"].ExpiresUnix != 0 {
		t.Fatalf("an unlimited client must serialize without limits: %+v", body.Users["bob"])
	}

	// The file holds every client's password in plain text.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat users file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("users file must be 0600, got %o", perm)
		}
	}
}

package anytls

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// TestNodeContract drives a real anytls-server through the manager, which is the
// only way to catch a drift between this package and the node's management API.
// Skipped unless ANYTLS_E2E_BINARY points at an anytls-server build.
func TestNodeContract(t *testing.T) {
	binary := os.Getenv("ANYTLS_E2E_BINARY")
	if binary == "" {
		t.Skip("set ANYTLS_E2E_BINARY to an anytls-server binary to run the node contract test")
	}

	binDir := t.TempDir()
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read %s: %v", binary, err)
	}
	if err := os.WriteFile(filepath.Join(binDir, GetBinaryName()), payload, 0o755); err != nil {
		t.Fatalf("install node binary: %v", err)
	}
	t.Setenv("XUI_BIN_FOLDER", binDir)

	port := freePort(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}
	t.Cleanup(mgr.StopAll)

	inbound := &model.Inbound{
		Id: 1, Tag: "inbound-anytls", Protocol: model.AnyTLS, Listen: "127.0.0.1", Port: port,
		Settings: `{"sni":"example.com","clients":[
			{"email":"alice","password":"pw-alice","enable":true},
			{"email":"bob","password":"pw-bob","enable":true}
		]}`,
	}
	inst, ok := InstanceFromInbound(inbound)
	if !ok {
		t.Fatal("expected a usable instance")
	}
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("start node: %v", err)
	}
	waitListening(t, port)

	// The node must report exactly the users the panel pushed, or traffic has
	// nowhere to land.
	users := waitStats(t, mgr, 2)
	if _, has := users["alice"]; !has {
		t.Fatalf("expected alice in /stats, got %v", keys(users))
	}
	if _, has := users["bob"]; !has {
		t.Fatalf("expected bob in /stats, got %v", keys(users))
	}

	// A client edit must be applied in place, not by restarting the node.
	orig := mgr.procs[1].proc
	inbound.Settings = `{"sni":"example.com","clients":[
		{"email":"alice","password":"pw-alice","enable":true},
		{"email":"carol","password":"pw-carol","enable":true}
	]}`
	edited, ok := InstanceFromInbound(inbound)
	if !ok {
		t.Fatal("expected a usable instance after the edit")
	}
	if err := mgr.Ensure(edited); err != nil {
		t.Fatalf("apply edit: %v", err)
	}
	if mgr.procs[1].proc != orig {
		t.Fatal("a client edit must not restart the node")
	}
	users = waitStats(t, mgr, 2)
	if _, gone := users["bob"]; gone {
		t.Fatalf("a removed client must disappear from /stats, got %v", keys(users))
	}
	if _, added := users["carol"]; !added {
		t.Fatalf("an added client must appear in /stats, got %v", keys(users))
	}

	// Reset-quota must be accepted for a user the node actually knows.
	mgr.ResetQuota("alice")

	deltas, online := mgr.CollectTraffic()
	if len(deltas) != 0 {
		t.Fatalf("an idle node must report no traffic, got %v", deltas)
	}
	if len(online) != 0 {
		t.Fatalf("an idle node must report nobody online, got %v", online)
	}
}

func keys(m map[string]statsUser) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func freePort(t *testing.T) int {
	t.Helper()
	port, err := FreeLocalPort()
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	return port
}

func waitListening(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("node never listened on 127.0.0.1:%d", port)
}

func waitStats(t *testing.T, mgr *Manager, want int) map[string]statsUser {
	t.Helper()
	mgr.mu.Lock()
	cur := mgr.procs[1]
	port, token := cur.apiPort, cur.apiToken
	mgr.mu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	var last map[string]statsUser
	for time.Now().Before(deadline) {
		users, ok := scrapeStats(port, token)
		if ok {
			last = users
			if len(users) == want {
				return users
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	raw, _ := json.Marshal(last)
	t.Fatalf("expected %d users in /stats, last saw %s", want, raw)
	return nil
}

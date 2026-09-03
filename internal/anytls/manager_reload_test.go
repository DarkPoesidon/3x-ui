package anytls

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary re-exec itself as a stand-in for the
// anytls-server child: with ANYTLS_FAKE_CHILD=1 it records its pid and blocks,
// so the manager can start and stop it without a real node binary.
func TestMain(m *testing.M) {
	if os.Getenv("ANYTLS_FAKE_CHILD") == "1" {
		if f, err := os.OpenFile(os.Getenv("ANYTLS_FAKE_PIDFILE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
		}
		select {}
	}
	os.Exit(m.Run())
}

func installFakeNode(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	payload, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, GetBinaryName()), payload, 0o755); err != nil {
		t.Fatalf("install fake node: %v", err)
	}
	pidFile := filepath.Join(binDir, "anytls-pids.txt")
	t.Setenv("XUI_BIN_FOLDER", binDir)
	t.Setenv("ANYTLS_FAKE_CHILD", "1")
	t.Setenv("ANYTLS_FAKE_PIDFILE", pidFile)
	return pidFile
}

func spawnCount(t *testing.T, pidFile string) int {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	return len(strings.Fields(string(data)))
}

func waitSpawnCount(t *testing.T, pidFile string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := spawnCount(t, pidFile)
		if got == want {
			return
		}
		if got > want {
			t.Fatalf("expected %d spawn(s), got %d", want, got)
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d spawn(s), still %d after timeout", want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func serverPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return port
}

func nodeInst(id int, users ...UserEntry) Instance {
	return Instance{Id: id, Tag: fmt.Sprintf("inbound-%d", id), Listen: "127.0.0.1", Port: 24000 + id, Users: users}
}

func TestEnsureActionFor(t *testing.T) {
	cases := []struct {
		name                                     string
		running                                  bool
		curStruct, curUsers, newStruct, newUsers string
		want                                     ensureAction
	}{
		{"dead process restarts", false, "a", "u", "a", "u", ensureRestart},
		{"structural change restarts", true, "a", "u", "b", "u", ensureRestart},
		{"users change reloads", true, "a", "u", "a", "v", ensureReload},
		{"identical is a noop", true, "a", "u", "a", "u", ensureNoop},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ensureActionFor(tc.running, tc.curStruct, tc.curUsers, tc.newStruct, tc.newUsers)
			if got != tc.want {
				t.Fatalf("ensureActionFor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyUsers(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   bool
	}{
		{"ok", http.StatusOK, true},
		{"not found on an older node", http.StatusNotFound, false},
		{"bad request", http.StatusBadRequest, false},
		{"unauthorized", http.StatusUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath, gotAuth string
			var gotBody usersFileBody
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			inst := nodeInst(1,
				UserEntry{Name: "alice", Password: "pw-a"},
				UserEntry{Name: "bob", Password: "pw-b", QuotaBytes: 1024, ExpiresUnix: 1780000000})
			if got := applyUsers(serverPort(t, srv), "sesame", inst); got != tc.want {
				t.Fatalf("applyUsers = %v, want %v", got, tc.want)
			}
			if gotMethod != http.MethodPut || gotPath != "/users" {
				t.Fatalf("expected PUT /users, got %s %s", gotMethod, gotPath)
			}
			if gotAuth != "Bearer sesame" {
				t.Fatalf("expected the bearer token on the request, got %q", gotAuth)
			}
			if gotBody.Users["alice"].Password != "pw-a" {
				t.Fatalf("payload must carry the password: %+v", gotBody)
			}
			if gotBody.Users["bob"].QuotaBytes != 1024 || gotBody.Users["bob"].ExpiresUnix != 1780000000 {
				t.Fatalf("payload must carry per-client limits: %+v", gotBody.Users["bob"])
			}
		})
	}

	t.Run("refused connection", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		port := serverPort(t, srv)
		srv.Close()
		if applyUsers(port, "", nodeInst(1, UserEntry{Name: "a", Password: "p"})) {
			t.Fatal("a refused connection must yield false")
		}
	})
}

func TestScrapeStats(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/stats" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"users":{"alice":{"connections":2,"bytes_in":10,"bytes_out":20},"bob":{"connections":0,"bytes_in":0,"bytes_out":0}}}`)
	}))
	defer srv.Close()

	users, ok := scrapeStats(serverPort(t, srv), "sesame")
	if !ok {
		t.Fatal("expected a successful scrape")
	}
	if gotAuth != "Bearer sesame" {
		t.Fatalf("expected the bearer token on the request, got %q", gotAuth)
	}
	if users["alice"].Connections != 2 || users["alice"].BytesIn != 10 || users["alice"].BytesOut != 20 {
		t.Fatalf("alice's counters must decode: %+v", users["alice"])
	}
	if _, present := users["bob"]; !present {
		t.Fatal("an idle user must still be reported")
	}
}

func TestScrapeStatsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	port := serverPort(t, srv)
	srv.Close()
	if _, ok := scrapeStats(port, ""); ok {
		t.Fatal("a refused connection must not report success")
	}
}

func TestResetQuotaEncodesEmail(t *testing.T) {
	// An email is not a path segment: one containing a slash would otherwise
	// address a different endpoint entirely.
	for _, email := range []string{"alice@example.com", "a/b@example.com", "spaced name"} {
		t.Run(email, func(t *testing.T) {
			gotPath := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath <- r.URL.EscapedPath()
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			resetQuota(serverPort(t, srv), "sesame", email)
			select {
			case path := <-gotPath:
				rest, ok := strings.CutPrefix(path, "/users/")
				if !ok {
					t.Fatalf("unexpected path %q", path)
				}
				segment, ok := strings.CutSuffix(rest, "/reset-quota")
				if !ok {
					t.Fatalf("unexpected path %q", path)
				}
				if strings.Contains(segment, "/") {
					t.Fatalf("the email must stay one path segment, got %q", path)
				}
				decoded, err := url.PathUnescape(segment)
				if err != nil || decoded != email {
					t.Fatalf("the email must round-trip, got %q (%v) from %q", decoded, err, path)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("expected a reset-quota request")
			}
		})
	}
}

func TestEnsureHotReloadKeepsProcess(t *testing.T) {
	pidFile := installFakeNode(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}

	inst := nodeInst(1, UserEntry{Name: "alice", Password: "pw-a"})
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	waitSpawnCount(t, pidFile, 1)
	orig := mgr.procs[1].proc
	if mgr.procs[1].apiToken == "" {
		t.Fatal("a started process must get an api token")
	}

	reloaded := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/users" {
			reloaded <- struct{}{}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	mgr.procs[1].apiPort = serverPort(t, srv)

	grown := nodeInst(1, UserEntry{Name: "alice", Password: "pw-a"}, UserEntry{Name: "bob", Password: "pw-b"})
	if err := mgr.Ensure(grown); err != nil {
		t.Fatalf("reload ensure: %v", err)
	}

	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("expected a PUT /users request")
	}
	if got := spawnCount(t, pidFile); got != 1 {
		t.Fatalf("hot reload must not spawn a new process, got %d", got)
	}
	if mgr.procs[1].proc != orig {
		t.Fatal("hot reload must keep the same process, so live connections survive")
	}
	if mgr.procs[1].usersFP != grown.usersFingerprint() {
		t.Fatal("stored users fingerprint must advance after a reload")
	}

	// The boot-time file must move with the live set, or a crash-restart would
	// come back with the old users.
	raw, err := os.ReadFile(usersPathForID(1))
	if err != nil {
		t.Fatalf("read users file: %v", err)
	}
	var body usersFileBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("users file must stay valid JSON: %v", err)
	}
	if body.Users["bob"].Password != "pw-b" {
		t.Fatalf("reloaded users file must carry the new client:\n%s", raw)
	}
	mgr.StopAll()
}

func TestEnsureReloadFallbackRestarts(t *testing.T) {
	pidFile := installFakeNode(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}

	if err := mgr.Ensure(nodeInst(2, UserEntry{Name: "alice", Password: "pw-a"})); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	waitSpawnCount(t, pidFile, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	mgr.procs[2].apiPort = serverPort(t, srv)

	if err := mgr.Ensure(nodeInst(2, UserEntry{Name: "carol", Password: "pw-c"})); err != nil {
		t.Fatalf("fallback ensure: %v", err)
	}
	waitSpawnCount(t, pidFile, 2)
	mgr.StopAll()
}

func TestEnsureNoopKeepsProcess(t *testing.T) {
	pidFile := installFakeNode(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}

	inst := nodeInst(3, UserEntry{Name: "alice", Password: "pw-a"}, UserEntry{Name: "bob", Password: "pw-b"})
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	waitSpawnCount(t, pidFile, 1)

	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("repeat ensure: %v", err)
	}
	if got := spawnCount(t, pidFile); got != 1 {
		t.Fatalf("an unchanged instance must not respawn, got %d", got)
	}
	mgr.StopAll()
}

func TestReconcileStopsUnwantedNodes(t *testing.T) {
	pidFile := installFakeNode(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}

	mgr.Reconcile([]Instance{
		nodeInst(4, UserEntry{Name: "alice", Password: "pw-a"}),
		nodeInst(5, UserEntry{Name: "bob", Password: "pw-b"}),
	})
	waitSpawnCount(t, pidFile, 2)

	mgr.Reconcile([]Instance{nodeInst(4, UserEntry{Name: "alice", Password: "pw-a"})})
	if _, still := mgr.procs[5]; still {
		t.Fatal("an inbound dropped from the desired set must have its node stopped")
	}
	if _, kept := mgr.procs[4]; !kept {
		t.Fatal("a still-desired inbound must keep its node")
	}
	if _, err := os.Stat(usersPathForID(5)); !os.IsNotExist(err) {
		t.Fatal("a stopped node's users file must be removed: it holds passwords")
	}
	mgr.StopAll()
}

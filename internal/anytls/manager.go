package anytls

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

type clientCounters struct {
	up   int64
	down int64
}

type managed struct {
	proc         *Process
	tag          string
	structuralFP string
	usersFP      string
	apiPort      int
	apiToken     string
	last         map[string]clientCounters
}

// Manager owns the set of running anytls-server processes keyed by inbound id.
type Manager struct {
	mu    sync.Mutex
	procs map[int]*managed
	// swept records that the one-time sweep of orphaned nodes has run.
	swept bool
}

var (
	managerOnce sync.Once
	manager     *Manager
)

// GetManager returns the process-wide anytls node manager singleton.
func GetManager() *Manager {
	managerOnce.Do(func() {
		manager = &Manager{procs: map[int]*managed{}}
	})
	return manager
}

// Ensure starts the node for an instance, or restarts it when its config
// changed. A no-op when the desired process already runs.
func (m *Manager) Ensure(inst Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepOrphansLocked()
	return m.ensureLocked(inst)
}

// sweepOrphansLocked kills nodes left by a previous x-ui run, once per process
// lifetime and before any of our own start.
func (m *Manager) sweepOrphansLocked() {
	if m.swept {
		return
	}
	m.swept = true
	if n := killStrayNodeProcesses(GetBinaryPath()); n > 0 {
		logger.Warningf("anytls: terminated %d orphaned anytls-server process(es) from a previous run", n)
	}
}

// ensureAction is how ensureLocked moves a running node to a desired instance:
// leave it, hot-apply its user set, or restart it.
type ensureAction int

const (
	ensureNoop ensureAction = iota
	ensureReload
	ensureRestart
)

// A structural change or dead process forces a restart; a users-only change is
// a candidate for an in-place apply.
func ensureActionFor(running bool, curStructFP, curUsersFP, newStructFP, newUsersFP string) ensureAction {
	if !running || curStructFP != newStructFP {
		return ensureRestart
	}
	if curUsersFP != newUsersFP {
		return ensureReload
	}
	return ensureNoop
}

func (m *Manager) ensureLocked(inst Instance) error {
	structFP := inst.structuralFingerprint()
	usersFP := inst.usersFingerprint()
	if cur, ok := m.procs[inst.Id]; ok {
		switch ensureActionFor(cur.proc.IsRunning(), cur.structuralFP, cur.usersFP, structFP, usersFP) {
		case ensureNoop:
			cur.tag = inst.Tag
			return nil
		case ensureReload:
			// Write the file first, so a crash-restart comes back with the
			// user set the panel last asked for.
			if err := writeUsersFile(usersPathForID(inst.Id), inst); err != nil {
				return err
			}
			if applyUsers(cur.apiPort, cur.apiToken, inst) {
				cur.tag = inst.Tag
				cur.usersFP = usersFP
				logger.Infof("anytls: applied user update to inbound %d in place", inst.Id)
				return nil
			}
			logger.Warningf("anytls: live user update unavailable for inbound %d, restarting", inst.Id)
			fallthrough
		case ensureRestart:
			_ = cur.proc.Stop()
			delete(m.procs, inst.Id)
		}
	}
	// Say plainly that the sidecar is missing: the raw exec error names a path
	// with no hint that the release is expected to ship this binary.
	if _, err := os.Stat(GetBinaryPath()); err != nil {
		return fmt.Errorf("anytls: sidecar binary %s is missing, so this inbound cannot start: %w", GetBinaryName(), err)
	}
	apiPort, err := FreeLocalPort()
	if err != nil {
		return err
	}
	apiToken, err := newAPIToken()
	if err != nil {
		return err
	}
	usersPath := usersPathForID(inst.Id)
	if err := writeUsersFile(usersPath, inst); err != nil {
		return err
	}
	proc := newProcess(renderArgs(inst, usersPath, apiPort), tokenEnv(apiToken), fmt.Sprintf("inbound %d", inst.Id))
	if err := proc.Start(); err != nil {
		return err
	}
	m.procs[inst.Id] = &managed{
		proc:         proc,
		tag:          inst.Tag,
		structuralFP: structFP,
		usersFP:      usersFP,
		apiPort:      apiPort,
		apiToken:     apiToken,
		last:         map[string]clientCounters{},
	}
	logger.Infof("anytls: started anytls-server for inbound %d on %s", inst.Id, inst.bindTo())
	return nil
}

// Remove stops and forgets the node process for an inbound id.
func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.procs[id]; ok {
		_ = cur.proc.Stop()
		delete(m.procs, id)
		_ = os.Remove(usersPathForID(id))
		logger.Infof("anytls: stopped anytls-server for inbound %d", id)
	}
}

// Reconcile stops processes no longer wanted and (re)starts the rest, at boot
// and periodically to recover from crashes.
func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepOrphansLocked()
	want := make(map[int]struct{}, len(desired))
	for _, inst := range desired {
		want[inst.Id] = struct{}{}
	}
	for id, cur := range m.procs {
		if _, ok := want[id]; !ok {
			_ = cur.proc.Stop()
			delete(m.procs, id)
			_ = os.Remove(usersPathForID(id))
		}
	}
	for _, inst := range desired {
		if err := m.ensureLocked(inst); err != nil {
			logger.Warningf("anytls: reconcile failed for inbound %d: %v", inst.Id, err)
		}
	}
}

// StopAll stops every managed node process. Called on panel shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cur := range m.procs {
		_ = cur.proc.Stop()
		_ = os.Remove(usersPathForID(id))
		delete(m.procs, id)
	}
}

// CollectTraffic returns per-client deltas since the last scrape plus online
// emails. Counters are absolute, so a node restart reads as a clamped-to-0 dip.
func (m *Manager) CollectTraffic() ([]Traffic, []string) {
	type snap struct {
		id       int
		apiPort  int
		apiToken string
		tag      string
		last     map[string]clientCounters
	}
	m.mu.Lock()
	snaps := make([]snap, 0, len(m.procs))
	for id, cur := range m.procs {
		if cur.proc == nil || !cur.proc.IsRunning() {
			continue
		}
		lastCopy := make(map[string]clientCounters, len(cur.last))
		maps.Copy(lastCopy, cur.last)
		snaps = append(snaps, snap{id: id, apiPort: cur.apiPort, apiToken: cur.apiToken, tag: cur.tag, last: lastCopy})
	}
	m.mu.Unlock()

	var out []Traffic
	var online []string
	for _, s := range snaps {
		users, ok := scrapeStats(s.apiPort, s.apiToken)
		if !ok {
			continue
		}
		newLast := make(map[string]clientCounters, len(users))
		for email, u := range users {
			up := u.BytesIn
			down := u.BytesOut
			newLast[email] = clientCounters{up: up, down: down}
			if u.Connections > 0 {
				online = append(online, email)
			}
			prev, had := s.last[email]
			if !had {
				continue
			}
			du := up - prev.up
			dd := down - prev.down
			if du < 0 {
				du = 0
			}
			if dd < 0 {
				dd = 0
			}
			if du > 0 || dd > 0 {
				out = append(out, Traffic{Tag: s.tag, Email: email, Up: du, Down: dd})
			}
		}

		m.mu.Lock()
		if cur, ok := m.procs[s.id]; ok {
			cur.last = newLast
		}
		m.mu.Unlock()
	}
	return out, online
}

// HasRunning reports whether any node process is currently alive.
func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cur := range m.procs {
		if cur.proc != nil && cur.proc.IsRunning() {
			return true
		}
	}
	return false
}

// ResetQuota opens a new quota window for one client on every running node,
// leaving the reported totals monotonic so delta accounting is unaffected.
func (m *Manager) ResetQuota(email string) {
	if email == "" {
		return
	}
	type target struct {
		port  int
		token string
	}
	m.mu.Lock()
	targets := make([]target, 0, len(m.procs))
	for _, cur := range m.procs {
		if cur.proc != nil && cur.proc.IsRunning() {
			targets = append(targets, target{cur.apiPort, cur.apiToken})
		}
	}
	m.mu.Unlock()
	for _, t := range targets {
		resetQuota(t.port, t.token, email)
	}
}

// renderArgs builds the node command line. Everything but the users file is
// structural, so a change to it needs the restart structuralFingerprint forces.
func renderArgs(inst Instance, usersPath string, apiPort int) []string {
	args := []string{
		"-l", inst.bindTo(),
		"--users-file", usersPath,
		"--api-bind-to", fmt.Sprintf("127.0.0.1:%d", apiPort),
	}
	if inst.SNI != "" {
		args = append(args, "--sni", inst.SNI)
	}
	if inst.CertFile != "" && inst.KeyFile != "" {
		args = append(args, "--cert", inst.CertFile, "--key", inst.KeyFile)
	}
	if inst.Forward != "" {
		args = append(args, "--forward", inst.Forward)
	}
	if inst.PaddingScheme != "" {
		args = append(args, "--padding-scheme", inst.PaddingScheme)
	}
	// Egress through the loopback SOCKS bridge the panel injects into the
	// generated Xray config, so it obeys the core's routing rules.
	if inst.RouteThroughXray && inst.XrayRoutePort > 0 {
		args = append(args, "--outbound-proxy", fmt.Sprintf("socks5://127.0.0.1:%d", inst.XrayRoutePort))
	}
	if inst.Debug {
		args = append(args, "--log", "debug")
	}
	return args
}

// tokenEnv keeps the API token out of argv: /proc/<pid>/cmdline is
// world-readable on Linux, and the token can rewrite every credential.
func tokenEnv(apiToken string) []string {
	if apiToken == "" {
		return nil
	}
	return append(os.Environ(), "ANYTLS_API_TOKEN="+apiToken)
}

// usersFileEntry is one user in the users file and in a PUT /users body.
type usersFileEntry struct {
	Password    string `json:"password"`
	QuotaBytes  int64  `json:"quota_bytes,omitempty"`
	ExpiresUnix int64  `json:"expires_unix,omitempty"`
}

type usersFileBody struct {
	Users map[string]usersFileEntry `json:"users"`
}

func usersPayload(inst Instance) usersFileBody {
	users := make(map[string]usersFileEntry, len(inst.Users))
	for _, u := range inst.Users {
		users[u.Name] = usersFileEntry{
			Password:    u.Password,
			QuotaBytes:  u.QuotaBytes,
			ExpiresUnix: u.ExpiresUnix,
		}
	}
	return usersFileBody{Users: users}
}

// writeUsersFile persists the boot-time user set. It holds every client's
// password in plain text, hence 0600 in a 0700 directory.
func writeUsersFile(path string, inst Instance) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(usersPayload(inst))
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

// statsUser is one /stats entry: bytes_in is the client's upload, bytes_out its
// download, both absolute since the node started.
type statsUser struct {
	Connections int64 `json:"connections"`
	BytesIn     int64 `json:"bytes_in"`
	BytesOut    int64 `json:"bytes_out"`
}

func scrapeStats(port int, token string) (map[string]statsUser, bool) {
	client := http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint(port, "/stats"), nil)
	if err != nil {
		return nil, false
	}
	authorize(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	var parsed struct {
		Users map[string]statsUser `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, false
	}
	return parsed.Users, true
}

// applyUsers pushes the user set to a running node, which keeps every
// connection whose password is unchanged. Only a 200 spares the caller a restart.
func applyUsers(port int, token string, inst Instance) bool {
	body, err := json.Marshal(usersPayload(inst))
	if err != nil {
		return false
	}
	client := http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, endpoint(port, "/users"), bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	authorize(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func resetQuota(port int, token, email string) {
	client := http.Client{Timeout: 3 * time.Second}
	path := fmt.Sprintf("/users/%s/reset-quota", url.PathEscape(email))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint(port, path), nil)
	if err != nil {
		return
	}
	authorize(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

func endpoint(port int, path string) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + path
}

// FreeLocalPort asks the OS for an unused loopback TCP port, for the node's
// management API and its SOCKS egress bridge.
func FreeLocalPort() (int, error) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// newAPIToken mints the bearer token a node and its manager share for that
// process's life; PUT /users can replace every credential the node serves.
func newAPIToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func authorize(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

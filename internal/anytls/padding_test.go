package anytls

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrongSchemeIsValid(t *testing.T) {
	if err := ValidateScheme(strongPaddingScheme); err != nil {
		t.Fatalf("the shipped strong scheme must load: %v", err)
	}
}

func TestValidateSchemeRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"no stop":          "0=30-30",
		"stop not numeric": "stop=lots\n0=30-30",
		"stop zero":        "stop=0\n0=30-30",
		"key not an index": "stop=2\nfirst=30-30",
		"range reversed":   "stop=2\n0=900-100",
		"range not sizes":  "stop=2\n0=small-large",
		"empty spec list":  "stop=2\n0=",
	}
	for name, scheme := range cases {
		if err := ValidateScheme(scheme); err == nil {
			t.Errorf("%s: expected a rejection, got none", name)
		}
	}
}

func TestValidateSchemeAcceptsCommentsAndChecks(t *testing.T) {
	scheme := "# a comment\nstop=3\n0=100-200\n1=50-90,c,300-900\n\n2=10-10\n"
	if err := ValidateScheme(scheme); err != nil {
		t.Fatalf("expected acceptance, got %v", err)
	}
}

func TestPaddingModeOfDefaultsForOlderInbounds(t *testing.T) {
	// Inbounds saved before paddingMode existed carry only a path, or nothing.
	if got := PaddingModeOf("", "/etc/x-ui/anytls/padding.txt"); got != PaddingModeFile {
		t.Errorf("a bare path must mean file mode, got %q", got)
	}
	if got := PaddingModeOf("", ""); got != PaddingModeDefault {
		t.Errorf("no padding config must mean default mode, got %q", got)
	}
	if got := PaddingModeOf("strong", ""); got != PaddingModeStrong {
		t.Errorf("an explicit mode must win, got %q", got)
	}
	if got := PaddingModeOf("nonsense", ""); got != PaddingModeDefault {
		t.Errorf("an unknown mode must fall back to default, got %q", got)
	}
}

func TestPreparePaddingFileWritesManagedScheme(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)

	path, err := preparePaddingFile(Instance{Id: 7, PaddingMode: PaddingModeStrong})
	if err != nil {
		t.Fatalf("strong mode must write a file: %v", err)
	}
	if want := filepath.Join(dir, "anytls", "anytls-7.padding.txt"); filepath.Clean(path) != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("written scheme must be readable: %v", err)
	}
	if string(body) != strongPaddingScheme {
		t.Fatal("the written scheme must be the strong one")
	}
}

func TestPreparePaddingFileDefaultModeWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)

	path, err := preparePaddingFile(Instance{Id: 7, PaddingMode: PaddingModeDefault})
	if err != nil {
		t.Fatalf("default mode must not fail: %v", err)
	}
	if path != "" {
		t.Fatalf("default mode must pass no scheme file, got %q", path)
	}
}

func TestPreparePaddingFileRejectsInvalidCustom(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)

	_, err := preparePaddingFile(Instance{Id: 7, PaddingMode: PaddingModeCustom, PaddingSchemeText: "0=30-30"})
	if err == nil {
		t.Fatal("a custom scheme with no stop= must be refused before the node starts")
	}
}

func TestPreparePaddingFileKeepsAdminPath(t *testing.T) {
	path, err := preparePaddingFile(Instance{Id: 7, PaddingMode: PaddingModeFile, PaddingScheme: "/etc/mine.txt"})
	if err != nil {
		t.Fatalf("file mode must not fail: %v", err)
	}
	if path != "/etc/mine.txt" {
		t.Fatalf("file mode must pass the admin's path, got %q", path)
	}
}

func TestCheckInstanceFilesNamesTheMissingFile(t *testing.T) {
	// The bug this guards: a path that does not exist made anytls-server exit
	// with a bare ENOENT naming nothing, restarted on every reconcile tick.
	err := checkInstanceFiles(Instance{PaddingMode: PaddingModeFile, PaddingScheme: "/nope/padding.txt"})
	if err == nil {
		t.Fatal("a missing padding file must stop the start")
	}
	if !strings.Contains(err.Error(), "/nope/padding.txt") {
		t.Fatalf("the error must name the file, got %v", err)
	}

	err = checkInstanceFiles(Instance{CertFile: "/nope/cert.pem", KeyFile: "/nope/key.pem"})
	if err == nil || !strings.Contains(err.Error(), "certificate file") {
		t.Fatalf("a missing certificate must be reported, got %v", err)
	}

	if err := checkInstanceFiles(Instance{PaddingMode: PaddingModeStrong}); err != nil {
		t.Fatalf("a panel-managed scheme has no admin path to check: %v", err)
	}
}

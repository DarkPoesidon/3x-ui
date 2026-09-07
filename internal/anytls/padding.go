package anytls

import (
	"fmt"
	"strconv"
	"strings"
)

// Padding modes an anytls inbound can carry in `paddingMode`.
//
// The sidecar takes a padding scheme as a file path, which used to mean the
// admin had to create that file on the host by hand. A path typed into a web
// form is not checked by anything, and a missing file makes anytls-server exit
// with a bare ENOENT the moment it starts -- the panel then restarts it on the
// next reconcile tick, forever, with nothing in the UI to say why. So the panel
// owns the scheme content instead, and writes the file itself next to the users
// file it already manages. PaddingModeFile stays for admins who really do want
// to manage their own file, and for inbounds created before this existed.
const (
	PaddingModeDefault = "default" // no --padding-scheme; the sidecar's built-in scheme
	PaddingModeStrong  = "strong"  // the hardened scheme below, written by the panel
	PaddingModeCustom  = "custom"  // paddingSchemeText, written by the panel
	PaddingModeFile    = "file"    // paddingScheme, a path the admin maintains
)

// strongPaddingScheme is a hardened alternative to the sidecar's built-in
// default. The default is fingerprintable by shape alone: its first record is
// always exactly 30 bytes and its fourth starts with a 9-byte fragment, which
// is a trivial rule for a DPI box to match. This one uses no fixed sizes, wider
// ranges, and a longer stop, so the early records of a session look less alike
// across connections.
//
// Every spec not preceded by `c` emits a record even with no payload to carry,
// which is the cover traffic; specs after a `c` only materialize when real data
// remains. Keeping the unconditional ones small and the wide ones behind `c`
// buys the camouflage without paying for it on idle sessions.
const strongPaddingScheme = `stop=14
0=520-1180
1=180-760,c,640-1720
2=300-900,c,700-2100,c,420-1300
3=240-880
4=160-640,c,880-2400
5=340-1120
6=200-780,c,560-1680,c,760-2200
7=280-960
8=150-700,c,900-2600
9=380-1240
10=220-820,c,640-1900
11=300-1040
12=180-720,c,780-2300
13=420-1360
`

// SchemeTextFor returns the scheme content the panel should write for this
// instance, and whether a managed file is needed at all. A false second return
// means either no padding file (default mode) or a path the admin owns.
func SchemeTextFor(mode, custom string) (string, bool) {
	switch mode {
	case PaddingModeStrong:
		return strongPaddingScheme, true
	case PaddingModeCustom:
		return custom, true
	default:
		return "", false
	}
}

// ValidateScheme rejects a scheme the sidecar would refuse to load. It mirrors
// PaddingFactory::new in anytls-rs: the map must be non-empty and carry a
// numeric `stop`. The per-index checks go further than the sidecar does, which
// silently ignores a malformed range, so a typo surfaces at save time instead
// of quietly disabling the padding it was supposed to configure.
func ValidateScheme(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("padding scheme is empty")
	}
	stopSeen := false
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("line %d: expected key=value, got %q", i+1, line)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "stop" {
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil || n == 0 {
				return fmt.Errorf("line %d: stop must be a positive number, got %q", i+1, value)
			}
			stopSeen = true
			continue
		}
		if _, err := strconv.ParseUint(key, 10, 32); err != nil {
			return fmt.Errorf("line %d: %q is neither \"stop\" nor a packet index", i+1, key)
		}
		if err := validateSpecList(value); err != nil {
			return fmt.Errorf("line %d (%s): %w", i+1, key, err)
		}
	}
	if !stopSeen {
		return fmt.Errorf("padding scheme has no \"stop=\" line")
	}
	return nil
}

func validateSpecList(value string) error {
	if value == "" {
		return fmt.Errorf("no size ranges")
	}
	for _, spec := range strings.Split(value, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "c" {
			continue
		}
		lo, hi, ok := strings.Cut(spec, "-")
		if !ok {
			return fmt.Errorf("%q is neither \"c\" nor a min-max range", spec)
		}
		min, errMin := strconv.Atoi(strings.TrimSpace(lo))
		max, errMax := strconv.Atoi(strings.TrimSpace(hi))
		if errMin != nil || errMax != nil || min <= 0 || max <= 0 {
			return fmt.Errorf("%q must be two positive byte counts", spec)
		}
		if min > max {
			return fmt.Errorf("%q has its minimum above its maximum", spec)
		}
	}
	return nil
}

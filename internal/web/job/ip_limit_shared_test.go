package job

import "testing"

// The failure this guards, seen on a live panel: five inbounds were served
// through Cloudflare, so Xray recorded the edge address as the client's. One
// client roaming the edges accumulated 955 "distinct IPs", tripped its own
// limit, and every ban took out an address other customers were arriving on.
func TestSharedAddressesFindsIntermediaries(t *testing.T) {
	observed := map[string]map[string]int64{
		"alice": {"104.28.155.79": 1, "5.202.123.136": 1},
		"bob":   {"104.28.155.79": 1, "37.156.153.216": 1},
		"carol": {"104.28.155.79": 1},
	}
	shared := sharedAddresses(observed)

	if _, ok := shared["104.28.155.79"]; !ok {
		t.Fatal("an address serving three clients must be treated as shared")
	}
	for _, own := range []string{"5.202.123.136", "37.156.153.216"} {
		if _, ok := shared[own]; ok {
			t.Errorf("%s belongs to one client and must stay bannable", own)
		}
	}
}

func TestSharedAddressesOnEmptyScan(t *testing.T) {
	if len(sharedAddresses(map[string]map[string]int64{})) != 0 {
		t.Fatal("an empty scan shares nothing")
	}
}

// A client's own address stays bannable — the limit has to keep working for
// ordinary direct connections.
func TestBanIsCollateralOnlyForSharedAddresses(t *testing.T) {
	j := &CheckClientIpJob{sharedIps: map[string]struct{}{"172.71.178.148": {}}}

	if !j.banIsCollateral("172.71.178.148") {
		t.Fatal("a shared address must not be handed to fail2ban")
	}
	if j.banIsCollateral("5.202.123.136") {
		t.Fatal("an address seen for a single client must stay bannable")
	}
}

// With no scan recorded the job must not start withholding bans.
func TestBanIsCollateralWithoutAScan(t *testing.T) {
	j := &CheckClientIpJob{}
	if j.banIsCollateral("5.202.123.136") {
		t.Fatal("an unpopulated scan must not exempt anything")
	}
}

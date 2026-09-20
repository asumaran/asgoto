package main

import "testing"

func TestTicketFrom(t *testing.T) {
	cases := []struct {
		branch, label, want string
	}{
		{"feat-FED-2030-stargate-oci-pipeline", "", "FED-2030"},
		{"FED-2035-stargate-stg-oci-validation", "", "FED-2035"},
		{"fed-2031", "", "FED-2031"},
		{"cronus/plat-1193-e2e-encryption-proof", "", "PLAT-1193"},
		{"", "fed-2031", "FED-2031"},                    // label fallback
		{"feat-observability-wiring", "some-label", ""}, // no ticket anywhere
		{"feat-digital-lhciFixClosedServerConnection", "", ""},
		{"e2e-tests", "", ""},      // digit inside the key: not a ticket
		{"v1-2-migration", "", ""}, // single letter before the dash: not a ticket
		{"main", "", ""},
		{"feat-FED-2030-FED-2031-x", "", "FED-2030"}, // first match wins
		{"fix_eshop-270_ssr", "", "ESHOP-270"},       // an underscore is no word boundary
	}
	for _, c := range cases {
		if got := ticketFrom(c.branch, c.label); got != c.want {
			t.Errorf("ticketFrom(%q, %q) = %q, want %q", c.branch, c.label, got, c.want)
		}
	}
}

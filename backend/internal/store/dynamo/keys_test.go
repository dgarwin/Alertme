package dynamo

import (
	"testing"
	"time"
)

func TestKeyFormats(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"userPK", userPK("u1"), "USER#u1"},
		{"devicSK", devicSK("tok1"), "DEVICE#tok1"},
		{"pairSK", pairSK("peer1"), "PAIR#peer1"},
		{"invitePK", invitePK("CODE1"), "INVITE#CODE1"},
		{"pagePK", pagePK("p1"), "PAGE#p1"},
		{"idemPK", idemPK("key1"), "IDEM#key1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
	}
}

func TestEventSK(t *testing.T) {
	at := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	got := eventSK(at, "created")
	want := "EVT#2026-07-04T12:00:00Z#created"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A non-UTC input must be normalized to UTC before formatting so ordering
	// stays consistent regardless of the caller's time zone.
	loc := time.FixedZone("EST", -5*60*60)
	inEST := time.Date(2026, 7, 4, 7, 0, 0, 0, loc) // == 12:00 UTC
	if got := eventSK(inEST, "created"); got != want {
		t.Errorf("non-UTC input: got %q, want %q", got, want)
	}
}

func TestPageGSISKsMatchFormatAndLexicalOrder(t *testing.T) {
	earlier := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	gsi1Earlier := pageGSI1SK(earlier, "p1")
	gsi1Later := pageGSI1SK(later, "p2")
	if gsi1Earlier >= gsi1Later {
		t.Errorf("GSI1SK lexical order does not match chronological order: %q >= %q", gsi1Earlier, gsi1Later)
	}

	// GSI2 must use the exact same format as GSI1 so ListPagesFor can build
	// one "since" boundary expression and reuse it against either index.
	if got, want := pageGSI2SK(earlier, "p1"), pageGSI1SK(earlier, "p1"); got != want {
		t.Errorf("pageGSI2SK format diverges from pageGSI1SK: got %q, want %q", got, want)
	}

	want := "PAGE#2026-01-01T00:00:00Z#p1"
	if gsi1Earlier != want {
		t.Errorf("got %q, want %q", gsi1Earlier, want)
	}
}

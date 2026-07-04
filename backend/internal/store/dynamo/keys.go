package dynamo

import (
	"fmt"
	"time"
)

// Key schema for the single table (PLAN-AWS.md §3). Implemented here, in one
// place, so every item type reads and writes the same shapes:
//
//	entity        PK              SK                    GSI1PK              GSI1SK
//	user          USER#<id>       PROFILE               —                   —
//	device        USER#<id>       DEVICE#<token>        —                   —
//	pairing       USER#<id>       PAIR#<peer>           —                   —
//	invite        INVITE#<code>   META                  —                   —
//	page          PAGE#<id>       META                  USER#<recipient>    PAGE#<rfc3339 created>#<id>
//	page event    PAGE#<id>       EVT#<rfc3339>#<type>  —                   —
//	idempotency   IDEM#<key>      META                  —                   —
//
// Sender history: page items also carry GSI1 mirror attributes under the
// sender via a second item projection is NOT used; instead ListPagesFor
// queries GSI1 for the recipient and a sender-keyed duplicate attribute set
// (GSI1PK2 pattern) is deliberately avoided — the sender polls specific page
// IDs it created, and history merges both via two queries in the store impl.

func userPK(id string) string        { return "USER#" + id }
func devicSK(token string) string    { return "DEVICE#" + token }
func pairSK(peerID string) string    { return "PAIR#" + peerID }
func invitePK(code string) string    { return "INVITE#" + code }
func pagePK(id string) string        { return "PAGE#" + id }
func idemPK(key string) string       { return "IDEM#" + key }
func eventSK(at time.Time, typ string) string {
	return fmt.Sprintf("EVT#%s#%s", at.UTC().Format(time.RFC3339Nano), typ)
}
func pageGSI1SK(created time.Time, id string) string {
	return fmt.Sprintf("PAGE#%s#%s", created.UTC().Format(time.RFC3339), id)
}

const skProfile = "PROFILE"
const skMeta = "META"

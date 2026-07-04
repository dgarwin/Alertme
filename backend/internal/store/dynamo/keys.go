package dynamo

import (
	"fmt"
	"time"
)

// Key schema for the single table (PLAN-AWS.md §3). Implemented here, in one
// place, so every item type reads and writes the same shapes:
//
//	entity        PK              SK                    GSI1PK              GSI1SK                       GSI2PK           GSI2SK
//	user          USER#<id>       PROFILE               —                   —                            —                —
//	device        USER#<id>       DEVICE#<token>        —                   —                            —                —
//	pairing       USER#<id>       PAIR#<peer>           —                   —                            —                —
//	invite        INVITE#<code>   META                  —                   —                            —                —
//	page          PAGE#<id>       META                  USER#<recipient>    PAGE#<rfc3339 created>#<id>  USER#<sender>    PAGE#<rfc3339 created>#<id>
//	page event    PAGE#<id>       EVT#<rfc3339>#<type>  —                   —                            —                —
//	idempotency   IDEM#<key>      META                  —                   —                            —                —
//
// Sender history: GSI1 (recipient) is not enough on its own — ListPagesFor
// must also return pages the caller sent. Rather than fold that into GSI1's
// key (which would collide inbox and sent-history queries on one index),
// page items carry a second projection, GSI2 (GSI2PK = USER#<sender>,
// GSI2SK = same "PAGE#<rfc3339 created>#<id>" format as GSI1SK). The store
// implementation queries both indexes and merges/dedupes/sorts in Go.

func userPK(id string) string     { return "USER#" + id }
func devicSK(token string) string { return "DEVICE#" + token }
func pairSK(peerID string) string { return "PAIR#" + peerID }
func invitePK(code string) string { return "INVITE#" + code }
func pagePK(id string) string     { return "PAGE#" + id }
func idemPK(key string) string    { return "IDEM#" + key }
func eventSK(at time.Time, typ string) string {
	return fmt.Sprintf("EVT#%s#%s", at.UTC().Format(time.RFC3339Nano), typ)
}
func pageGSI1SK(created time.Time, id string) string {
	return fmt.Sprintf("PAGE#%s#%s", created.UTC().Format(time.RFC3339), id)
}

// pageGSI2SK mirrors pageGSI1SK's format so ListPagesFor can build the same
// "since" lower-bound key expression against either index.
func pageGSI2SK(created time.Time, id string) string {
	return pageGSI1SK(created, id)
}

const skProfile = "PROFILE"
const skMeta = "META"

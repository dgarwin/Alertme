package api

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

// crockford32Alphabet excludes I, L, O, U to avoid transcription mistakes —
// standard Crockford Base32.
const crockford32Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// randomInviteCode returns a random n-character Crockford Base32 string, used
// for invite codes (10 chars per the route contract).
func randomInviteCode(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(crockford32Alphabet)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err) // crypto/rand failure is unrecoverable
		}
		b[i] = crockford32Alphabet[idx.Int64()]
	}
	return string(b)
}

// randomID returns a random hex identifier for pages and users created
// server-side; no particular format is required by the route contracts.
func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

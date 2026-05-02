// Package subscriber — Pool + lookup index tests.
//
// Purpose:
//
//	Verifies NewPool builds the expected indexes (id, username,
//	acct-session-id, framed-ip-address) and Pool.Lookup dispatches an
//	inbound packet to the right SubscriberTarget by walking the
//	priority order documented in pool.go (session-id > username >
//	framed-ip).
//
// Related files:
//   - pkg/subscriber/pool.go
//   - pkg/subscriber/lookup.go
//
// Briefing: .orchestration/briefings/2b-subscriber.md
package subscriber

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

func mkCreds(n int) []config.Credential {
	out := make([]config.Credential, n)
	for i := 0; i < n; i++ {
		out[i] = config.Credential{
			Username: byteName('a' + byte(i)),
			Password: "p",
		}
	}
	return out
}

func byteName(b byte) string { return string([]byte{b}) }

func TestNewPool_BuildsIndexes(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 3
	creds := []config.Credential{
		{Username: "alpha", Password: "1"},
		{Username: "bravo", Password: "2"},
		{Username: "charlie", Password: "3"},
	}
	p := NewPool(creds, cfg)
	require.NotNil(t, p)
	require.Equal(t, 3, p.Len())

	// Get by id
	require.NotNil(t, p.Get(0))
	require.NotNil(t, p.Get(1))
	require.NotNil(t, p.Get(2))
	assert.Nil(t, p.Get(99))

	// Lookup by username
	assert.Equal(t, "alpha", p.LookupByUsername("alpha").Username())
	assert.Equal(t, "bravo", p.LookupByUsername("bravo").Username())
	assert.Nil(t, p.LookupByUsername(""))
	assert.Nil(t, p.LookupByUsername("nobody"))

	// Lookup by session id
	for _, sub := range []*Subscriber{p.Get(0), p.Get(1), p.Get(2)} {
		assert.Same(t, sub, p.LookupBySessionID(sub.SessionID()))
	}
	assert.Nil(t, p.LookupBySessionID(""))

	// Lookup by framed-ip
	for _, sub := range []*Subscriber{p.Get(0), p.Get(1), p.Get(2)} {
		assert.Same(t, sub, p.LookupByFramedIP(sub.FramedIP().String()))
	}
	assert.Nil(t, p.LookupByFramedIP(""))
}

func TestNewPool_RecyclesCredentialsWithUniqueUsernames(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 5
	creds := []config.Credential{
		{Username: "u1", Password: "p"},
		{Username: "u2", Password: "p"},
	}
	p := NewPool(creds, cfg)
	require.Equal(t, 5, p.Len())

	usernames := make(map[string]bool)
	for i := 0; i < 5; i++ {
		u := p.Get(uint32(i)).Username()
		assert.False(t, usernames[u], "username %q was duplicated for id %d", u, i)
		usernames[u] = true
	}
	// First two keep originals; subsequent are suffixed.
	assert.Equal(t, "u1", p.Get(0).Username())
	assert.Equal(t, "u2", p.Get(1).Username())
	assert.Equal(t, "u1+2", p.Get(2).Username())
	assert.Equal(t, "u2+3", p.Get(3).Username())
	assert.Equal(t, "u1+4", p.Get(4).Username())
}

func TestNewPool_EmptyConfig(t *testing.T) {
	p := NewPool(nil, nil)
	require.NotNil(t, p)
	assert.Equal(t, 0, p.Len())
	assert.Nil(t, p.LookupByUsername("anyone"))
}

func TestPool_Range(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 4
	p := NewPool(mkCreds(4), cfg)

	var seen []uint32
	p.Range(func(s *Subscriber) { seen = append(seen, s.ID()) })
	assert.Equal(t, []uint32{0, 1, 2, 3}, seen)
}

func TestPool_Lookup_PrefersSessionID(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 2
	p := NewPool([]config.Credential{
		{Username: "first", Password: "p"},
		{Username: "second", Password: "p"},
	}, cfg)

	target := p.Get(1)
	pkt := &radius.Packet{Code: radius.CodeCoARequest}
	require.NoError(t, pkt.Attributes.AddString(radius.AttrUserName, "first")) // wrong subscriber
	require.NoError(t, pkt.Attributes.AddString(radius.AttrAcctSessionID, target.SessionID()))

	got, ok := p.Lookup(pkt)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, target.ID(), got.ID(), "session-id should win over username")
}

func TestPool_Lookup_FallsBackToUsername(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 1
	p := NewPool([]config.Credential{{Username: "lone", Password: "p"}}, cfg)

	pkt := &radius.Packet{Code: radius.CodeDisconnectRequest}
	require.NoError(t, pkt.Attributes.AddString(radius.AttrUserName, "lone"))

	got, ok := p.Lookup(pkt)
	require.True(t, ok)
	assert.Equal(t, uint32(0), got.ID())
}

func TestPool_Lookup_FallsBackToFramedIP(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 1
	p := NewPool([]config.Credential{{Username: "fipuser", Password: "p"}}, cfg)
	target := p.Get(0)

	pkt := &radius.Packet{Code: radius.CodeDisconnectRequest}
	require.NoError(t, pkt.Attributes.AddIPv4(radius.AttrFramedIPAddress, target.FramedIP()))

	got, ok := p.Lookup(pkt)
	require.True(t, ok)
	assert.Equal(t, target.ID(), got.ID())
}

func TestPool_Lookup_NoMatch(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 1
	p := NewPool([]config.Credential{{Username: "x", Password: "p"}}, cfg)

	pkt := &radius.Packet{Code: radius.CodeCoARequest}
	require.NoError(t, pkt.Attributes.AddString(radius.AttrUserName, "ghost"))
	got, ok := p.Lookup(pkt)
	assert.False(t, ok)
	assert.Nil(t, got)

	got, ok = p.Lookup(nil)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestBuildLookup_NilPool(t *testing.T) {
	fn := BuildLookup(nil)
	require.NotNil(t, fn)
	assert.Nil(t, fn(&radius.Packet{}))
}

func TestBuildLookup_DispatchesViaPool(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 1
	p := NewPool([]config.Credential{{Username: "dispatch", Password: "p"}}, cfg)
	fn := BuildLookup(p)

	pkt := &radius.Packet{Code: radius.CodeCoARequest}
	require.NoError(t, pkt.Attributes.AddString(radius.AttrUserName, "dispatch"))
	got := fn(pkt)
	require.NotNil(t, got)
	assert.Equal(t, uint32(0), got.ID())

	// Miss returns nil (not a typed nil — confirm).
	miss := fn(&radius.Packet{})
	assert.Nil(t, miss)
}

// SubscriberTarget is duck-typed; verify *Subscriber satisfies it
// at compile time.
var _ SubscriberTarget = (*Subscriber)(nil)

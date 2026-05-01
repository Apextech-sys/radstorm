// Package radius — tests for Code constants and helpers.
//
// Purpose:
//
//	Verifies the Code.String / IsRequest / IsResponse classifiers cover
//	every code radstorm sends or receives. Cheap unit tests; no I/O.
//
// Related files:
//   - pkg/radius/code.go
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: tests; no public surface.
package radius

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCodeString(t *testing.T) {
	cases := map[Code]string{
		CodeAccessRequest:      "Access-Request",
		CodeAccessAccept:       "Access-Accept",
		CodeAccessReject:       "Access-Reject",
		CodeAccountingRequest:  "Accounting-Request",
		CodeAccountingResponse: "Accounting-Response",
		CodeAccessChallenge:    "Access-Challenge",
		CodeDisconnectRequest:  "Disconnect-Request",
		CodeDisconnectACK:      "Disconnect-ACK",
		CodeDisconnectNAK:      "Disconnect-NAK",
		CodeCoARequest:         "CoA-Request",
		CodeCoAACK:             "CoA-ACK",
		CodeCoANAK:             "CoA-NAK",
	}
	for c, want := range cases {
		assert.Equal(t, want, c.String(), "Code(%d)", uint8(c))
	}
	assert.Contains(t, Code(99).String(), "Unknown")
}

func TestCodeClassifiers(t *testing.T) {
	requests := []Code{CodeAccessRequest, CodeAccountingRequest, CodeCoARequest, CodeDisconnectRequest}
	for _, c := range requests {
		assert.True(t, c.IsRequest(), "%s should be a request", c)
		assert.False(t, c.IsResponse(), "%s should not be a response", c)
	}
	responses := []Code{
		CodeAccessAccept, CodeAccessReject, CodeAccessChallenge,
		CodeAccountingResponse,
		CodeCoAACK, CodeCoANAK, CodeDisconnectACK, CodeDisconnectNAK,
	}
	for _, c := range responses {
		assert.True(t, c.IsResponse(), "%s should be a response", c)
		assert.False(t, c.IsRequest(), "%s should not be a request", c)
	}
}

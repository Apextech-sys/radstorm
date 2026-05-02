// Package server — RFC 5176 CoA + Disconnect listener for radstorm.
//
// Purpose:
//
//	Per-packet validation and response logic for the listener. Implements
//	RFC 5176 §2.4 rules: drop on missing/invalid Message-Authenticator,
//	NAK 503 on unknown subscriber, NAK 401 on malformed VSAs, ACK
//	otherwise. Dispatches to SubscriberTarget.OnCoA / OnDisconnect on a
//	fresh goroutine AFTER the response is on the wire so latency
//	measurement is not skewed by the callback.
//
// Related files:
//   - pkg/server/listener.go (drives readLoop + calls handlePacket)
//   - pkg/server/lookup.go   (SubscriberLookup + SubscriberTarget contracts)
//   - pkg/radius             (decode, NewCoAAck/NewCoANak, NewDisconnectAck/NewDisconnectNak)
//   - pkg/events             (event constructors + tag conventions)
//
// Briefing: .orchestration/briefings/2c-server-listener.md
//
// Contract: internal — all entry points are package-private.
package server

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

// handlePacket is the per-datagram entry point invoked from readLoop. It
// is the single source of truth for what radstorm does with an inbound
// dynamic-authorization packet.
//
// recvAt is the monotonic timestamp captured by the read loop the moment
// ReadFromUDP returned — every latency measurement here measures from
// that anchor.
func (l *Listener) handlePacket(wire []byte, remoteAddr *net.UDPAddr, recvAt time.Time) {
	remote := remoteAddr.String()

	// Decode is structural-only; cryptographic validation is below.
	pkt, err := radius.Decode(wire, l.opts.SharedSecret)
	if err != nil {
		// Malformed datagram — emit a generic error event and drop.
		l.opts.Collector.Submit(events.NewError(
			0,
			l.offsetMs(recvAt),
			events.EventTypeValidationFailed,
			0,
			fmt.Sprintf("server: decode packet from %s: %v", remote, err),
		))
		l.pktDropped.Add(1)
		return
	}

	switch pkt.Code {
	case radius.CodeCoARequest:
		l.handleCoA(pkt, wire, remote, recvAt)
	case radius.CodeDisconnectRequest:
		l.handleDisconnect(pkt, wire, remote, recvAt)
	default:
		// We don't speak Access-Request / Accounting-Request server-side;
		// log via event and ignore.
		l.opts.Collector.Submit(events.NewError(
			0,
			l.offsetMs(recvAt),
			events.EventTypeValidationFailed,
			0,
			fmt.Sprintf("server: unexpected code %s from %s", pkt.Code, remote),
		))
		l.pktDropped.Add(1)
	}
}

// handleCoA implements the CoA-Request side of RFC 5176 §2.4.
func (l *Listener) handleCoA(pkt *radius.Packet, wire []byte, remote string, recvAt time.Time) {
	// 1. Message-Authenticator MUST be present and valid (RFC 5176 §2.3).
	//    If missing or wrong, drop silently and emit coa_dropped.
	if !pkt.ValidateMessageAuthenticator(wire, l.opts.SharedSecret) {
		l.opts.Collector.Submit(l.coaEvent(
			0, recvAt, events.EventTypeCoADropped, pkt, remote, len(wire), 0, 0,
			map[string]string{"drop_reason": "invalid_message_authenticator"},
		))
		l.pktDropped.Add(1)
		return
	}

	// 2. Decode and tag VSAs. A malformed VSA → NAK 401.
	tags, vsaErr := vsaTags(pkt)
	subTags := standardTags(pkt)
	for k, v := range subTags {
		tags[k] = v
	}

	// Always emit coa_received once we know the packet auth was valid,
	// before deciding ACK vs NAK. This gives operators visibility into
	// every (well-authenticated) inbound CoA regardless of disposition.
	l.opts.Collector.Submit(l.coaEvent(
		0, recvAt, events.EventTypeCoAReceived, pkt, remote, len(wire), 0, 0, copyTags(tags),
	))

	if vsaErr != nil {
		l.respondCoANak(pkt, recvAt, remote, radius.ErrorCauseUnsupportedAttribute, tags, vsaErr.Error())
		return
	}

	// 3. Look up the targeted subscriber. Unknown → NAK 503.
	target, ok := l.opts.Lookup.Lookup(pkt)
	if !ok {
		l.respondCoANak(pkt, recvAt, remote, radius.ErrorCauseSessionContextNotFound, tags, "subscriber not found")
		return
	}

	// 4. Build + send ACK. After the ACK is on the wire (latency
	//    measured), dispatch to OnCoA on a separate goroutine so the
	//    callback does not skew our μs-precision latency measurement.
	tags["sub_id"] = strconv.FormatUint(uint64(target.ID()), 10)
	tags["username"] = target.Username()

	ack, err := radius.NewCoAAck(pkt, l.opts.SharedSecret)
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			target.ID(), l.offsetMs(recvAt), events.EventTypeInternalError, 0,
			fmt.Sprintf("server: build CoA-ACK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	encoded, err := ack.Encode()
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			target.ID(), l.offsetMs(recvAt), events.EventTypeInternalError, 0,
			fmt.Sprintf("server: encode CoA-ACK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	if err := l.writeReply(encoded, remote); err != nil {
		l.opts.Collector.Submit(events.NewError(
			target.ID(), l.offsetMs(recvAt), events.EventTypeInternalError, 0,
			fmt.Sprintf("server: write CoA-ACK to %s: %v", remote, err),
		))
		l.pktDropped.Add(1)
		return
	}
	sentAt := time.Now()
	latencyUs := sentAt.Sub(recvAt).Microseconds()

	l.opts.Collector.Submit(l.coaEvent(
		target.ID(), recvAt, events.EventTypeCoAAcked, pkt, remote, len(encoded), latencyUs, 0, tags,
	))
	l.pktAcked.Add(1)

	// Fire the subscriber callback on its own goroutine — it can run
	// arbitrarily long FSM logic without blocking the next packet.
	go target.OnCoA(pkt)
}

// respondCoANak sends a CoA-NAK with the given Error-Cause and emits the
// coa_naked event. Errors during build/send are logged via the error
// event channel rather than swallowed.
func (l *Listener) respondCoANak(pkt *radius.Packet, recvAt time.Time, remote string, cause uint32, tags map[string]string, reason string) {
	tags["nak_reason"] = reason

	nak, err := radius.NewCoANak(pkt, l.opts.SharedSecret, cause)
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			0, l.offsetMs(recvAt), events.EventTypeInternalError, int32(cause),
			fmt.Sprintf("server: build CoA-NAK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	encoded, err := nak.Encode()
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			0, l.offsetMs(recvAt), events.EventTypeInternalError, int32(cause),
			fmt.Sprintf("server: encode CoA-NAK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	if err := l.writeReply(encoded, remote); err != nil {
		l.opts.Collector.Submit(events.NewError(
			0, l.offsetMs(recvAt), events.EventTypeInternalError, int32(cause),
			fmt.Sprintf("server: write CoA-NAK to %s: %v", remote, err),
		))
		l.pktDropped.Add(1)
		return
	}
	sentAt := time.Now()
	latencyUs := sentAt.Sub(recvAt).Microseconds()

	l.opts.Collector.Submit(l.coaEvent(
		0, recvAt, events.EventTypeCoANaked, pkt, remote, len(encoded), latencyUs, int32(cause), tags,
	))
	l.pktNaked.Add(1)
}

// handleDisconnect implements the Disconnect-Request side of RFC 5176 §2.4.
// Mirrors handleCoA with the disconnect-flavored constructors and event
// types.
func (l *Listener) handleDisconnect(pkt *radius.Packet, wire []byte, remote string, recvAt time.Time) {
	if !pkt.ValidateMessageAuthenticator(wire, l.opts.SharedSecret) {
		l.opts.Collector.Submit(l.disconnectEvent(
			0, recvAt, events.EventTypeCoADropped, pkt, remote, len(wire), 0, 0,
			map[string]string{"drop_reason": "invalid_message_authenticator"},
		))
		l.pktDropped.Add(1)
		return
	}

	tags, vsaErr := vsaTags(pkt)
	for k, v := range standardTags(pkt) {
		tags[k] = v
	}

	l.opts.Collector.Submit(l.disconnectEvent(
		0, recvAt, events.EventTypeDisconnectReceived, pkt, remote, len(wire), 0, 0, copyTags(tags),
	))

	if vsaErr != nil {
		l.respondDisconnectNak(pkt, recvAt, remote, radius.ErrorCauseUnsupportedAttribute, tags, vsaErr.Error())
		return
	}

	target, ok := l.opts.Lookup.Lookup(pkt)
	if !ok {
		l.respondDisconnectNak(pkt, recvAt, remote, radius.ErrorCauseSessionContextNotFound, tags, "subscriber not found")
		return
	}
	tags["sub_id"] = strconv.FormatUint(uint64(target.ID()), 10)
	tags["username"] = target.Username()

	ack, err := radius.NewDisconnectAck(pkt, l.opts.SharedSecret)
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			target.ID(), l.offsetMs(recvAt), events.EventTypeInternalError, 0,
			fmt.Sprintf("server: build Disconnect-ACK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	encoded, err := ack.Encode()
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			target.ID(), l.offsetMs(recvAt), events.EventTypeInternalError, 0,
			fmt.Sprintf("server: encode Disconnect-ACK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	if err := l.writeReply(encoded, remote); err != nil {
		l.opts.Collector.Submit(events.NewError(
			target.ID(), l.offsetMs(recvAt), events.EventTypeInternalError, 0,
			fmt.Sprintf("server: write Disconnect-ACK to %s: %v", remote, err),
		))
		l.pktDropped.Add(1)
		return
	}
	sentAt := time.Now()
	latencyUs := sentAt.Sub(recvAt).Microseconds()

	l.opts.Collector.Submit(l.disconnectEvent(
		target.ID(), recvAt, events.EventTypeDisconnectAcked, pkt, remote, len(encoded), latencyUs, 0, tags,
	))
	l.pktAcked.Add(1)

	go target.OnDisconnect(pkt)
}

// respondDisconnectNak — sibling of respondCoANak.
func (l *Listener) respondDisconnectNak(pkt *radius.Packet, recvAt time.Time, remote string, cause uint32, tags map[string]string, reason string) {
	tags["nak_reason"] = reason

	nak, err := radius.NewDisconnectNak(pkt, l.opts.SharedSecret, cause)
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			0, l.offsetMs(recvAt), events.EventTypeInternalError, int32(cause),
			fmt.Sprintf("server: build Disconnect-NAK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	encoded, err := nak.Encode()
	if err != nil {
		l.opts.Collector.Submit(events.NewError(
			0, l.offsetMs(recvAt), events.EventTypeInternalError, int32(cause),
			fmt.Sprintf("server: encode Disconnect-NAK: %v", err),
		))
		l.pktDropped.Add(1)
		return
	}
	if err := l.writeReply(encoded, remote); err != nil {
		l.opts.Collector.Submit(events.NewError(
			0, l.offsetMs(recvAt), events.EventTypeInternalError, int32(cause),
			fmt.Sprintf("server: write Disconnect-NAK to %s: %v", remote, err),
		))
		l.pktDropped.Add(1)
		return
	}
	sentAt := time.Now()
	latencyUs := sentAt.Sub(recvAt).Microseconds()

	l.opts.Collector.Submit(l.disconnectEvent(
		0, recvAt, events.EventTypeDisconnectNaked, pkt, remote, len(encoded), latencyUs, int32(cause), tags,
	))
	l.pktNaked.Add(1)
}

// writeReply unicasts the encoded reply to the remote that sent the
// request. Uses WriteToUDP rather than WriteTo so the address type is
// always correct.
func (l *Listener) writeReply(encoded []byte, remote string) error {
	addr, err := net.ResolveUDPAddr("udp", remote)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", remote, err)
	}
	_, err = l.conn.WriteToUDP(encoded, addr)
	return err
}

// offsetMs returns ms since the listener's TestT0. Two events from the
// same listener can be subtracted to get an in-test relative timeline.
func (l *Listener) offsetMs(at time.Time) int64 {
	return at.Sub(l.t0).Milliseconds()
}

// coaEvent factors out the common Event construction for the four CoA
// event types so the call sites stay readable.
func (l *Listener) coaEvent(subID uint32, recvAt time.Time, eventType string, pkt *radius.Packet, remote string, bytes int, latencyUs int64, errCause int32, tags map[string]string) events.Event {
	e := events.NewCoAEvent(
		subID,
		l.offsetMs(recvAt),
		eventType,
		int16(pkt.Identifier),
		l.localAddr,
		remote,
		int32(bytes),
		latencyUs,
		errCause,
	)
	e.RadiusCode = int8(pkt.Code)
	e.Tags = tags
	return e
}

// disconnectEvent — sibling of coaEvent.
func (l *Listener) disconnectEvent(subID uint32, recvAt time.Time, eventType string, pkt *radius.Packet, remote string, bytes int, latencyUs int64, errCause int32, tags map[string]string) events.Event {
	e := events.NewDisconnectEvent(
		subID,
		l.offsetMs(recvAt),
		eventType,
		int16(pkt.Identifier),
		l.localAddr,
		remote,
		int32(bytes),
		latencyUs,
		errCause,
	)
	e.RadiusCode = int8(pkt.Code)
	e.Tags = tags
	return e
}

// standardTags pulls a few standard RADIUS attributes off the packet and
// formats them as event tag values. Used so operators can correlate the
// event log to the inbound CoA they observed at the server.
func standardTags(pkt *radius.Packet) map[string]string {
	out := map[string]string{}
	if u := pkt.Attributes.GetString(radius.AttrUserName); u != "" {
		out["user_name"] = u
	}
	if s := pkt.Attributes.GetString(radius.AttrAcctSessionID); s != "" {
		out["acct_session_id"] = s
	}
	if cs := pkt.Attributes.GetString(radius.AttrCallingStationID); cs != "" {
		out["calling_station_id"] = cs
	}
	if ip, ok := pkt.Attributes.GetIPv4(radius.AttrFramedIPAddress); ok {
		out["framed_ip"] = ip.String()
	}
	if ip, ok := pkt.Attributes.GetIPv4(radius.AttrNASIPAddress); ok {
		out["nas_ip"] = ip.String()
	}
	if id := pkt.Attributes.GetString(radius.AttrNASIdentifier); id != "" {
		out["nas_identifier"] = id
	}
	return out
}

// vsaTags decodes every Vendor-Specific attribute on the packet into
// readable tags. Returns an error if any VSA payload is malformed — the
// listener treats that as RFC 5176 error 401 (Unsupported Attribute).
//
// Huawei VSAs we know about are rendered with their friendly names so
// the tags are easy to scan in test output. Unknown VSAs are still
// recorded so operators can see what came in.
func vsaTags(pkt *radius.Packet) (map[string]string, error) {
	out := map[string]string{}
	vsAttrs := pkt.Attributes.GetAll(radius.AttrVendorSpecific)
	for i, attr := range vsAttrs {
		vsas, err := radius.DecodeVSAs(attr.Value)
		if err != nil {
			return out, fmt.Errorf("vsa[%d]: %w", i, err)
		}
		for _, v := range vsas {
			key := vsaTagKey(v)
			out[key] = vsaTagValue(v)
		}
	}
	return out, nil
}

// vsaTagKey returns a stable readable key for a decoded VSA. Huawei VSAs
// we've named in pkg/radius/vendor.go get a friendly key; everything else
// falls back to "vsa_<vendor>_<type>".
func vsaTagKey(v radius.VendorAttribute) string {
	if v.VendorID == radius.VendorHuawei {
		switch v.VendorType {
		case radius.HuaweiInputPeakRate:
			return "huawei_input_peak_rate"
		case radius.HuaweiInputAverageRate:
			return "huawei_input_average_rate"
		case radius.HuaweiInputBasicRate:
			return "huawei_input_basic_rate"
		case radius.HuaweiOutputPeakRate:
			return "huawei_output_peak_rate"
		case radius.HuaweiSubscriberQoSProfile:
			return "huawei_subscriber_qos_profile"
		case radius.HuaweiConnectID:
			return "huawei_connect_id"
		case radius.HuaweiAcctSessionID:
			return "huawei_acct_session_id"
		case radius.HuaweiServiceType:
			return "huawei_service_type"
		}
	}
	return fmt.Sprintf("vsa_%d_%d", v.VendorID, v.VendorType)
}

// vsaTagValue renders a VSA value. 4-byte values are printed as decimal
// (the most common Huawei rate-plan shape). All other lengths are hex-
// encoded so the operator at least sees what arrived.
func vsaTagValue(v radius.VendorAttribute) string {
	if len(v.Value) == 4 {
		// Big-endian uint32 — matches Huawei rate-plan VSAs.
		n := uint32(v.Value[0])<<24 | uint32(v.Value[1])<<16 | uint32(v.Value[2])<<8 | uint32(v.Value[3])
		return strconv.FormatUint(uint64(n), 10)
	}
	// Strings (Acct-Session-Id, Connect-Id) are usually printable ASCII.
	if isPrintableASCII(v.Value) {
		return string(v.Value)
	}
	return fmt.Sprintf("0x%x", v.Value)
}

// isPrintableASCII returns true if every byte is in the printable ASCII
// range. Used to decide whether to render a VSA value as a string.
func isPrintableASCII(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// copyTags returns a shallow copy of the input map. We need this because
// the listener emits coa_received using the tags it accumulated, then
// continues to add to that same map for the subsequent coa_acked /
// coa_naked event — without the copy both events would share the
// post-mutation map.
func copyTags(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

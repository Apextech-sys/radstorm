# RADIUS Protocol Reference

This is a quick-reference for the protocol details radstorm implements. Authoritative source is the relevant RFC.

## RFCs

- **RFC 2865** — Remote Authentication Dial In User Service (RADIUS)
- **RFC 2866** — RADIUS Accounting
- **RFC 2869** — RADIUS Extensions (incl. Message-Authenticator, EAP-Message)
- **RFC 3162** — RADIUS and IPv6
- **RFC 3576** — Dynamic Authorization Extensions (CoA, Disconnect) — superseded
- **RFC 5176** — Dynamic Authorization Extensions (current)
- **RFC 6929** — Extended Attributes (we don't currently emit these)

## Packet structure

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Code      |  Identifier   |            Length             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                                                               |
|                         Authenticator                         |
|                          (16 bytes)                           |
|                                                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Attributes ...
```

### Codes used by radstorm

| Code | Name | Direction (from radstorm POV) |
|---:|---|---|
| 1 | Access-Request | outbound (we send) |
| 2 | Access-Accept | inbound (server replies) |
| 3 | Access-Reject | inbound |
| 4 | Accounting-Request | outbound |
| 5 | Accounting-Response | inbound |
| 11 | Access-Challenge | inbound (we don't expect; logged if seen) |
| 40 | Disconnect-Request | inbound (server-initiated) |
| 41 | Disconnect-ACK | outbound (we ack) |
| 42 | Disconnect-NAK | outbound |
| 43 | CoA-Request | inbound |
| 44 | CoA-ACK | outbound |
| 45 | CoA-NAK | outbound |

## Identifier

- 8-bit (0–255)
- Must be unique per `(srcIP, srcPort, dstIP, dstPort)` for the duration of a request transaction (until ACK or final timeout)
- On retransmit, **a new Identifier MUST be allocated** to avoid colliding with a stale-but-still-in-flight original

## Request Authenticator

- 16 bytes, randomly generated for Access-Request
- For Accounting-Request: MD5(Code|Identifier|Length|16-zero-bytes|Attributes|SharedSecret)
- For CoA/Disconnect-Request: same as Accounting (RFC 5176)

## Response Authenticator

For all replies (Access-Accept/Reject, Accounting-Response, CoA/Disconnect-ACK/NAK):
`MD5(Code|Identifier|Length|RequestAuthenticator|Attributes|SharedSecret)`

We MUST validate this on every reply. A mismatched Response Authenticator means dropped packet (logged).

## Authentication methods we support

### PAP (Password Authentication Protocol)

User-Password (attr 2) is XOR-encrypted with `MD5(SharedSecret|RequestAuthenticator)`, padded to 16-byte boundary. Subsequent 16-byte blocks XOR with `MD5(SharedSecret|previous_ciphertext_block)`.

### CHAP (Challenge Handshake Authentication Protocol)

- CHAP-Password (attr 3): `[CHAP-Identifier(1)] [MD5(CHAP-Identifier | password | challenge)(16)]` (17 bytes total)
- CHAP-Challenge (attr 60): the random challenge (use Request Authenticator if attr 60 absent)

We generate a fresh CHAP-Identifier and Challenge per request.

## Message-Authenticator (RFC 2869)

- Attribute 80, 16 bytes
- HMAC-MD5 of the entire packet (with the Message-Authenticator value field zeroed during computation) keyed by the shared secret
- **REQUIRED on CoA-Request and Disconnect-Request** per RFC 5176; we MUST validate on inbound and SHOULD include on our ACK/NAK responses

## Standard attributes we emit (Access-Request)

| ID | Name | Notes |
|---:|---|---|
| 1 | User-Name | from credentials |
| 2 | User-Password | PAP only |
| 3 | CHAP-Password | CHAP only |
| 4 | NAS-IP-Address | from config |
| 5 | NAS-Port | per-subscriber unique |
| 6 | Service-Type | typically Framed-User (2) |
| 7 | Framed-Protocol | typically PPP (1) for PPPoE |
| 30 | Called-Station-Id | per-config |
| 31 | Calling-Station-Id | subscriber MAC |
| 32 | NAS-Identifier | from config |
| 60 | CHAP-Challenge | CHAP only |
| 61 | NAS-Port-Type | typically Virtual (5) for PPPoE, Ethernet (15) for MAC-auth |
| 87 | NAS-Port-Id | per-subscriber, often `slot/port/vlan` form |

## Standard attributes we emit (Accounting-Request, Acct-Status-Type=Start)

| ID | Name |
|---:|---|
| 1 | User-Name |
| 4 | NAS-IP-Address |
| 5 | NAS-Port |
| 8 | Framed-IP-Address (mock-assigned per subscriber) |
| 31 | Calling-Station-Id |
| 32 | NAS-Identifier |
| 40 | Acct-Status-Type (1 = Start) |
| 44 | Acct-Session-Id (unique per session) |
| 45 | Acct-Authentic (1 = RADIUS) |
| 61 | NAS-Port-Type |
| 87 | NAS-Port-Id |

## Huawei VSAs (vendor-id 2011)

Encoded as standard Vendor-Specific (attr 26) with vendor 2011. Some Huawei attributes are TLV-encoded inside the VSA payload; the dictionary indicates which.

For radstorm, the dictionary file is sourced from FreeRADIUS' `dictionary.huawei`. We compile the relevant subset into the binary at build time using `layeh.com/radius/dictionarygen` or equivalent.

Key VSAs we care about RECEIVING (in CoA-Request from a real BNG-targeting policy):

| Attr | Name | Purpose |
|---:|---|---|
| 26.2011.1 | Huawei-Input-Peak-Rate | rate-plan change |
| 26.2011.2 | Huawei-Input-Average-Rate | |
| 26.2011.3 | Huawei-Input-Basic-Rate | |
| 26.2011.5 | Huawei-Output-Peak-Rate | |
| 26.2011.18 | Huawei-Subscriber-QoS-Profile | |
| 26.2011.20 | Huawei-Connect-Id | session targeting |
| 26.2011.26 | Huawei-Acct-Session-Id | session targeting |
| 26.2011.65 | Huawei-Service-Type | |

(Full dictionary lives in `pkg/radius/dictionaries/huawei.dict` after Wave 1.)

## Retransmit policy

Default (configurable per scenario):
- Initial timeout: 5s
- Max retries: 3
- Backoff: 1s, 2s, 4s after each failed attempt (exponential, capped)
- New Identifier allocated per retransmit
- Subscriber transitions to `auth_failed` / `acct_failed` after final timeout

## Error causes (RFC 5176)

When NAKing CoA/Disconnect, we set Error-Cause (attr 101):

| Code | Meaning | When we use it |
|---:|---|---|
| 401 | Unsupported Attribute | malformed VSA we can't decode |
| 402 | Missing Attribute | required attribute absent |
| 403 | NAS Identification Mismatch | NAS-IP doesn't match ours |
| 404 | Invalid Request | generic |
| 503 | Session Context Not Found | subscriber lookup failed |
| 506 | Resources Unavailable | rate-limited (we don't currently rate-limit but reserve) |

## Things we don't implement

- EAP (no Access-Challenge handling)
- Tunneled attributes (Tunnel-Type, etc.) on the OUTBOUND side; we accept them on inbound CoA but don't act
- Extended Attributes (RFC 6929)
- DTLS (RFC 6614) — UDP only
- RadSec (RFC 6614 over TLS)

# dsync phone protocol (v1)

How the dsync phone app (Expo Go) talks to dsync on a computer. Computers talk to each
other over TLS with pinned certificates and find each other with UDP broadcast; Expo Go
can do neither, so phones use this simpler protocol instead.

## Transport and encryption

- Plain HTTP to `http://<computer address>:47102`. Every request is `POST` with
  `Content-Type: text/plain`.
- Every request and response body is a **sealed envelope**:
  `base64( nonce[24] || secretbox(envelope_json, nonce, key) )`
  where `secretbox` is NaCl's XSalsa20-Poly1305 (`tweetnacl`'s `nacl.secretbox`, Go's
  `golang.org/x/crypto/nacl/secretbox`) and `key` is the 32-byte key from the QR code.
  Standard base64 with padding.
- `envelope_json` is UTF-8 JSON: `{"t": <unix ms when sealed>, "b": <body>}`. The computer
  refuses envelopes more than 2 minutes old or in the future, and each nonce only once.
  Nonces are 24 random bytes (tweetnacl `nacl.randomBytes` with its PRNG seeded from
  `expo-crypto`'s `getRandomValues`).
- Errors come back as a non-200 status with a **plain text** (not sealed) message:
  `400` bad request, `401` unknown phone or the body didn't open (wrong key),
  `404` no such message/upload, `410` pairing code expired or used, `413` too big.

### Test vectors

key = bytes 0..31 (`00 01 02 … 1f`), nonce = bytes 100..123 (`64 65 … 7b`):

| plaintext (UTF-8) | sealed |
|---|---|
| `{"t":1790752000000,"b":{"text":"héllo dsync"}}` | `ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp7Ju5EbL6NXn4buApjwG6t8nmb7esAh/nQgMoWpge0kygOmhSFhoRxYIR4J+x+qfOqwKxddCNYu4CGb3kBIm6g` |
| (empty) | `ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp79JVy1hlCgePIf7tOIQaTLA==` |

## Pairing (QR code)

The computer shows (Settings → Connect a phone) a QR code containing a URL:

```
dsync://pair?v=1&pid=<pairing id>&key=<key, base64url without padding>&name=<computer name, URL-encoded>&cid=<computer id>&os=<linux|windows|darwin>&port=47102&addr=<ip1>,<ip2>,...
```

`addr` lists the computer's addresses (Wi-Fi/Ethernet first, then Tailscale). The phone
tries them in order and keeps the first that answers. A pairing code works once and for
10 minutes.

### `POST /phone/v1/pair`

Header `X-Dsync-Pair: <pairing id>`. Body `{"name": "<phone name>", "platform": "ios"|"android"}`.
Response `{"phoneId": "<id>", "computerId": "<id>", "computerName": "<name>", "os": "<os>"}`.
From then on the phone uses `phoneId` and the same key.

## Requests after pairing

Every request carries the header `X-Dsync-Phone: <phoneId>`.

### Messages

A message, as seen from the phone:

```json
{
  "id": 42,               // increasing per computer
  "time": 1790752000000,  // unix ms
  "fromPhone": true,      // sent by the phone (false: sent by the computer)
  "text": "hello",        // or absent for files
  "file": {"name": "a.jpg", "size": 1234, "status": "done", "error": ""}  // or absent
}
```

`file.status` is `active`, `done`, `failed` or `canceled`. Files the computer sends to the
phone are `done` as soon as they're offered: the phone downloads them when the user wants.

### `POST /phone/v1/sync`

Body `{"since": <last message id the phone has, 0 at first>}`.
Response `{"computer": {"id": "...", "name": "...", "os": "..."}, "messages": [<message>...]}`
with this phone's messages whose id is greater than `since`, oldest first, at most 200.
The phone polls this every 2 seconds while it's open; a poll also marks the phone online
on the computer.

### `POST /phone/v1/text`

Body `{"text": "..."}` (at most 1 MB). Response `{"message": <message>}`.

### Sending a file to the computer

1. `POST /phone/v1/upload/start` body `{"name": "photo.jpg", "size": 123456}` →
   `{"uploadId": "<id>", "chunkSize": 524288}`.
2. `POST /phone/v1/upload/chunk` body `{"uploadId": "...", "offset": <bytes>, "data": "<base64>"}`
   → `{"received": <total bytes so far>}`. Chunks go in order; `offset` must equal the bytes
   received so far (`400` otherwise). At most `chunkSize` bytes per chunk.
3. `POST /phone/v1/upload/finish` body `{"uploadId": "..."}` → `{"message": <message>}`.
   Fails with `400` if fewer than `size` bytes arrived.

Integrity comes from the secretbox authentication of each chunk and the offset/size checks.
The file lands in the computer's received-files folder and shows in its conversation with
the phone.

### `POST /phone/v1/download`

Body `{"messageId": 42, "offset": <bytes>, "length": <bytes, at most 1048576>}` →
`{"data": "<base64>", "size": <whole file size>, "eof": true|false}`.
Only for file messages the computer sent to this phone.

### `POST /phone/v1/unpair`

Body `{}` → `{}`. The computer forgets the phone.

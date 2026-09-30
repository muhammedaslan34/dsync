# dsync for phones

A small React Native app that lets a phone join dsync: send text, photos and files to
your computers, and save or share what they send back. It runs in **Expo Go**, so there
is nothing to build or install besides Expo Go itself.

It talks to dsync on the computer with the phone protocol in
[`../docs/phone-protocol.md`](../docs/phone-protocol.md) (plain HTTP on port 47102,
every body sealed with NaCl secretbox using a key shared through the QR code).

## Run it

1. Install **Expo Go** on the phone (App Store / Play Store).
2. On the laptop:

   ```sh
   cd mobile
   npm install
   npx expo start
   ```

3. Scan the QR code in the terminal: with the Camera app on iPhone, or from inside
   Expo Go on Android.

The phone and the laptop must be on the same Wi-Fi. If Expo Go can't load the app, let
the Expo dev server through the laptop's firewall: port **8081/tcp**, for example
`sudo ufw allow 8081/tcp` or
`sudo firewall-cmd --add-port=8081/tcp`. (If that isn't possible, `npx expo start --tunnel`
works through the internet instead.)

## Pair with a computer

1. On the computer, open dsync → **Settings → Connect a phone**. It shows a QR code
   (valid once, for 10 minutes).
2. In the phone app tap **Connect a computer** and point the camera at the code.
   You can also paste the `dsync://pair?...` link instead of scanning (handy for testing).
3. The phone tries each of the computer's addresses and keeps the first that answers.

The phone must reach the computer on port **47102/tcp**, so both need to be on the same
network (or the same Tailscale network), with dsync running and allowed through the
computer's firewall.

To unpair, open the computer's conversation, tap the gear, then **Forget this computer**.

## Using it

- The list shows each paired computer with an online dot (green when it answered in the
  last few seconds) and the last message.
- In a conversation, type or paste (clipboard button) text and send it; tap **+** to send
  photos, videos or files. Text bubbles have a copy button (or long-press to copy).
- Files from the computer have a **Save / Share** button: the phone downloads the file and
  opens the share sheet, from where you can save it to Files/Photos or send it on.

## Development

```sh
npm run test:crypto     # the sealed-envelope code against the protocol's test vectors
npx tsc --noEmit        # type check
npx expo-doctor         # dependency / config check
```

Code layout:

- `src/crypto.ts` – base64, UTF-8, secretbox envelopes (no React Native imports, tested with Node)
- `src/random.ts` – seeds tweetnacl's random numbers from `expo-crypto`
- `src/dsync.ts` – the protocol: pairing URL, sealed requests, pair / sync / text / upload / download / unpair
- `src/storage.ts` – paired computers in secure storage
- `src/store.ts` – app state, polling, sending and downloads
- `src/screens/` – computers list, QR scanner, conversation, computer settings

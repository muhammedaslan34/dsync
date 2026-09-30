# dsync for phones

A small React Native app that lets a phone join dsync: send text, photos and files to
your computers, and save or share what they send back. Install it from a release (APK for
Android, .ipa for iPhone), or run it in **Expo Go** while working on it.

It talks to dsync on the computer with the phone protocol in
[`../docs/phone-protocol.md`](../docs/phone-protocol.md) (plain HTTP on port 47102,
every body sealed with NaCl secretbox using a key shared through the QR code).

## Install it

Each [release](https://github.com/muhammedaslan34/dsync/releases) has the phone app,
built by `.github/workflows/release.yml`:

- **Android:** download `dsync-<version>-android.apk` on the phone and open it. Android
  asks once to allow installing apps from the browser (or Files). Later versions install
  over it and keep your paired computers.
- **iPhone:** `dsync-<version>-ios-unsigned.ipa` isn't signed, because that needs a paid
  Apple Developer account. Install it with [AltStore](https://altstore.io) or
  [Sideloadly](https://sideloadly.io) from a computer, signed with your own Apple ID.
  With a free Apple ID it has to be refreshed every 7 days (AltStore does that on its
  own). On iOS 16 and later, turn on **Settings → Privacy & Security → Developer Mode**
  first.

The Android build is signed with dsync's release key (kept outside the repo; the
workflow reads it from the `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`,
`ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD` secrets). Every release has to use the same
key, or phones refuse the update. The app's version follows `../wails.json`.

## Run it in Expo Go (development)

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

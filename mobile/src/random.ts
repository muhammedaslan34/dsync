// tweetnacl looks for window.crypto / Node's crypto for randomness; neither
// exists in React Native, so feed it from expo-crypto instead. Import this
// once, before anything seals.
import nacl from 'tweetnacl';
import { getRandomValues } from 'expo-crypto';

nacl.setPRNG((x: Uint8Array, n: number) => {
  // getRandomValues fills at most 65536 bytes per call.
  const buf = new Uint8Array(n);
  for (let off = 0; off < n; off += 65536) {
    getRandomValues(buf.subarray(off, Math.min(n, off + 65536)));
  }
  for (let i = 0; i < n; i++) x[i] = buf[i];
  buf.fill(0);
});

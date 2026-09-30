// A QR code drawn with plain views (no native code). Always left to right:
// a mirrored QR code doesn't scan.
import { memo, useMemo } from 'react';
import { View } from 'react-native';
import qrcode from 'qrcode-generator';

function QRCode({ value, size }: { value: string; size: number }) {
  const rows = useMemo(() => {
    const qr = qrcode(0, 'M');
    qr.addData(value, 'Byte');
    qr.make();
    const n = qr.getModuleCount();
    const out: { n: number; runs: [number, number][] } = { n, runs: [] };
    const all: [number, number][][] = [];
    for (let r = 0; r < n; r++) {
      const runs: [number, number][] = [];
      for (let c = 0; c < n; ) {
        if (!qr.isDark(r, c)) {
          c++;
          continue;
        }
        let e = c;
        while (e < n && qr.isDark(r, e)) e++;
        runs.push([c, e - c]);
        c = e;
      }
      all.push(runs);
    }
    return { n: out.n, rows: all };
  }, [value]);
  const quiet = 4; // the white margin scanners need, in modules
  const cell = size / (rows.n + 2 * quiet);
  return (
    <View style={{ width: size, height: size, backgroundColor: '#fff', padding: cell * quiet, direction: 'ltr' }}>
      {rows.rows.map((runs, r) => (
        <View key={r} style={{ height: cell, flexDirection: 'row' }}>
          {runs.map(([c, len]) => (
            <View key={c} style={{ position: 'absolute', left: c * cell, width: len * cell + 0.5, height: cell + 0.5, backgroundColor: '#000' }} />
          ))}
        </View>
      ))}
    </View>
  );
}

export default memo(QRCode);

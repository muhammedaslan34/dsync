// Cleartext is required by the phone protocol on a LAN, but it must never be
// used to contact an Internet address. Keep this module platform-independent
// so the policy can be tested outside React Native.

function parseIPv4(address: string): number[] | null {
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(address)) return null;
  const textParts = address.split('.');
  // URL parsers historically accept octal-looking IPv4 components. Requiring
  // canonical decimal prevents an allowed-looking 010.x address becoming 8.x.
  if (textParts.some((part) => part.length > 1 && part.startsWith('0'))) return null;
  const parts = textParts.map(Number);
  if (parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) return null;
  return parts;
}

function parseIPv6(address: string): number[] | null {
  let text = address.toLowerCase();
  const zone = text.indexOf('%');
  if (zone >= 0) {
    if (zone === text.length - 1 || !/^[a-z0-9_.-]+$/.test(text.slice(zone + 1))) return null;
    text = text.slice(0, zone);
  }
  if (!text.includes(':') || text.indexOf('::') !== text.lastIndexOf('::')) return null;

  const convert = (parts: string[]): number[] | null => {
    const out: number[] = [];
    for (let i = 0; i < parts.length; i++) {
      const part = parts[i];
      if (!part) return null;
      if (part.includes('.')) {
        if (i !== parts.length - 1) return null;
        const ipv4 = parseIPv4(part);
        if (!ipv4) return null;
        out.push((ipv4[0] << 8) | ipv4[1], (ipv4[2] << 8) | ipv4[3]);
      } else {
        if (!/^[0-9a-f]{1,4}$/.test(part)) return null;
        out.push(parseInt(part, 16));
      }
    }
    return out;
  };

  if (text.includes('::')) {
    const [leftText, rightText] = text.split('::');
    const left = leftText ? convert(leftText.split(':')) : [];
    const right = rightText ? convert(rightText.split(':')) : [];
    if (!left || !right || left.length + right.length >= 8) return null;
    return [...left, ...Array(8 - left.length - right.length).fill(0), ...right];
  }
  const full = convert(text.split(':'));
  return full?.length === 8 ? full : null;
}

function isLocalIPv4(parts: number[]): boolean {
  const [a, b] = parts;
  return (
    a === 10 ||
    a === 127 ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168)
  );
}

/** True only for literal loopback, RFC1918/ULA, or link-local addresses. */
export function isLocalNetworkAddress(raw: string): boolean {
  let address = raw.trim();
  if (address.startsWith('[') && address.endsWith(']')) address = address.slice(1, -1);
  if (address.toLowerCase() === 'localhost') return true;

  const ipv4 = parseIPv4(address);
  if (ipv4) return isLocalIPv4(ipv4);

  const ipv6 = parseIPv6(address);
  if (!ipv6) return false;
  // ::1
  if (ipv6.slice(0, 7).every((word) => word === 0) && ipv6[7] === 1) return true;
  // fc00::/7 (unique local) and fe80::/10 (link-local).
  if ((ipv6[0] & 0xfe00) === 0xfc00 || (ipv6[0] & 0xffc0) === 0xfe80) return true;
  // IPv4-mapped IPv6; apply the same IPv4 policy to the embedded address.
  if (ipv6.slice(0, 5).every((word) => word === 0) && ipv6[5] === 0xffff) {
    return isLocalIPv4([ipv6[6] >> 8, ipv6[6] & 0xff, ipv6[7] >> 8, ipv6[7] & 0xff]);
  }
  return false;
}

/** Removes public, hostname, and malformed targets while preserving order. */
export function localNetworkAddresses(addresses: string[]): string[] {
  return addresses.map((address) => address.trim()).filter((address) => isLocalNetworkAddress(address));
}

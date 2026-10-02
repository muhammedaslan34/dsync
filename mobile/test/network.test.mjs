import assert from 'node:assert/strict';
import fs from 'node:fs';
import { isLocalNetworkAddress, localNetworkAddresses } from '../src/network.ts';

let passed = 0;
function test(name, fn) {
  fn();
  passed++;
  console.log('ok  ' + name);
}

test('allows loopback, private, and link-local IPv4', () => {
  for (const address of [
    'localhost',
    'LOCALHOST',
    '127.0.0.1',
    '127.255.255.254',
    '10.0.0.1',
    '10.255.255.254',
    '172.16.0.1',
    '172.31.255.254',
    '192.168.0.1',
    '169.254.12.34',
  ]) {
    assert.equal(isLocalNetworkAddress(address), true, address);
  }
});

test('allows loopback, private, and link-local IPv6', () => {
  for (const address of [
    '::1',
    '[::1]',
    'fc00::1',
    'fd12:3456:789a::1',
    'fe80::1234',
    'fe80::1%en0',
    '::ffff:192.168.1.20',
  ]) {
    assert.equal(isLocalNetworkAddress(address), true, address);
  }
});

test('rejects public, hostname, and malformed targets', () => {
  for (const address of [
    'example.com',
    'localhost.example',
    '8.8.8.8',
    '172.15.0.1',
    '172.32.0.1',
    '192.169.1.1',
    '169.253.1.1',
    '100.64.0.1',
    '1.2.3',
    '0x7f.0.0.1',
    '010.0.0.1',
    '0127.0.0.1',
    '192.168.01.1',
    '127.0.0.1.example',
    '2001:4860:4860::8888',
    '::ffff:8.8.8.8',
    'fe80::1%bad zone',
    '',
  ]) {
    assert.equal(isLocalNetworkAddress(address), false, address);
  }
});

test('filters a mixed QR address list without reordering allowed targets', () => {
  assert.deepEqual(
    localNetworkAddresses([' 8.8.8.8 ', ' 192.168.1.4 ', 'host.example', 'fd00::4']),
    ['192.168.1.4', 'fd00::4'],
  );
});

test('iOS ATS permits local networking without arbitrary loads', () => {
  const app = JSON.parse(fs.readFileSync(new URL('../app.json', import.meta.url), 'utf8'));
  assert.deepEqual(app.expo.ios.infoPlist.NSAppTransportSecurity, { NSAllowsLocalNetworking: true });
});

console.log(`\n${passed} network policy tests passed`);

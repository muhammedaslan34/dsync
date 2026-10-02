import assert from 'node:assert/strict';
import { safeFileName } from '../src/safe-file-name.ts';

assert.equal(safeFileName('invoice\u202efdp.exe'), 'invoicefdp.exe');
assert.equal(safeFileName('safe\u200bname.pdf'), 'safename.pdf');
assert.equal(safeFileName('\u2066photo.jpg\u2069'), 'photo.jpg');
assert.equal(safeFileName('../bad:name.txt'), '_bad_name.txt');

console.log('filename tests passed');

// Minimal node "assert" built on chai.
const chai = require('chai');
function assert(value, message) { chai.assert.ok(value, message); }
assert.ok = assert;
assert.equal = (a, b, m) => chai.assert.equal(a, b, m);
assert.notEqual = (a, b, m) => chai.assert.notEqual(a, b, m);
assert.strictEqual = (a, b, m) => chai.assert.strictEqual(a, b, m);
assert.notStrictEqual = (a, b, m) => chai.assert.notStrictEqual(a, b, m);
assert.deepEqual = (a, b, m) => chai.assert.deepEqual(a, b, m);
assert.deepStrictEqual = (a, b, m) => chai.assert.deepEqual(a, b, m);
assert.notDeepEqual = (a, b, m) => chai.assert.notDeepEqual(a, b, m);
assert.throws = (fn, err, m) => chai.assert.throws(fn, err, m);
assert.doesNotThrow = (fn, m) => chai.assert.doesNotThrow(fn, m);
assert.fail = (m) => chai.assert.fail(m);
module.exports = assert;

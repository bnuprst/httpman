// Libraries available to Postman scripts through require(). Each entry is a
// lazy factory so a library is only initialized when a script needs it.
/* global globalThis */
globalThis.__modules = {
  ajv: () => require('ajv'),
  assert: () => require('./assert.js'),
  atob: () => globalThis.atob,
  btoa: () => globalThis.btoa,
  buffer: () => require('buffer'),
  chai: () => require('chai'),
  cheerio: () => require('cheerio'),
  'crypto-js': () => require('crypto-js'),
  'csv-parse/lib/sync': () => require('csv-parse/lib/sync'),
  events: () => require('events'),
  lodash: () => require('lodash'),
  moment: () => require('moment'),
  path: () => require('path-browserify'),
  punycode: () => require('punycode/'),
  querystring: () => require('querystring-es3'),
  stream: () => require('stream-browserify'),
  string_decoder: () => require('string_decoder/'),
  timers: () => ({ setTimeout: globalThis.setTimeout, clearTimeout: globalThis.clearTimeout, setInterval: globalThis.setInterval, clearInterval: globalThis.clearInterval, setImmediate: globalThis.setImmediate, clearImmediate: globalThis.clearImmediate }),
  tv4: () => require('tv4'),
  url: () => require('url/'),
  util: () => require('util/'),
  uuid: () => {
    const u = require('uuid');
    // Postman's uuid module is callable (returns a v4 id) and exposes v1..v5.
    const fn = function () { return u.v4.apply(null, arguments); };
    Object.assign(fn, u);
    return fn;
  },
  xml2js: () => require('xml2js'),
  'postman-collection': () => globalThis.__sdk,
};

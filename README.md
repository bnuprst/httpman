# httpman

A local HTTP client for Postman collections, in the spirit of the Postman app from years ago. It's a fast native desktop app and a newman-style command-line runner. It has no accounts, no sync, no cloud and no AI. Nothing leaves your machine except the requests you send.

```
npm install -g httpman-app
httpman                      # start the desktop app
httpman run my.postman_collection.json -e dev.postman_environment.json
```

Windows is the primary platform. macOS and Linux are supported too.

## Features

**Postman compatibility**
- Imports and exports **Collection v2.1 and v2.0**, and imports legacy **v1** collections and Postman **data dumps**.
- Environments and globals in Postman's file format.
- Unknown fields are kept when you save, so a collection stays usable in Postman.
- Postman's own semantics are matched where it matters: URLs are built from parsed parts, `protocolProfileBehavior` (redirects, SSL) is honored, CSV data files are parsed the way newman parses them, and environment files can be UTF-8/UTF-16 with a BOM.
- Imports **cURL** commands. Exports code snippets for cURL (bash/cmd), PowerShell, fetch, Python requests, HTTPie and raw HTTP.

**Requests**
- All methods. Query/path params are synced with the URL. `{{variables}}` are highlighted, and you can hover to see the resolved value.
- Bodies: raw (JSON/XML/HTML/JS/text), form-data with files, x-www-form-urlencoded, binary file, GraphQL.
- Auth: Basic, Bearer, API Key, Digest, OAuth 1.0, OAuth 2.0 (token), Hawk, AWS Signature v4. Auth is inherited from folders and collections.
- Variable scopes work like Postman's: global → collection → environment → data → local. Dynamic variables (`{{$guid}}`, `{{$timestamp}}`, `{{$randomEmail}}`, …) are supported.
- Cookie jar with a cookie manager. Redirect, SSL and proxy settings. Timings (DNS/TCP/TLS/TTFB). Response preview for HTML, images and PDF.
- History, a console with request/response details, and light/dark themes.

**Scripts and tests**
- Pre-request and test scripts run at collection, folder and request level, in Postman's order.
- The `pm.*` API is supported: `pm.test`, `pm.expect` (Chai), `pm.response.to.have.status/header/jsonBody/jsonSchema…`, `pm.environment/globals/collectionVariables/variables/iterationData`, `pm.request` mutation, `pm.sendRequest` (callback or `await`), `pm.cookies.jar()`, `pm.execution.setNextRequest/skipRequest`, and `pm.visualizer`.
- The legacy API works too: `tests["…"]`, `responseBody`, `responseCode`, `postman.setEnvironmentVariable`, `postman.setNextRequest`, `xml2Json`, …
- Bundled libraries: Sugar.js 1.4 prototype extensions (`responseBody.has(…)`), `lodash` (`_`), `moment`, `crypto-js` (`CryptoJS`), `uuid`, `chai`, `ajv` (v6, like Postman), `tv4`, `cheerio`, `xml2js`, `csv-parse/lib/sync`, plus Node shims (`buffer`, `url`, `querystring`, `path`, `util`, `events`, `assert`, `timers`).
- `setTimeout`, promises and `async`/`await` work, and so does top-level `return`.

**Compatibility testing:** httpman is checked against newman's integration fixtures, run against a local postman-echo stand-in. All fixtures pass except a few:

- Hosts that can't be reached offline.
- Echo endpoints the stand-in doesn't implement.
- `Content-Length: 0` on custom HTTP methods with a disabled body. Go's HTTP client doesn't send it.

NTLM and Akamai EdgeGrid auth aren't implemented. Their settings are kept in the collection.

**Collection runner**
- Run a collection or folder for N iterations, with a CSV/JSON data file and an optional delay. You can pick which requests to run, stop on the first failure, and keep variable changes.

## Command line

`httpman run` takes newman's flags:

```
httpman run <collection-file | workspace collection name/id> [options]

  -e, --environment <file|name>   -g, --globals <file>
  -d, --iteration-data <file>     -n, --iteration-count <n>
  --folder <name>                 --env-var key=value   --global-var key=value
  --delay-request <ms>            --timeout-request <ms>   --timeout-script <ms>
  -k, --insecure                  --ignore-redirects       --bail
  -r, --reporters cli,json,junit  --reporter-json-export <file>
  --reporter-junit-export <file>  --export-environment <file>
  --export-globals <file>         --export-collection <file>
  --working-dir <dir>             --no-color  --silent  --verbose
```

The exit code is non-zero when any request, assertion or script fails. That makes it a drop-in for `newman run` in CI. Other commands:

```
httpman import <file>...   # add collections/environments to the desktop workspace
httpman list               # list workspace collections and environments
httpman version
```

## Where data lives

Everything is stored as plain files in the workspace folder:

- **Windows:** `%AppData%\httpman`
- **macOS:** `~/Library/Application Support/httpman`
- **Linux:** `~/.config/httpman`

You can override the location with the `HTTPMAN_HOME` environment variable or `httpman --workspace <dir>`.

```
collections/<id>.postman_collection.json
environments/<id>.postman_environment.json
globals.postman_globals.json
history.json  cookies.json  settings.json  state.json
```

Collections and environments are ordinary Postman exports, so you can keep a workspace in git.

## Requirements

- **Windows 10/11:** uses the Microsoft Edge **WebView2** runtime, which is preinstalled on current Windows.
- **macOS 11+.**
- **Linux:** GTK 3 and WebKitGTK 4.1 (`libwebkit2gtk-4.1-0`). The CLI has no runtime dependencies.

The npm package is a small launcher plus one prebuilt binary for your platform (`httpman-app-<os>-<arch>`), installed as an optional dependency. Nothing is downloaded at install time.

If the desktop app doesn't start when launched through npm, run it with `HTTPMAN_FOREGROUND=1 httpman` to see its error output.

## Building from source

Requirements: Go 1.26+ and Node.js 18+. On Linux you also need `libgtk-3-dev libwebkit2gtk-4.1-dev`. On macOS you need the Xcode command-line tools.

```
node scripts/build.mjs                 # desktop app for this machine → dist/bin/<os>-<arch>/
node scripts/build.mjs --nogui         # CLI-only build, no CGO or WebView needed
node scripts/build.mjs --target windows/amd64   # cross-compile the Windows app from any OS
```

The Windows build doesn't need CGO, so you can cross-compile it from Linux or macOS.

Development with live reload uses the [Wails CLI](https://wails.io) (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`): run `wails dev`. On Linux, add `-tags webkit2_41`.

Project layout:

| Path | |
|---|---|
| `main.go`, `gui.go` | entry point: CLI dispatch and the Wails desktop shell |
| `internal/collection` | Postman collection model, v1/v2.0/v2.1 parsing |
| `internal/vars` | variable scopes, `{{var}}` substitution, dynamic variables, environments |
| `internal/httpclient` | HTTP engine: bodies, auth (basic/digest/oauth1/awsv4/…), redirects, timings |
| `internal/script` | goja-based script sandbox; `sandbox/` holds the JavaScript `pm` API and libraries, bundled into `internal/script/sandbox.bundle.js` |
| `internal/runner` | request execution (script chain, auth inheritance) and the collection runner |
| `internal/cli` | `httpman run` and its reporters |
| `internal/app` | backend for the desktop UI |
| `frontend/` | React + TypeScript UI |
| `npm/`, `scripts/` | npm launcher and packaging/build scripts |

Tests: `go test ./...` and `cd frontend && npm run typecheck`. After changing `sandbox/src`, run `cd sandbox && npm ci && npm run build` and commit the regenerated bundle.

## Releasing

Push a tag `vX.Y.Z`. The release workflow does the following:

1. Builds the binaries on Windows, macOS and Linux.
2. Attaches them to a GitHub release.
3. Stages the npm packages through npm trusted publishing. Each package trusts `release.yml` in the `npm` environment, so no npm token is stored.

Then approve the staged versions with 2FA: run `npm stage list` and `npm stage approve <id>` for each one, approving the platform packages before `httpman-app`.

## License

MIT

# Storage management API

A client talks to a hosted Atomic server with two HTTP clients. `HttpRemote` speaks the project protocol that push, pull and clone use. `StorageClient` speaks the atomic-storage management API: create, read, update and delete on workspaces, projects and other management resources, with bearer authentication, JSON bodies and a response envelope that it unwraps ([`storage.rs#L1-L8`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L1-L8)). This page documents every request `StorageClient` issues, the requests the `atomic-teams` crate issues through it ([`lib.rs#L1-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/lib.rs#L1-L6)), and the one management request the CLI sends with its own HTTP client, identity registration. The project protocol is documented in [project.md](project.md).

The server is not in the source. This page describes what the client sends and what it accepts. Anything decided only by the server is listed under [Open questions](#open-questions).

## Overview

### Base URLs

`StorageClient` holds one base URL and appends each method's path to it as is: each helper builds the URL as `format!("{}{}", self.base_url, path)`, as `get` does here ([`storage.rs#L149-L151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L149-L151)). The constructor strips trailing slashes from the base ([`storage.rs#L128-L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L128-L132)), so `https://example.com/` becomes `https://example.com` ([`storage.rs#L634-L638`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L634-L638)). The client does not know which base a path needs; whoever builds it picks the base. Three bases are in use.

| Base | Value | Built by | Used for |
|---|---|---|---|
| `{org_base}` | `{scheme}://{org}.{host}[:{port}]`, or the server URL unchanged on a single-tenant server ([Org host](../transport.md#org-host)) | `build_client_with_org` in the CLI ([`client.rs#L212-L218`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L212-L218)) | Workspaces, projects, identity resolution by email or name from member commands, orgs other than list and create, org members, teams, workspace grants. Org grants and domains have no caller, so their base is assumed. |
| `{apex}` | The configured server URL, such as `https://atomic.storage` ([Apex host](../transport.md#apex-host)) | `build_apex_client` and `build_apex_client_as` ([`client.rs#L91-L94`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L91-L94), [`client.rs#L173`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L173)) | Agents, delegations, lookup by public key, `GET /orgs`, `POST /orgs`, and identity resolution by name from `atomic intent verify` |
| `{origin}` | Scheme, host and port of a project remote URL | `signer_refresh::client_for_remote` in libatomic ([`signer_refresh.rs#L8-L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L8-L21)) | Signer-key discovery after a pull or on a knowledge-graph reindex |

**Org base.** How `{org_base}` is formed from the configured server URL and the org slug, the single-tenant case included, is documented once, on [Org host](../transport.md#org-host). The CLI learns that a server is single-tenant from the registration response; see [`execute`](#execute).

**Which org.** The org slug is the `--org` flag when given (an empty value is an error), else the profile's `default_org`, else the name of the identity the profile authenticates as. Registration seeds that name as the personal org ([`client.rs#L319-L346`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L319-L346)). The profile is the one named by `--server`, else `default_server`, else the legacy `[server]` block ([`lib.rs#L292-L324`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-config/src/lib.rs#L292-L324)).

**Apex.** The management apex is the profile's `url` as configured; how it is chosen, and how it differs from the token apex that push, pull and clone derive from a project URL, is on [Apex host](../transport.md#apex-host). The agent and delegation endpoints are apex endpoints because an agent belongs to a human, not to an org, according to a comment in `StorageClient` ([`storage.rs#L431-L437`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L431-L437)). `GET /orgs` and `POST /orgs` span orgs and go to the bare host, according to the CLI ([`client.rs#L61-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L61-L72)).

**Relation to the project URL.** A project's remote URL is `{org_base}/workspaces/{workspace}/projects/{project_slug}/code` ([`init.rs#L182-L187`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L182-L187)), so the management API and the project protocol share a host; how the CLI builds and stores that URL is on [Project URL](../transport.md#project-url).

**Path parameters.** Workspace, project, org and team slugs, UUIDs and public keys go into the path without escaping ([`storage.rs#L324-L327`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L324-L327), [`storage.rs#L550`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L550), [`team.rs#L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L64)). A `Uuid` argument is formatted in its hyphenated form, as a test of the domain paths shows ([`domain.rs#L127-L135`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L127-L135)). Email addresses, names, delegation IDs and DIDs go through `urlencoding::encode` ([`storage.rs#L415-L418`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L415-L418), [`storage.rs#L491`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L491), [`storage.rs#L571`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L571)). That function is external, from urlencoding 2.1.3 ([`Cargo.lock#L4988-L4990`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L4988-L4990)): it percent-encodes every byte except ASCII letters, digits, `-`, `.`, `_` and `~` ([docs](https://docs.rs/urlencoding/2.1.3/urlencoding/fn.encode.html)), so `urn:atomic:delegation:X` travels as `urn%3Aatomic%3Adelegation%3AX`.

### Request headers

`StorageClient` sets its headers as the `reqwest::Client`'s default headers, so every request carries them, `GET` and `DELETE` included ([`storage.rs#L104-L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L104-L126)). Default headers are reqwest behaviour, from reqwest 0.12.28 ([`Cargo.lock#L3677-L3679`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L3677-L3679), [docs](https://docs.rs/reqwest/0.12.28/reqwest/struct.ClientBuilder.html#method.default_headers)).

| Header | Value | When |
|---|---|---|
| `Authorization` | `Bearer {token}` | Always ([`storage.rs#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L113-L117)) |
| `Content-Type` | `application/json` | Always, including requests with no body ([`storage.rs#L118`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L118)) |
| `User-Agent` | `atomic/{version}`, where `{version}` is the crate version ([`storage.rs#L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L120), [`lib.rs#L177-L178`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/lib.rs#L177-L178)), which is the workspace version ([`Cargo.toml#L3`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/Cargo.toml#L3)): `0.22.1` at this commit ([`Cargo.toml#L21-L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.toml#L21-L22)) | Always |
| `atomic-delegation` | The encoded delegation certificate | Only when the client was built with a delegation ([`storage.rs#L104-L112`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L104-L112)) |

A token or delegation that is not a valid HTTP header value, such as one containing a newline, fails when the client is built rather than when a request is sent ([`storage.rs#L105-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L105-L117), [`storage.rs#L621-L632`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L621-L632)). atomic-remote enables reqwest's `gzip` and `deflate` features ([`Cargo.toml#L20-L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/Cargo.toml#L20-L21)); see [Compression](../transport.md#compression).

**The token.** The bearer token is a short-lived EdDSA JWT that the client signs itself; there is no login endpoint ([`token.rs#L1-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/auth/token.rs#L1-L6)). All three CLI builders mint it for the apex URL, including for an org-scoped client ([`client.rs#L223-L224`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L223-L224), [`client.rs#L98-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L98-L100), [`client.rs#L173-L174`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L173-L174)), so its `aud` claim is the apex, trimmed, lowercased and without a trailing slash ([`token.rs#L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/auth/token.rs#L164), [`token.rs#L201-L206`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/auth/token.rs#L201-L206)). See [Tokens](../auth.md#tokens).

**The delegation header.** The header is `atomic-delegation`, the lowercase spelling of `atomic_canonical::delegation::DELEGATION_HEADER` (`Atomic-Delegation`), and a test keeps the two in step ([`storage.rs#L14-L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L14-L17), [`delegation.rs#L277`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-canonical/src/delegation.rs#L277), [`storage.rs#L604-L612`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L604-L612)). The value is the certificate canonicalized with JCS and encoded as base64url without padding ([`delegation.rs#L292-L295`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-canonical/src/delegation.rs#L292-L295)). The CLI's apex clients send it when the authenticating identity is delegated and its active certificate for that server can be loaded from the identity store; otherwise they send none ([`client.rs#L102-L104`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L102-L104), [`client.rs#L175-L178`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L175-L178), [`client.rs#L118-L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L118-L127)). Org-scoped clients never send it: `build_client_with_org` calls `StorageClient::new` ([`client.rs#L226-L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L226-L228)), which passes no delegation ([`storage.rs#L86-L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L86-L88)). See [Delegation](../auth.md#delegation) and [Request headers](../auth.md#request-headers).

**No timeouts or retries.** The client builder sets only the default headers and the user agent ([`storage.rs#L122-L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L122-L126)), and each helper sends its request once ([`storage.rs#L154-L161`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L154-L161)). Callers add their own limits; signer discovery gives each lookup 10 seconds ([`signer_refresh.rs#L30-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L30-L34)).

### Request bodies

There are five helpers. `get` sends no body ([`storage.rs#L149-L162`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L149-L162)). `post` and `put` send the body serialized as JSON with reqwest's `.json` ([`storage.rs#L164-L182`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L164-L182), [`storage.rs#L199-L217`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L199-L217)). `post_empty` sends a `POST` with no body ([`storage.rs#L184-L197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L184-L197)). `delete` sends no body ([`storage.rs#L219-L229`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L219-L229)). The helpers are public, which is how `atomic-teams` sends its own paths through the same client ([`org.rs#L39-L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L39-L42)).

### Response envelope

Every response other than a successful `DELETE` must be an [ApiResponse](#apiresponse) envelope with camelCase keys ([`storage_types.rs#L10-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L10-L24)):

```json
{"success": true, "data": {"id": "00000000-0000-0000-0000-000000000001", "name": "remote signer", "status": "active", "createdAt": "2026-10-15T00:00:00Z", "publicKey": "..."}}
{"success": false, "error": {"code": "NOT_FOUND", "message": "workspace not found"}}
```

The success example is a test fixture's reply to a key lookup ([`signer_refresh.rs#L36-L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/signer_refresh.rs#L36-L39)); the error example is from a test of the error path ([`storage.rs#L655-L662`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L655-L662)).

`handle_response` works through a response in this order ([`storage.rs#L241-L278`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L241-L278)):

1. Check the `X-Atomic-Min-Version` header and print a warning when this client is older than it ([`storage.rs#L246`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L246), [`version.rs#L45-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/version.rs#L45-L65)). See [Version negotiation](../transport.md#version-negotiation).
2. Read the whole body as text ([`storage.rs#L248-L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L248-L251)).
3. Send a status outside 2xx to the [error path](#error-mapping) ([`storage.rs#L253-L255`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L253-L255)).
4. Parse the body as `ApiResponse<T>`. A body that does not parse fails with `invalid response JSON: … (body: …)`, quoting at most 2000 bytes of it ([`storage.rs#L257-L266`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L257-L266), [`storage.rs#L26-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L26-L31)).
5. Fail on `success: false` with `{code}: {message}` from `error`, or with `request failed with no error details` when `error` is absent ([`storage.rs#L268-L273`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L268-L273)).
6. Fail on `success: true` without `data`, with `response had success=true but no data` ([`storage.rs#L275-L277`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L275-L277)). A successful response to anything but `DELETE` must carry `data`.

Every failure in steps 2, 4, 5 and 6 is a `RemoteError::Other` carrying only the message ([`storage.rs#L248-L277`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L248-L277), [`error.rs#L291-L294`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/error.rs#L291-L294)). So a 2xx response with `success: false` keeps neither the status nor a typed code, and `is_auth_error()` and `is_not_found()` are false for it ([`error.rs#L344-L364`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/error.rs#L344-L364)).

The client accepts any 2xx status and does not check which one. A successful `DELETE` is not parsed: after the version check, any 2xx returns `Ok(())` and the body is ignored ([`storage.rs#L231-L238`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L231-L238)). The envelope's `metadata` is never read, and no method sends paging parameters; for example, `list_workspaces` requests `/workspaces` with no query ([`storage.rs#L311-L314`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L311-L314)).

### Error mapping

For a status outside 2xx, `parse_error_body` looks for a message in this order ([`storage.rs#L280-L295`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L280-L295)):

1. If the body parses as an `ApiResponse` with an `error`, use `error.message` ([`storage.rs#L281-L286`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L281-L286)).
2. Else, if it parses as a bare [ApiError](#apierror), `{"code": …, "message": …}`, use `message`. A comment in the code says some endpoints return this on validation and auth failures ([`storage.rs#L288-L292`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L288-L292)).
3. Else the message is `HTTP {status}` ([`storage.rs#L294`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L294)).

The error `code` is dropped on this path. The status picks the constructor, and every constructor produces `RemoteError::HttpError { status, message }` ([`storage.rs#L297-L305`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L297-L305), [`error.rs#L296-L334`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/error.rs#L296-L334)). Its text is `HTTP error {status}: {message}` ([`error.rs#L99-L106`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/error.rs#L99-L106)).

| Status | Constructor | Recognised by |
|---|---|---|
| 401 | `unauthorized` | `is_auth_error()` ([`error.rs#L344-L352`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/error.rs#L344-L352)) |
| 403 | `forbidden` | `is_auth_error()` |
| 404 | `not_found` | `is_not_found()` ([`error.rs#L354-L364`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/error.rs#L354-L364)) |
| 409 | `conflict` | status only |
| any other | `server_error` | status only |

Tests pin the mapping: a 401 envelope keeps its message and counts as an auth error, a 404 envelope counts as not found, and a non-JSON 502 becomes a message containing `502` ([`storage.rs#L646-L669`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L646-L669)). A failure before any response arrives, such as a refused connection, becomes `RemoteError::Other("request failed: …")` ([`storage.rs#L154-L159`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L154-L159), [`error.rs#L291-L294`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/error.rs#L291-L294)), which is neither an auth error nor a not-found error. See [Error kinds](../errors.md#error-kinds) and [Error responses](../errors.md#error-responses).

**atomic-teams.** Every `atomic-teams` function passes a `RemoteError` through `map_remote_error` together with a context string such as `org acme` ([`error.rs#L48-L78`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/error.rs#L48-L78)):

| `RemoteError` | `TeamsError` |
|---|---|
| `HttpError` 404 | `TeamNotFound` when the context contains "team", else `MemberNotFound` when it contains "member" or "identity", else `OrgNotFound`. The context string, not the server's message, becomes the error text. |
| `HttpError` 403 | `PermissionDenied(message)` |
| `HttpError` 409 | `AlreadyExists(message)` |
| `HttpError` with any other status, 401 included | `Remote(err)` |
| any other variant, such as `Other` | `Remote(err)` |

The 404 test is a case-insensitive substring match on the whole context string, slugs included ([`error.rs#L58-L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/error.rs#L58-L68)). An org or workspace slug that contains `team`, `member` or `identity` therefore changes which variant a 404 becomes; the variants named in the sections below assume slugs without those words. The module's tests show 404 with `org acme`, `team backend` and `member xyz`, and 403, 409 and 500 ([`error.rs#L111-L151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/error.rs#L111-L151)).

### Index

| Method | Request | Base | Crate |
|---|---|---|---|
| [`list_workspaces`](#list_workspaces) | `GET /workspaces` | org | atomic-remote |
| [`create_workspace`](#create_workspace) | `POST /workspaces` | org | atomic-remote |
| [`get_workspace`](#get_workspace) | `GET /workspaces/{slug}` | org | atomic-remote |
| [`update_workspace`](#update_workspace) | `PUT /workspaces/{slug}` | org | atomic-remote |
| [`delete_workspace`](#delete_workspace) | `DELETE /workspaces/{slug}` | org | atomic-remote |
| [`list_projects`](#list_projects) | `GET /workspaces/{ws}/projects` | org | atomic-remote |
| [`create_project`](#create_project) | `POST /workspaces/{ws}/projects` | org | atomic-remote |
| [`get_project`](#get_project) | `GET /workspaces/{ws}/projects/{project}` | org | atomic-remote |
| [`update_project`](#update_project) | `PUT /workspaces/{ws}/projects/{project}` | org | atomic-remote |
| [`delete_project`](#delete_project) | `DELETE /workspaces/{ws}/projects/{project}` | org | atomic-remote |
| [`resolve_identity_by_email`](#resolve_identity_by_email) | `GET /identities/resolve?email={email}` | org | atomic-remote |
| [`resolve_identity_by_name`](#resolve_identity_by_name) | `GET /identities/resolve?name={name}` | org or apex | atomic-remote |
| [`resolve_identity_by_public_key`](#resolve_identity_by_public_key) | `GET /identities/by-key/{key}` | apex or origin | atomic-remote |
| [`resolve_identity_by_did`](#resolve_identity_by_did) | `GET /identities/by-did/{did}` | no caller | atomic-remote |
| [`enroll_agent`](#enroll_agent) | `POST /identities/agents` | apex | atomic-remote |
| [`list_agents`](#list_agents) | `GET /identities/agents` | apex | atomic-remote |
| [`get_agent`](#get_agent) | `GET /identities/agents/{id}` | no caller | atomic-remote |
| [`retire_agent`](#retire_agent) | `DELETE /identities/agents/{id}` | no caller | atomic-remote |
| [`set_agent_epoch`](#set_agent_epoch) | `POST /identities/agents/{id}/epoch` | apex | atomic-remote |
| [`clear_agent_epoch`](#clear_agent_epoch) | `DELETE /identities/agents/{id}/epoch` | no caller | atomic-remote |
| [`push_delegation`](#push_delegation) | `POST /delegations` | apex | atomic-remote |
| [`list_delegations`](#list_delegations) | `GET /delegations` | no caller | atomic-remote |
| [`revoke_delegation`](#revoke_delegation) | `POST /delegations/{id}/revoke` | apex | atomic-remote |
| [`set_delegator_epoch`](#set_delegator_epoch) | `POST /delegations/epoch` | apex | atomic-remote |
| [`delegation_status`](#delegation_status) | `GET /delegations/{id}/status` | apex | atomic-remote |
| [`create_org`](#create_org) | `POST /orgs` | apex | atomic-teams |
| [`list_my_orgs`](#list_my_orgs) | `GET /orgs` | apex | atomic-teams |
| [`get_org`](#get_org) | `GET /orgs/{slug}` | org | atomic-teams |
| [`update_org`](#update_org) | `PUT /orgs/{slug}` | org | atomic-teams |
| [`delete_org`](#delete_org) | `DELETE /orgs/{slug}` | org | atomic-teams |
| [`upgrade_org`](#upgrade_org) | `POST /orgs/{slug}/upgrade` | org | atomic-teams |
| [`list_members`](#list_members) | `GET /orgs/{slug}/members` | org | atomic-teams |
| [`add_member`](#add_member) | `POST /orgs/{slug}/members` | org | atomic-teams |
| [`get_member`](#get_member) | `GET /orgs/{slug}/members/{identity}` | no caller | atomic-teams |
| [`update_member_role`](#update_member_role) | `PUT /orgs/{slug}/members/{identity}` | org | atomic-teams |
| [`remove_member`](#remove_member) | `DELETE /orgs/{slug}/members/{identity}` | org | atomic-teams |
| [`list_domains`](#list_domains) | `GET /orgs/{slug}/domains` | no caller | atomic-teams |
| [`claim_domain`](#claim_domain) | `POST /orgs/{slug}/domains` | no caller | atomic-teams |
| [`verify_domain`](#verify_domain) | `POST /orgs/{slug}/domains/{id}/verify` | no caller | atomic-teams |
| [`revoke_domain`](#revoke_domain) | `DELETE /orgs/{slug}/domains/{id}` | no caller | atomic-teams |
| [`list_teams`](#list_teams) | `GET /teams` | org | atomic-teams |
| [`create_team`](#create_team) | `POST /teams` | org | atomic-teams |
| [`get_team`](#get_team) | `GET /teams/{team}` | org | atomic-teams |
| [`update_team`](#update_team) | `PUT /teams/{team}` | org | atomic-teams |
| [`delete_team`](#delete_team) | `DELETE /teams/{team}` | org | atomic-teams |
| [`list_team_members`](#list_team_members) | `GET /teams/{team}/members` | org | atomic-teams |
| [`add_team_member`](#add_team_member) | `POST /teams/{team}/members` | org | atomic-teams |
| [`update_team_member_role`](#update_team_member_role) | `PUT /teams/{team}/members/{identity}` | org | atomic-teams |
| [`remove_team_member`](#remove_team_member) | `DELETE /teams/{team}/members/{identity}` | org | atomic-teams |
| [`list_org_grants`](#list_org_grants) | `GET /orgs/{slug}/grants` | no caller | atomic-teams |
| [`add_org_grant`](#add_org_grant) | `POST /orgs/{slug}/grants` | no caller | atomic-teams |
| [`revoke_org_grant`](#revoke_org_grant) | `DELETE /orgs/{slug}/grants/{type}/{id}` | no caller | atomic-teams |
| [`list_workspace_grants`](#list_workspace_grants) | `GET /workspaces/{ws}/grants` | org | atomic-teams |
| [`add_workspace_grant`](#add_workspace_grant) | `POST /workspaces/{ws}/grants` | org | atomic-teams |
| [`revoke_workspace_grant`](#revoke_workspace_grant) | `DELETE /workspaces/{ws}/grants/{type}/{id}` | org | atomic-teams |
| [`execute`](#execute) | `POST /register` | apex | atomic-cli |

"No caller" means no code in the repository calls the method at this commit, so no base is established for it. Every row above has a section below with its source.

## Workspaces

### `list_workspaces`

`GET {org_base}/workspaces` ([`storage.rs#L311-L314`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L311-L314)). Lists every workspace visible to the authenticated identity.

- **Parameters**: none.
- **Request body**: none.
- **Response**: `data` is an array of [WorkspaceInfo](#workspaceinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic workspace list`, on an org-scoped client ([`list.rs#L90-L91`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/list.rs#L90-L91)).

### `create_workspace`

`POST {org_base}/workspaces` with a [CreateWorkspaceRequest](#createworkspacerequest) body ([`storage.rs#L316-L322`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L316-L322)). The request carries a name but no slug; the server returns the slug in the response.

```json
{"name": "my-workspace", "description": "A workspace", "visibility": "public"}
```

- **Parameters**: none.
- **Response**: `data` is the new [WorkspaceInfo](#workspaceinfo).
- **Errors**: the [common mapping](#error-mapping). `atomic project init` treats any error whose text contains `409`, `conflict` or `Conflict` as "the workspace already exists" and carries on ([`init.rs#L144-L163`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L144-L163), [`rpc.rs#L7317-L7335`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/rpc.rs#L7317-L7335)).
- **Callers**: `atomic workspace create` ([`create.rs#L84-L94`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/create.rs#L84-L94)), and `atomic project init` when `get_workspace` fails for any reason; it then creates a workspace whose `name` is the slug it went on to use ([`init.rs#L130-L143`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L130-L143), [`rpc.rs#L7309-L7316`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/rpc.rs#L7309-L7316)).

### `get_workspace`

`GET {org_base}/workspaces/{slug}` ([`storage.rs#L324-L327`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L324-L327)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Workspace slug, inserted without escaping |

- **Request body**: none.
- **Response**: `data` is a [WorkspaceInfo](#workspaceinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Callers**: `atomic workspace show` ([`show.rs#L71-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/show.rs#L71-L72)), `atomic workspace set` to check the slug ([`set.rs#L191-L192`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/set.rs#L191-L192)), `atomic workspace delete` before it prompts, unless `--force` ([`delete.rs#L76-L79`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/delete.rs#L76-L79)), and `atomic project init` ([`init.rs#L130-L135`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L130-L135), [`rpc.rs#L7307-L7311`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/rpc.rs#L7307-L7311)).

### `update_workspace`

`PUT {org_base}/workspaces/{slug}` with an [UpdateWorkspaceRequest](#updateworkspacerequest) body ([`storage.rs#L329-L336`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L329-L336)). Only the fields that are set are sent, and a request with none set is `{}` ([`storage_types.rs#L424-L442`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L424-L442)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Workspace slug, inserted without escaping |

- **Response**: `data` is the updated [WorkspaceInfo](#workspaceinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic workspace update` ([`update.rs#L82-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/update.rs#L82-L93)).

### `delete_workspace`

`DELETE {org_base}/workspaces/{slug}` ([`storage.rs#L338-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L338-L341)). The CLI warns that this deletes the workspace and all its projects ([`delete.rs#L81-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/delete.rs#L81-L84)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Workspace slug, inserted without escaping |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic workspace delete` ([`delete.rs#L98-L101`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/delete.rs#L98-L101)).

## Projects

### `list_projects`

`GET {org_base}/workspaces/{workspace_slug}/projects` ([`storage.rs#L347-L354`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L347-L354)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug, inserted without escaping |

- **Request body**: none.
- **Response**: `data` is an array of [ProjectInfo](#projectinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic project list`, which falls back to the profile's default workspace for the org when `--workspace` is not given ([`list.rs#L77-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/list.rs#L77-L82), [`client.rs#L398-L407`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L398-L407)).

### `create_project`

`POST {org_base}/workspaces/{workspace_slug}/projects` with a [CreateProjectRequest](#createprojectrequest) body ([`storage.rs#L356-L364`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L356-L364)). The request body uses snake_case `default_view`, while the response uses camelCase `defaultView` ([`storage_types.rs#L446-L460`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L446-L460), [`storage_types.rs#L528-L550`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L528-L550)).

```json
{"name": "my-project", "description": "A project", "default_view": "main", "kind": "rust", "visibility": "private"}
```

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug, inserted without escaping |

- **Response**: `data` is the new [ProjectInfo](#projectinfo). The CLI uses its `slug` to build the project URL ([`init.rs#L176-L187`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L176-L187)).
- **Errors**: the [common mapping](#error-mapping).
- **Callers**: `atomic project create`, which defaults `default_view` to `dev` and `visibility` to `private` ([`create.rs#L89-L97`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/create.rs#L89-L97), [`create.rs#L140-L151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/create.rs#L140-L151)), and `atomic project init`, which always sends `default_view: "dev"` ([`init.rs#L168-L179`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L168-L179)). `ProjectInit::run` first calls `rpc::project_init` ([`init.rs#L103-L105`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L103-L105)), which sends the same `get_workspace`, `create_workspace` and `create_project` sequence before it opens a repository session ([`rpc.rs#L7301-L7364`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/rpc.rs#L7301-L7364)). No session opens outside a repository ([`mod.rs#L120-L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/service/mod.rs#L120-L127)); then `rpc::project_init` returns false after the requests have been sent, and `ProjectInit::execute` fails at its repository check before sending any of its own ([`init.rs#L107-L124`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L107-L124)).

### `get_project`

`GET {org_base}/workspaces/{workspace_slug}/projects/{project_slug}` ([`storage.rs#L366-L377`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L366-L377)). A test sends `GET /workspaces/ws/projects/missing` and answers with a 404 and `{"success":false,"error":{"code":"NOT_FOUND","message":"project not found"}}` ([`delete.rs#L178-L201`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/delete.rs#L178-L201)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug, inserted without escaping |
| `project_slug` | string | Required | Project slug, inserted without escaping |

- **Request body**: none.
- **Response**: `data` is a [ProjectInfo](#projectinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Callers**: `atomic project show`, which takes the project as `workspace/project` ([`show.rs#L81-L87`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/show.rs#L81-L87), [`mod.rs#L53-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/mod.rs#L53-L58)), and `atomic project delete` before it prompts, unless `--force` ([`delete.rs#L99-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/delete.rs#L99-L115)).

### `update_project`

`PUT {org_base}/workspaces/{workspace_slug}/projects/{project_slug}` with an [UpdateProjectRequest](#updateprojectrequest) body ([`storage.rs#L379-L391`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L379-L391)). Only the fields that are set are sent ([`storage_types.rs#L478-L491`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L478-L491)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug, inserted without escaping |
| `project_slug` | string | Required | Project slug, inserted without escaping |

- **Response**: `data` is the updated [ProjectInfo](#projectinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic project update` ([`update.rs#L97-L109`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/update.rs#L97-L109)).

### `delete_project`

`DELETE {org_base}/workspaces/{workspace_slug}/projects/{project_slug}` ([`storage.rs#L393-L404`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L393-L404)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug, inserted without escaping |
| `project_slug` | string | Required | Project slug, inserted without escaping |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: the [common mapping](#error-mapping). The CLI prints "Project '…' not found in workspace '…'" for a not-found error ([`delete.rs#L131-L144`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/delete.rs#L131-L144)).
- **Caller**: `atomic project delete` ([`delete.rs#L117-L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/delete.rs#L117-L120)).

## Identities

### `resolve_identity_by_email`

`GET {org_base}/identities/resolve?email={email}` ([`storage.rs#L410-L420`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L410-L420)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `email` | query string | Required | Email address, percent-encoded with `urlencoding::encode` |

- **Request body**: none.
- **Response**: `data` is an [IdentityInfo](#identityinfo).
- **Errors**: the [common mapping](#error-mapping). The CLI turns not-found into "No identity found with email '…'" with a hint to register ([`client.rs#L469-L487`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L469-L487)).
- **Caller**: `resolve_identity` in the CLI, for an identifier that is not a UUID and contains `@` ([`client.rs#L463-L470`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L463-L470)). A UUID is used as is, with no request. The org and team member commands call it with their org-scoped client ([`member.rs#L303-L309`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/member.rs#L303-L309), [`member.rs#L285-L291`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/member.rs#L285-L291)).

### `resolve_identity_by_name`

`GET {base}/identities/resolve?name={name}` ([`storage.rs#L422-L429`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L422-L429)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `name` | query string | Required | Display name, percent-encoded with `urlencoding::encode` |

- **Request body**: none.
- **Response**: `data` is an [IdentityInfo](#identityinfo).
- **Errors**: the [common mapping](#error-mapping). `resolve_identity` turns not-found into "No identity found with name '…'" ([`client.rs#L497-L514`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L497-L514)).
- **Callers**: `resolve_identity`, for an identifier that is neither a UUID nor contains `@`, on the member commands' org-scoped client ([`client.rs#L497-L501`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L497-L501)). `atomic intent verify` calls it on an apex client to fetch a signer's key, and fails if the reply has no `publicKey` ([`verify.rs#L210-L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/intent/verify.rs#L210-L228)).

### `resolve_identity_by_public_key`

`GET {base}/identities/by-key/{public_key}` ([`storage.rs#L538-L551`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L538-L551)). The method formats its string argument into the path as given, with no escaping and no check of its form ([`storage.rs#L550`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L550)). Its documentation describes that argument as the base32 Ed25519 public key, the same value as a JWT's `kid`, which is URL-safe and so needs no escaping, and says the server endpoint is unauthenticated ([`storage.rs#L538-L545`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L538-L545)). The client still sends its `Authorization` header, since every request carries it ([`storage.rs#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L113-L117)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `public_key` | string, one path segment | Required | Inserted verbatim. The two callers send different forms: `atomic identity lookup-key` sends a base32 Ed25519 public key without padding, 52 characters `A-Z2-7` ([`keypair.rs#L52-L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-identity/src/keypair.rs#L52-L54), [`lookup_key.rs#L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/lookup_key.rs#L20)), and signer discovery sends a `did:atomic:` DID, colons unescaped (see Callers). Which forms the server accepts is an [open question](#open-questions). See [Identities and DIDs](../auth.md#identities-and-dids). |

- **Request body**: none.
- **Response**: `data` is an [IdentityInfo](#identityinfo).
- **Errors**: the [common mapping](#error-mapping). `atomic identity lookup-key` turns a 404 into "No identity registered for public key … on that server" ([`lookup_key.rs#L140-L151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/lookup_key.rs#L140-L151)).
- **Callers**:
  - `atomic identity lookup-key`, on an apex client ([`lookup_key.rs#L97-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/lookup_key.rs#L97-L100)). It uppercases the input, checks that it decodes to a key, and refuses a `did:atomic:` value because a DID is a fingerprint of the key, not the key ([`lookup_key.rs#L116-L137`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/lookup_key.rs#L116-L137)).
  - Signer-key discovery in libatomic, on an `{origin}` client that reuses the remote's `Authorization` and `atomic-delegation` headers and is built only when the remote has a bearer token ([`signer_refresh.rs#L10-L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L10-L21)). It passes each unresolved attestation signer's **DID** as the key ([`signer_refresh.rs#L26-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L26-L34)), and its test expects the request line `GET /identities/by-key/{did}` with the bearer token ([`signer_refresh.rs#L31-L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/signer_refresh.rs#L31-L35)). It keeps a returned key only when the key matches the DID, and treats an error, a timeout or a missing key as a cache miss ([`signer_refresh.rs#L35-L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L35-L47)). The daemon runs it after a pull and on a knowledge-graph reindex ([`services_sync_remote.rs#L1189-L1191`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1189-L1191), [`services_sync_remote.rs#L1468-L1473`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1468-L1473), [`services_agent.rs#L4341-L4357`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L4341-L4357)).

### `resolve_identity_by_did`

`GET {base}/identities/by-did/{did}`, with the DID percent-encoded ([`storage.rs#L553-L575`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L553-L575)). The method's documentation says the endpoint is authenticated, unlike `by-key` ([`storage.rs#L561-L563`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L561-L563)). Nothing in the repository calls this method at this commit.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `did` | string | Required | A DID, percent-encoded. The method's documentation describes `did:atomic:` ([`storage.rs#L553-L557`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L553-L557)), and the binding check below also accepts a `did:key:` ([`did.rs#L54-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-canonical/src/did.rs#L54-L58)). Which forms the server accepts is not in the source. See [Identities and DIDs](../auth.md#identities-and-dids). |

- **Request body**: none.
- **Response**: `data` is an [IdentityInfo](#identityinfo). The client then requires `publicKey` to be present, to decode as a base32 key, and to match the requested DID, either as its `did:atomic` fingerprint or its `did:key` form; anything else is an error, not a success ([`storage.rs#L578-L598`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L578-L598), [`did.rs#L54-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-canonical/src/did.rs#L54-L58)). Tests cover a matching key, a stranger's key, a missing key and an unparseable key ([`storage.rs#L681-L706`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L681-L706)).
- **Errors**: the [common mapping](#error-mapping), plus `RemoteError::Other` for a failed binding check.

## Agents

### `enroll_agent`

`POST {apex}/identities/agents` with an [EnrollAgentRequest](#enrollagentrequest) body ([`storage.rs#L439-L450`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L439-L450)). The caller must be the human delegator named in the certificate; the method's documentation says the server checks the certificate's proof against the caller's registered key ([`storage.rs#L441-L444`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L441-L444)).

- **Parameters**: none.
- **Response**: `data` is an [AgentIdentityInfo](#agentidentityinfo). A CLI test answers with this body ([`json_output_contract_test.rs#L402-L409`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/json_output_contract_test.rs#L402-L409)):

  ```json
  {"success": true, "data": {"id": "<uuid>", "name": "agent", "did": "did:atomic:test", "parentIdentityId": "<uuid>", "softwareAgent": null, "status": "active", "createdAt": "2026-10-27T00:00:00Z"}}
  ```

- **Errors**: the [common mapping](#error-mapping). `atomic identity agent create` treats an error whose text contains `404` or `not found` as "this server does not support agent identities yet" and warns; any other error becomes `Failed to enroll agent: …` ([`create.rs#L296-L310`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/create.rs#L296-L310), [`create.rs#L416-L419`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/create.rs#L416-L419)). Either way the command reports the failure, keeps the agent key and certificate it has already stored, and still succeeds; it binds the agent in the config only after a successful enrollment ([`create.rs#L198-L230`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/create.rs#L198-L230)). A test answers 404 and 500 with `{"error": "enrollment unavailable"}` and expects the command to succeed with the failure reported on stderr ([`json_output_contract_test.rs#L440-L476`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/json_output_contract_test.rs#L440-L476)).
- **Caller**: `atomic identity agent create`, on an apex client authenticated as the delegator ([`create.rs#L278-L294`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/create.rs#L278-L294)). Its test checks the request line `POST /identities/agents` and that `certificate.proof` is an object ([`json_output_contract_test.rs#L434-L437`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/json_output_contract_test.rs#L434-L437)).

### `list_agents`

`GET {apex}/identities/agents` ([`storage.rs#L452-L455`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L452-L455)). Lists the agents enrolled under the authenticated identity.

- **Parameters**: none.
- **Request body**: none.
- **Response**: `data` is an array of [AgentIdentityInfo](#agentidentityinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic identity agent revoke`, which finds the agent by its `did`, not its name, before bumping its epoch ([`revoke.rs#L213-L215`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/revoke.rs#L213-L215), [`revoke.rs#L221-L232`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/revoke.rs#L221-L232)).

### `get_agent`

`GET {apex}/identities/agents/{agent_id}` ([`storage.rs#L457-L460`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L457-L460)). Nothing in the repository calls this method at this commit.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `agent_id` | string | Required | The agent's identity UUID, inserted without escaping |

- **Request body**: none.
- **Response**: `data` is an [AgentIdentityInfo](#agentidentityinfo).
- **Errors**: the [common mapping](#error-mapping).

### `retire_agent`

`DELETE {apex}/identities/agents/{agent_id}` ([`storage.rs#L462-L469`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L462-L469)). The method's documentation says the server revokes the agent's delegations and marks its key unusable, keeping the identity so past changes stay attributable. Nothing in the repository calls this method at this commit.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `agent_id` | string | Required | The agent's identity UUID, inserted without escaping |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: the [common mapping](#error-mapping).

### `set_agent_epoch`

`POST {apex}/identities/agents/{agent_id}/epoch` with no body ([`storage.rs#L497-L506`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L497-L506)). The method's documentation says every grant issued to the agent before now stops being valid, including grants the server has never seen ([`storage.rs#L497-L502`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L497-L502)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `agent_id` | string | Required | The agent's identity UUID, inserted without escaping: the `id` of an [AgentIdentityInfo](#agentidentityinfo) ([`revoke.rs#L241-L242`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/revoke.rs#L241-L242)) |

- **Response**: `data` is an [EpochInfo](#epochinfo).
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic identity agent revoke`, before it deny-lists each known grant with [`revoke_delegation`](#revoke_delegation) ([`revoke.rs#L165-L169`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/revoke.rs#L165-L169), [`revoke.rs#L190-L208`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/revoke.rs#L190-L208)).

### `clear_agent_epoch`

`DELETE {apex}/identities/agents/{agent_id}/epoch` ([`storage.rs#L508-L512`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L508-L512)). The method's documentation says this brings unexpired grants back. Nothing in the repository calls this method at this commit.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `agent_id` | string | Required | The agent's identity UUID, inserted without escaping |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: the [common mapping](#error-mapping).

## Delegations

The certificate and revocation documents these requests carry are described in [Delegation](../auth.md#delegation). The CLI drives these requests from `atomic identity grant`, which is also reachable as `atomic identity delegation` ([`mod.rs#L279-L280`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/mod.rs#L279-L280)); its `publish` subcommand is also reachable as `push` ([`delegation.rs#L56-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L56-L62)).

### `push_delegation`

`POST {apex}/delegations` with a [PushDelegationRequest](#pushdelegationrequest) body ([`storage.rs#L471-L477`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L471-L477)). Uploads a new or renewed certificate.

- **Parameters**: none.
- **Response**: `data` is a [DelegationInfo](#delegationinfo).
- **Errors**: the [common mapping](#error-mapping). Both callers report a failure as a warning and carry on ([`delegation.rs#L219-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L219-L225), [`renew.rs#L211-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/renew.rs#L211-L225)).
- **Callers**: `atomic identity grant publish`, once per locally held certificate (or only the one it is given) that passes the ingest check, verifies, is unexpired and has its delegator on this machine, each on an apex client authenticated as that delegator ([`delegation.rs#L179-L184`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L179-L184), [`delegation.rs#L192-L219`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L192-L219)), and `atomic identity agent renew` to publish the renewed grant ([`renew.rs#L198-L211`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/renew.rs#L198-L211)).

### `list_delegations`

`GET {apex}/delegations` ([`storage.rs#L479-L482`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L479-L482)). Lists the delegations the authenticated identity issued. Nothing in the repository calls this method at this commit; `atomic identity grant list` reads the local identity store instead ([`delegation.rs#L253-L255`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L253-L255)).

- **Parameters**: none.
- **Request body**: none.
- **Response**: `data` is an array of [DelegationInfo](#delegationinfo).
- **Errors**: the [common mapping](#error-mapping).

### `revoke_delegation`

`POST {apex}/delegations/{delegation_id}/revoke` with a [RevokeDelegationRequest](#revokedelegationrequest) body ([`storage.rs#L484-L495`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L484-L495)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `delegation_id` | string | Required | The delegation URN, `urn:atomic:delegation:<base32>` ([`storage_types.rs#L171-L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L171-L172), [`delegation.rs#L541-L543`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L541-L543)), percent-encoded, so its colons travel as `%3A` |

- **Response**: `data` is the revoked [DelegationInfo](#delegationinfo).
- **Errors**: the [common mapping](#error-mapping). The CLI records the revocation locally before the request, sends no request at all with `--local`, and only warns when the server refuses it ([`delegation.rs#L522-L533`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L522-L533), [`delegation.rs#L545-L552`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L545-L552)).
- **Callers**: `atomic identity grant revoke`, authenticated as the delegator ([`delegation.rs#L535-L543`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L535-L543)), and `atomic identity agent revoke` for each grant it revoked ([`revoke.rs#L200-L208`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/agent/revoke.rs#L200-L208)).

### `set_delegator_epoch`

`POST {apex}/delegations/epoch` with no body ([`storage.rs#L514-L521`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L514-L521)). The method's documentation says this invalidates every grant the caller has issued, to every agent.

- **Parameters**: none.
- **Response**: `data` is an [EpochInfo](#epochinfo). The CLI prints `delegationsValidFrom` when present ([`delegation.rs#L586-L592`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L586-L592)).
- **Errors**: the [common mapping](#error-mapping).
- **Caller**: `atomic identity grant revoke --all-mine`, which refuses `--local` because only the server can apply it, and authenticates as the default identity ([`delegation.rs#L487-L488`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L487-L488), [`delegation.rs#L564-L571`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L564-L571), [`delegation.rs#L573-L579`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L573-L579)).

### `delegation_status`

`GET {apex}/delegations/{delegation_id}/status` ([`storage.rs#L523-L536`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L523-L536)). The method's documentation says the server answers without authentication, so that anyone auditing a change's `delegation_id` can check it.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `delegation_id` | string | Required | The delegation URN, percent-encoded |

- **Request body**: none.
- **Response**: `data` is a [DelegationStatusInfo](#delegationstatusinfo).
- **Errors**: the [common mapping](#error-mapping). The CLI prints the error as "not checked" rather than failing ([`delegation.rs#L427-L433`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L427-L433)).
- **Caller**: `atomic identity grant verify`'s revocation check, on an apex client authenticated as the delegator, and only when the delegator is on this machine ([`delegation.rs#L416-L427`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/delegation.rs#L416-L427)).

## Organizations

These functions are in `atomic-teams` and send their requests through a `StorageClient` ([`lib.rs#L1-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/lib.rs#L1-L6)). Errors go through [`map_remote_error`](#error-mapping).

### `create_org`

`POST {apex}/orgs` with a [CreateOrgRequest](#createorgrequest) body ([`org.rs#L27-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L27-L45)). The function's documentation says the client must target the apex ([`org.rs#L17-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L17-L19)). The client derives the slug from the name ([`org.rs#L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L33)): it lowercases, keeps ASCII letters and digits, turns each run of other characters into one `-`, and drops leading and trailing dashes ([`org.rs#L133-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L133-L152)).

```json
{"slug": "acme-corp", "name": "Acme Corp", "email": "admin@acme.com"}
```

The example is from a test ([`org.rs#L180-L191`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L180-L191)).

- **Parameters**: none.
- **Response**: `data` is an [OrgInfo](#orginfo).
- **Errors**: `map_remote_error` with context `org {name}`; the documentation names `AlreadyExists` for a taken slug ([`org.rs#L23-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L23-L26)).
- **Caller**: `atomic org create`, on an apex client ([`create.rs#L82-L87`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/create.rs#L82-L87)).

### `list_my_orgs`

`GET {apex}/orgs` ([`org.rs#L63-L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L63-L80)). Lists every org the caller belongs to, with the caller's role.

- **Parameters**: none.
- **Request body**: none.
- **Response**: `data` is an array of [MyOrgInfo](#myorginfo).
- **Errors**: `map_remote_error` with context `list my orgs`.
- **Caller**: `atomic org list`, on an apex client ([`list.rs#L78-L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/list.rs#L78-L80)).

### `get_org`

`GET {org_base}/orgs/{slug}` ([`org.rs#L53-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L53-L61)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Org slug, inserted without escaping. May differ from the org in the base URL. |

- **Request body**: none.
- **Response**: `data` is an [OrgInfo](#orginfo).
- **Errors**: `map_remote_error` with context `org {slug}`, so a 404 is `OrgNotFound` (see the slug caveat under [Error mapping](#error-mapping)).
- **Callers**: `atomic org show`, with the slug argument or else the client's own org ([`show.rs#L88-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/show.rs#L88-L93)), `atomic org set` to verify a slug on a client built for that org ([`set.rs#L156-L157`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/set.rs#L156-L157)), and `atomic org delete` ([`delete.rs#L86-L91`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/delete.rs#L86-L91)).

### `update_org`

`PUT {org_base}/orgs/{slug}` with an [UpdateOrgRequest](#updateorgrequest) body ([`org.rs#L82-L107`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L82-L107)). Fields that are not set are omitted, and the documentation says the server leaves them unchanged ([`org.rs#L84-L85`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L84-L85)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Current org slug |

- **Response**: `data` is the updated [OrgInfo](#orginfo).
- **Errors**: `map_remote_error` with context `org {slug}`.
- **Caller**: `atomic org update` ([`update.rs#L101-L106`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/update.rs#L101-L106)).

### `delete_org`

`DELETE {org_base}/orgs/{slug}` ([`org.rs#L109-L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L109-L127)). The documentation says the caller must be an owner ([`org.rs#L111-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L111-L117)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Org slug |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: `map_remote_error` with context `org {slug}`.
- **Caller**: `atomic org delete` ([`delete.rs#L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/delete.rs#L116)).

### `upgrade_org`

`POST {org_base}/orgs/{slug}/upgrade` with no body ([`org.rs#L154-L166`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L154-L166)). The documentation, which sits above `slugify` rather than this function, says the server decides the plan transition ([`org.rs#L129-L133`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L129-L133)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Org slug |

- **Response**: `data` is the updated [OrgInfo](#orginfo).
- **Errors**: `map_remote_error` with context `org {slug}`.
- **Caller**: `atomic org upgrade` ([`upgrade.rs#L87-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/upgrade.rs#L87-L92)).

### `list_members`

`GET {org_base}/orgs/{org_slug}/members` ([`member.rs#L13-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L13-L28)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |

- **Request body**: none.
- **Response**: `data` is an array of [OrgMemberInfo](#orgmemberinfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.
- **Caller**: `atomic org member list` ([`member.rs#L167-L170`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/member.rs#L167-L170)).

### `add_member`

`POST {org_base}/orgs/{org_slug}/members` with an [AddMemberRequest](#addmemberrequest) body ([`member.rs#L30-L50`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L30-L50)).

```json
{"identity_id": "00000000-0000-0000-0000-000000000000", "role": "member"}
```

The field names and role spelling are from a test ([`member.rs#L126-L135`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L126-L135)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |

- **Response**: `data` is the new [OrgMemberInfo](#orgmemberinfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.
- **Caller**: `atomic org member add`, after resolving the identity argument with [`resolve_identity_by_email`](#resolve_identity_by_email) or [`resolve_identity_by_name`](#resolve_identity_by_name) ([`member.rs#L303-L309`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/member.rs#L303-L309)).

### `get_member`

`GET {org_base}/orgs/{org_slug}/members/{identity_id}` ([`member.rs#L52-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L52-L71)). Nothing in the repository calls this function at this commit.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |
| `identity_id` | UUID | Required | Member's identity UUID, hyphenated |

- **Request body**: none.
- **Response**: `data` is an [OrgMemberInfo](#orgmemberinfo).
- **Errors**: `map_remote_error` with context `member identity {id} in org {slug}`, so a 404 is `MemberNotFound`, or `TeamNotFound` if the org slug contains `team`.

### `update_member_role`

`PUT {org_base}/orgs/{org_slug}/members/{identity_id}` with an [UpdateMemberRoleRequest](#updatememberrolerequest) body ([`member.rs#L73-L95`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L73-L95)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |
| `identity_id` | UUID | Required | Member's identity UUID, hyphenated |

- **Response**: `data` is the updated [OrgMemberInfo](#orgmemberinfo).
- **Errors**: `map_remote_error` with context `member identity {id} in org {slug}`.
- **Caller**: `atomic org member update` ([`member.rs#L380-L386`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/member.rs#L380-L386)).

### `remove_member`

`DELETE {org_base}/orgs/{org_slug}/members/{identity_id}` ([`member.rs#L97-L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L97-L120)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |
| `identity_id` | UUID | Required | Member's identity UUID, hyphenated |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: `map_remote_error` with context `member identity {id} in org {slug}`. The documentation says removing the last owner fails with `LastOwner`, mapped from a 409 ([`member.rs#L99-L101`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L99-L101)), but `map_remote_error` maps every 409 to `AlreadyExists` ([`error.rs#L73-L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/error.rs#L73-L75)); see [Open questions](#open-questions).
- **Caller**: `atomic org member remove` ([`member.rs#L451-L477`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/org/member.rs#L451-L477)).

### `list_domains`

`GET {org_base}/orgs/{org_slug}/domains` ([`domain.rs#L13-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L13-L31)). Nothing in the repository calls the domain functions at this commit, so the base is assumed from the path.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |

- **Request body**: none.
- **Response**: `data` is an array of [DomainAliasInfo](#domainaliasinfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.

### `claim_domain`

`POST {org_base}/orgs/{org_slug}/domains` with a [ClaimDomainRequest](#claimdomainrequest) body ([`domain.rs#L33-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L33-L58)). The documentation says the domain starts in `pending` status and is verified with [`verify_domain`](#verify_domain) after the DNS records are in place ([`domain.rs#L35-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L35-L36)).

```json
{"domain": "eng.acme.com"}
```

The example value is from a test ([`domain.rs#L117-L125`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L117-L125)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |

- **Response**: `data` is the new [DomainAliasInfo](#domainaliasinfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.

### `verify_domain`

`POST {org_base}/orgs/{org_slug}/domains/{domain_id}/verify` with no body ([`domain.rs#L60-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L60-L82)). The documentation says the server checks the DNS records and moves the domain from `pending` to `verified` ([`domain.rs#L62-L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L62-L63)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |
| `domain_id` | UUID | Required | The domain alias's `id`, hyphenated |

- **Response**: `data` is the updated [DomainAliasInfo](#domainaliasinfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.

### `revoke_domain`

`DELETE {org_base}/orgs/{org_slug}/domains/{domain_id}` ([`domain.rs#L84-L105`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/domain.rs#L84-L105)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |
| `domain_id` | UUID | Required | The domain alias's `id`, hyphenated |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: `map_remote_error` with context `org {org_slug}`.

## Teams

Team routes carry no org slug: the org is the one in the client's base URL. The documentation says two orgs can each have a team with the same slug, so that `/teams/engineering` names a different team on `https://delta.<domain>` than on `https://atomic.<domain>` ([`team.rs#L16-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L16-L19)); a test shows only that the two full URLs differ ([`team.rs#L183-L200`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L183-L200)). Each function takes an `org_slug` argument that is never put in the path: it is used in a debug assertion that it equals the client's org and, for some functions, in the error context ([`team.rs#L20-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L20-L26), [`team_member.rs#L32-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L32-L37)).

### `list_teams`

`GET {org_base}/teams` ([`team.rs#L11-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L11-L26)). The documentation says secret teams are returned only to members and org admins ([`team.rs#L13-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L13-L14)).

- **Parameters**: none.
- **Request body**: none.
- **Response**: `data` is an array of [TeamInfo](#teaminfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.
- **Callers**: `atomic team list` ([`list.rs#L76-L79`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/list.rs#L76-L79)), and `atomic workspace grant` to turn `--team <slug>` into a team UUID ([`grant.rs#L294-L306`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/grant.rs#L294-L306)).

### `create_team`

`POST {org_base}/teams` with a [CreateTeamRequest](#createteamrequest) body ([`team.rs#L28-L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L28-L51)). The documentation says the server derives the slug from `name` and applies its own default visibility when none is sent ([`team.rs#L30-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L30-L31)).

```json
{"name": "Backend", "description": "Backend engineering", "visibility": "secret"}
```

The example is from a test ([`team.rs#L124-L135`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L124-L135)).

- **Parameters**: none.
- **Response**: `data` is the new [TeamInfo](#teaminfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.
- **Caller**: `atomic team create` ([`create.rs#L120-L123`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/create.rs#L120-L123)).

### `get_team`

`GET {org_base}/teams/{team_slug}` ([`team.rs#L53-L69`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L53-L69)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `team_slug` | string | Required | Team slug within the base URL's org |

- **Request body**: none.
- **Response**: `data` is a [TeamInfo](#teaminfo).
- **Errors**: `map_remote_error` with context `team {team_slug}`, so a 404 is `TeamNotFound`.
- **Callers**: `atomic team show` ([`show.rs#L89-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/show.rs#L89-L92)) and `atomic team delete` ([`delete.rs#L88-L94`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/delete.rs#L88-L94)).

### `update_team`

`PUT {org_base}/teams/{team_slug}` with an [UpdateTeamRequest](#updateteamrequest) body ([`team.rs#L71-L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L71-L96)). Fields that are not set are omitted and, according to the documentation, left unchanged ([`team.rs#L73-L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L73-L74)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `team_slug` | string | Required | Team slug within the base URL's org |

- **Response**: `data` is the updated [TeamInfo](#teaminfo).
- **Errors**: `map_remote_error` with context `team {team_slug}`.
- **Caller**: `atomic team update` ([`update.rs#L134-L137`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/update.rs#L134-L137)).

### `delete_team`

`DELETE {org_base}/teams/{team_slug}` ([`team.rs#L98-L114`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L98-L114)). The documentation says the team's memberships and team-scoped grants go with it ([`team.rs#L100-L102`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L100-L102)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `team_slug` | string | Required | Team slug within the base URL's org |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: `map_remote_error` with context `team {team_slug}`.
- **Caller**: `atomic team delete` ([`delete.rs#L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/delete.rs#L119)).

### `list_team_members`

`GET {org_base}/teams/{team_slug}/members` ([`team_member.rs#L14-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L14-L38)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `team_slug` | string | Required | Team slug within the base URL's org |

- **Request body**: none.
- **Response**: `data` is an array of [TeamMemberInfo](#teammemberinfo).
- **Errors**: `map_remote_error` with context `team {org}/{team}`.
- **Caller**: `atomic team member list` ([`member.rs#L171-L175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/member.rs#L171-L175)).

### `add_team_member`

`POST {org_base}/teams/{team_slug}/members` with an [AddTeamMemberRequest](#addteammemberrequest) body ([`team_member.rs#L40-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L40-L64)).

```json
{"identity_id": "00000000-0000-0000-0000-000000000000", "role": "contributor"}
```

The field names and role spelling are from a test ([`team_member.rs#L120-L130`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L120-L130)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `team_slug` | string | Required | Team slug within the base URL's org |

- **Response**: `data` is the new [TeamMemberInfo](#teammemberinfo).
- **Errors**: `map_remote_error` with context `team {org}/{team}`.
- **Caller**: `atomic team member add`, after resolving the identity argument ([`member.rs#L285-L291`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/member.rs#L285-L291)).

### `update_team_member_role`

`PUT {org_base}/teams/{team_slug}/members/{identity_id}` with an [UpdateTeamMemberRoleRequest](#updateteammemberrolerequest) body ([`team_member.rs#L66-L90`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L66-L90)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `team_slug` | string | Required | Team slug within the base URL's org |
| `identity_id` | UUID | Required | Member's identity UUID, hyphenated |

- **Response**: `data` is the updated [TeamMemberInfo](#teammemberinfo).
- **Errors**: `map_remote_error` with context `team member {id} in {org}/{team}`. The documentation names `MemberNotFound` for a non-member ([`team_member.rs#L70-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L70-L71)), but the context contains "team", which `map_remote_error` checks first, so a 404 is `TeamNotFound` ([`error.rs#L61-L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/error.rs#L61-L68)).
- **Caller**: `atomic team member update` ([`member.rs#L375-L381`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/member.rs#L375-L381)).

### `remove_team_member`

`DELETE {org_base}/teams/{team_slug}/members/{identity_id}` ([`team_member.rs#L92-L114`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L92-L114)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `team_slug` | string | Required | Team slug within the base URL's org |
| `identity_id` | UUID | Required | Member's identity UUID, hyphenated |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: as for [`update_team_member_role`](#update_team_member_role): a 404 is `TeamNotFound`, although the documentation names `MemberNotFound` ([`team_member.rs#L96-L99`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L96-L99)).
- **Caller**: `atomic team member remove` ([`member.rs#L478-L484`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/team/member.rs#L478-L484)).

## Grants

A grant binds a user or team to a relation on an org or a workspace ([`grant.rs#L1-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L1-L20)). The add and revoke functions refuse the `everyone` subject before sending anything, because, according to the code's comment, the mutation endpoints require a concrete user or team UUID ([`grant.rs#L29-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L29-L40)). The `{subject_type}` path segment is the subject's lowercase name, `user` or `team` ([`grant.rs#L119-L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L119-L120), [`types.rs#L182-L190`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L182-L190)). The add bodies and the grant responses use snake_case, which comments in the code say matches the server ([`types.rs#L498-L507`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L498-L507), [`types.rs#L716-L732`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L716-L732)).

### `list_org_grants`

`GET {org_base}/orgs/{org_slug}/grants` ([`grant.rs#L46-L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L46-L63)). Nothing in the repository calls the org grant functions at this commit.

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |

- **Request body**: none.
- **Response**: `data` is an array of [GrantInfo](#grantinfo).
- **Errors**: `map_remote_error` with context `org {org_slug}`.

### `add_org_grant`

`POST {org_base}/orgs/{org_slug}/grants` with an [AddGrantRequest](#addgrantrequest) body ([`grant.rs#L65-L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L65-L96)). The module documentation says org grants accept the `owner` relation as well as `read`, `write` and `admin` ([`grant.rs#L15-L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L15-L17)).

```json
{"subject_type": "team", "subject_id": "00000000-0000-0000-0000-000000000000", "relation": "write"}
```

The shape is from a test ([`grant.rs#L219-L230`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L219-L230)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |

- **Response**: `data` is the new [GrantInfo](#grantinfo).
- **Errors**: `TeamsError::InvalidInput` without a request for an `everyone` subject; otherwise `map_remote_error` with context `org {org_slug}`.

### `revoke_org_grant`

`DELETE {org_base}/orgs/{org_slug}/grants/{subject_type}/{subject_id}` ([`grant.rs#L98-L125`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L98-L125)). The documentation says the server removes every relation the subject holds on the org in one call ([`grant.rs#L100-L102`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L100-L102)). A test shows `/orgs/acme/grants/team/{id}` ([`grant.rs#L260-L269`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L260-L269)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `org_slug` | string | Required | Org slug |
| `subject_type` | string | Required | `user` or `team` |
| `subject_id` | UUID | Required | The user's identity UUID or the team's UUID, hyphenated |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: `InvalidInput` for `everyone`; otherwise `map_remote_error` with context `org {org_slug}`.

### `list_workspace_grants`

`GET {org_base}/workspaces/{workspace_slug}/grants` ([`grant.rs#L131-L147`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L131-L147)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug |

- **Request body**: none.
- **Response**: `data` is an array of [WorkspaceGrantInfo](#workspacegrantinfo). A test parses a public workspace's grant with subject type `everyone` and a null `subject_id` ([`types.rs#L757-L778`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L757-L778)).
- **Errors**: `map_remote_error` with context `workspace {slug}`, so a 404 is `OrgNotFound` unless the slug contains `team`, `member` or `identity`.
- **Caller**: `atomic workspace grant list` ([`grant.rs#L102-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/grant.rs#L102-L103)).

### `add_workspace_grant`

`POST {org_base}/workspaces/{workspace_slug}/grants` with an [AddGrantRequest](#addgrantrequest) body ([`grant.rs#L149-L180`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L149-L180)). The module documentation says workspace grants use `read`, `write` and `admin` ([`grant.rs#L9-L10`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L9-L10)), and the CLI's `--permission` refuses `owner` ([`grant.rs#L323-L334`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/grant.rs#L323-L334)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug |

- **Response**: `data` is the new [WorkspaceGrantInfo](#workspacegrantinfo).
- **Errors**: `InvalidInput` for `everyone`; otherwise `map_remote_error` with context `workspace {slug}`.
- **Caller**: `atomic workspace grant add` ([`grant.rs#L192-L198`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/grant.rs#L192-L198)).

### `revoke_workspace_grant`

`DELETE {org_base}/workspaces/{workspace_slug}/grants/{subject_type}/{subject_id}` ([`grant.rs#L182-L208`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L182-L208)). The documentation says the server removes every relation the subject holds on the workspace in one call ([`grant.rs#L184-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L184-L186)). A test shows `/workspaces/my-ws/grants/user/{id}` ([`grant.rs#L271-L280`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L271-L280)).

| Name | Type | Required | Meaning |
|---|---|---|---|
| `workspace_slug` | string | Required | Workspace slug |
| `subject_type` | string | Required | `user` or `team` |
| `subject_id` | UUID | Required | The user's identity UUID or the team's UUID, hyphenated |

- **Request body**: none.
- **Response**: any 2xx; the body is ignored.
- **Errors**: `InvalidInput` for `everyone`; otherwise `map_remote_error` with context `workspace {slug}`.
- **Caller**: `atomic workspace grant revoke` ([`grant.rs#L256-L261`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/workspace/grant.rs#L256-L261)).

## Registration

### `execute`

`POST {server_url}/register`, sent by `Register::execute` in `atomic identity register` ([`register.rs#L183-L196`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L183-L196)). It sends a local identity to the server; the module documentation says the server verifies the signature and creates a tenant whose slug is derived from the identity's username ([`register.rs#L1-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L1-L6)). This request does not go through `StorageClient`: it uses a plain `reqwest::Client::new()`, sends the body with `.json`, and the only header the code sets is `Content-Type: application/json` ([`register.rs#L186-L191`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L186-L191)). It sends no `Authorization` header: authentication is the signature in the body ([`register.rs#L61-L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L61-L63)). `{server_url}` is the command's argument with trailing slashes removed ([`register.rs#L184`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L184), [`register.rs#L493-L503`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L493-L503)); it becomes the profile's apex.

The command refuses agent and delegated identities before it sends anything, because registering creates a tenant ([`register.rs#L138-L153`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L138-L153)).

**Request body.** The body is built with `serde_json::json!` rather than a Rust type ([`register.rs#L174-L181`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L174-L181)).

| Field | Type | Required | Meaning |
|---|---|---|---|
| `username` | string | Required | The identity's name ([`register.rs#L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L164)) |
| `email` | string or null | Always sent; `null` when the identity has no email | The identity's email, an `Option<String>` that `json!` writes as `null` when unset ([`register.rs#L177`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L177), [`identity.rs#L291-L293`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-identity/src/identity.rs#L291-L293)). It is not part of the signed payload, and the source does not say what the server does with it; see [Open questions](#open-questions). |
| `public_key` | string | Required | Base32 Ed25519 public key without padding ([`register.rs#L165`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L165), [`identity.rs#L362-L364`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-identity/src/identity.rs#L362-L364), [`keypair.rs#L52-L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-identity/src/keypair.rs#L52-L54)) |
| `timestamp` | string | Required | The current UTC time in RFC 3339 with whole seconds and a `Z` suffix ([`register.rs#L166-L167`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L166-L167)) |
| `signature` | string | Required | Base32 without padding of the Ed25519 signature over the signing payload ([`register.rs#L169-L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L169-L172)) |

**Signing payload.** `signature` covers a four-line text built from `username`, `public_key` and `timestamp`; `email` is not signed ([`register.rs#L163-L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L163-L172)). The exact bytes, the signature algorithm and the encodings are documented once, under [Registration](../auth.md#registration).

**Response.** The command does not check `X-Atomic-Min-Version` on this response ([`register.rs#L198-L209`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L198-L209)). On a 2xx status the body must be JSON, or the command fails with "Invalid server response" ([`register.rs#L204-L206`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L204-L206)). The fields are read from `data` when the body has one and from the top-level object otherwise, so both the [ApiResponse](#apiresponse) envelope and a bare object work ([`register.rs#L208-L209`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L208-L209)). The keys are snake_case, and each is read only when its value is a JSON string; any other value counts as absent ([`register.rs#L211-L222`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L211-L222), [`register.rs#L470-L473`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L470-L473)):

| Field | Type | Required | Meaning |
|---|---|---|---|
| `tenant_id` | string | Optional | Printed; `(unknown)` when absent ([`register.rs#L211-L214`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L211-L214)) |
| `slug` | string | Optional | The tenant's org slug; defaults to the username ([`register.rs#L215-L218`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L215-L218)). The command notes when it differs from the username ([`register.rs#L286-L295`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L286-L295)). |
| `base_url` | string | Optional | Printed; `(unknown)` when absent ([`register.rs#L219-L222`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L219-L222)) |
| `mode` | string | Optional | `single` marks a single-tenant server. Any other value, or none, means multi-tenant ([`register.rs#L464-L474`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L464-L474), [`register.rs#L505-L529`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L505-L529)). |
| `role` | string | Optional | The org role the identity received on a single-tenant server ([`register.rs#L470-L473`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L470-L473), [`register.rs#L279-L284`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L279-L284)) |

The command then saves the server URL, `default_org = slug`, `single_tenant` and the identity's name in the server profile ([`register.rs#L382-L391`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L382-L391), [`register.rs#L413-L425`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L413-L425)). Later management requests therefore go to `{scheme}://{slug}.{host}`, or to the server URL itself on a single-tenant server; see [Base URLs](#base-urls).

**Errors.** On any other status, the message is the body's top-level `message`, else `error.message`, else `Server returned {status}` ([`register.rs#L311-L328`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L311-L328)). A 409 also prints a hint that the public key is already registered ([`register.rs#L332-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L332-L341)). The command fails with `CliError::RemoteError` carrying the message and URL ([`register.rs#L343-L346`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L343-L346)). A connection failure fails with "Failed to connect to server" ([`register.rs#L193-L196`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L193-L196)), and a body that cannot be read, on any status, with "Failed to read response body" ([`register.rs#L198-L201`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L198-L201)).

## Out of scope

- `atomic update` checks for a release with `GET https://api.github.com/repos/atomicdotdev/atomic/releases/latest`, a GitHub API request, not an Atomic server request ([`update.rs#L265-L280`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/update.rs#L265-L280)). The installer URL on `atomic.storage` is only printed in upgrade instructions, never fetched ([`update.rs#L319`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/update.rs#L319), [`update.rs#L384`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/update.rs#L384)).
- atomic-repository's AI features call Anthropic, OpenAI and Voyage AI endpoints chosen from environment variables ([`mod.rs#L104-L129`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/ai/mod.rs#L104-L129), [`mod.rs#L205-L207`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/ai/mod.rs#L205-L207), [`mod.rs#L240-L242`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/ai/mod.rs#L240-L242), [`tools.rs#L235-L240`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/ai/tools.rs#L235-L240)).
- The project-identity requests that libatomic's `identity_transport` makes after a clone or pull and before a push go through `HttpRemote` on the project URL, so they belong to the project protocol ([`identity_transport.rs#L1-L8`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/identity_transport.rs#L1-L8)). So does `atomic repo identity adopt` ([`repo.rs#L100-L102`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/repo.rs#L100-L102), [`repo.rs#L121-L123`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/repo.rs#L121-L123)). See [project.md](project.md).

## Types

These are the JSON bodies of the management API, from `atomic-remote/src/storage_types.rs` and `atomic-teams/src/types.rs`. The JSON names follow each type's serde attributes. The "Required" column for a response type says what the client needs in order to deserialize it; for a request type, it says what the client sends.

Conventions that apply to every table:

- **UUIDs** are strings in the hyphenated form, as in the tests' fixtures ([`storage_types.rs#L504-L514`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L504-L514)). The `uuid` crate is 1.28.0 ([`Cargo.lock#L5006-L5008`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L5006-L5008)).
- **Timestamps** (`DateTime<Utc>`) are RFC 3339 strings such as `2025-01-01T00:00:00Z`, as in the same fixtures. The `chrono` crate is 0.4.45 ([`Cargo.lock#L774-L776`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L774-L776)).
- **Absent fields.** serde, which is external (serde 1.0.229, [`Cargo.lock#L3896-L3897`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L3896-L3897)), reads a missing `Option` field as `None` even without `#[serde(default)]`, so every field marked Optional may be absent or `null`. A field marked Required must be present. A test shows `{"success":true}` deserializing with every optional envelope field empty ([`storage_types.rs#L382-L392`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L382-L392)).
- **Aliases.** Where a field lists "also accepted", `#[serde(alias)]` lets the client read the snake_case spelling too. The client always writes the first spelling.
- **Unknown keys.** No type in either file uses `#[serde(deny_unknown_fields)]` ([`storage_types.rs#L14-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L14-L24), [`types.rs#L209-L231`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L209-L231)), so serde ignores keys the client does not know.

### ApiResponse

`ApiResponse<T>`, the envelope around every response body except a successful `DELETE`. camelCase ([`storage_types.rs#L10-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L10-L24)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `success` | boolean | Required | Whether the request succeeded |
| `data` | `T` | Optional | The payload. The client requires it when `success` is true ([`storage.rs#L275-L277`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L275-L277)). |
| `error` | [ApiError](#apierror) | Optional | Present on failure |
| `metadata` | [ResponseMetadata](#responsemetadata) | Optional | Paging information. The client never reads it. |

### ApiError

The error payload inside an [ApiResponse](#apiresponse), and also a bare error body on some endpoints ([`storage_types.rs#L26-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L26-L34), [`storage.rs#L288-L292`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L288-L292)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `code` | string | Required | A machine-readable code such as `NOT_FOUND` or `UNAUTHORIZED` ([`storage.rs#L649`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L649), [`storage.rs#L658-L659`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L658-L659)) |
| `message` | string | Required | A human-readable reason. On an error status it becomes the `HttpError` message. |
| `details` | any JSON | Optional | Never read by the client |

### ResponseMetadata

Paging information on list responses. camelCase ([`storage_types.rs#L36-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L36-L46)). A test shows the camelCase keys ([`storage_types.rs#L362-L380`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L362-L380)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `page` | uint32 | Required | Page number |
| `perPage` | uint32 | Required | Page size; also accepted: `per_page` |
| `total` | uint64 | Required | Total number of items |
| `totalPages` | uint32 | Required | Number of pages; also accepted: `total_pages` |

### Visibility

A string enum for workspaces and projects: `"public"` or `"private"` ([`storage_types.rs#L48-L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L48-L54), [`storage_types.rs#L310-L323`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L310-L323)). serde matches these spellings exactly, while the `FromStr` implementation that CLI flags go through lowercases its input first ([`storage_types.rs#L65-L77`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L65-L77)).

### WorkspaceInfo

A workspace. camelCase ([`storage_types.rs#L83-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L83-L100)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | Workspace ID |
| `tenantId` | UUID | Required | The owning tenant; also accepted: `tenant_id` |
| `name` | string | Required | Display name |
| `slug` | string | Required | The slug used in workspace paths |
| `description` | string | Optional | Description |
| `ownerId` | UUID | Required | Owner's identity; also accepted: `owner_id` |
| `visibility` | [Visibility](#visibility) | Required | `public` or `private` |
| `createdAt` | timestamp | Required | Also accepted: `created_at` |
| `updatedAt` | timestamp | Required | Also accepted: `updated_at` |

A test parses this body ([`storage_types.rs#L503-L514`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L503-L514)):

```json
{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "workspace", "slug": "workspace", "description": null, "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "public", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}
```

### ProjectInfo

A project. camelCase ([`storage_types.rs#L102-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L102-L119)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | Project ID |
| `workspaceId` | UUID | Required | The owning workspace; also accepted: `workspace_id` |
| `name` | string | Required | Display name |
| `slug` | string | Required | The slug used in project paths and the project URL |
| `description` | string | Optional | Description |
| `defaultView` | string | Required | The default view's name; also accepted: `default_view` |
| `visibility` | [Visibility](#visibility) | Required | `public` or `private` |
| `createdAt` | timestamp | Required | Also accepted: `created_at` |
| `updatedAt` | timestamp | Required | Also accepted: `updated_at` |

A test parses this body ([`storage_types.rs#L529-L540`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L529-L540)):

```json
{"id": "00000000-0000-0000-0000-000000000010", "workspaceId": "00000000-0000-0000-0000-000000000020", "name": "project", "slug": "project", "description": "desc", "defaultView": "main", "visibility": "private", "createdAt": "2025-06-01T12:00:00Z", "updatedAt": "2025-06-01T12:00:00Z"}
```

### IdentityInfo

A registered identity, returned by the resolve and lookup endpoints. camelCase ([`storage_types.rs#L121-L136`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L121-L136)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | The server's identity ID |
| `name` | string | Required | Display name |
| `status` | string | Required | Lifecycle status, such as `active` ([`signer_refresh.rs#L36-L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/signer_refresh.rs#L36-L39)) |
| `createdAt` | timestamp | Required | No snake_case alias |
| `publicKey` | string | Optional | Base32 Ed25519 public key, 52 characters without padding. The field's documentation says older servers omit it. Also accepted: `public_key`. |

### AgentIdentityInfo

An agent identity enrolled under a human. camelCase ([`storage_types.rs#L138-L165`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L138-L165)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | The agent's identity ID, used in `/identities/agents/{agent_id}` paths |
| `name` | string | Required | Display name |
| `did` | string | Required | `did:atomic:…` for the agent's key |
| `parentIdentityId` | UUID | Required | The human the agent acts for |
| `softwareAgent` | string | Optional | A software label such as `urn:atomic:agent:claude-code` |
| `status` | string | Required | Lifecycle status |
| `createdAt` | timestamp | Required | |
| `delegationsValidFrom` | timestamp | Optional | The agent's epoch: grants issued before it are rejected |
| `publishedDelegations` | array of [DelegationInfo](#delegationinfo) | Optional, defaults to `[]` | Grants a client chose to publish, newest first. The field's documentation calls this advisory and usually incomplete. |

### DelegationInfo

A delegation certificate as the server holds it. camelCase ([`storage_types.rs#L167-L183`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L167-L183)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | Required | `urn:atomic:delegation:<base32>` |
| `delegatorIdentityId` | UUID | Required | The issuing human |
| `delegateIdentityId` | UUID | Required | The agent |
| `status` | string | Required | `active`, `expired`, `revoked` or `superseded`, according to the field's documentation |
| `issuedAt` | timestamp | Required | |
| `expiresAt` | timestamp | Optional | |
| `certificate` | any JSON | Optional | The signed certificate, verbatim, so a client can verify it again, according to the field's documentation. Omitted when serialized without one. |

### EpochInfo

The result of setting an epoch. camelCase ([`storage_types.rs#L185-L193`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L185-L193)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `identityId` | UUID | Required | The agent or delegator whose epoch changed |
| `delegationsValidFrom` | timestamp | Optional | The new epoch. Null means the epoch was cleared. |

### DelegationStatusInfo

The public status of a delegation. camelCase ([`storage_types.rs#L195-L209`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L195-L209)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | Required | The delegation ID |
| `revoked` | boolean | Required | Whether the grant is on the server's deny-list. The field's documentation says an unknown ID answers the same as a live one. |

### CreateWorkspaceRequest

The body of [`create_workspace`](#create_workspace). camelCase, though every field is one word ([`storage_types.rs#L215-L223`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L215-L223)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Required | Workspace name |
| `description` | string | Optional, omitted when unset ([`storage_types.rs#L410-L420`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L410-L420)) | Description |
| `visibility` | [Visibility](#visibility) | Required | `public` or `private` |

### UpdateWorkspaceRequest

The body of [`update_workspace`](#update_workspace). Every field is omitted when unset ([`storage_types.rs#L225-L238`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L225-L238)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Optional | New name |
| `description` | string | Optional | New description |
| `visibility` | [Visibility](#visibility) | Optional | New visibility |

### CreateProjectRequest

The body of [`create_project`](#create_project). **snake_case** ([`storage_types.rs#L240-L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L240-L251)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Required | Project name |
| `description` | string | Optional, omitted when unset | Description |
| `default_view` | string | Required | The default view's name |
| `kind` | string | Optional, omitted when unset ([`storage_types.rs#L462-L474`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L462-L474)) | A language hint. The CLI documents `rust`, `python`, `node`, `go`, `java`, `ruby` and `zig` ([`create.rs#L83-L87`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/create.rs#L83-L87)). |
| `visibility` | [Visibility](#visibility) | Required | `public` or `private` |

### UpdateProjectRequest

The body of [`update_project`](#update_project). **snake_case**, and every field is omitted when unset ([`storage_types.rs#L253-L268`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L253-L268)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Optional | New name |
| `description` | string | Optional | New description |
| `default_view` | string | Optional | New default view |
| `visibility` | [Visibility](#visibility) | Optional | New visibility |

### EnrollAgentRequest

The body of [`enroll_agent`](#enroll_agent). camelCase ([`storage_types.rs#L553-L569`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L553-L569)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Required | The agent identity's name |
| `email` | string or null | Always sent; `null` when unset | The field has no `skip_serializing_if`, so serde writes `null` |
| `publicKey` | string | Required | The agent's base32 Ed25519 public key |
| `certificate` | JSON object | Required | The signed `AgentDelegation` document. See [Delegation](../auth.md#delegation). The type's documentation says the server checks that `publicKey` and the certificate agree. |

### PushDelegationRequest

The body of [`push_delegation`](#push_delegation) ([`storage_types.rs#L571-L577`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L571-L577)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `certificate` | JSON object | Required | The signed `AgentDelegation` document |

### RevokeDelegationRequest

The body of [`revoke_delegation`](#revoke_delegation) ([`storage_types.rs#L579-L585`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L579-L585)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `revocation` | JSON object | Required | The signed `DelegationRevocation` document. See [Delegation](../auth.md#delegation). |

### OrgRole

A string enum: `"owner"`, `"admin"` or `"member"` ([`types.rs#L17-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L17-L27), [`member.rs#L147-L156`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L147-L156)).

### TeamRole

A string enum, strongest first: `"maintainer"`, `"contributor"`, `"collaborator"`, `"consumer"` ([`types.rs#L52-L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L52-L67)).

### TeamVisibility

A string enum: `"visible"` or `"secret"` ([`types.rs#L97-L105`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L97-L105), [`types.rs#L915-L932`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L915-L932)).

### GrantRelation

A string enum: `"read"`, `"write"`, `"admin"` or `"owner"` ([`types.rs#L128-L140`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L128-L140)).

### GrantSubjectType

A string enum: `"user"`, `"team"` or `"everyone"`. According to the type's documentation, the server returns `everyone` for public grants and the mutation endpoints accept only `user` or `team`; the client refuses to send `everyone` ([`grant.rs#L33-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/grant.rs#L33-L40), [`types.rs#L167-L180`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L167-L180)).

### OrgInfo

An org. camelCase ([`types.rs#L209-L231`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L209-L231)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | Org ID |
| `slug` | string | Required | Org slug, such as `acme` |
| `name` | string | Required | Display name |
| `email` | string | Optional | Contact email |
| `kind` | string | Required | Such as `personal` or `team` |
| `plan` | string | Required | Billing plan, such as `free`, `team` or `enterprise` |
| `createdAt` | timestamp | Required | Also accepted: `created_at` |
| `updatedAt` | timestamp | Required | Also accepted: `updated_at` |

### MyOrgInfo

An org the caller belongs to, with the caller's membership. camelCase ([`types.rs#L233-L268`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L233-L268)). A test shows that snake_case keys are also accepted ([`types.rs#L638-L660`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L638-L660)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id`, `slug`, `name`, `email`, `kind`, `plan`, `createdAt`, `updatedAt` | | | As in [OrgInfo](#orginfo) |
| `role` | string | Required | The caller's role: `owner`, `admin` or `member` |
| `joinedAt` | timestamp | Required | Also accepted: `joined_at` |
| `invitedBy` | UUID | Optional | Who invited the caller; also accepted: `invited_by` |

### OrgMemberInfo

An org membership. camelCase, with snake_case aliases ([`types.rs#L270-L308`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L270-L308)). A test comment says the server sends snake_case ([`types.rs#L843-L861`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L843-L861)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `orgId` | UUID | Required | Also accepted: `org_id` |
| `identityId` | UUID | Required | Also accepted: `identity_id` |
| `role` | [OrgRole](#orgrole) | Required | |
| `joinedAt` | timestamp | Required | Also accepted: `joined_at` |
| `invitedBy` | UUID | Optional | Also accepted: `invited_by` |
| `name` | string | Optional | The member's display name |
| `publicKey` | string | Optional | Base32 Ed25519 key; also accepted: `public_key` |
| `status` | string | Optional | `active`, `suspended` or `deleted` |
| `email` | string | Optional | Preferred email |

The last four carry `#[serde(default)]`; a comment says older servers omit them ([`types.rs#L289-L307`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L289-L307)), and a test parses a body without them ([`types.rs#L820-L841`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L820-L841)).

### TeamInfo

A team. camelCase ([`types.rs#L310-L333`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L310-L333)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | Team ID, used as a grant subject |
| `orgId` | UUID | Required | Also accepted: `org_id` |
| `slug` | string | Required | Team slug, such as `backend-eng` |
| `name` | string | Required | Display name |
| `description` | string | Optional | |
| `visibility` | [TeamVisibility](#teamvisibility) | Required | |
| `createdAt` | timestamp | Required | Also accepted: `created_at` |
| `updatedAt` | timestamp | Required | Also accepted: `updated_at` |

### TeamMemberInfo

A team membership. camelCase ([`types.rs#L335-L353`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L335-L353)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `teamId` | UUID | Required | Also accepted: `team_id` |
| `identityId` | UUID | Required | Also accepted: `identity_id` |
| `role` | [TeamRole](#teamrole) | Required | |
| `addedAt` | timestamp | Required | Also accepted: `added_at` |
| `addedBy` | UUID | Required | Who added the member; also accepted: `added_by` |

### GrantInfo

A grant on an org. **snake_case**, with no renames, matching the server's `OrgGrantResponse` according to the type's documentation ([`types.rs#L355-L372`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L355-L372)). A test parses a snake_case body ([`types.rs#L716-L732`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L716-L732)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | Grant ID |
| `subject_type` | [GrantSubjectType](#grantsubjecttype) | Required | |
| `subject_id` | UUID | Optional | Null for an `everyone` grant |
| `relation` | [GrantRelation](#grantrelation) | Required | |
| `granted_by` | UUID | Optional | |
| `granted_at` | timestamp | Required | |

### WorkspaceGrantInfo

A grant on a workspace. **snake_case**, with no renames, matching the server's `GrantResponse` according to the type's documentation ([`types.rs#L374-L401`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L374-L401)). A test parses a snake_case body ([`types.rs#L734-L755`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L734-L755)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | ID of the access tuple |
| `subject_type` | [GrantSubjectType](#grantsubjecttype) | Required | |
| `subject_id` | UUID | Optional | Null for an `everyone` grant |
| `subject_relation` | string | Optional | The subject's relation within its team, usually null |
| `relation` | [GrantRelation](#grantrelation) | Required | `read`, `write` or `admin`, according to the field's documentation; the type also admits `owner` |
| `object_type` | string | Required | Always `workspace`, according to the documentation |
| `object_id` | UUID | Required | The workspace ID |
| `granted_at` | timestamp | Required | |
| `granted_by` | UUID | Optional | |

### DomainAliasInfo

A domain an org claims. camelCase, with no aliases ([`types.rs#L403-L419`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L403-L419)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `id` | UUID | Required | Used as `{domain_id}` in paths |
| `orgId` | UUID | Required | |
| `domain` | string | Required | Such as `eng.acme.com` |
| `status` | string | Required | Such as `pending` or `verified` |
| `verificationMethod` | string | Required | Such as `dns-txt` or `dns-cname` |
| `verificationToken` | string | Optional | One-time token for DNS verification |

### CreateOrgRequest

The body of [`create_org`](#create_org). camelCase ([`types.rs#L425-L433`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L425-L433)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `slug` | string | Required | Derived from `name` by the client |
| `name` | string | Required | Display name |
| `email` | string | Optional, omitted when unset ([`org.rs#L193-L203`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L193-L203)) | Contact email |

### UpdateOrgRequest

The body of [`update_org`](#update_org). Every field is omitted when unset, so an empty update is `{}` ([`types.rs#L435-L443`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L435-L443), [`org.rs#L216-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/org.rs#L216-L225)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Optional | New name |
| `email` | string | Optional | New contact email |

### AddMemberRequest

The body of [`add_member`](#add_member). **snake_case** ([`types.rs#L445-L451`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L445-L451)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `identity_id` | UUID | Required | The identity to add |
| `role` | [OrgRole](#orgrole) | Required | |

### UpdateMemberRoleRequest

The body of [`update_member_role`](#update_member_role) ([`types.rs#L453-L458`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L453-L458)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `role` | [OrgRole](#orgrole) | Required | The new role |

### CreateTeamRequest

The body of [`create_team`](#create_team). camelCase ([`types.rs#L460-L469`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L460-L469)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Required | Team name |
| `description` | string | Optional, omitted when unset | |
| `visibility` | [TeamVisibility](#teamvisibility) | Optional, omitted when unset ([`team.rs#L137-L148`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L137-L148)) | |

### UpdateTeamRequest

The body of [`update_team`](#update_team). Every field is omitted when unset ([`types.rs#L471-L481`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L471-L481)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | Optional | |
| `description` | string | Optional | |
| `visibility` | [TeamVisibility](#teamvisibility) | Optional | |

### AddTeamMemberRequest

The body of [`add_team_member`](#add_team_member). **snake_case** ([`types.rs#L483-L489`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L483-L489)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `identity_id` | UUID | Required | The identity to add |
| `role` | [TeamRole](#teamrole) | Required | |

### UpdateTeamMemberRoleRequest

The body of [`update_team_member_role`](#update_team_member_role) ([`types.rs#L491-L496`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L491-L496)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `role` | [TeamRole](#teamrole) | Required | The new role |

### AddGrantRequest

The body of [`add_org_grant`](#add_org_grant) and [`add_workspace_grant`](#add_workspace_grant). **snake_case**, with no renames ([`types.rs#L498-L507`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L498-L507)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `subject_type` | [GrantSubjectType](#grantsubjecttype) | Required | `user` or `team`; the client refuses `everyone` |
| `subject_id` | UUID | Required | The user's identity UUID or the team's UUID |
| `relation` | [GrantRelation](#grantrelation) | Required | |

### ClaimDomainRequest

The body of [`claim_domain`](#claim_domain) ([`types.rs#L509-L514`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L509-L514)).

| JSON name | Type | Required | Meaning |
|---|---|---|---|
| `domain` | string | Required | The domain to claim |

## Open questions

- **Success status codes.** The client accepts any 2xx and never checks which ([Response envelope](#response-envelope)). Which codes the server returns, such as 201 for a create or 204 for a delete, is not in the source.
- **Paging.** [ResponseMetadata](#responsemetadata) exists, but no method sends paging parameters and the client never reads `metadata` ([`storage.rs#L311-L314`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L311-L314)). Whether list endpoints page their results, and at what default size, is decided by the server. If they do, the client sees only the first page.
- **Body of a successful `DELETE`.** The client ignores it ([`storage.rs#L231-L238`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L231-L238)), so whether the server sends an envelope, an empty body or a 204 is not knowable from the source.
- **Which bases each endpoint accepts.** The client picks a base by convention, and the same path is called on different hosts: `/identities/resolve?name=` on the org base by the member commands and on the apex by `atomic intent verify` ([`client.rs#L497-L501`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L497-L501), [`verify.rs#L210-L216`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/intent/verify.rs#L210-L216)), and `/identities/by-key/` on the apex by `lookup-key` and on a project remote's origin by signer discovery ([`lookup_key.rs#L97-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/lookup_key.rs#L97-L100), [`signer_refresh.rs#L19-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L19-L20)). Whether the server serves every endpoint on every host is not in the source.
- **`by-key` with a DID.** Signer discovery sends a `did:atomic:` DID to `/identities/by-key/` and expects the key back ([`signer_refresh.rs#L26-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L26-L34), [`signer_refresh.rs#L31-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/signer_refresh.rs#L31-L32)). `lookup-key` refuses a DID for the same endpoint because a DID is not a key ([`lookup_key.rs#L119-L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/lookup_key.rs#L119-L128)), and a separate `/identities/by-did/` endpoint exists with no caller ([`storage.rs#L553-L575`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L553-L575)). Which forms the server's `by-key` endpoint accepts is unknown. Signer discovery's comment expects older servers to miss ([`signer_refresh.rs#L23-L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/signer_refresh.rs#L23-L25)).
- **Unauthenticated endpoints.** `by-key` and `delegation_status` are documented as unauthenticated on the server ([`storage.rs#L543-L545`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L543-L545), [`storage.rs#L525-L526`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L525-L526)), but the client always sends `Authorization` ([`storage.rs#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L113-L117)). Whether the server rejects an invalid or expired token on those endpoints is unknown.
- **Delegation on org-scoped clients.** `delegation_for` says it exists so that the few paths an agent drives, "reading its own project list, say", carry the certificate ([`client.rs#L112-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L112-L117)). But `atomic project list` uses an org-scoped client ([`list.rs#L77-L78`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/list.rs#L77-L78)), and org-scoped clients never send `atomic-delegation` ([`client.rs#L226-L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/client.rs#L226-L228)). The comment and the code disagree; this page documents the code.
- **`TeamsError` documentation and code.** `remove_member` documents `LastOwner` from a 409, but nothing produces `LastOwner` and every 409 becomes `AlreadyExists` ([`member.rs#L99-L101`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/member.rs#L99-L101), [`error.rs#L73-L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/error.rs#L73-L75)). `update_team_member_role` and `remove_team_member` document `MemberNotFound`, but their 404 becomes `TeamNotFound` ([`team_member.rs#L70-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team_member.rs#L70-L71), [`error.rs#L61-L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/error.rs#L61-L68)). A Go client that needs to tell these cases apart has to read the server's status and message itself.
- **`OrgInfo` for non-members.** `MyOrgInfo`'s documentation says `OrgInfo` redacts `email` and `plan` for non-members ([`types.rs#L235-L238`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L235-L238)), but `OrgInfo.plan` is a required string ([`types.rs#L223-L224`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L223-L224)). If the server omits `plan`, the client fails to deserialize; if it sends `null`, it fails too. What the server sends is unknown.
- **Response key casing.** Some response types accept snake_case aliases and some do not. `IdentityInfo.createdAt` has no alias ([`storage_types.rs#L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L128)), and `DomainAliasInfo` has none at all ([`types.rs#L403-L419`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L403-L419)). Tests and comments say the server sends snake_case for org members and grants ([`types.rs#L843-L844`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L843-L844), [`types.rs#L717-L719`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L717-L719)), while another test comment says the server emits camelCase for `MyOrgInfo` and only earlier deployments used snake_case ([`types.rs#L638-L641`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/types.rs#L638-L641)). Which casing each endpoint actually uses is decided by the server; a Go client should accept both spellings wherever the Rust client does.
- **Error codes.** Only `NOT_FOUND` and `UNAUTHORIZED` appear in the source ([`storage.rs#L649`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L649), [`storage.rs#L658-L659`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage.rs#L658-L659)). The full set of `ApiError.code` values is the server's.
- **Slugs.** The server derives team slugs from the name, according to the documentation ([`team.rs#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-teams/src/team.rs#L30)), and the workspace and project create requests carry no slug ([`storage_types.rs#L215-L223`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L215-L223), [`storage_types.rs#L240-L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/storage_types.rs#L240-L251)), so the server picks those too. `atomic project init` creates a missing workspace with `name` set to the slug it then uses in the project URL, which assumes the server keeps such a name as the slug ([`init.rs#L138-L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L138-L142), [`init.rs#L182-L187`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L182-L187)). Registration warns that the tenant slug may be "normalized or collision-resolved" ([`register.rs#L286-L295`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L286-L295)). The rules are not in the source.
- **Registration.** How much clock skew the server allows on `timestamp`, whether it accepts `"email": null` and what it does with an `email` that the signature does not cover, and the exact meaning of `tenant_id` and `base_url` are decided by the server. The signing domain is commented as having to match the server's `registration.rs`, which is not in the source ([`register.rs#L56-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/identity/register.rs#L56-L57)).
- **Project kinds.** The CLI's help text says `kind` is passed to the server to configure language-specific defaults ([`init.rs#L75-L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/project/init.rs#L75-L80)). Which values the server accepts, and what it does with an unknown one, is not in the source.

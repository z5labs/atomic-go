# Project protocol

The project protocol is every HTTP request that `atomic_remote::HttpRemote` sends to one project's base URL. The client was written against the `atomic-api` servers ([`mod.rs#L1-L4`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L1-L4)); the server code is not in the Atomic repository, so this page documents the protocol as the client speaks it. Anything only the server decides is listed under [Open questions](#open-questions).

The protocol has two generations on the same URL:

- **The `/code` sync protocol.** Push, pull and clone move objects and view refs through one endpoint, the base URL itself, as a binary [SyncPack](#syncpack) or [SyncWants](#syncwants) body selected by a request header ([`code_sync.rs#L1-L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L1-L13)). This is what the CLI and the daemon use at this commit; see [Which methods have callers](#which-methods-have-callers).
- **The query-string protocol.** Older per-operation requests that select an operation with a query parameter such as `?changelist=` or `?insert=` ([`lib.rs#L64-L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/lib.rs#L64-L75)). The client still implements them, but apart from `download_change` neither the CLI nor the daemon calls them at this commit.

The project-identity requests, and the view inventory on a sibling path, complete the set.

## Overview

### How a request is addressed

Every method starts from `base_url`, the parsed URL of the project's protocol endpoint, which the client takes as given ([`mod.rs#L149-L153`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L149-L153), [`mod.rs#L204-L205`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L204-L205)). How that URL is built from a server, workspace and project is on [Server URLs](../transport.md#server-urls). The code's own examples disagree on its shape: the doc comments use `https://api.example.com/tenant/t/portfolio/p/project/pr/code` ([`mod.rs#L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L18)), while the newer modules and tests use `https://h/workspaces/w/projects/p/code` ([`bare.rs#L19-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L19-L20), [`project_identity_transport.rs#L30-L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L30-L33)).

The client selects an operation in one of three ways:

| Selector | Form | Used by |
|---|---|---|
| Query string | The base URL followed by `?` and the method's query, built with `format!("{}?…", self.base_url, …)` | All [reads](#reads) and [writes](#writes), and [project identity](#project-identity) |
| Protocol header | The bare base URL with `Atomic-Protocol: sync/1`; without the header the server is said to answer with a JSON project-info response instead ([`sync.rs#L4-L12`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L4-L12)) | [`sync_push`](#sync_push), [`sync_pull`](#sync_pull) |
| Sibling path | The base URL with one trailing `/` and then a trailing `/code` removed, then `/` and a resource path ([`bare.rs#L17-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L17-L26)) | [`list_view_refs`](#list_view_refs) |

The query-string methods append the query directly to the serialized base URL ([`queries.rs#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L30)). **The client does not URL-encode any value it interpolates**: view names, hashes, states and layer lists go into the string exactly as the caller passed them ([`queries.rs#L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L84), [`upload.rs#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L43), [`download.rs#L114-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L114-L115)). A Go client that wants to match the wire must build the raw query the same way rather than use `url.Values.Encode`. What happens to a value containing `&`, `=`, `#`, `+`, `%` or a space is listed under [Open questions](#open-questions); which characters a view name may contain is on [View names](../types.md#view-names). Some selectors are bare keys with no `=` (`?id`, `?views`, `?manifest`, `?provenance-list`) and one has an empty value (`state=`) ([`queries.rs#L131`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L131), [`queries.rs#L394`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L394), [`download.rs#L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L172), [`queries.rs#L514`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L514), [`queries.rs#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L30)).

Four selectors serve both a read and a write, told apart by the HTTP method: `?attest=`, `?provenance=`, `?view-manifest=` and `?project-identity=1` ([`queries.rs#L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L341), [`upload.rs#L330`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L330), [`queries.rs#L562`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L562), [`upload.rs#L380`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L380), [`queries.rs#L442`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L442), [`upload.rs#L145`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L145), [`identity.rs#L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L132), [`identity.rs#L170`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L170)).

In the query-parameter tables below, **Required** means the client always sends the parameter. What a server does without it is not in the source.

### Requests the server does not recognise

The client's comments say a server that predates an endpoint ignores the unknown query and answers with its generic JSON info response ([`queries.rs#L385-L388`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L385-L388), [`identity.rs#L15-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L15-L18)). A test models such a server as answering `{"workspace":"w","project":"p","stack":"dev"}` ([`project_identity_transport.rs#L94-L99`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L94-L99)). Several methods detect that answer and degrade instead of failing. The list of them, with what each returns, is kept once, on [Feature detection by response shape](../transport.md#feature-detection-by-response-shape); each method's section below gives its own detail.

### Common request setup

Every request carries the client's default headers: `User-Agent: atomic-<version>` and `Accept-Encoding: zstd, gzip, deflate` ([`mod.rs#L55-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L55-L59), [`mod.rs#L71-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L71-L72), [`mod.rs#L207-L213`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L207-L213)). It also carries every configured extra header, which is how credentials reach the server; a name or value that is not a valid HTTP header is dropped without an error ([`mod.rs#L215-L223`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L215-L223)). The identity requests ride "the same credentials (the client's extra headers)" as the sync protocol ([`identity.rs#L13-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L13-L14)). Which credential headers are sent is on [Request headers](../auth.md#request-headers); timeouts, redirects and decompression are on [transport.md](../transport.md) and [Compression](../transport.md#compression).

Each section below lists only the headers that method adds. Every `GET` except [`sync_pull`](#sync_pull) sends no request body and no `Content-Type` ([`queries.rs#L33-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L33-L38), [`download.rs#L36-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L36-L41), [`bare.rs#L34-L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L34-L39), [`identity.rs#L133-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L133-L138)); the write sections give each `POST` body.

### Common response handling

Every method except [`list_view_refs`](#list_view_refs), [`fetch_project_identity`](#fetch_project_identity) and [`bind_project_identity`](#bind_project_identity) passes the response headers to `check_min_version_header` before it looks at the status ([`queries.rs#L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L40), [`code_sync.rs#L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L56), [`bare.rs#L34-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L34-L40), [`identity.rs#L133-L139`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L133-L139)). That function only prints an upgrade warning when `X-Atomic-Min-Version` names a newer version; it never fails the request ([`version.rs#L45-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/version.rs#L45-L65)). See [Version negotiation](../transport.md#version-negotiation).

Unless a section says otherwise, the client handles statuses the same way in every method (for example [`get_state`](#get_state), [`queries.rs#L44-L66`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L44-L66)):

| Status | Client result |
|---|---|
| `200 OK` | Success; the body is parsed as the section describes. No other 2xx status counts as success unless the section says so. |
| `404 Not Found` | A method-specific result, given in the [index](#index) |
| `401 Unauthorized`, `403 Forbidden` | [`AuthenticationFailed`](../errors.md#authenticationfailed), carrying the URL and the response body as text |
| Anything else | [`HttpError`](../errors.md#httperror), carrying the numeric status and the response body as text |

When the error body cannot be read, the client uses an empty string ([`queries.rs#L58-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L58-L65)). A failure to send the request becomes [`ConnectionFailed`](../errors.md#connectionfailed) ([`queries.rs#L33-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L33-L38)), and so does a failure to read a success body that the method reads as text or bytes ([`queries.rs#L46-L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L46-L49)). The JSON success bodies of [`sync_push`](#sync_push) and [`fetch_project_identity`](#fetch_project_identity) are the exceptions; their sections say what happens. The error variants are described on [Error kinds](../errors.md#error-kinds); the not-found variants are [`ViewNotFound`](../errors.md#viewnotfound), [`ChangeNotFound`](../errors.md#changenotfound), [`TagNotFound`](../errors.md#tagnotfound) and [`RepositoryNotFound`](../errors.md#repositorynotfound), and the others a method can return are [`MissingDependencies`](../errors.md#missingdependencies), [`StateMismatch`](../errors.md#statemismatch), [`ProtocolError`](../errors.md#protocolerror), [`VersionMismatch`](../errors.md#versionmismatch) and [`Other`](../errors.md#other).

### Which methods have callers

At this commit every push, pull and clone goes through `sync_push` and `sync_pull`. Inside a repository, `atomic push` and `atomic pull` run libatomic's `PushChanges` and `PullChanges` handlers, in process or in the daemon; the CLI's own push and pull bodies run only when `Service::open` returns `None`, outside a repository or in a remote sandbox, and `atomic clone` always runs the CLI's own body ([Which code sends the requests](../flows.md#which-code-sends-the-requests)). Of the query-string methods, only `download_change` has a production caller. No push, pull or clone flow sends any other query-string request, whatever the module comments describe ([Retired per-object sequences](../flows.md#retired-per-object-sequences)).

| Method | Production callers |
|---|---|
| [`sync_push`](#sync_push) | The `PushChanges` handler that `atomic push` runs ([`services_sync_remote.rs#L864`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L864)); the CLI-local push body ([`command.rs#L1035`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/command.rs#L1035)) |
| [`sync_pull`](#sync_pull) | The `PushChanges` handler's advertisement and verification ([`services_sync_remote.rs#L519-L525`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L519-L525), [`services_sync_remote.rs#L868-L869`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L868-L869)) and the `PullChanges` handler's pull ([`services_sync_remote.rs#L1199-L1205`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1199-L1205)); CLI clone ([`command.rs#L919`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L919), [`command.rs#L1037`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L1037)); the CLI-local push and pull bodies ([`command.rs#L613-L614`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/command.rs#L613-L614), [`command.rs#L1044-L1045`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/command.rs#L1044-L1045), [`command.rs#L570-L571`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/pull/command.rs#L570-L571)) |
| [`list_view_refs`](#list_view_refs) | CLI `view list --remote` ([`list.rs#L480-L481`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/view/list.rs#L480-L481)); the `PullChanges` handler's SetId check ([`services_sync_remote.rs#L1592`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1592)); CLI clone's SetId check ([`command.rs#L506`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L506)); the CLI-local pull body ([`command.rs#L857`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/pull/command.rs#L857)) |
| [`fetch_project_identity`](#fetch_project_identity), [`bind_project_identity`](#bind_project_identity) | libatomic's identity transport, shared by the CLI and the daemon ([`identity_transport.rs#L4-L8`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/identity_transport.rs#L4-L8), [`identity_transport.rs#L101`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/identity_transport.rs#L101), [`identity_transport.rs#L272-L273`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/identity_transport.rs#L272-L273)) |
| [`download_change`](#download_change) | The CLI's git-binding change source ([`transport.rs#L623-L627`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/git/transport.rs#L623-L627), [`transport.rs#L638`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/git/transport.rs#L638)) |
| [`upload_change`](#upload_change), [`get_chunk_manifest`](#get_chunk_manifest) | None in compiled code. The only call sites are in `atomic-cli/tests/fixtures/helpers_c5.rs` and `helpers_c6.rs` ([`helpers_c5.rs#L353-L354`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/fixtures/helpers_c5.rs#L353-L354), [`helpers_c5.rs#L365`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/fixtures/helpers_c5.rs#L365), [`helpers_c6.rs#L300`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/fixtures/helpers_c6.rs#L300), [`helpers_c6.rs#L311`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/fixtures/helpers_c6.rs#L311)), which a Git-import test reads as bytes with `include_bytes!` and does not compile ([`git_import_generated_modify_test.rs#L150-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/git_import_generated_modify_test.rs#L150-L152)) |
| [`get_state`](#get_state), [`get_changelist`](#get_changelist) | Only `SyncEngine` inside atomic-remote ([`sync.rs#L535`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/sync.rs#L535), [`sync.rs#L575`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/sync.rs#L575)), which nothing outside the crate uses |
| All other methods | None |

The `bare` module says the per-object REST calls and view-ref CAS were retired when push, pull and clone moved to `/code` ([`bare.rs#L1-L7`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L1-L7)), and the design document lists `?view-manifest` as replaced by the bare push ([`view-snapshot-sync-design.md#L356-L357`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/docs/view-snapshot-sync-design.md?plain=1#L356-L357)). Whether current servers still serve the query-string endpoints is an [open question](#open-questions).

### Index

| Method | HTTP request | Request body | Success | `404` becomes |
|---|---|---|---|---|
| [`sync_push`](#sync_push) | `POST {base}` + `Atomic-Protocol: sync/1` | [SyncPack](#syncpack) | 200 or 201, JSON [PushSummary](#pushsummary) | `HttpError` |
| [`sync_pull`](#sync_pull) | `GET {base}` + `Atomic-Protocol: sync/1` | [SyncWants](#syncwants) | 200, [SyncPack](#syncpack) | `HttpError` |
| [`get_state`](#get_state) | `GET {base}?view={view}&state=` | none | 200, one [StateResponse](../types.md#stateresponse) line | `ViewNotFound` |
| [`get_changelist`](#get_changelist) | `GET {base}?view={view}&changelist={from}` | none | 200, [ChangelistEntry](../types.md#changelistentry) lines | `ViewNotFound` |
| [`get_id`](#get_id) | `GET {base}?view={view}&id` | none | 200, text | `ViewNotFound` |
| [`download_change`](#download_change) | `GET {base}?change={hash}` | none | 200, change file bytes | `ChangeNotFound` |
| [`download_change_to_file`](#download_change_to_file) | `GET {base}?change={hash}` | none | 200, change file bytes | `ChangeNotFound` |
| [`download_change_layers`](#download_change_layers) | `GET {base}?change={hash}&layers={layers}` | none | 200, change bytes | `ChangeNotFound` |
| [`get_chunk_manifest`](#get_chunk_manifest) | `GET {base}?change={hash}&manifest` | none | 200, JSON [ChunkManifest](#chunkmanifest) | `ChangeNotFound` |
| [`download_tag`](#download_tag) | `GET {base}?tag={state}` | none | 200, length-prefixed bytes | `TagNotFound` |
| [`list_remote_tags`](#list_remote_tags) | `GET {base}?tags={view}` | none | 200, `HASH NAME` lines | empty list |
| [`download_attestation`](#download_attestation) | `GET {base}?attest={hash}` | none | 200, bytes | `ProtocolError` |
| [`download_provenance`](#download_provenance) | `GET {base}?provenance={hash}` | none | 200, bytes | `ProtocolError` |
| [`get_provenance_list`](#get_provenance_list) | `GET {base}?provenance-list` | none | 200, hash lines | empty list |
| [`list_views`](#list_views) | `GET {base}?views` | none | 200, [RemoteViewInfo](../types.md#remoteviewinfo) lines | empty list |
| [`get_view_manifest`](#get_view_manifest) | `GET {base}?view-manifest={view}` | none | 200, [ViewManifest](#viewmanifest) text | `None` |
| [`upload_change`](#upload_change) | `POST {base}?insert={hash}&view={view}` | change file bytes | 200 | `RepositoryNotFound` |
| [`upload_change_streamed`](#upload_change_streamed) | `POST {base}?insert={hash}&view={view}` | change file bytes, streamed | 200 | `RepositoryNotFound` |
| [`upload_change_file`](#upload_change_file) | as `upload_change_streamed` | | | |
| [`store_change`](#store_change) | `POST {base}?store={hash}` | change file bytes | 200 | `RepositoryNotFound` |
| [`put_view_manifest`](#put_view_manifest) | `POST {base}?view-manifest={view}` | [ViewManifest](#viewmanifest) text | 200 | `RepositoryNotFound` |
| [`upload_tag`](#upload_tag) | `POST {base}?tagup={state}&view={view}` | tag bytes | 200 | `ViewNotFound` |
| [`fork_view`](#fork_view) | `POST {base}?fork_from={source}&view={target}` | none | 200, JSON | `ViewNotFound` |
| [`upload_attestation`](#upload_attestation) | `POST {base}?attest={hash}` | attestation bytes | 200 | `RepositoryNotFound` |
| [`upload_provenance`](#upload_provenance) | `POST {base}?provenance={hash}` | provenance bytes | 200 | `RepositoryNotFound` |
| [`list_view_refs`](#list_view_refs) | `GET {project}/refs/views` | none | 200, [RemoteViewInfo](../types.md#remoteviewinfo) lines | `RepositoryNotFound` |
| [`fetch_project_identity`](#fetch_project_identity) | `GET {base}?project-identity=1` | none | 200, JSON [IdentityResponse](#identityresponse) | `Unsupported` |
| [`bind_project_identity`](#bind_project_identity) | `POST {base}?project-identity=1` | JSON identity document | 200 or 201 | `Unsupported` |

`{base}` is the project base URL and `{project}` the sibling-path prefix described in [How a request is addressed](#how-a-request-is-addressed).

## Sync protocol

Both sync requests go to the bare base URL, with no query string ([`code_sync.rs#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L43), [`code_sync.rs#L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L80)). They differ from every other request in two headers:

| Header | Value | Source |
|---|---|---|
| `Atomic-Protocol` | `sync/1` | [`sync.rs#L44-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L44-L52) |
| `Content-Type` | `application/vnd.atomic.sync.v1` | [`sync.rs#L54-L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L54-L56) |

The suffix of `sync/1` is to be bumped on any wire-breaking change ([`sync.rs#L49-L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L49-L51)). Both directions encode their body with postcard and then compress it with zstd; see [SyncPack](#syncpack) for the byte layout.

### `sync_push`

`POST {base}`, body an encoded [SyncPack](#syncpack): the objects the client contributes plus the [RefRecord](#refrecord) compare-and-swaps that move each view ref ([`code_sync.rs#L11`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L11), [`sync.rs#L16-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L16-L20)).

**Callers at this commit.** Every push: the `PushChanges` handler that `atomic push` runs inside a repository, and the CLI-local push body ([Which methods have callers](#which-methods-have-callers)). The request's place in the push is on [Push](../flows.md#push).

**Request**

- Method and URL: `POST` on the base URL, no query ([`code_sync.rs#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L43), [`code_sync.rs#L47-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L47-L52)).
- Headers: `Atomic-Protocol: sync/1` and `Content-Type: application/vnd.atomic.sync.v1` ([`code_sync.rs#L50-L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L50-L51)).
- Body: `SyncPack::encode()` of the pack ([`code_sync.rs#L44-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L44-L46)). An encoding failure is a `ProtocolError` before anything is sent.

What a pack contains on push (which changes, snapshots, sidecars and tags, and in what order relative to the other requests) is on [Push](../flows.md#push). The daemon's [PushChanges](../../rpc/services/sync.md#pushchanges) handler is one producer.

**Response**

| Status | Client result | Source |
|---|---|---|
| `200 OK` or `201 Created` | The body is parsed as JSON [PushSummary](#pushsummary). A body that is not that JSON yields a summary of zeros, not an error. | [`code_sync.rs#L58-L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L58-L60) |
| `409 Conflict` | `ProtocolError` carrying the body text. The comment calls this a divergent, non-fast-forward ref move that the user should resolve by pulling first. | [`code_sync.rs#L37-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L37-L41), [`code_sync.rs#L61-L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L61-L63) |
| `401`, `403` | `AuthenticationFailed` with the body text | [`code_sync.rs#L64-L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L64-L67) |
| Anything else, including `404` | `HttpError` with the status and body text | [`code_sync.rs#L68-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L68-L71) |

The CLI and the daemon discard the summary; they only map the error ([`command.rs#L1035-L1038`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/command.rs#L1035-L1038), [`services_sync_remote.rs#L864-L867`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L864-L867)).

The objects module describes the server's side: it stores every object after checking its content address, and moves each ref only if `expect_old` is the current target and an ancestor of `new_target` (fast-forward only) ([`sync.rs#L16-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L16-L20), [`sync.rs#L155-L159`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L155-L159)). The test peer in libatomic's sync tests models that contract: it answers `409` "ref CAS refused" when `expect_old` differs from the stored ref, `400` when a ref's `new_target` names a view snapshot that is neither in the pack nor stored, and otherwise `{"stored":<objects>,"refs_moved":<refs>}` ([`local_sync_test.rs#L173-L206`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/local_sync_test.rs#L173-L206)). The test peer is not the server; see [Open questions](#open-questions).

### `sync_pull`

`GET {base}` with a body: an encoded [SyncWants](#syncwants) naming the views the client wants and the object keys it already holds. The response is a [SyncPack](#syncpack) of the objects the client is missing plus the current target of each requested ref ([`code_sync.rs#L75-L78`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L75-L78), [`sync.rs#L21-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L21-L24)).

**Callers at this commit.** Every push (twice), pull and clone ([Which methods have callers](#which-methods-have-callers)). Each request's place in its flow is on [Push](../flows.md#push), [Pull](../flows.md#pull) and [Clone](../flows.md#clone).

**Request**

- Method and URL: `GET` on the base URL, no query, **with a request body** ([`code_sync.rs#L80-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L80-L92)).
- Headers: the same two as `sync_push` ([`code_sync.rs#L87-L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L87-L88)).
- Body: `SyncWants::encode()` ([`code_sync.rs#L81-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L81-L83)).

The callers send three shapes of `SyncWants`. The push and pull rows cite libatomic's handlers, which `atomic push` and `atomic pull` run inside a repository; the CLI-local bodies, which run only when `Service::open` returns `None`, send the same shapes ([CLI-local push body](../flows.md#cli-local-push-body), [CLI-local pull body](../flows.md#cli-local-pull-body)).

| Purpose | `refs` | `haves` | `refs_only` | Source |
|---|---|---|---|---|
| Ref advertisement before and after a push | the pushed view's ancestor chain, root to leaf, with the leaf under its remote name | empty | `true` | [`sync.rs#L237-L245`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L237-L245), [`services_sync_remote.rs#L492-L525`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L492-L525), [`services_sync_remote.rs#L868-L869`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L868-L869) |
| Pull of one view | that view's remote name | base32 hash of every change the local repository has registered | `true` for a dry run, else `false` | [`services_sync_remote.rs#L1177-L1182`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1177-L1182), [`services_sync_remote.rs#L1199-L1205`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1199-L1205) |
| Clone | the one view, or empty for every view | empty | `false` | [`command.rs#L1029-L1037`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L1029-L1037) |

Without `--all-views`, and only when nothing failed to apply, the clone also sends `SyncWants::advertise(vec![])` (all views, refs only) to list the remote's other views ([`command.rs#L1287-L1308`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L1287-L1308), [`command.rs#L916-L919`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L916-L919)).

**Response**

| Status | Client result | Source |
|---|---|---|
| `200 OK` | The body is decoded with `SyncPack::decode` | [`code_sync.rs#L98-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L98-L103) |
| `401`, `403` | `AuthenticationFailed` with the body text | [`code_sync.rs#L110-L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L110-L113) |
| Anything else, including `404` and `409` | `HttpError` with the status and body text | [`code_sync.rs#L114-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L114-L117) |

A 200 body that fails to decompress or to deserialize becomes `VersionMismatch`, carrying the client's version, the server's `X-Atomic-Min-Version` value if it sent one, and the decoder's message. A body that inflates past the 2 GiB limit becomes `ProtocolError` "failed to decode sync pack: …" ([`code_sync.rs#L94-L108`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L94-L108), [`sync.rs#L62-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L62-L65), [`sync.rs#L330-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L330-L341)). The client does not check the response `Content-Type`.

The objects module says the server returns only objects whose keys are not in `haves`, and with `refs_only` only the view refs and their view-snapshot objects for the requested views and their ancestor chains, without change bodies or sidecars ([`sync.rs#L206-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L206-L225)). The test peer does the same: it follows `parent_view` from each requested view, returns every ref when `refs` is empty, drops objects named in `haves`, keeps only `View` objects when `refs_only` is set, leaves `expect_old` empty in every returned ref, and sets `Content-Type: application/vnd.atomic.sync.v1` ([`local_sync_test.rs#L125-L168`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/local_sync_test.rs#L125-L168)). A test server for clone answers `Content-Type: application/octet-stream` instead, which the client accepts ([`clone_sidecar_integration_test.rs#L159-L163`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/clone_sidecar_integration_test.rs#L159-L163)).

How a client turns the returned pack into views and changes is on [Pull](../flows.md#pull) and [Clone](../flows.md#clone).

## Reads

### `get_state`

`GET {base}?view={view}&state=`. Returns the view's current position and Merkle state ([`queries.rs#L19-L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L19-L30)).

**Callers at this commit.** Only atomic-remote's `SyncEngine` ([`sync.rs#L535`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/sync.rs#L535)), which no push, pull or clone uses ([Changelist dichotomy](../flows.md#changelist-dichotomy)). The pull and clone modules' doc comments still describe querying the remote state as a step ([`mod.rs#L17-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/pull/mod.rs#L17-L27), [`mod.rs#L107-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/mod.rs#L107-L110)); that sequence is retired, and no pull or clone sends this request ([Retired per-object sequences](../flows.md#retired-per-object-sequences)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `view` | string | **Required** | View name, not URL-encoded |
| `state` | empty | **Required** | Selector; always sent as `state=` with an empty value |

**Response.** On 200 the client reads the whole body as text and parses it with `StateResponse::parse`; see [StateResponse](../types.md#stateresponse) ([`queries.rs#L45-L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L45-L56)). It passes the whole body, not only its first line, and the parser trims it and splits it on any whitespace, newlines included ([`types.rs#L387-L412`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/types.rs#L387-L412)). `-` means an empty view ([`types.rs#L394-L396`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/types.rs#L394-L396)). A body that does not parse is a `ProtocolError` "Failed to parse state response: …" ([`queries.rs#L53-L55`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L53-L55)).

Examples from the parser's tests: `42 ABC123 DEF456` is position 42, Merkle `ABC123`, tag Merkle `DEF456`; `-` is empty ([`types.rs#L374-L385`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/types.rs#L374-L385)). A fourth token, when present, is the view's SetId ([`types.rs#L306-L309`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/types.rs#L306-L309)).

**Errors.** `404` becomes `ViewNotFound` for the requested view ([`queries.rs#L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L57)); other statuses follow [Common response handling](#common-response-handling).

### `get_changelist`

`GET {base}?view={view}&changelist={from}`. Returns the view's change log from a position ([`queries.rs#L69-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L69-L84)).

**Callers at this commit.** Only atomic-remote's `SyncEngine` ([`sync.rs#L575`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/sync.rs#L575)), which no push, pull or clone uses ([Changelist dichotomy](../flows.md#changelist-dichotomy)). The pull and clone modules' doc comments still describe computing a changelist delta or fetching a changelist as a step ([`mod.rs#L17-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/pull/mod.rs#L17-L27), [`mod.rs#L107-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/mod.rs#L107-L110)); that sequence is retired, and no pull or clone sends this request ([Retired per-object sequences](../flows.md#retired-per-object-sequences)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `view` | string | **Required** | View name, not URL-encoded |
| `changelist` | unsigned 64-bit decimal | **Required** | Starting sequence number. The doc comment says the result starts "from the given position" ([`queries.rs#L74-L78`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L74-L78)); whether that is inclusive is server-side. |

**Response.** On 200 the body is read as text and split into lines. Each line is trimmed, blank lines are skipped, and every other line must parse as a [ChangelistEntry](../types.md#changelistentry) ([`queries.rs#L99-L108`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L99-L108), [`mod.rs#L294-L312`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L294-L312)). One bad line fails the whole call with `ProtocolError` "Failed to parse changelist entry: …" ([`mod.rs#L304-L306`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/mod.rs#L304-L306)). An empty body is an empty list ([`tests.rs#L123-L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/tests.rs#L123-L127)).

A test body with three entries, the second tagged (trailing `.`):

```text
0.ABC.DEF
1.GHI.JKL.
2.MNO.PQR
```

([`tests.rs#L139-L147`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/tests.rs#L139-L147)). Blank and whitespace-only lines between entries are ignored ([`tests.rs#L149-L154`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/tests.rs#L149-L154)).

**Errors.** `404` becomes `ViewNotFound` ([`queries.rs#L109`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L109)).

### `get_id`

`GET {base}?view={view}&id`. The doc comment says it returns the view's UUID ([`queries.rs#L121-L131`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L121-L131)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `view` | string | **Required** | View name, not URL-encoded |
| `id` | flag | **Required** | Bare key with no `=` |

**Response.** On 200 the body is read as text and returned with leading and trailing whitespace removed. The client does not check that it is a UUID ([`queries.rs#L145-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L145-L152)).

**Errors.** `404` becomes `ViewNotFound` ([`queries.rs#L153`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L153)).

### `download_change`

`GET {base}?change={hash}`. Downloads one change file ([`queries.rs#L165-L175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L165-L175)).

**Callers at this commit.** Only the CLI's git-binding change source ([`transport.rs#L638`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/git/transport.rs#L638)). No push, pull or clone sends it; they receive change files inside a [SyncPack](#syncpack).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `change` | base32 change hash | **Required** | The change to fetch, not URL-encoded ([`queries.rs#L169-L175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L169-L175)). The CLI's caller passes `Hash::to_base32()` ([`transport.rs#L638`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/git/transport.rs#L638)); see [Base32 hash text](../types.md#base32-hash-text). |

**Response.** On 200 the client returns the raw body bytes without inspecting them ([`queries.rs#L189-L197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L189-L197)). The bytes are a V3 change file; see [the change-file container](../change-format/container.md). The git-binding caller stores them as an `ObjectFamily::Change` record keyed by the hash it asked for. It only warns about a change that fails, and returns an error only when none of the wanted changes was served ([`transport.rs#L637-L655`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/git/transport.rs#L637-L655)).

**Errors.** `404` becomes `ChangeNotFound` for the hash ([`queries.rs#L198`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L198)).

### `download_change_to_file`

Same request as [`download_change`](#download_change): `GET {base}?change={hash}` ([`download.rs#L32-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L32-L34)). The only differences are on the client side: on 200 it reads the whole body, creates the destination's parent directory, writes the bytes to the file and returns the byte count ([`download.rs#L47-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L47-L72)). A directory or write failure is `Other` ([`download.rs#L54-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L54-L65)). `404` becomes `ChangeNotFound` ([`download.rs#L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L74)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

The doc comment says this avoids holding the full change in memory ([`download.rs#L18-L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L18-L22)), but the code buffers the whole body with `bytes()` before writing it ([`download.rs#L48-L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L48-L51), [`download.rs#L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L63)).

### `download_change_layers`

`GET {base}?change={hash}&layers={layers}`. Asks the server for only some layers of a change ([`download.rs#L86-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L86-L115)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `change` | base32 change hash | **Required** | As in [`download_change`](#download_change) |
| `layers` | [LayerSelection](#layerselection) query value | **Required** | `all`, or a comma-separated subset of `graph`, `semantic`, `content` in that order. The comma is not URL-encoded. |

The test of the URL shows `layers=graph,content` for a thin pull and `layers=all` for every layer ([`tests.rs#L20-L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/tests.rs#L20-L43)).

**Response.** On 200 the client returns the raw body bytes ([`download.rs#L129-L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L129-L141)). The doc comment says the result may be a subset of the full change, and that a server without `?layers=` support returns the full change ([`download.rs#L97-L98`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L97-L98), [`download.rs#L105-L108`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L105-L108)). The client cannot tell which it got. The layout of a layer-filtered response is an [open question](#open-questions).

**Errors.** `404` becomes `ChangeNotFound` ([`download.rs#L143`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L143)).

### `get_chunk_manifest`

`GET {base}?change={hash}&manifest`. Fetches the list of content chunks in a change, which the doc comment calls the starting point for delta transfer; no request at this commit performs such a transfer ([Delta transfer negotiation](../transport.md#delta-transfer-negotiation)) ([`download.rs#L155-L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L155-L172)).

**Callers at this commit.** None in compiled code; the only call sites are in test fixture files that are read as bytes, not compiled ([Which methods have callers](#which-methods-have-callers)). No push, pull or clone sends it.

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `change` | base32 change hash | **Required** | As in [`download_change`](#download_change) |
| `manifest` | flag | **Required** | Bare key with no `=` |

**Response**

| Status | Client result | Source |
|---|---|---|
| `200` with a `Content-Type` that contains `json` | The body is parsed as JSON [ChunkManifest](#chunkmanifest); a parse failure is `ProtocolError` "Failed to parse chunk manifest: …" | [`download.rs#L186-L210`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L186-L210) |
| `200` with any other or no `Content-Type` | `None`: the client assumes the server ignored `&manifest` and sent the change itself | [`download.rs#L211-L219`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L211-L219) |
| `400 Bad Request` | `None`, taken to mean the server does not support `?manifest` | [`download.rs#L226-L230`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L226-L230) |
| `404` | `ChangeNotFound` | [`download.rs#L221`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L221) |

The fixture file that calls this, kept as test data, notes that the endpoint returns the server's manifest for an existing change and is not a negotiation; a `POST ?negotiate_chunks` endpoint is a TODO and is not implemented in the client ([`helpers_c5.rs#L367-L384`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/fixtures/helpers_c5.rs#L367-L384)).

### `download_tag`

`GET {base}?tag={state}`. Downloads a tag in its "short" format ([`queries.rs#L210-L220`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L210-L220)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `tag` | base32 Merkle state | **Required** | The tagged state, not URL-encoded ([`queries.rs#L214-L220`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L214-L220)) |

**Response.** On 200 the body is an 8-byte big-endian unsigned length `n` followed by at least `n` bytes of short tag data. The client returns bytes `8` to `8+n` and ignores anything after them ([`queries.rs#L240-L261`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L240-L261)).

```text
offset 0   u64, big-endian    n = length of the short tag data
offset 8   n bytes            short tag data (returned to the caller)
offset 8+n anything           ignored
```

A body shorter than 8 bytes is `ProtocolError` "Tag response too short (missing length prefix)"; one shorter than `8+n` is `ProtocolError` "Tag response truncated: expected {n} bytes, got {len-8}" ([`queries.rs#L242-L258`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L242-L258)). The layout of the short tag data itself is an [open question](#open-questions).

**Errors.** `404` becomes `TagNotFound` for the state ([`queries.rs#L263`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L263)).

### `list_remote_tags`

`GET {base}?tags={view}`. Lists the tags of a view as `(hash, name)` pairs, to be fetched one by one with [`download_tag`](#download_tag) ([`queries.rs#L275-L281`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L275-L281)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `tags` | string | **Required** | View name, not URL-encoded |

**Response.** On 200 the body is read as text, one tag per line in the form `HASH NAME`. Each line is trimmed and blank lines are skipped; the line is split at the first space, so the name may itself contain spaces. A non-blank line with no space is skipped without an error ([`queries.rs#L295-L314`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L295-L314)).

**Errors.** `404` yields an empty list: the comment reads it as "View doesn't exist or no tags" ([`queries.rs#L316-L319`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L316-L319)).

### `download_attestation`

`GET {base}?attest={hash}`. Downloads one attestation ([`queries.rs#L331-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L331-L341)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `attest` | base32 attestation hash | **Required** | Not URL-encoded ([`queries.rs#L335-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L335-L341)) |

**Response.** On 200 the client returns the raw body bytes ([`queries.rs#L355-L362`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L355-L362)). They are a serialized attestation: a 4-byte magic followed by a postcard payload ([`attestation.rs#L389-L403`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/attestation.rs#L389-L403)).

**Errors.** `404` becomes `ProtocolError` "Attestation not found: {hash}", not a not-found variant ([`queries.rs#L363-L366`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L363-L366)).

### `download_provenance`

`GET {base}?provenance={hash}`. Downloads one provenance graph ([`queries.rs#L552-L562`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L552-L562)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `provenance` | base32 provenance-graph hash | **Required** | Not URL-encoded ([`queries.rs#L556-L562`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L556-L562)) |

**Response.** On 200 the client returns the raw body bytes ([`queries.rs#L576-L587`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L576-L587)). They are a serialized provenance graph: a 4-byte magic followed by a postcard payload ([`mod.rs#L259-L273`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance_graph/mod.rs#L259-L273)).

**Errors.** `404` becomes `ProtocolError` "Provenance graph not found: {hash}" ([`queries.rs#L588-L591`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L588-L591)).

### `get_provenance_list`

`GET {base}?provenance-list`. Lists every provenance-graph hash the remote holds, as a flat inventory the client diffs against its own ([`queries.rs#L492-L514`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L492-L514)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `provenance-list` | flag | **Required** | Bare key with no `=` ([`tests.rs#L55-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/tests.rs#L55-L62)) |

**Response.** On 200 the body is read as text; each line is trimmed, blank lines are dropped and every other line is returned as a hash string, in server order. The client does not validate the hashes ([`queries.rs#L528-L539`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L528-L539)).

**Errors.** `404` yields an empty list ([`queries.rs#L540`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L540)). The doc comment says servers without the extension "return an error status" that callers should treat as unsupported ([`queries.rs#L506-L508`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L506-L508)); apart from 404, such a status is an `HttpError`, and no caller exists at this commit to do the degrading.

### `list_views`

`GET {base}?views`. Lists every view on the remote ([`queries.rs#L378-L394`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L378-L394)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)). `atomic view list --remote` uses [`list_view_refs`](#list_view_refs) instead.

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `views` | flag | **Required** | Bare key with no `=` |

**Response.** On 200 each line of the body is passed to the lenient `RemoteViewInfo::parse`, and lines it rejects are dropped ([`queries.rs#L408-L417`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L408-L417)). The row format is on [RemoteViewInfo](../types.md#remoteviewinfo): tab-separated `name`, `scope`, `parent`, `change_count`, `state` and an optional `set_id` ([`types.rs#L658-L667`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/types.rs#L658-L667)). The lenient parser only needs a non-empty name and scope; a missing or non-numeric count becomes 0 ([`types.rs#L674-L685`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/types.rs#L674-L685)). An older server's JSON info blob therefore yields an empty list rather than an error ([`queries.rs#L385-L388`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L385-L388)). The doc comment says the server sorts views by name ([`queries.rs#L390-L392`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L390-L392)).

**Errors.** `404` yields an empty list ([`queries.rs#L418`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L418)).

[`list_view_refs`](#list_view_refs) returns the same row type from a different path with a strict parser.

### `get_view_manifest`

`GET {base}?view-manifest={view}`. Fetches a view's manifest: its header line and change log as stored on the remote ([`queries.rs#L430-L442`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L430-L442)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)). The push module's doc comment still describes `GET ?view-manifest=` per view as a push step ([`mod.rs#L29-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/mod.rs#L29-L34), [`mod.rs#L131-L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/mod.rs#L131-L132)); that sequence is retired, and no push sends this request ([Retired per-object sequences](../flows.md#retired-per-object-sequences)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `view-manifest` | string | **Required** | View name, not URL-encoded |

**Response**

| Status and body | Client result | Source |
|---|---|---|
| `200`, body empty or only whitespace | `None` (no such view) | [`queries.rs#L470-L471`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L470-L471) |
| `200`, first non-blank line contains a tab | `Some(text)`: the body exactly as received, not trimmed and not parsed | [`queries.rs#L465-L473`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L465-L473) |
| `200`, first non-blank line has no tab | `ProtocolError` "server does not support view manifests (?view-manifest)" | [`queries.rs#L461-L478`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L461-L478) |
| `404` | `None` | [`queries.rs#L480`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L480) |

The client is string-level; parsing belongs to atomic-repository ([`queries.rs#L439-L440`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L439-L440)). The text format is [ViewManifest](#viewmanifest).

## Writes

### `upload_change`

`POST {base}?insert={hash}&view={view}`. Uploads a change file and inserts it into a view ([`upload.rs#L35-L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L35-L43), [`lib.rs#L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/lib.rs#L74)).

**Callers at this commit.** None in compiled code; the only call sites are in test fixture files that are read as bytes, not compiled ([Which methods have callers](#which-methods-have-callers)). No push sends it; a push carries change files in a [SyncPack](#syncpack).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `insert` | base32 change hash | **Required** | Hash of the change in the body, not URL-encoded |
| `view` | string | **Required** | Target view name, not URL-encoded |

The URL test checks only that `insert=ABCDEF123456` and `view=dev` both appear ([`tests.rs#L9-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/tests.rs#L9-L18)).

**Request headers.** `Content-Type: application/octet-stream` ([`upload.rs#L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L49)).

**Request body.** The raw change file bytes, sent as one buffered body ([`upload.rs#L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L41), [`upload.rs#L46-L50`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L46-L50)). See [the change-file container](../change-format/container.md) and [Change hash](../change-format/container.md#change-hash) for how `insert` relates to the bytes.

**Response.** `200` is success; the body is ignored ([`upload.rs#L59-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L59-L62)).

**Errors**

| Status | Client result | Source |
|---|---|---|
| `404` | `RepositoryNotFound` with the request URL | [`upload.rs#L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L63) |
| `400` or `500` whose body contains both `missing` and `dependenc` | `MissingDependencies` listing every hash-like token in the body, or `HttpError` if there is none | [`upload.rs#L68-L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L68-L80) |
| `400` or `500`, any other body | `HttpError` | [`upload.rs#L80-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L80-L82) |
| `401`, `403`, other | as in [Common response handling](#common-response-handling) | [`upload.rs#L64-L87`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L64-L87) |

**Hash extraction.** The client splits the message on every character that is not an ASCII uppercase letter or digit, and keeps each piece of 40 or more characters drawn only from `A`-`Z` and `2`-`7` ([`upload.rs#L17-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L17-L32)). The comment gives the reason: a Blake3 change hash is 52 base32 characters, and 40 keeps ordinary words out ([`upload.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L22-L24)). Every such token is returned, not only the missing one: for the server message below the test expects both hashes, the dependent change and its dependency ([`upload.rs#L536-L552`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L536-L552)).

```text
missing changes: Manifest for view 'orange-night-44fb': change GJGC6OK6JT77OB2PDGFYEC2BUISJWQNQVT345SNIZ2CKKXGV652A depends on FUGMXMFD7FJ6QARZ4RML2W5W6W3RGE2K7ILTZ55XKQ4Z3XZ4YQ4A, which is neither earlier in the log nor present locally
```

### `upload_change_streamed`

Same request as [`upload_change`](#upload_change), `POST {base}?insert={hash}&view={view}` with `Content-Type: application/octet-stream` ([`upload.rs#L443`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L443), [`upload.rs#L463-L468`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L463-L468)). The differences:

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

- The body is streamed from a file on disk rather than buffered ([`upload.rs#L421-L425`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L421-L425), [`upload.rs#L460-L461`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L460-L461)).
- The client sets `Content-Length` explicitly from the file's size; the doc comment says this lets the server validate the upload without buffering ([`upload.rs#L423-L425`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L423-L425), [`upload.rs#L450-L456`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L450-L456), [`upload.rs#L467`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L467)). See [Streaming](../transport.md#streaming).
- Failing to open the file or read its metadata is `Other`, before any request ([`upload.rs#L446-L456`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L446-L456)).

The status handling is identical to `upload_change`, including the missing-dependency extraction ([`upload.rs#L476-L503`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L476-L503)).

### `upload_change_file`

A wrapper that calls [`upload_change_streamed`](#upload_change_streamed) with the same arguments; it sends the same request ([`upload.rs#L506-L529`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L506-L529)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

### `store_change`

`POST {base}?store={hash}`. Stores a change file without applying it to any view. The doc comment says the server writes it content-addressed and idempotently, answering 200 if it already exists, and that view membership is declared separately with [`put_view_manifest`](#put_view_manifest) ([`upload.rs#L91-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L91-L100)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)). The push module's doc comment still describes `POST ?store=` per change as a push step ([`mod.rs#L35-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/mod.rs#L35-L36), [`mod.rs#L140-L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/mod.rs#L140-L141)); that sequence is retired, and no push sends this request ([Retired per-object sequences](../flows.md#retired-per-object-sequences)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `store` | base32 change hash | **Required** | Hash of the change in the body, not URL-encoded |

**Request.** `Content-Type: application/octet-stream`; body the raw change file bytes ([`upload.rs#L103-L108`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L103-L108)).

**Response.** `200` is success; `404` is `RepositoryNotFound`. There is no missing-dependency handling: `400` and `500` are plain `HttpError` ([`upload.rs#L115-L129`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L115-L129)).

### `put_view_manifest`

`POST {base}?view-manifest={view}`. Declares a view's identity and change log. The doc comment says the server creates the view with the declared scope and parent if absent, fast-forwards its log and checks that the resulting Merkle equals the declared state; every referenced change must already be on the server, and manifests are pushed root to leaf so a draft's parent exists first ([`upload.rs#L132-L145`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L132-L145)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)). The push module's doc comment still describes `POST ?view-manifest=` per view as a push step ([`mod.rs#L37-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/mod.rs#L37-L38), [`mod.rs#L143-L144`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/mod.rs#L143-L144)); that sequence is retired, and no push sends this request ([Retired per-object sequences](../flows.md#retired-per-object-sequences)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `view-manifest` | string | **Required** | View name, not URL-encoded |

**Request.** `Content-Type: text/plain`; body the [ViewManifest](#viewmanifest) text as the caller passed it ([`upload.rs#L152-L157`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L152-L157)).

**Response and errors**

| Status | Client result | Source |
|---|---|---|
| `200` | Success | [`upload.rs#L165-L168`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L165-L168) |
| `404` | `RepositoryNotFound` | [`upload.rs#L169`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L169) |
| `409` | `ProtocolError` with the body; the comment lists divergence, identity mismatch and state mismatch | [`upload.rs#L170-L174`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L170-L174) |
| `400` whose body contains `missing` and `change` | `MissingDependencies` from the [hash extraction](#upload_change), or `HttpError` if no hash is found | [`upload.rs#L179-L190`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L179-L190) |
| `400`, any other body | `HttpError` | [`upload.rs#L191-L193`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L191-L193) |
| `401`, `403`, other (including `500`) | as in [Common response handling](#common-response-handling) | [`upload.rs#L175-L198`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L175-L198) |

The trigger words differ from `upload_change`, which looks for `dependenc` and also inspects `500` ([`upload.rs#L68-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L68-L73)).

### `upload_tag`

`POST {base}?tagup={state}&view={view}`. Uploads a tag in short format ([`upload.rs#L202-L210`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L202-L210)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)). A push carries tags as `Tag` objects in a [SyncPack](#syncpack).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `tagup` | base32 Merkle state | **Required** | The tagged state, not URL-encoded |
| `view` | string | **Required** | Target view name, not URL-encoded |

**Request.** `Content-Type: application/octet-stream`; body the short tag data as given, with no length prefix added ([`upload.rs#L213-L218`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L213-L218)). Compare the 8-byte prefix that [`download_tag`](#download_tag) strips.

**Response.** `200` is success; the body is ignored ([`upload.rs#L225-L229`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L225-L229)).

**Errors**

| Status | Client result | Source |
|---|---|---|
| `404` | `ViewNotFound` for `view` | [`upload.rs#L230`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L230) |
| `400` or `500` whose body contains `Wrong state` or `state mismatch` | `StateMismatch`, with remote state `"unknown"` and the requested state | [`upload.rs#L235-L239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L235-L239) |
| `400` or `500`, any other body | `HttpError` | [`upload.rs#L240-L242`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L240-L242) |

### `fork_view`

`POST {base}?fork_from={source}&view={target}`. Creates a view on the server from an existing one. The doc comment says no data is duplicated and the changelog is copied in one transaction ([`upload.rs#L251-L270`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L251-L270)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `fork_from` | string | **Required** | Existing source view, not URL-encoded |
| `view` | string | **Required** | New view to create, not URL-encoded |

**Request.** No body and no `Content-Type` ([`upload.rs#L273-L278`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L273-L278)).

**Response.** On 200 the body must be JSON; the client reads the `changes` member as an unsigned integer and returns it, using 0 when it is absent or not an unsigned integer ([`upload.rs#L284-L301`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L284-L301)). A body that is not JSON is `ProtocolError` "Invalid fork response: …" ([`upload.rs#L291-L292`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L291-L292)). No other member is read, and no test shows a full response.

**Errors.** `400` is `ProtocolError` with the body; `404` is `ViewNotFound` for the *source* view ([`upload.rs#L303-L307`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L303-L307)).

### `upload_attestation`

`POST {base}?attest={hash}`. Uploads an attestation, which the doc comment describes as a graph-level audit node stored apart from changes ([`upload.rs#L319-L330`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L319-L330)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)). A push carries attestations as `Attest` objects in a [SyncPack](#syncpack).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `attest` | base32 attestation hash | **Required** | Not URL-encoded |

**Request.** `Content-Type: application/octet-stream`; body the serialized attestation ([`upload.rs#L328`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L328), [`upload.rs#L335-L338`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L335-L338)).

**Response.** `200` is success; the body is ignored ([`upload.rs#L345-L349`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L345-L349)).

**Errors.** `404` is `RepositoryNotFound`; `400` is `ProtocolError` "Failed to upload attestation: {body}" ([`upload.rs#L350-L361`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L350-L361)).

### `upload_provenance`

`POST {base}?provenance={hash}`. Uploads a provenance graph ([`upload.rs#L369-L380`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L369-L380)). Same shape as [`upload_attestation`](#upload_attestation), with these differences: the selector is `provenance`, the body is a serialized provenance graph, and a `400` is `ProtocolError` "Failed to upload provenance graph: {body}" ([`upload.rs#L378`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L378), [`upload.rs#L385-L388`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L385-L388), [`upload.rs#L405-L411`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L405-L411)).

**Callers at this commit.** None. No CLI, daemon or libatomic code sends this request, and no push, pull or clone flow uses it ([Which methods have callers](#which-methods-have-callers)). A push carries provenance graphs as `Provenance` objects in a [SyncPack](#syncpack).

## View inventory

### `list_view_refs`

`GET {project}/refs/views`. The view inventory, which the comment says the server composes from the `.view` objects; it backs `atomic view list --remote` ([`bare.rs#L28-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L28-L32)).

**URL.** The client takes the serialized base URL, removes one trailing `/`, removes a trailing `/code` if there is one, and appends `/refs/views` ([`bare.rs#L21-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L21-L26)). Tests:

| Base URL | Request URL |
|---|---|
| `https://h/workspaces/w/projects/p/code` | `https://h/workspaces/w/projects/p/refs/views` |
| `https://h/workspaces/w/projects/p` | `https://h/workspaces/w/projects/p/refs/views` |

([`bare.rs#L73-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L73-L93)). There is no query string and no extra header.

**Response.** On 200 the body is read as text. Blank lines are skipped; every other line must pass `RemoteViewInfo::parse_strict`, and the first that fails ends the call with `ProtocolError` "Invalid view inventory row {n}: {reason}", where `n` is the 1-based line number in the body, blank lines counted ([`bare.rs#L41-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L41-L58)). The strict parser requires 5 or 6 tab-separated fields, a non-empty name, a scope of `shared` or `draft` (any case) and a numeric change count ([`types.rs#L631-L656`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/types.rs#L631-L656)). Rows the tests reject:

```text
dev	shared	-	not-a-count	-
dev	unknown	-	0	-
{"workspace":"w"}
```

([`bare.rs#L95-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L95-L100)). The row type is [RemoteViewInfo](../types.md#remoteviewinfo). Callers read its sixth field, the effective SetId, to check that a pull or clone reproduced the remote set ([`command.rs#L494-L499`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L494-L499), [`command.rs#L506`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/clone/command.rs#L506)).

**Errors**

| Status | Client result | Source |
|---|---|---|
| `404` | `RepositoryNotFound` with the request URL | [`bare.rs#L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L60) |
| Anything else, **including `401` and `403`** | `HttpError` with the status and body text | [`bare.rs#L61-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L61-L64) |

Unlike the other methods, this one does not map `401`/`403` to `AuthenticationFailed` and does not read `X-Atomic-Min-Version` ([`bare.rs#L34-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L34-L40), [`bare.rs#L60-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L60-L64)).

## Project identity

The module comment says the server binds one canonical `ProjectIdentity` JSON-LD object per project, first write wins; clone and pull read it so every clone shares one identity, and the first push to an unbound project offers the local one ([`identity.rs#L1-L12`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L1-L12)). Both requests use `?project-identity=1` on the base URL without the sync header ([`identity.rs#L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L132), [`identity.rs#L170`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L170)). Neither reads `X-Atomic-Min-Version` ([`identity.rs#L133-L139`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L133-L139), [`identity.rs#L173-L181`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L173-L181)).

### `fetch_project_identity`

`GET {base}?project-identity=1`. Reads the remote's binding ([`identity.rs#L130-L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L130-L132)). A test pins the request line as `GET /workspaces/w/projects/p/code?project-identity=1` ([`project_identity_transport.rs#L77-L81`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L77-L81)).

**Query parameters**

| Name | Type | Required | Meaning |
|---|---|---|---|
| `project-identity` | literal `1` | **Required** | Selector; always the value `1` |

**Response.** On 200 the body is parsed as JSON [IdentityResponse](#identityresponse) ([`identity.rs#L140-L143`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L140-L143)). A body that does not deserialize into that shape, for example one that is not JSON, is `ProtocolError` "invalid project-identity response: …", not `Unsupported`.

| Status and body | Result | Source |
|---|---|---|
| `200`, `"bound": true` and an `identity` member | `Bound(identity)` | [`identity.rs#L144-L145`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L144-L145) |
| `200`, `"bound": false` | `Unbound`, whatever else the body holds | [`identity.rs#L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L146) |
| `200`, no `bound` member, or `"bound": true` without `identity` | `Unsupported`: taken to be an older server's project-info ping | [`identity.rs#L147-L149`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L147-L149) |
| `404`, `405`, `501` | `Unsupported` | [`identity.rs#L152-L156`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L152-L156) |
| `401`, `403` | `AuthenticationFailed` | [`identity.rs#L157-L160`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L157-L160) |
| Anything else | `HttpError` | [`identity.rs#L161-L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L161-L164) |

Examples from the wire tests:

```json
{"bound": true, "hash": "abab…ab", "identity": {"@context": {"atomic": "https://atomic.dev/ns/workflow#", "dcterms": "http://purl.org/dc/terms/"}, "@type": "atomic:ProjectIdentity", "atomic:projectId": "AAAA…A", "atomic:schemaVersion": 1, "dcterms:created": "2026-10-26T00:00:00Z"}}
{"bound": false}
{"workspace": "w", "project": "p", "stack": "dev"}
```

The first is bound (`hash` is `"ab"` repeated 32 times and the project id 52 `A`s), the second unbound and the third an older server, read as `Unsupported` ([`project_identity_transport.rs#L16-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L16-L24), [`project_identity_transport.rs#L64-L104`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L64-L104)).

libatomic's caller treats any error from this call as "nothing to adopt" so that a pull never fails on it ([`identity_transport.rs#L98-L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/identity_transport.rs#L98-L113)). The `PullChanges` handler sends this request on every pull and applies a bound identity to the local repository before it checks for a dry run, so a dry run and a download-only pull also send it and can change the repository's project identity ([`services_sync_remote.rs#L1222-L1238`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1222-L1238), [`services_sync_remote.rs#L1305-L1306`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L1305-L1306)); see [Request 2: project identity](../flows.md#request-2-project-identity).

### `bind_project_identity`

`POST {base}?project-identity=1`. Offers the local identity document for binding, first write wins ([`identity.rs#L168-L170`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L168-L170)). A test pins the request line as `POST /workspaces/w/projects/p/code?project-identity=1` and the header `content-type: application/json` ([`project_identity_transport.rs#L118-L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L118-L126)).

**Request**

- Query: `project-identity=1`, as in [`fetch_project_identity`](#fetch_project_identity).
- Header: `Content-Type: application/json` ([`identity.rs#L176`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L176)).
- Body: the identity document re-serialized with `serde_json::to_vec` ([`identity.rs#L171-L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L171-L172)). libatomic passes the repository's stored identity object, parsed from its bytes ([`identity_transport.rs#L257-L264`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/identity_transport.rs#L257-L264)). A freshly minted document has `@context`, `@type` `atomic:ProjectIdentity`, `atomic:projectId` (base32), `atomic:schemaVersion` 1 and `dcterms:created`; one minted with a key for a newly created project also carries `atomic:issuer` and a proof ([`project_identity.rs#L35-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/project_identity.rs#L35-L52)). The document, its signature and its hash are described on [Project identity](../auth.md#project-identity).

**Response**

| Status | Result | Source |
|---|---|---|
| `200`, `201` | `Bound`; the body is not read | [`identity.rs#L182`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L182) |
| `409` | `Conflict`, after reading the body as [BindServerError](#bindservererror) and then calling [`fetch_project_identity`](#fetch_project_identity) to learn what the remote holds | [`identity.rs#L183-L191`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L183-L191) |
| `404`, `405`, `501` | `Unsupported` | [`identity.rs#L192-L194`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L192-L194) |
| `401`, `403` | `AuthenticationFailed` | [`identity.rs#L195-L198`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L195-L198) |
| Anything else | `HttpError` | [`identity.rs#L199-L202`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L199-L202) |

On a `409` the client therefore sends a second request, a `GET` of the same URL. Its outcome becomes the conflict's `remote`: `Bound(document)` or `Unbound` when it succeeds, `Unknown` when it fails or reports `Unsupported` ([`identity.rs#L76-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L76-L100)). A test covers a server that answers the `409` and then reports the project unbound ([`project_identity_transport.rs#L196-L219`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L196-L219)). Neither read is fatal ([`identity.rs#L184-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L184-L186)).

Before a push, libatomic turns `Conflict` into an error that refuses the push, and `Unsupported` into a no-op ([`identity_transport.rs#L272-L283`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/identity_transport.rs#L272-L283)).

## Types

### SyncPack

The `/code` push body and pull response: a batch of objects plus the ref moves that make them reachable. On push the client fills both lists; on pull the server fills `objects` with what the client lacks and `refs` with each requested view's current target ([`sync.rs#L172-L182`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L172-L182)).

| Field | Type | Required | Meaning |
|---|---|---|---|
| `objects` | list of [ObjectRecord](#objectrecord) | **Required** | Objects being transferred |
| `refs` | list of [RefRecord](#refrecord) | **Required** | Ref moves on push, current targets on pull |

**Encoding.** `encode` serializes the value with postcard and compresses the result with zstd at level 3; `decode` decompresses and deserializes it ([`sync.rs#L304-L307`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L304-L307), [`sync.rs#L319-L325`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L319-L325), [`sync.rs#L58-L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L58-L60)). Decompression is streamed and stops with `TooLarge` once the output passes 2 GiB ([`sync.rs#L62-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L62-L65), [`sync.rs#L327-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L327-L341)). The module comment says both sides link this crate so the framing is agreed byte for byte ([`sync.rs#L31-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L31-L37)). A test checks that the wire body is smaller than the raw postcard for repetitive input, so compression is really applied ([`sync.rs#L421-L441`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L421-L441)), and another that garbage fails to decode ([`sync.rs#L460-L463`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L460-L463)).

Both codecs are external crates: postcard 1.1.3 ([`Cargo.lock#L2938-L2939`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L2938-L2939)) and the `zstd` crate 0.14.3 ([`Cargo.lock#L5652-L5653`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L5652-L5653)). The zstd output is a standard zstd frame ([RFC 8878](https://www.rfc-editor.org/rfc/rfc8878)). The postcard wire rules these types follow, for structs, length-prefixed lists, strings and byte vectors, `Option` and `bool` tags, enum variant indices and the absence of any effect of `#[serde(default)]`, are documented once, on [Postcard encoding](../change-format/container.md#postcard-encoding). The one point specific to this page is that [ObjectFamily](#objectfamily) is a unit enum, so a family is written as the varint of its index, `Change` 0 to `Conflict` 5 ([`sync.rs#L67-L86`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L67-L86)).

So, before compression, a `SyncPack` is:

```text
varint                      number of objects
  per object:
    varint                  family index (ObjectFamily)
    varint length, bytes    key (UTF-8)
    varint length, bytes    object bytes
varint                      number of refs
  per ref:
    varint length, bytes    name (UTF-8)
    0x00                    expect_old absent, or
    0x01 varint length, bytes   expect_old present
    varint length, bytes    new_target (UTF-8)
```

and a `SyncWants` is:

```text
varint                      number of refs
  per ref:   varint length, bytes   view name (UTF-8)
varint                      number of haves
  per have:  varint length, bytes   object key (UTF-8)
0x00 or 0x01                refs_only
```

Two examples, before compression. They were computed with postcard 1.1.3 from the field definitions cited here; they are not taken from Atomic's tests.

- `SyncWants::advertise(vec!["dev"])`: `01 03 64 65 76 00 01`.
- A pack with one `View` object keyed `bb` with bytes `{}`, a genesis ref `dev` → `bb` and a ref `f` moving from `bb` to `cc`:

  ```text
  01                 1 object
  04 02 62 62        family View, key "bb"
  02 7b 7d           bytes "{}"
  02                 2 refs
  03 64 65 76 00     name "dev", expect_old absent
  02 62 62           new_target "bb"
  01 66 01 02 62 62  name "f", expect_old "bb"
  02 63 63           new_target "cc"
  ```

### SyncWants

The pull and clone request body ([`sync.rs#L206-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L206-L225)). It is encoded like [SyncPack](#syncpack).

| Field | Type | Required | Meaning |
|---|---|---|---|
| `refs` | list of strings | **Required** | View names to fetch; empty means every view on the remote |
| `haves` | list of strings | **Required** | Object keys of any family the client already has; the server is to treat it as a set |
| `refs_only` | bool | **Required** | `true` asks for only the view refs and their view-snapshot objects, for the requested views and their ancestor chains: the ref advertisement a push uses. `false` is a full pull or clone. |

`SyncWants::all(haves)` wants every view with `refs_only` false; `SyncWants::advertise(refs)` sends no `haves` and `refs_only` true ([`sync.rs#L227-L245`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L227-L245)).

### ObjectRecord

One content-addressed object in transit ([`sync.rs#L129-L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L129-L141)).

| Field | Type | Required | Meaning |
|---|---|---|---|
| `family` | [ObjectFamily](#objectfamily) | **Required** | Which family the object belongs to; the comment says it is trusted only for routing |
| `key` | string | **Required** | The object's key; see the table below |
| `bytes` | byte vector | **Required** | The object's bytes |

The comments say `key` is the lowercase-hex Blake3 of `bytes` and that the receiver verifies that before storing ([`sync.rs#L129-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L129-L138), [`lib.rs#L39-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/lib.rs#L39-L40), [`lib.rs#L53-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/lib.rs#L53-L57)). The producers in this repository use that rule only for view snapshots:

| Family | Key the producers use | Bytes | Source |
|---|---|---|---|
| `Change` | The change hash, base32 | The change file exactly as stored on disk, or the change re-serialized when no file is on disk | [`services_sync_remote.rs#L375-L399`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L375-L399), [`services_sync_remote.rs#L689-L694`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L689-L694) |
| `View` | Lowercase-hex Blake3 of the snapshot's canonical JSON | [ViewSnapshot](#viewsnapshot) canonical JSON | [`services_sync_remote.rs#L704-L710`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L704-L710), [`view_snapshot.rs#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L113-L117) |
| `Attest` | The attestation hash, base32 | 4-byte magic and postcard payload | [`services_sync_remote.rs#L741-L748`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L741-L748), [`attestation.rs#L389-L403`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/attestation.rs#L389-L403) |
| `Provenance` | The provenance-graph hash, base32 | 4-byte magic and postcard payload | [`services_sync_remote.rs#L778-L785`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L778-L785), [`mod.rs#L259-L273`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance_graph/mod.rs#L259-L273) |
| `Tag` | The tag's content hash, base32 | Postcard of the tag record | [`services_sync_remote.rs#L797-L808`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L797-L808), [`tags.rs#L14-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/tags.rs#L14-L18) |

The clone test builds a pack the same way: the change keyed by its base32 hash, the provenance graph and attestation by the base32 Blake3 of their bytes, the snapshot by its content key ([`clone_sidecar_integration_test.rs#L66`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/clone_sidecar_integration_test.rs#L66), [`clone_sidecar_integration_test.rs#L72-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/clone_sidecar_integration_test.rs#L72-L73), [`clone_sidecar_integration_test.rs#L82-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/clone_sidecar_integration_test.rs#L82-L83), [`clone_sidecar_integration_test.rs#L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/tests/it/clone_sidecar_integration_test.rs#L100)). Whether the server checks keys per family is an [open question](#open-questions). The two text forms are on [Base32 hash text](../types.md#base32-hash-text) and [Lowercase hex](../types.md#lowercase-hex).

### ObjectFamily

The object families that ride the transport, in declaration order, which is also their postcard index ([`sync.rs#L67-L86`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L67-L86)). Each maps to a storage key segment ([`sync.rs#L88-L101`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L88-L101)):

| Index | Variant | Segment | Meaning |
|---|---|---|---|
| 0 | `Change` | `changes` | A `.change` object |
| 1 | `Tag` | `tags` | A `.tag` object, a named signed state |
| 2 | `Provenance` | `provenance` | A `.provenance` object, the AI and decision graph sidecar for a change |
| 3 | `Attest` | `attest` | An `.attest` object, an attestation sidecar |
| 4 | `View` | `views` | A [ViewSnapshot](#viewsnapshot) |
| 5 | `Conflict` | `conflicts` | A `.conflict` object, a view's graph-backed conflict state |

The segments are described as stable on the wire ([`sync.rs#L89-L91`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L89-L91)).

### RefRecord

A view-ref compare-and-swap in a push, or a current target in a pull response ([`sync.rs#L155-L170`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L155-L170), [`sync.rs#L172-L175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L172-L175)).

| Field | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | **Required** | The view name |
| `expect_old` | optional string | **Required** on the wire (the `Option` tag byte) | The target the client believes the ref holds, or none for a ref it believes does not exist yet. Unused in pull responses. |
| `new_target` | string | **Required** | Key of the view snapshot the ref should point at |

On push the daemon sets `expect_old` to the target the remote advertised for that view, or none if it advertised none ([`services_sync_remote.rs#L699-L715`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L699-L715)).

### ViewSnapshot

An immutable snapshot of a view's identity, lineage and membership, carried as a `View` object and keyed by the Blake3 of its canonical JSON ([`view_snapshot.rs#L1-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L1-L14), [`view_snapshot.rs#L49-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L49-L82)).

| Field | JSON type | Required | Meaning |
|---|---|---|---|
| `scope` | string | **Required** | `shared` or `draft`. A reader treats any other value as `shared` ([`view_snapshot.rs#L38-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L38-L46)). |
| `parent_view` | string | **Optional**: omitted when none | Parent view name; none for a root view |
| `prev` | array of strings | **Optional**: omitted when empty | Predecessor snapshot keys (hex Blake3); empty for the first snapshot, more than one only for a view merge |
| `own_changes` | array of strings | **Required** | Base32 change hashes in log order; see the note below |
| `own_set_id` | string | **Required** | Base32 order-invariant [SetId](../types.md#setid) of `own_changes`, computed by the producer |
| `merkle_state` | string | **Optional**: omitted when none | Base32 [Merkle state](../types.md#merkle-state), the fold of the change log; none for an empty view |

**Canonical bytes and key.** The bytes are `serde_json::to_vec` of the struct: compact JSON, members in the order of the table, absent members left out ([`view_snapshot.rs#L106-L111`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L106-L111), [`view_snapshot.rs#L250-L258`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L250-L258)). The key is the lowercase hex of the 32-byte Blake3 digest of those bytes ([`view_snapshot.rs#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L113-L117), [`lib.rs#L45-L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/lib.rs#L45-L51)). A test checks that the same value always gives the same key and that the bytes round-trip ([`view_snapshot.rs#L206-L216`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L206-L216)). A Go client that mints snapshots must reproduce serde_json's bytes exactly to get the same key: no whitespace, and only `"`, `\` and control characters escaped (serde_json 1.0.154, [`Cargo.lock#L3950-L3951`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L3950-L3951); external, see its [escape table](https://docs.rs/crate/serde_json/1.0.154/source/src/ser.rs)). Go's `encoding/json` escapes `<`, `>` and `&` unless HTML escaping is turned off ([`Encoder.SetEscapeHTML`](https://pkg.go.dev/encoding/json#Encoder.SetEscapeHTML)), and always escapes U+2028 and U+2029 ([`Marshal`](https://pkg.go.dev/encoding/json#Marshal)), so a Go client should write these bytes itself. Blake3 is the external `blake3` crate 1.8.7 ([`Cargo.lock#L656-L657`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L656-L657)).

For example, a shared root view with one change serializes as follows; this is derived from the struct definition, not copied from a test:

```json
{"scope":"shared","own_changes":["C1"],"own_set_id":"SETID","merkle_state":"MERKLE"}
```

**What `own_changes` holds.** Both producers, the `PushChanges` handler and the CLI-local push body, mint the snapshot from the view's [ViewManifest](#viewmanifest) and copy the manifest's whole change list into `own_changes`, and fold `own_set_id` from the same list ([`services_sync_remote.rs#L346-L373`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L346-L373), [`command.rs#L89-L94`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/command.rs#L89-L94)). The handler takes that manifest from `Repository::view_manifest`, changing only its name to the remote view name ([`services_sync_remote.rs#L548-L551`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L548-L551)), and `view_manifest` writes the view's stored log as `iter_changes(&view, 0)` returns it ([`views.rs#L892-L917`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/views.rs#L892-L917)). The `view_manifest` doc comment and the manifest module say that for a draft this stored log includes the prefix inherited from the parent at fork time ([`views.rs#L841-L845`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/views.rs#L841-L845), [`manifest.rs#L10-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L10-L14)). Two other comments say otherwise: the `ViewSnapshot` field comment says a draft's `own_changes` holds only the draft's own changes, the effective set being composed on read through `parent_view` ([`view_snapshot.rs#L70-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L70-L73)), and a comment inside `view_manifest` says the manifest "still carries this view's own log" ([`views.rs#L866-L868`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/views.rs#L866-L868)). This page documents the code: `own_changes` is the manifest's log, unchanged. The CLI's comment says the server mints snapshots from manifests the same way, so that both sides produce byte-identical objects ([`command.rs#L87-L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/push/command.rs#L87-L88)). The disagreement is listed once, under [Open questions](#open-questions).

The client reads a snapshot back into manifest text with `to_manifest_text`, writing `own_changes` as the log ([`view_snapshot.rs#L176-L188`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L176-L188)); the daemon does this for every advertised ref and then verifies the result as a manifest ([`services_sync_remote.rs#L190-L216`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L190-L216)).

### PushSummary

The JSON body of a successful [`sync_push`](#sync_push) ([`code_sync.rs#L25-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L25-L34)).

| Field | Type | Required | Meaning |
|---|---|---|---|
| `stored` | unsigned integer | **Optional**, default 0 | Number of objects stored or already present |
| `refs_moved` | unsigned integer | **Optional**, default 0 | Number of view refs the CAS batch moved |

The test peer answers `{"stored": <objects in the pack>, "refs_moved": <refs in the pack>}` with `Content-Type: application/json` ([`local_sync_test.rs#L191-L206`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/local_sync_test.rs#L191-L206)).

### ViewManifest

The text format of [`get_view_manifest`](#get_view_manifest) and [`put_view_manifest`](#put_view_manifest), owned by atomic-repository ([`manifest.rs#L16-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L16-L31)):

```text
name\tscope\tparent\tstate
<change hash, base32>
<change hash, base32>
...
```

- The header has four tab-separated fields: `name`; `scope`, `shared` or `draft`; `parent`, a view name or `-` for a root view; `state`, the base32 Merkle fold of the log or `-` for an empty view ([`manifest.rs#L28-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L28-L31)).
- Each following line is one base32 change hash, in log order. The module and field comments say that for a draft the log includes the prefix inherited from its parent, which other comments dispute (see [`own_changes`](#viewsnapshot) and [Open questions](#open-questions)) ([`manifest.rs#L10-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L10-L14), [`manifest.rs#L94-L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L94-L96)).
- The writer writes scope in lower case, `-` for a missing parent and `-` for the state of an empty log, and ends every line, the header included, with `\n` ([`manifest.rs#L144-L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L144-L164)).
- The reader skips blank lines before the header and fails with `Empty` if there is none ([`manifest.rs#L174-L180`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L174-L180)). It needs at least four header fields, a non-empty name and scope, and trims each field; otherwise it fails with `MalformedHeader` carrying the header line ([`manifest.rs#L182-L201`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L182-L201)). It matches the scope in any case, failing with `UnknownScope` on anything but `shared` or `draft`, accepts an empty `parent` or `state` as none, and fails with `InvalidState` when `state` is neither empty, `-` nor a base32 Merkle ([`manifest.rs#L203-L217`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L203-L217), [`manifest.rs#L233-L239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L233-L239)). It trims each log line, skips blank ones, and fails with `InvalidHash` on a line that is not a base32 hash, numbering lines from 1 with the header as line 1, blank lines after it counted ([`manifest.rs#L219-L231`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L219-L231)). Header fields after the fourth are ignored ([`manifest.rs#L166-L170`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L166-L170)).
- The error type lists these variants ([`manifest.rs#L45-L78`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L45-L78)). Parsing checks structure only. `verify` requires the declared `state` to equal the fold of the log, else `StateMismatch` with both values, `state_n = H(state_{n-1} || hash_n)` from the zero Merkle ([`manifest.rs#L3-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L3-L6), [`manifest.rs#L104-L112`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L104-L112), [`manifest.rs#L131-L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L131-L142)); see [Merkle state](../types.md#merkle-state).

`ViewSnapshot::to_manifest_text` writes the same layout from a snapshot ([`view_snapshot.rs#L179-L188`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L179-L188)); its test expects `feature\tdraft\tdev\tMERKLE\n` followed by one placeholder hash per line ([`view_snapshot.rs#L260-L267`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L260-L267)).

### LayerSelection

The value of the `layers` query parameter of [`download_change_layers`](#download_change_layers) ([`layers.rs#L16-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L16-L20), [`download.rs#L114-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L114-L115)). It is a set of [Layer](../types.md#layer) values ([`layers.rs#L42-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L42-L46)).

`to_query_value` writes `all` when all three layers are selected, and otherwise the selected names joined by `,` in the fixed order `graph`, `semantic`, `content` ([`layers.rs#L160-L177`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L160-L177)). An empty selection writes the empty string, so the request ends in `layers=`. The presets:

| Preset | Query value |
|---|---|
| `all()`, the default | `all` |
| `thin_pull()` | `graph,content` |
| `thin_review()` | `semantic,content` |
| `graph_only()` | `graph` |

([`layers.rs#L22-L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L22-L29), [`layers.rs#L152-L159`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L152-L159), [`layers.rs#L209-L214`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L209-L214)). The reverse, `from_query_value`, is for a server or a test: it trims the value, reads `all` in any case as every layer, otherwise splits on `,`, trims each name, matches it in any case and drops unknown names ([`layers.rs#L131-L143`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L131-L143), [`types.rs#L52-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/types.rs#L52-L59)).

The doc comment says HEADER, DEPS and PROVENANCE sections are included whatever the selection ([`layers.rs#L18-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L18-L20)); that is a statement about the server.

### ChunkManifest

The JSON body of [`get_chunk_manifest`](#get_chunk_manifest): an ordered list of a change's content chunks ([`layers.rs#L226-L231`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L226-L231), [`layers.rs#L259-L263`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L259-L263)).

| Field | Type | Required | Meaning |
|---|---|---|---|
| `entries` | array of [ChunkManifestEntry](../types.md#chunkmanifestentry) | **Required** | The chunks, in order. An empty array is a change with no content chunks ([`layers.rs#L271-L276`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L271-L276)). |

The wire example in the doc comment ([`layers.rs#L232-L243`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L232-L243)):

```json
{
  "entries": [
    {"index": 0, "hash": "aabb...", "compressed_size": 32000, "uncompressed_size": 65536},
    {"index": 1, "hash": "ccdd...", "compressed_size": 28000, "uncompressed_size": 65536}
  ]
}
```

Each `hash` is 64 hex characters: the client writes lower case and checks the length on read ([`types.rs#L168-L181`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/types.rs#L168-L181), [`types.rs#L187-L193`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/types.rs#L187-L193)); the entry fields are on [ChunkManifestEntry](../types.md#chunkmanifestentry). How chunks are cut is on [the change-file container](../change-format/container.md).

### IdentityResponse

The JSON body of a 200 [`fetch_project_identity`](#fetch_project_identity) response. The client's struct is private ([`identity.rs#L118-L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L118-L127)).

| Member | Type | Required | Meaning |
|---|---|---|---|
| `bound` | boolean | **Optional** | Whether the project has a binding. Absent means an older server. |
| `identity` | object | **Optional**; needed when `bound` is true | The bound identity document, returned to the caller as-is |
| `hash` | string | **Ignored** | Sent by the test server (64 hex characters); the client does not read it ([`project_identity_transport.rs#L69`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L69)) |

### BindServerError

The JSON body of a `409` [`bind_project_identity`](#bind_project_identity) response ([`identity.rs#L39-L50`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L39-L50)).

| Member | Type | Required | Meaning |
|---|---|---|---|
| `code` | string | **Optional** | Machine-readable reason, such as `IDENTITY_CONFLICT` |
| `message` | string | **Optional** | Human-readable reason, quoted to the operator |

The client accepts the members at the top level or nested under `error`. If the body is not JSON it records no server error; a member that is not a string counts as absent ([`identity.rs#L52-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L52-L73)). The tests use both spellings ([`project_identity_transport.rs#L133-L137`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L133-L137), [`project_identity_transport.rs#L167-L171`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L167-L171)):

```json
{"error": {"code": "IDENTITY_CONFLICT"}}
{"code": "IDENTITY_CONFLICT", "message": "project already bound"}
```

## Open questions

- **Server behaviour.** Every statement here about what the server does comes from client comments or from test servers written for the client ([`local_sync_test.rs#L104-L207`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/local_sync_test.rs#L104-L207), [`project_identity_transport.rs#L26-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L26-L58)). The server (atomic-api / atomic-storage) is not in the repository, so status codes, bodies and validation rules beyond what the client parses are unconfirmed.
- **Are the query-string endpoints still served?** Neither the CLI nor the daemon calls them, except `download_change` ([Which methods have callers](#which-methods-have-callers)), the `bare` module says the per-object REST transport was retired ([`bare.rs#L3-L7`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L3-L7)), and the design document lists `?view-manifest` as replaced ([`view-snapshot-sync-design.md#L356-L357`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/docs/view-snapshot-sync-design.md?plain=1#L356-L357)). Whether a current server still answers `?insert=`, `?store=`, `?view-manifest=`, `?tagup=`, `?fork_from=` and the reads is not stated.
- **Unencoded query values.** The client does not percent-encode view names or hashes in the query ([How a request is addressed](#how-a-request-is-addressed)). How the server decodes such values is kept once, under [Open questions](../transport.md#open-questions) on transport.md.
- **Base URL shape.** The source shows three shapes of project URL; whether servers still serve the older ones is kept once, under [Open questions](../transport.md#open-questions) on transport.md.
- **Sync request without the header.** The objects module says `…/code` without `Atomic-Protocol` answers plain JSON, a project-info ping or browse response ([`sync.rs#L9-L12`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L9-L12)); the shape of that JSON is not defined in the source beyond the test's `{"workspace","project","stack"}` ([`project_identity_transport.rs#L96-L99`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/tests/project_identity_transport.rs#L96-L99)).
- **GET with a body.** `sync_pull` sends its body on a `GET`; how servers and proxies treat that is kept once, under [Open questions](../transport.md#open-questions) on transport.md.
- **Push status codes.** The client accepts `200` and `201` for a push and maps only `409` specially ([`code_sync.rs#L57-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/code_sync.rs#L57-L71)). Which status the server uses for a missing view snapshot, a key that fails verification or a missing dependency is not in the source; the test peer uses `400` for malformed packs and missing snapshots ([`local_sync_test.rs#L173-L189`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/local_sync_test.rs#L173-L189)).
- **Object keys.** `ObjectRecord`'s comment says every key is the lowercase-hex Blake3 of the bytes and that the receiver verifies that ([`sync.rs#L129-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/sync.rs#L129-L138)), and the integrity helper checks exactly that, comparing strings ([`lib.rs#L53-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/lib.rs#L53-L57)). The producers follow it only for `View` objects; they key `Change`, `Attest`, `Provenance` and `Tag` objects by base32 Atomic hashes ([ObjectRecord](#objectrecord)), and the client's closure fetch requires a base32 key for a change ([`binding_fetch.rs#L162-L187`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/binding_fetch.rs#L162-L187)). Whether the server verifies keys, and by which rule per family, is not stated.
- **`own_changes` for drafts.** The producers copy a view's stored log into `own_changes` unchanged ([ViewSnapshot](#viewsnapshot)). Comments disagree on what that log is for a draft and on what `own_changes` should hold: the manifest module and the `view_manifest` doc comment say a draft's log includes the inherited prefix ([`manifest.rs#L10-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/manifest.rs#L10-L14), [`views.rs#L841-L845`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/views.rs#L841-L845)), an inline comment in `view_manifest` says the manifest carries the view's own log ([`views.rs#L866-L868`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/views.rs#L866-L868)), and the `ViewSnapshot` comment says a draft's `own_changes` lists only its own changes ([`view_snapshot.rs#L70-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-objects/src/view_snapshot.rs#L70-L73)). Copying the log unchanged keeps the daemon's round trip through `to_manifest_text` and `verify` consistent ([`services_sync_remote.rs#L190-L243`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L190-L243)). Whether a draft's stored log really holds the inherited prefix at this commit, which form the server expects, and how it computes effective sets from it, are not settled by the source. This page documents what the producers send.
- **Layer-filtered downloads.** Nothing in the repository produces a `?layers=` response, so whether it is a valid V3 container with sections left out, or another format, is unknown. Whether the server always includes HEADER, DEPS and PROVENANCE, as the `Layer` and `LayerSelection` comments say ([`types.rs#L23-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/types.rs#L23-L24), [`layers.rs#L18-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L18-L20)), and what it does with the empty `layers=` that an empty [LayerSelection](#layerselection) produces ([`layers.rs#L165-L177`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/streaming/layers.rs#L165-L177)), are also the server's decision ([`download.rs#L105-L108`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L105-L108)).
- **Short tag format.** `?tag=` returns an 8-byte length and "short tag data", and `?tagup=` uploads it ([`queries.rs#L240-L261`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L240-L261), [`upload.rs#L202-L208`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L202-L208)). No code in the repository builds or parses the short data, so its layout is unknown. The `/code` protocol carries tags as postcard tag records instead ([`tags.rs#L14-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/tags.rs#L14-L18)).
- **`changelist` start.** Whether `?changelist={from}` includes the entry at `from` is not stated ([`queries.rs#L74-L78`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L74-L78)).
- **`?id`.** The doc comment says the body is the view's UUID ([`queries.rs#L121-L129`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L121-L129)); the client does not check it and nothing calls it.
- **`download_change_to_file` buffering.** The doc comment says the method avoids holding the whole change in memory ([`download.rs#L18-L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L18-L22)), but the code reads the whole body before writing it ([`download.rs#L48-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/download.rs#L48-L65)). This affects only the client.
- **Unsupported provenance list.** The doc comment says servers without the extension answer with an error status that callers should treat as "unsupported" ([`queries.rs#L506-L508`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L506-L508)); the code turns only `404` into an empty list and every other error status into `HttpError` ([`queries.rs#L540-L548`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/queries.rs#L540-L548)).
- **Missing-dependency detection.** The client recognises missing dependencies by substrings of the error body (`missing` with `dependenc` or `change`) and pulls out every hash-like token, including the dependent change ([`upload.rs#L68-L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L68-L80), [`upload.rs#L184-L190`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L184-L190), [`upload.rs#L536-L552`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/upload.rs#L536-L552)). The server's message format is not specified, so this is a heuristic.
- **Identity document bytes and `hash`.** `bind_project_identity` re-serializes a parsed `serde_json::Value`, so the member order on the wire depends on serde_json's map type ([`identity.rs#L171-L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/identity.rs#L171-L172)), while the local hash is computed over the JCS canonical form ([`project_identity.rs#L27-L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/project_identity.rs#L27-L33)). Whether the server canonicalizes before hashing, and what the `hash` in its response is computed over, is not stated.
- **`list_view_refs` and authentication.** It reports `401` and `403` as `HttpError`, not `AuthenticationFailed` ([`bare.rs#L61-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-remote/src/http/bare.rs#L61-L64)). Whether the inventory needs credentials at all is not stated.

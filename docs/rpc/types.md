# Shared types

This page documents every message and enum declared under `proto/atomic/common/`, except `ErrorCode` and `ErrorInfo`, which are on the [errors page](errors.md#errorcode). All of them are in protobuf package `atomic` ([`common.proto#L36-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L36-L38)). The files are marked as drafts: they are add-only, and field numbers are never reused or renumbered ([`common.proto#L3`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L3)).

Each section separates the contract (the `.proto` declaration and its comments) from what the handlers in `src/daemon/` actually do. Where they disagree, the handler behaviour is stated and the difference is listed under [Open questions](#open-questions).

The general contract rules in `common.proto` apply to all of these types: mutations carry an idempotency key, observation times are supplied by the caller, remote clients address repositories by persistent ID and never by path, no storage tables or transaction handles cross the wire, and a client that cannot reach the service fails rather than opening storage itself ([`common.proto#L7-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L7-L34)).

## common.proto

### ContractVersion

The contract revision constant, shared by `GetCapabilities` and the `x-atomic-contract-version` request header ([`common.proto#L42-L48`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L42-L48)).

| Value | # | Meaning |
|---|---|---|
| `CONTRACT_VERSION_UNSPECIFIED` | 0 | Default value. |
| (reserved) | 1 | Revision 1 is retired; its draft behaviour is not wire-compatible with revision 2 ([`common.proto#L43-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L43-L46)). |
| `CONTRACT_VERSION_2` | 2 | The current revision ([`common.proto#L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L47)). `GetCapabilities` returns `protocol_version: 2` ([`services.rs#L216-L217`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L216-L217)). |

How a client uses it is described in [Version negotiation](transport.md#version-negotiation).

### RepositoryRef

A globally qualified reference to one repository. One daemon serves many repository databases, and every repository-scoped request names exactly one repository with this message ([`common.proto#L50-L66`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L50-L66)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `authority` | 1 | `string` | The local daemon (`"local"`) or a configured hosted authority ([`common.proto#L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L63)). |
| `repository_id` | 2 | `bytes` | Persistent repository ID, never a path ([`common.proto#L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L64)). |
| `workspace_id` | 3 | `optional string` | The contract says it is required for working-copy operations ([`common.proto#L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L65)). |

Contract: IDs are persistent, assigned at init or by `DaemonService.RegisterRepository`, and recorded in the working copy's `.atomic` directory; remote clients use registered IDs only ([`common.proto#L54-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L54-L61)). Every repository request opens with `RepositoryRef repository = 1` ([`README.md#L77-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/README.md?plain=1#L77-L83)).

What libatomic does:

- Every `RepositoryRef` a handler issues has `authority: "local"`, a `repository_id` equal to the 32-byte BLAKE3 digest of the canonical repository root path, and no `workspace_id` ([`state.rs#L66-L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L66-L75), [`state.rs#L87-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L87-L93)). The handler comment calls the path digest a "slice limitation" against the RFC's init-assigned IDs ([`state.rs#L66-L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L66-L70)).
- A request is resolved against an in-memory registry. An `authority` other than `"local"` fails with `ERROR_CODE_INVALID_ARGUMENT`, and an ID that is not registered fails with `ERROR_CODE_REPOSITORY_NOT_FOUND` ("resolve it by path first"); there is no filesystem fallback ([`state.rs#L329-L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L329-L348)). The registry is a field of `DaemonState` and starts empty ([`state.rs#L284-L299`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L284-L299)), so a reference is valid only on the state that registered it. Obtain one with [ResolveRepository](services/daemon.md#resolverepository); see [Repository resolution](transport.md#repository-resolution).
- No non-sandbox handler reads `workspace_id`. A sandbox request that sets it is refused with `ERROR_CODE_FORBIDDEN` ([`sandbox_grants.rs#L509-L520`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L509-L520)).

### ViewRef

A stable, opaque view identity. The name is a display hint; the contract says authorization uses `view_id`, recreating a name never revives a grant, and handlers must verify that a supplied name agrees with the ID ([`common.proto#L68-L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L68-L74)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `view_id` | 1 | `bytes` | Opaque view ID ([`common.proto#L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L72)). |
| `name` | 2 | `optional string` | Display name of the view ([`common.proto#L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L73)). |

What libatomic does:

- The handlers issue `view_id` as the BLAKE3 digest of the view name ([`sandbox_grants.rs#L558-L561`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L558-L561), [`sandbox_wire.rs#L65-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L65-L71)); `ResolveRepository` computes the current view's ID the same way ([`services.rs#L267-L272`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L267-L272)).
- The write-path fences compare views by `name`, falling back to the target view when `name` is absent, and do not read `view_id` ([`services.rs#L1239-L1249`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1239-L1249)). Sandbox target checks compare both the name and, when non-empty, the ID ([`sandbox_grants.rs#L527-L530`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L527-L530)).

### ViewSnapshot

A coherent effective perspective of a view: repository and target identity and scope, contributing ancestors, the visible dependency closure and the view-scoped vault revision. Servers derive it; callers echo it and never construct it. It is not a transaction handle ([`common.proto#L76-L85`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L76-L85)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `view` | 1 | [`ViewRef`](#viewref) | The view the snapshot is of ([`common.proto#L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L82)). |
| `effective_state` | 2 | [`Hash`](#hash) | Server-derived digest of the effective state; the fence ([`common.proto#L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L83)). |
| `own_merkle` | 3 | optional [`Hash`](#hash) | The view's own Merkle state; the contract calls it diagnostic and insufficient as an effective fence ([`common.proto#L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L84)). |

What libatomic does:

- Handlers build snapshots from the view name, its effective state and, optionally, its own Merkle state ([`sandbox_wire.rs#L73-L81`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L73-L81)).
- libatomic has three snapshot fences, and they compare different fields. Every one reports a stale snapshot with `ERROR_CODE_VIEW_STALE`, gRPC `FAILED_PRECONDITION` ([`state.rs#L435-L439`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L435-L439)).

  | Fence | Used by | What it compares |
  |---|---|---|
  | `check_record_fence` | [Record](services/repository-mutation.md#record), before and after workspace entry ([`services.rs#L1539`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1539), [`services.rs#L1571`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1571)) | `ExpectedState.merkle_root`, or a `view_snapshot`'s `own_merkle` and, when that is absent, its `effective_state`, against the view's own Merkle state; other kinds are refused with `ERROR_CODE_INVALID_ARGUMENT` ([`services.rs#L1178-L1224`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1178-L1224)). An `effective_state` is therefore compared with the own state. |
  | `check_view_fence` | [InsertChanges](services/repository-mutation.md#insertchanges), all arms including the promotion, before and after workspace entry ([`services.rs#L2285`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L2285), [`services.rs#L2308`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L2308)) | `own_merkle` against the view's own root when present, otherwise `effective_state` against the view's effective state ([`services.rs#L1256-L1275`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1256-L1275)). Its doc comment also names the vault writes ([`services.rs#L1228-L1230`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1228-L1230)), but no vault handler calls it; the vault writes ignore `expected_snapshot` ([VaultService](services/vault.md#behaviour-common-to-every-method)). |
  | `snapshot_fence` | SandboxService data operations ([Snapshots and fencing](services/sandbox.md#snapshots-and-fencing)) | `effective_state` only ([`sandbox_wire.rs#L83-L87`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L83-L87)). |

  Both repository fences name the view by `ViewRef.name`, defaulting to the target ([`services.rs#L1181-L1186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1181-L1186), [`services.rs#L1239-L1243`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1239-L1243)). The first two narrow the contract: a snapshot that carries `own_merkle` is fenced on the own-log state, which the contract says is not sufficient. Many other methods declare a snapshot or `ExpectedState` field and do not read it, for example [CreateTag](services/tag.md#createtag), which reads only `meta`, `name`, `message` and `force` ([`services_tag.rs#L80-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L80-L83)); each method page marks such fields **Ignored**.
- `CONTRACT.md` says view-dependent mutations require the matching snapshot, and rejecting an absent one is a domain precondition despite proto3 optionality ([`CONTRACT.md#L312-L316`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L312-L316)).

### HashAlgorithm

| Value | # | Meaning |
|---|---|---|
| `HASH_ALGORITHM_UNSPECIFIED` | 0 | Default value. |
| `HASH_ALGORITHM_BLAKE3` | 1 | Atomic's content hash, a 32-byte digest ([`common.proto#L87-L90`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L87-L90)). |

### Hash

A content or state hash. The contract says it is always BLAKE3-32 today ([`common.proto#L92-L97`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L92-L97)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `value` | 1 | `bytes` | The digest bytes ([`common.proto#L95`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L95)). |
| `algorithm` | 2 | [`HashAlgorithm`](#hashalgorithm) | The digest algorithm ([`common.proto#L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L96)). |

What libatomic does: every hash a handler returns has `algorithm: HASH_ALGORITHM_BLAKE3` ([`convert.rs#L7-L12`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/convert.rs#L7-L12)). The shared decoder accepts `HASH_ALGORITHM_BLAKE3` or `HASH_ALGORITHM_UNSPECIFIED` and requires exactly 32 bytes ([`sandbox_wire.rs#L37-L48`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L37-L48)). Some handlers check only the length, for example `GetChange` resolving a `ChangeRef` by hash ([`services_query.rs#L335-L345`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_query.rs#L335-L345)).

### Identity

A verified identity; Atomic uses DIDs ([`common.proto#L99-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L99-L103)). [EventEnvelope](#eventenvelope) uses it for `source` and `authenticated_principal` ([`event.proto#L26-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L26-L27)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `did` | 1 | `string` | The DID ([`common.proto#L101`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L101)). |
| `display_name` | 2 | `optional string` | Human-readable name ([`common.proto#L102`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L102)). |

### RequestMeta

The common mutation header: an idempotency key and the caller-supplied observation time ([`common.proto#L105-L109`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L105-L109)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `request_id` | 1 | `string` | UUID v4, echoed by the server ([`common.proto#L107`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L107)). |
| `observed_at` | 2 | `optional google.protobuf.Timestamp` | The caller's "now" ([`common.proto#L108`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L108)). |

Contract: the server echoes `request_id`, detects replays and returns the original outcome; different contents under the same key fail with `ERROR_CODE_IDEMPOTENCY_MISMATCH`; replay state is scoped by authority, repository, principal or grant, method and request ID ([`common.proto#L8-L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L8-L17)). The server must not substitute its own clock for observation times, and observation time never controls expiry, authorization, deadlines or leases ([`common.proto#L18-L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L18-L22)). `CONTRACT.md` requires a non-empty UUID `request_id` on ordinary mutations ([`CONTRACT.md#L54-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L54-L59)).

What libatomic does: no replay detection exists (see [ResponseMeta](#responsemeta)). Only `MaintenanceService.CompactDatabase` validates that `request_id` is a UUID ([`compact.rs#L35-L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_maintenance/compact.rs#L35-L44)). The provenance handlers read `observed_at` as whole seconds and use 0 when it is absent ([`services_provenance.rs#L155-L163`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_provenance.rs#L155-L163)). See [Request metadata and idempotency](transport.md#request-metadata-and-idempotency).

### ResponseMeta

Present on every successful ordinary mutation response, at field 100 ([`common.proto#L111-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L111-L117)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `request_id` | 1 | `string` | The echoed `RequestMeta.request_id` ([`common.proto#L114`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L114)). |
| `replayed` | 2 | `bool` | Whether the response is a replay of an earlier outcome ([`common.proto#L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L115)). |
| `snapshot` | 3 | optional [`ViewSnapshot`](#viewsnapshot) | The view snapshot after the mutation ([`common.proto#L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L116)). `CONTRACT.md` says direct RPC writes return the new snapshot here ([`CONTRACT.md#L202`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L202)). |

What libatomic does: the shared helper echoes `request_id` only when the request carried a `RequestMeta`, and otherwise returns no `ResponseMeta` ([`services_agent.rs#L1233-L1239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L1233-L1239)). Some handlers build it directly and always set it, echoing an empty ID when the request had none, for example ([`services.rs#L289-L297`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L289-L297)) and ([`compact.rs#L99-L109`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_maintenance/compact.rs#L99-L109)). Every `ResponseMeta` that libatomic constructs sets `replayed: false`, so a client never sees `replayed: true` at this commit. Only one sets `snapshot`: [SubmitChange](services/sandbox.md#submitchange) always returns `ResponseMeta` and sets `snapshot` to the view's new snapshot after an accepted submission ([`services_sandbox.rs#L806-L816`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sandbox.rs#L806-L816), [`services_sandbox.rs#L839-L843`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sandbox.rs#L839-L843)). Every other `snapshot` is unset.

### SessionTurn

A (session, turn) pair, used by provenance and agent integration ([`common.proto#L119-L123`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L119-L123)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `session_id` | 1 | `string` | Session ID ([`common.proto#L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L121)). |
| `turn_number` | 2 | `uint32` | Turn number within the session ([`common.proto#L122`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L122)). |

### VersionedBytes

Schema-tagged opaque bytes, for payloads whose encoding is owned elsewhere ([`common.proto#L125-L133`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L125-L133)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `schema` | 1 | `string` | Schema identifier, e.g. `"atomic.change.v3"` ([`common.proto#L131`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L131)). |
| `payload` | 2 | `bytes` | The encoded payload ([`common.proto#L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L132)). |

The sandbox wire decoder refuses a `VersionedBytes` whose `schema` is not the one it expects ([`sandbox_wire.rs#L57-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L57-L62)). Schema strings that libatomic's handlers write or check:

| Schema | Content | Source |
|---|---|---|
| `atomic.change.v3` | V3 change-file bytes | [`sandbox_wire.rs#L23-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L23-L24) |
| `atomic.prov.graph.v1` | Serialized provenance graph | [`sandbox_wire.rs#L25-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L25-L26) |
| `atomic.session.turn.v1` | A session turn as JSON | [`sandbox_wire.rs#L27-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L27-L28) |
| `atomic.session.checkpoint-publication.v1` | A checkpoint publication record as JSON | [`sandbox_wire.rs#L29-L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L29-L30) |
| `atomic.attestation.v1` | Serialized attestation (ATST magic + postcard) | [`sandbox_wire.rs#L31-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L31-L32) |
| `atomic.vault.entry.bundle.v1` | Vault entry bundle | [`services_agent.rs#L797`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L797) |
| `atomic.attestation.detail.v1` | Attestation detail | [`services_agent.rs#L2930`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2930) |
| `atomic.provenance.summary.v1` | Provenance summary | [`services_agent.rs#L3663`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L3663) |
| `atomic.query.ask.v1` | Knowledge `ask` result | [`services_agent.rs#L4318`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L4318) |
| `atomic.session.ledgers.v1` | Session ledger listing | [`services_agent.rs#L4734`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L4734) |
| `atomic.agent.reasoning.v1` | Agent reasoning | [`services_agent.rs#L5143`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L5143) |
| `atomic.prov.input.v1` | Provenance input | [`services_agent.rs#L5225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L5225) |
| `atomic.session.ledger.v1` | One session ledger | [`services_query.rs#L651`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_query.rs#L651) |
| `atomic.triage.candidates.v1` | Triage candidates | [`services_triage.rs#L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L113) |
| `atomic.triage.report.v1` | Triage report | [`services_triage.rs#L154`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L154) |

The method pages describe where each schema appears.

### ExpectedState

An expected-state precondition on a mutation. The contract says a mismatch fails with `ERROR_CODE_PRECONDITION_FAILED`, or `ERROR_CODE_GENERATION_CONFLICT` for provenance generation fencing ([`common.proto#L135-L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L135-L146)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `generation` | 1 | `uint64` (oneof `kind`) | Provenance or attempt generation fencing ([`common.proto#L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L141)). |
| `merkle_root` | 2 | [`Hash`](#hash) (oneof `kind`) | View state as the caller last saw it ([`common.proto#L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L142)). |
| `sequence` | 3 | `uint64` (oneof `kind`) | Sequence-number fencing ([`common.proto#L143`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L143)). |
| `view_snapshot` | 4 | [`ViewSnapshot`](#viewsnapshot) (oneof `kind`) | Required for effective-view mutations ([`common.proto#L144`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L144)). |

What libatomic does: which kinds a method accepts is decided per method. For example, the `Record` fence accepts only `merkle_root` or `view_snapshot`, refuses the other kinds with `ERROR_CODE_INVALID_ARGUMENT`, and reports a mismatch with `ERROR_CODE_VIEW_STALE`, not `ERROR_CODE_PRECONDITION_FAILED` ([`services.rs#L1172-L1226`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1172-L1226)). See the method pages, for example [Record](services/repository-mutation.md#record).

### ChangeRef

How a request names a change: by hash, by log sequence (the CLI's `#N`), or by a unique hash prefix ([`common.proto#L148-L158`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L148-L158)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `hash` | 1 | [`Hash`](#hash) (oneof `kind`) | The change hash ([`common.proto#L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L152)). |
| `sequence` | 2 | `uint64` (oneof `kind`) | Position in the current view's log ([`common.proto#L148-L153`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L148-L153)). |
| `prefix` | 3 | `string` (oneof `kind`) | Case-insensitive unique prefix, resolved against the whole change store ([`common.proto#L154-L156`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L154-L156)). |

What libatomic does: `GetChange` resolves all three kinds. A sequence is looked up in the request's view or the current view; a prefix is trimmed and upper-cased, and no match or an ambiguous match fails with `ERROR_CODE_NOT_FOUND` ([`services_query.rs#L334-L395`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_query.rs#L334-L395)). Other methods narrow the kinds they accept; `Revise` refuses a sequence with `ERROR_CODE_INVALID_ARGUMENT` ([`services.rs#L2151-L2172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L2151-L2172)). See [GetChange](services/repository-query.md#getchange) and [Revise](services/repository-mutation.md#revise).

### ChangeBundle

The wire form of a change interchange object, today V3 change-file bytes ([`common.proto#L160-L167`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L160-L167)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `hash` | 1 | [`Hash`](#hash) | The change hash ([`common.proto#L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L164)). |
| `schema` | 2 | `string` | Payload schema, e.g. `"atomic.change.v3"` ([`common.proto#L165`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L165)). |
| `payload` | 3 | `bytes` | The change bytes ([`common.proto#L166`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L166)). |

What libatomic does: bundles it returns have `schema: "atomic.change.v3"` ([`sandbox_wire.rs#L308-L314`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L308-L314)). On input an empty `schema` is read as V3, and any other schema is refused ([`sandbox_wire.rs#L316-L321`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_wire.rs#L316-L321)).

### ChangeSignature

A signature over an attested target by an identity ([`common.proto#L169-L174`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L169-L174)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `identity_did` | 1 | `string` | Signer DID ([`common.proto#L171`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L171)). |
| `signature` | 2 | `bytes` | Signature bytes ([`common.proto#L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L172)). |
| `signed_at` | 3 | `optional google.protobuf.Timestamp` | Signing time ([`common.proto#L173`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L173)). |

It appears in `GetChangeResponse.signatures` ([`repository_query_messages.proto#L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/repository_query/repository_query_messages.proto#L119)), which the `GetChange` handler always returns empty ([`services_query.rs#L542`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_query.rs#L542)).

## change.proto

These types follow Atomic's model, not Git's: a change has no commit parents; its position in a view is a log sequence plus the Merkle state after applying it, and its graph edges are dependencies. Authors are name/email records, not DIDs ([`change.proto#L1-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L1-L14)).

### Author

A change author ([`change.proto#L23-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L23-L27)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `name` | 1 | `string` | Author name ([`change.proto#L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L25)). |
| `email` | 2 | `optional string` | Author email ([`change.proto#L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L26)). |

Handlers copy both fields from the stored change header ([`convert.rs#L14-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/convert.rs#L14-L19)).

### ChangeLogEntry

A change's position in a view's log, with the Merkle state after applying it ([`change.proto#L29-L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L29-L43)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `hash` | 1 | [`Hash`](#hash) | Change hash ([`change.proto#L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L33)). |
| `sequence` | 2 | `uint64` | Position in the view's log, the CLI's `#N` ([`change.proto#L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L34)). |
| `state` | 3 | [`Hash`](#hash) | View Merkle state after this change ([`change.proto#L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L35)). |
| `message` | 4 | `optional string` | Change message ([`change.proto#L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L36)). |
| `description` | 5 | `optional string` | Change description ([`change.proto#L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L37)). |
| `authors` | 6 | `repeated` [`Author`](#author) | Authors ([`change.proto#L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L38)). |
| `recorded_at` | 7 | `optional google.protobuf.Timestamp` | The change header's timestamp ([`change.proto#L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L39)). |
| `dependencies` | 8 | `repeated` [`Hash`](#hash) | Change-graph dependency edges ([`change.proto#L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L40)). |
| `view` | 9 | `optional string` | Which view's log the entry is from ([`change.proto#L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L41)). |
| `is_tagged` | 10 | `bool` | Whether the entry is tagged ([`change.proto#L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L42)). |

What libatomic does: the `Log` handler fills these fields from the history entry, but always returns `dependencies` empty and sets `view` to the requested view or the current view ([`services.rs#L416-L421`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L416-L421), [`services.rs#L473-L490`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L473-L490)). See [Log](services/repository-query.md#log).

### ChangeInfo

Change metadata: header, dependency hashes and section counts and flags. Sections themselves are fetched selectively ([`change.proto#L45-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L45-L64)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `hash` | 1 | [`Hash`](#hash) | Change hash ([`change.proto#L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L52)). |
| `message` | 2 | `optional string` | Change message ([`change.proto#L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L53)). |
| `description` | 3 | `optional string` | Change description ([`change.proto#L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L54)). |
| `authors` | 4 | `repeated` [`Author`](#author) | Authors ([`change.proto#L55`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L55)). |
| `recorded_at` | 5 | `optional google.protobuf.Timestamp` | Header timestamp ([`change.proto#L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L56)). |
| `dependencies` | 6 | `repeated` [`Hash`](#hash) | Dependency hashes ([`change.proto#L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L57)). |
| `graph_section_count` | 7 | `uint32` | Number of graph sections ([`change.proto#L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L58)). |
| `semantic_section_count` | 8 | `uint32` | Number of semantic sections ([`change.proto#L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L59)). |
| `content_chunk_count` | 9 | `uint32` | Number of content chunks ([`change.proto#L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L60)). |
| `has_provenance` | 10 | `bool` | The change carries provenance ([`change.proto#L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L61)). |
| `has_unhashed` | 11 | `bool` | The change has an unhashed section ([`change.proto#L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L62)). |
| `has_signature` | 12 | `bool` | The change is signed ([`change.proto#L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L63)). |

What libatomic does: `GetChange` fills the header fields, dependencies and the three flags, and sets all three section counts to 0 ([`services_query.rs#L516-L539`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_query.rs#L516-L539)). The record-response shape leaves `dependencies` empty and the counts 0 ([`services.rs#L1026-L1052`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1026-L1052)). Some previews return a hash-only `ChangeInfo` for a change they could not load ([`services.rs#L1054-L1071`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1054-L1071)).

### ConflictInfo

A working-copy conflict ([`change.proto#L66-L77`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L66-L77)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `path` | 1 | `string` | Conflicted path ([`change.proto#L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L68)). |
| `view` | 2 | `optional string` | View of the conflict ([`change.proto#L69`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L69)). |
| `base` | 3 | optional [`Hash`](#hash) | Base state ([`change.proto#L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L70)). |
| `kind` | 4 | `optional string` | Conflict kind ([`change.proto#L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L74)). |
| `line` | 5 | `optional uint64` | Marker line ([`change.proto#L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L75)). |
| `sides` | 6 | `repeated string` | Change-hash sides involved ([`change.proto#L76`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L76)). |

Fields 4 to 6 carry a `SHAPE-CONFIRM` marker: they are drafted render details not yet verified against source ([`change.proto#L71-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/change.proto#L71-L73); [`README.md#L122-L124`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/README.md?plain=1#L122-L124)). `ListConflicts` sets `view` and `base` to none, `kind` to the lower-cased conflict kind, and fills `line` and `sides` from the conflict record ([`services.rs#L588-L599`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L588-L599)). See [ListConflicts](services/repository-query.md#listconflicts).

## pagination.proto

The pagination primitive, seeded from the frozen-provenance cursor; every paginated read is meant to use the same cursor message ([`pagination.proto#L5-L12`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L5-L12)).

### PageCursor

An opaque cursor: `seq` selects an item and `offset` a position inside it when items span pages. Servers issue cursors; clients treat them as opaque and echo them back ([`pagination.proto#L18-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L18-L24)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `seq` | 1 | `uint64` | Item selector ([`pagination.proto#L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L22)). |
| `offset` | 2 | `uint64` | Position within the item ([`pagination.proto#L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L23)). |

What libatomic does: interpretation is per method, and only one method returns a continuation cursor. The methods whose messages carry a `PageCursor` or [PageBudget](#pagebudget):

| Method | `cursor` | `budget` | Continuation |
|---|---|---|---|
| [Log](services/repository-query.md#log) | `seq` is the starting sequence; `offset` is not read ([`services.rs#L425`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L425)) | `max_items` is the limit; `max_bytes` is not read ([`services.rs#L426-L430`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L426-L430)) | `next_cursor` is never set ([`services.rs#L491-L494`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L491-L494)) |
| [ListVaultEntries](services/vault.md#listvaultentries) | Only absence is accepted; a set cursor returns `UNIMPLEMENTED` ([`services_agent.rs#L2000-L2004`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2000-L2004)) | `max_items` truncates the list ([`services_agent.rs#L2038-L2041`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2038-L2041)) | `next_cursor` is never set ([`services_agent.rs#L2075-L2077`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2075-L2077), [`services_agent.rs#L2147-L2151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2147-L2151), [`services_agent.rs#L2291-L2295`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2291-L2295), [`services_agent.rs#L2330-L2334`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2330-L2334), [`services_agent.rs#L2368-L2372`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L2368-L2372)) |
| [ListAttestations](services/attestation.md#listattestations) | Not read ([`services_agent.rs#L3563-L3729`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L3563-L3729)) | Not read | `next_cursor` is never set ([`services_agent.rs#L3626`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L3626), [`services_agent.rs#L3672`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L3672), [`services_agent.rs#L3726`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L3726)) |
| [LoadFrozenEnvelopes](services/provenance.md#loadfrozenenvelopes) | `seq` and `offset` are both read ([`services_provenance.rs#L626-L632`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_provenance.rs#L626-L632)) | `max_bytes` is the page budget ([`services_provenance.rs#L633-L637`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_provenance.rs#L633-L637)) | A cursor per fragment, and `next` is always set, even on the last page ([`services_provenance.rs#L657-L677`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_provenance.rs#L657-L677)); the end is `{seq: frozen_event_count, offset: 0}`, which libatomic's reassembler treats as the end ([`provenance_core.rs#L242-L246`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/provenance_core.rs#L242-L246)) |
| [Replay](services/reactor.md#replay) | A consumer checkpoint ([`reactor_messages.proto#L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L60)) | n/a | No handler |

The pagination contract says servers return the next cursor when more data remains ([`pagination.proto#L26-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L26-L28)); `Log`, `ListVaultEntries` and `ListAttestations` do not, so a client cannot page through them.

### PageBudget

A client page-size request. Servers clamp it to the negotiated limits (`GetCapabilities.limits`) and return the next cursor when more data remains ([`pagination.proto#L26-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L26-L32)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `max_items` | 1 | `optional uint32` | Maximum items per page ([`pagination.proto#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L30)). |
| `max_bytes` | 2 | `optional uint32` | Maximum bytes per page ([`pagination.proto#L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L31)). |

What libatomic does: see the table under [PageCursor](#pagecursor). No handler clamps a budget to the advertised limits; the limits are listed under [GetCapabilities](services/daemon.md#getcapabilities).

## event.proto

### EventEnvelope

The Reactor event envelope. It is a contract draft: the `ReactorService` server is planned for Phase 3 ([`event.proto#L3-L7`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L3-L7)). libatomic has no `ReactorService` handler at this commit, so this type is documented from the contract only; see the [Reactor page](services/reactor.md#publish). Durability semantics belong to the server, but the envelope carries the identity fields they depend on: a globally unique `event_id` for deduplication, a daemon-assigned ingestion sequence, repository- and session-local sequences, and causation and correlation IDs ([`event.proto#L9-L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L9-L15)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `event_id` | 1 | `string` | Globally unique, UUID v4 ([`event.proto#L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L25)). |
| `source` | 2 | [`Identity`](#identity) | Producing agent or principal; requires explicit delegation rights ([`event.proto#L26-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L26-L28)). |
| `authenticated_principal` | 3 | [`Identity`](#identity) | Output only, derived by middleware; inbound assertions are refused ([`event.proto#L27-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L27-L28)). |
| `repository_id` | 4 | `optional bytes` | Repository; must agree with the publish request's repository when both are present ([`event.proto#L29-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L29-L31)). |
| `workspace_id` | 5 | `optional string` | Workspace ([`event.proto#L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L32)). |
| `session_id` | 6 | `optional string` | Session ([`event.proto#L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L33)). |
| `session_turn` | 7 | `optional uint32` | Turn within the session ([`event.proto#L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L34)). |
| `ingestion_sequence` | 8 | `optional uint64` | Assigned by the daemon at ingest; absent on publish ([`event.proto#L36-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L36-L37)). |
| `repository_sequence` | 9 | `optional uint64` | Repository-local sequence, assigned at ingest ([`event.proto#L36-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L36-L38)). |
| `session_sequence` | 10 | `optional uint64` | Session-local sequence, assigned at ingest ([`event.proto#L36-L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L36-L39)). |
| `occurred_at` | 11 | `google.protobuf.Timestamp` | Caller-supplied observation time ([`event.proto#L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L41)). |
| `ingested_at` | 12 | `optional google.protobuf.Timestamp` | Daemon clock at ingest ([`event.proto#L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L42)). |
| `causation_id` | 13 | `optional string` | What caused this event ([`event.proto#L13-L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L13-L15), [`event.proto#L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L44)). |
| `correlation_id` | 14 | `optional string` | What work this event belongs to ([`event.proto#L13-L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L13-L15), [`event.proto#L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L45)). |
| `type_url` | 15 | `string` | Schema or type URL of the payload family ([`event.proto#L47-L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L47-L49)). |
| `opaque` | 16 | `bytes` (oneof `payload`) | Opaque payload; stable named families are planned for Phase 3 ([`event.proto#L50-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L50-L52)). |

`CONTRACT.md` adds that inbound `authenticated_principal` and daemon-assigned sequence or time assertions are refused ([`CONTRACT.md#L340-L343`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L340-L343)).

## Method options

`options.proto` declares four custom options as extensions of `google.protobuf.MethodOptions` ([`options.proto#L57-L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L57-L70)). Every method declares all four, with the negotiation-pair exception for capabilities described below ([`README.md#L72-L76`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/README.md?plain=1#L72-L76)). The comment says the numbers may still be corrected until review and the first tagged release, and are add-only thereafter; 51001 and 51002 are permanent ([`options.proto#L3-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L3-L6)).

| Option | Extension # | Type | Meaning |
|---|---|---|---|
| `execution_scope` | 51001 | [`ExecutionScope`](#executionscope) | Intended execution scope; every method declares one ([`options.proto#L58-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L58-L59)). |
| `required_capability` | 51002 | `repeated string` | Required endpoint capabilities ([`options.proto#L60-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L60-L65)). See [Required capability](#required-capability). |
| `allowed_caller` | 51003 | `repeated` [`CallerClass`](#callerclass) | Explicit caller allowlist; missing or unspecified means deny ([`options.proto#L66-L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L66-L68)). |
| `effect` | 51004 | `repeated` [`RpcEffect`](#rpceffect) | Allowed effects ([`options.proto#L69`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L69)). |

`CONTRACT.md` defines these as four independent checks. Missing or `UNSPECIFIED` annotations fail closed; an endpoint may advertise fewer methods but cannot widen caller eligibility or effects; the advertised matrix comes from the descriptors; a client preflight does not replace server checks ([`CONTRACT.md#L13-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L13-L24)). `check_contract.py` verifies the declarations in a compiled descriptor set: a non-unspecified scope, a non-empty caller allowlist and effect list, and zero capabilities exactly for the negotiation pair ([`check_contract.py#L105-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L105-L115)). It checks the wire contract, not runtime authorization ([`check_contract.py#L2-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L2-L6)).

Because the options are protobuf extensions, they are carried in the compiled descriptor set that libatomic exports; see [Descriptor set](transport.md#descriptor-set).

Enforcement in libatomic: no handler reads these options. The only capability checks in the handlers are against sandbox grants ([`sandbox_grants.rs#L495-L504`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L495-L504)); the maintenance compaction handler states that the serving transport must enforce `LOCAL` and `maintenance.admin` before calling it ([`compact.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_maintenance/compact.rs#L22-L24)). `GetCapabilities` advertises option values that differ from the descriptors for most methods; see [GetCapabilities](services/daemon.md#getcapabilities).

### ExecutionScope

Where a method is expected to run. Scope, negotiated capabilities and authorization are three distinct checks ([`options.proto#L22-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L22-L31)).

| Value | # | Meaning |
|---|---|---|
| `EXECUTION_SCOPE_UNSPECIFIED` | 0 | Not allowed on any method ([`check_contract.py#L112`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L112)). |
| `EXECUTION_SCOPE_LOCAL` | 1 | Local daemon only: working-copy and admin operations ([`options.proto#L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L28)). |
| `EXECUTION_SCOPE_REMOTE` | 2 | Hosted server only: org policy, hosted grants ([`options.proto#L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L29)). |
| `EXECUTION_SCOPE_BOTH` | 3 | Same semantics on local daemon and hosted server ([`options.proto#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L30)). |

`LOCAL` is placement, not caller trust; `BOTH` includes an `atomicd` that accepts iroh requests ([`README.md#L91-L95`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/README.md?plain=1#L91-L95)). Local-only methods reject network principals even when they hold similarly named capabilities ([`CONTRACT.md#L26-L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L26-L30)). The descriptor gate requires a `LOCAL` method to allow only `CALLER_CLASS_LOCAL` ([`check_contract.py#L116-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L116-L117)).

### CallerClass

Caller trust, independent of endpoint placement. Middleware establishes it; no request body may assert it. Socket access is not authorization ([`options.proto#L33-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L33-L40)).

| Value | # | Meaning |
|---|---|---|
| `CALLER_CLASS_UNSPECIFIED` | 0 | Not allowed in an allowlist ([`check_contract.py#L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L113)). |
| `CALLER_CLASS_LOCAL` | 1 | A local caller ([`options.proto#L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L37)). |
| `CALLER_CLASS_REMOTE_IDENTITY` | 2 | An authenticated remote identity ([`options.proto#L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L38)). |
| `CALLER_CLASS_SANDBOX` | 3 | A sandbox credential ([`options.proto#L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L39)). A sandbox credential cannot become an unrestricted remote identity ([`options.proto#L66-L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L66-L67)). |

The descriptor gate pins the set of methods that allow `CALLER_CLASS_SANDBOX` to a reviewed list ([`check_contract.py#L20-L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L20-L39), [`check_contract.py#L128-L134`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L128-L134)). In libatomic a request is treated as a sandbox caller when the host attached a grant to it or it carries `x-atomic-sandbox-token-bin`; otherwise it is a local, trusted caller ([`sandbox_grants.rs#L9-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L9-L20)). See [Authentication and authorization](transport.md#authentication-and-authorization).

### RpcEffect

Allowed effects, not permissions ([`options.proto#L42-L55`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L42-L55)).

| Value | # | Meaning |
|---|---|---|
| `RPC_EFFECT_UNSPECIFIED` | 0 | Not allowed in an effect list ([`check_contract.py#L114`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L114)). |
| `RPC_EFFECT_REPOSITORY_READ` | 1 | Repository read; never acquires a writer, even for lazy repair or an aborted projection ([`options.proto#L42-L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L42-L43)). |
| `RPC_EFFECT_REPOSITORY_WRITE` | 2 | Repository write; queues per repository ([`options.proto#L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L44)). |
| `RPC_EFFECT_WORKSPACE_READ` | 3 | Working-copy read ([`options.proto#L50`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L50)). |
| `RPC_EFFECT_WORKSPACE_WRITE` | 4 | Working-copy write; requires the workspace lease ([`options.proto#L43-L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L43-L44)). |
| `RPC_EFFECT_DAEMON_READ` | 5 | Daemon storage read ([`options.proto#L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L52)). |
| `RPC_EFFECT_DAEMON_WRITE` | 6 | Daemon storage write, never a global queue that serializes all repository writes ([`options.proto#L44-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L44-L45)). |
| `RPC_EFFECT_EXTERNAL_IO` | 7 | An external effect, declared separately from storage and daemon effects ([`options.proto#L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L54); [`CONTRACT.md#L329`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L329)). `ExplainTurns` declares it for a Claude CLI subprocess ([`provenance_service.proto#L194`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/provenance/provenance_service.proto#L194)). |

Rules the descriptor gate enforces: workspace effects only on `LOCAL` methods and never for sandbox callers ([`check_contract.py#L118-L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L118-L120)); a method without write effects may not require a `.write` or `.admin` capability or return `ResponseMeta` ([`check_contract.py#L121-L123`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L121-L123)); a method with write effects, other than Reactor publish, must require a write, admin or listed mutation capability, take `RequestMeta`, and return `ResponseMeta` at field 100 ([`check_contract.py#L100-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L100-L103), [`check_contract.py#L124-L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L124-L127)); `RPC_EFFECT_WORKSPACE_WRITE` requires `working-copy.write` ([`check_contract.py#L132-L133`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L132-L133)).

### Required capability

`required_capability` (extension 51002) lists the endpoint permissions a method needs; all listed capabilities must be granted on the resource ([`CONTRACT.md#L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L19)). Tokens use dot style (`repository.read`); the RFC is inconsistent between dot and colon style, and dot style is flagged for ratification ([`options.proto#L11-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L11-L14)). `DaemonService.Health` and `GetCapabilities` declare no capabilities, because a client must discover an endpoint before it knows which capabilities it holds ([`options.proto#L60-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L60-L65)); the descriptor gate enforces that exception ([`check_contract.py#L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/check_contract.py#L115)).

The tokens used across the service protos at this commit, with the number of rpcs that require each and the first declaration:

| Token | Rpcs | First declaration |
|---|---|---|
| `agent.checkpoint` | 4 | [`provenance_service.proto#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/provenance/provenance_service.proto#L30) |
| `attestation.read` | 3 | [`attestation_service.proto#L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/attestation/attestation_service.proto#L19) |
| `attestation.write` | 2 | [`attestation_service.proto#L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/attestation/attestation_service.proto#L29) |
| `daemon.admin` | 2 | [`daemon_service.proto#L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L51) |
| `daemon.read` | 2 | [`daemon_service.proto#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L43) |
| `gitinterop.read` | 1 | [`git_interop_service.proto#L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L27) |
| `gitinterop.write` | 2 | [`git_interop_service.proto#L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L18) |
| `knowledge.read` | 2 | [`knowledge_service.proto#L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/knowledge/knowledge_service.proto#L22) |
| `knowledge.write` | 1 | [`knowledge_service.proto#L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/knowledge/knowledge_service.proto#L37) |
| `maintenance.admin` | 3 | [`maintenance_service.proto#L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/maintenance/maintenance_service.proto#L17) |
| `maintenance.read` | 1 | [`maintenance_service.proto#L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/maintenance/maintenance_service.proto#L35) |
| `provenance.read` | 7 | [`provenance_service.proto#L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/provenance/provenance_service.proto#L22) |
| `provenance.write` | 7 | [`provenance_service.proto#L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/provenance/provenance_service.proto#L16) |
| `reactor.checkpoint` | 1 | [`reactor_service.proto#L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L49) |
| `reactor.publish` | 2 | [`reactor_service.proto#L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L17) |
| `reactor.subscribe` | 2 | [`reactor_service.proto#L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L33) |
| `repository.read` | 16 | [`knowledge_service.proto#L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/knowledge/knowledge_service.proto#L16) |
| `repository.write` | 13 | [`git_interop_service.proto#L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L19) |
| `sandbox.admin` | 3 | [`sandbox_service.proto#L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/sandbox/sandbox_service.proto#L20) |
| `sandbox.create` | 3 | [`sandbox_service.proto#L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/sandbox/sandbox_service.proto#L44) |
| `sandbox.read` | 3 | [`sandbox_service.proto#L90`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/sandbox/sandbox_service.proto#L90) |
| `sandbox.submit` | 1 | [`sandbox_service.proto#L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/sandbox/sandbox_service.proto#L128) |
| `sync.read` | 1 | [`sync_service.proto#L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/sync/sync_service.proto#L29) |
| `sync.write` | 1 | [`sync_service.proto#L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/sync/sync_service.proto#L17) |
| `triage.read` | 2 | [`triage_service.proto#L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L16) |
| `vault.read` | 6 | [`vault_service.proto#L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/vault/vault_service.proto#L35) |
| `vault.write` | 6 | [`vault_service.proto#L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/vault/vault_service.proto#L16) |
| `view.create` | 2 | [`view_service.proto#L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/view/view_service.proto#L26) |
| `view.delete` | 1 | [`view_service.proto#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/view/view_service.proto#L43) |
| `view.promote` | 1 | [`view_service.proto#L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/view/view_service.proto#L53) |
| `view.switch` | 1 | [`view_service.proto#L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/view/view_service.proto#L34) |
| `view.write` | 1 | [`view_service.proto#L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/view/view_service.proto#L65) |
| `working-copy.read` | 10 | [`maintenance_service.proto#L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/maintenance/maintenance_service.proto#L45) |
| `working-copy.write` | 18 | [`git_interop_service.proto#L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L20) |

Only a subset may be delegated to a sandbox grant: `sandbox.read`, `sandbox.submit`, `provenance.read`, `provenance.write`, `agent.checkpoint`, `vault.read`, `vault.write`, `knowledge.read`, `attestation.read` and `attestation.write` ([`CONTRACT.md#L142-L150`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L142-L150)). libatomic holds the same list in `DELEGABLE_CAPABILITIES` ([`sandbox_grants.rs#L49-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L49-L62)). A sandbox call that needs a capability the grant lacks fails with `ERROR_CODE_FORBIDDEN` ([`sandbox_grants.rs#L495-L504`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L495-L504)).

## Open questions

- **ViewRef IDs are derived from names.** The contract says authorization uses `view_id` and that deleting and recreating a name never revives a grant ([`common.proto#L68-L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L68-L70)). libatomic derives `view_id` as the BLAKE3 digest of the name ([`sandbox_grants.rs#L558-L561`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L558-L561)), so a recreated view gets the same `view_id`. The sandbox grant keeps a separate internal numeric view ID for that case ([`sandbox_grants.rs#L149-L151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L149-L151)), which is not on the wire. Whether clients may rely on `view_id` being stable for a name is not stated.
- **Which ViewSnapshot field fences.** The contract says `own_merkle` is diagnostic and insufficient as a fence ([`common.proto#L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L84); [`CONTRACT.md#L312-L314`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L312-L314)), but `check_view_fence` prefers `own_merkle` whenever it is present ([`services.rs#L1256-L1264`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1256-L1264)). A client that echoes a full snapshot is fenced on the own-log state.
- **ExpectedState mismatch code.** The contract names `ERROR_CODE_PRECONDITION_FAILED` ([`common.proto#L135-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L135-L138)); the handlers return `ERROR_CODE_VIEW_STALE` ([`services.rs#L1215-L1223`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1215-L1223)). Both travel as gRPC `FAILED_PRECONDITION` ([`state.rs#L435-L439`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L435-L439)).
- **RepositoryRef IDs.** The contract describes IDs assigned at init and recorded under `.atomic` ([`common.proto#L57-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L57-L61)); libatomic uses a digest of the canonical path, called a slice limitation ([`state.rs#L66-L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L66-L70)). Moving a repository changes its ID. Whether a later revision changes the ID scheme is not stated.
- **workspace_id is unused.** The contract says `RepositoryRef.workspace_id` is required for working-copy operations ([`common.proto#L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L65)), and `ERROR_CODE_WORKSPACE_REQUIRED` exists for its absence ([`error.proto#L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/error.proto#L31)). No libatomic handler issues or checks it outside sandbox refusals ([`state.rs#L87-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L87-L93), [`sandbox_grants.rs#L515-L520`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/sandbox_grants.rs#L515-L520)).
- **Idempotency is not implemented.** `RequestMeta` and `ResponseMeta` promise replay detection and `replayed: true` ([`common.proto#L8-L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L8-L17)); every `ResponseMeta` libatomic builds has `replayed: false` ([`services_agent.rs#L1233-L1239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L1233-L1239)), and only `SubmitChange` returns the post-mutation snapshot that `CONTRACT.md` asks of direct RPC writes ([`CONTRACT.md#L202`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L202); [`services_sandbox.rs#L839-L843`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sandbox.rs#L839-L843)). Whether the serving transport adds replay handling is not decided in libatomic.
- **Capability style.** Dot-style tokens are flagged for ratification ([`options.proto#L11-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L11-L14); [`COVERAGE.md#L264-L265`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L264-L265)); the vocabulary may change.
- **Extension numbers 51003 and 51004.** Only 51001 and 51002 are called permanent; the others are correctable until the first tagged release ([`options.proto#L3-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L3-L6)).

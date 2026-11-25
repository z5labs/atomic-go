# TriageService

TriageService is the promotion-review surface. Candidates are the change sets pending promotion from one view into another, and the review is the canonical report that `atomic triage review --json` emits [`triage_messages.proto#L1-L8`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L1-L8). The service definition is marked DRAFT, add-only [`triage_service.proto#L1-L3`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L1-L3). Both rpcs declare `EXECUTION_SCOPE_BOTH`, callers `CALLER_CLASS_LOCAL` and `CALLER_CLASS_REMOTE_IDENTITY`, capability `triage.read` and effect `RPC_EFFECT_REPOSITORY_READ` [`triage_service.proto#L12-L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L12-L29).

`TriageImpl` implements both rpcs. Each one resolves a (feature, target) view pair and returns the repository's own JSON serialization of the result as [VersionedBytes](../types.md#versionedbytes) [`services_triage.rs#L1-L11`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L1-L11) [`services_triage.rs#L86-L159`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L86-L159).

| Method | Implemented | In-process `LocalService` | Advertised by GetCapabilities |
|---|---|---|---|
| [ListTriageCandidates](#listtriagecandidates) | partial | not routed | no |
| [GenerateTriageReview](#generatetriagereview) | partial | not routed | no |

Neither method is in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119), and LocalService answers `UNIMPLEMENTED` for both [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360).

<a id="view-pair-resolution"></a>
**View-pair resolution.** Both handlers resolve the pair the same way [`services_triage.rs#L26-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L26-L64):

- Feature: the requested view, which must exist, or the repository's current view when none is given [`services_triage.rs#L34-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L34-L40).
- Target: the requested view, which must exist, or the feature view's parent when none is given [`services_triage.rs#L41-L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L41-L47).
- A root feature view with no target fails `INVALID_ARGUMENT` "'…' is a root view — it has no parent to promote into" [`services_triage.rs#L48-L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L48-L56).
- A named view that does not exist fails `INVALID_ARGUMENT` "No view named '…'", and the message names the current view [`services_triage.rs#L66-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L66-L84).

Refusals use `domain_status`, so messages carry the `atomic:<CODE_NAME>: ` prefix [`state.rs#L449-L452`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L449-L452); see [how handlers produce errors](../errors.md#how-handlers-produce-errors).

## Methods

### ListTriageCandidates

`rpc ListTriageCandidates(ListTriageCandidatesRequest) returns (ListTriageCandidatesResponse)`, unary [`triage_service.proto#L14-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L14-L20).

Lists the promotion candidates for `atomic triage candidates` [`triage_service.proto#L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L13). The handler opens the repository read-only, resolves the view pair and runs the repository's candidate-set computation (view diff, dependency-closure additions, baggage classification) [`services_triage.rs#L3-L7`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L3-L7) [`services_triage.rs#L97-L107`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L97-L107).

All values below come from the rpc's [method options](../types.md#method-options) [`triage_service.proto#L15-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L15-L19).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_REPOSITORY_READ` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `triage.read` ([required capability](../types.md#required-capability)) |
| Implemented | partial: the handler returns `candidate_set` [`services_triage.rs#L88-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L88-L119) but never fills the structured `candidates` list |
| In-process `LocalService` | not routed [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `ListTriageCandidatesRequest` [`triage_messages.proto#L18-L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L18-L22)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Required** | Unwrapped before resolving [`services_triage.rs#L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L93). |
| `from_view` | 2 | `optional string` | **Optional** | Feature view. Absent selects the current view [`services_triage.rs#L95-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L95-L100) [`services_triage.rs#L34-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L34-L40). Presence is what counts: a present empty string is checked as a view name. |
| `to_view` | 3 | `optional string` | **Optional** | Target view. Absent selects the feature view's parent [`services_triage.rs#L41-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L41-L62). |

**Response**: `ListTriageCandidatesResponse` [`triage_messages.proto#L23-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L23-L32)

| Field | # | Type | Meaning |
|---|---|---|---|
| `candidates` | 1 | `repeated` [TriageCandidate](#triagecandidate) | Always empty at this commit [`services_triage.rs#L111`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L111). |
| `candidate_set` | 2 | `optional` [VersionedBytes](../types.md#versionedbytes) | Always set. `schema` is `atomic.triage.candidates.v1` and `payload` is the JSON serialization of the repository's candidate set [`services_triage.rs#L101-L105`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L101-L105) [`services_triage.rs#L112-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L112-L115), the same document the CLI's `--json` emits [`triage_messages.proto#L25-L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L25-L29). |
| `feature` | 3 | `string` | The resolved feature view [`services_triage.rs#L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L116). |
| `target` | 4 | `string` | The resolved target view [`services_triage.rs#L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L117). |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `INVALID_ARGUMENT` | `ERROR_CODE_INVALID_ARGUMENT` | `from_view` or `to_view` names a view that does not exist | [`services_triage.rs#L66-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L66-L84) |
| `INVALID_ARGUMENT` | `ERROR_CODE_INVALID_ARGUMENT` | The feature view is a root view and `to_view` is absent | [`services_triage.rs#L48-L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L48-L56) |
| `INVALID_ARGUMENT` | `ERROR_CODE_INVALID_ARGUMENT` | The parent lookup reports the feature view as not found | [`services_triage.rs#L57-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L57-L59) |
| `INTERNAL` | `ERROR_CODE_REPOSITORY` | The read-only open, a view lookup or the candidate-set computation fails; see [how handlers produce errors](../errors.md#how-handlers-produce-errors) | [`services_triage.rs#L98-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L98-L103) [`services_triage.rs#L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L60) [`services_triage.rs#L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L67) |
| `INTERNAL` | none (plain status) | JSON encoding of the candidate set fails | [`services_triage.rs#L104-L105`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L104-L105) |
| `INVALID_ARGUMENT` / `NOT_FOUND` | `ERROR_CODE_INVALID_ARGUMENT` / `ERROR_CODE_REPOSITORY_NOT_FOUND` | Repository reference not served or not registered | [`state.rs#L331-L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L331-L348) |
| `INTERNAL` | none (plain status) | The blocking task fails to join | [`services_triage.rs#L108-L109`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L108-L109) |

**Behaviour**

- The handler takes no repository gate and uses the read-only open [`services_triage.rs#L92-L98`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L92-L98) [`state.rs#L212-L229`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L212-L229).
- The request has no `RequestMeta` and the response no `ResponseMeta` [`triage_messages.proto#L18-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L18-L32).

### GenerateTriageReview

`rpc GenerateTriageReview(GenerateTriageReviewRequest) returns (GenerateTriageReviewResponse)`, unary [`triage_service.proto#L22-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L22-L28).

Builds the triage review for `atomic triage review [--json]` [`triage_service.proto#L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L21). The handler resolves the view pair, builds the canonical report with `atomic_repository::triage::build_report` and returns it as JSON. Renderings such as the dashboard, walkthrough, HTML and attestation export are produced client-side from that report [`services_triage.rs#L7-L11`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L7-L11) [`services_triage.rs#L132-L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L132-L146).

All values below come from the rpc's [method options](../types.md#method-options) [`triage_service.proto#L23-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L23-L27).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_REPOSITORY_READ` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `triage.read` ([required capability](../types.md#required-capability)) |
| Implemented | partial: the handler returns `report` [`services_triage.rs#L121-L158`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L121-L158) but never fills the structured `layers` and `risks` |
| In-process `LocalService` | not routed [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `GenerateTriageReviewRequest` [`triage_messages.proto#L40-L48`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L40-L48)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Required** | Unwrapped before resolving [`services_triage.rs#L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L126). |
| `from_view` | 2 | `string` | **Optional** | Feature view. Empty selects the current view [`services_triage.rs#L134-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L134-L138). |
| `to_view` | 3 | `string` | **Optional** | Target view. Empty selects the feature view's parent [`services_triage.rs#L134-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L134-L138). |
| `report` | 4 | `optional` [VersionedBytes](../types.md#versionedbytes) | **Ignored** | The handler reads only `repository`, `from_view` and `to_view` [`services_triage.rs#L125-L131`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L125-L131). The proto comment describes the full canonical report, the same text as on the response [`triage_messages.proto#L44-L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L44-L47). |

**Response**: `GenerateTriageReviewResponse` [`triage_messages.proto#L49-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L49-L59)

| Field | # | Type | Meaning |
|---|---|---|---|
| `layers` | 1 | `repeated` [TriageLayer](#triagelayer) | Always empty at this commit [`services_triage.rs#L151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L151); the proto says this projection stays unfilled until a caller wants a parser-free walk [`triage_messages.proto#L55-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L55-L57). |
| `risks` | 2 | `repeated` [TriageRisk](#triagerisk) | Always empty at this commit [`services_triage.rs#L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L152). |
| `report` | 3 | `optional` [VersionedBytes](../types.md#versionedbytes) | Always set. `schema` is `atomic.triage.report.v1` and `payload` is the JSON serialization of the repository's triage report [`services_triage.rs#L139-L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L139-L142) [`services_triage.rs#L153-L156`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L153-L156). A test decodes it and reads the resolved `inputs.feature`/`inputs.target`, `summary.changes`, `inputs.candidate_changes`, `verdict` and `findings[].code` [`triage_service_test.rs#L104-L130`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/triage_service_test.rs#L104-L130). |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `INVALID_ARGUMENT` | `ERROR_CODE_INVALID_ARGUMENT` | `from_view` or `to_view` names a view that does not exist | [`services_triage.rs#L66-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L66-L84) [`triage_service_test.rs#L133-L149`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/triage_service_test.rs#L133-L149) |
| `INVALID_ARGUMENT` | `ERROR_CODE_INVALID_ARGUMENT` | The feature view is a root view and `to_view` is empty | [`services_triage.rs#L48-L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L48-L56) |
| `INVALID_ARGUMENT` | `ERROR_CODE_INVALID_ARGUMENT` | The parent lookup reports the feature view as not found | [`services_triage.rs#L57-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L57-L59) |
| `INTERNAL` | `ERROR_CODE_REPOSITORY` | The read-only open, a view lookup or `build_report` fails; see [how handlers produce errors](../errors.md#how-handlers-produce-errors) | [`services_triage.rs#L133-L140`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L133-L140) |
| `INTERNAL` | none (plain status) | JSON encoding of the report fails | [`services_triage.rs#L141-L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L141-L142) |
| `INVALID_ARGUMENT` / `NOT_FOUND` | `ERROR_CODE_INVALID_ARGUMENT` / `ERROR_CODE_REPOSITORY_NOT_FOUND` | Repository reference not served or not registered | [`state.rs#L331-L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L331-L348) |
| `INTERNAL` | none (plain status) | The blocking task fails to join | [`services_triage.rs#L145-L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L145-L146) |

**Behaviour**

- Unlike ListTriageCandidates, this handler takes the repository's exclusive gate before opening the repository read-only [`services_triage.rs#L128-L133`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L128-L133). It therefore waits behind, and blocks, other gated calls on the same repository [`state.rs#L54-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L54-L58).
- Empty strings mean "not given" here, while ListTriageCandidates distinguishes absent from empty through `optional` fields [`services_triage.rs#L134-L137`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L134-L137) [`services_triage.rs#L95-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L95-L100).

## Types

These nested messages are part of the contract, but no handler fills them at this commit [`services_triage.rs#L110-L111`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L110-L111) [`services_triage.rs#L150-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L150-L152).

### TriageCandidate

One candidate change [`triage_messages.proto#L33-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L33-L38): `change` (1, [Hash](../types.md#hash)), `intent_id` (2, `optional string`, linked vault provenance), `views` (3, `repeated string`, views carrying the change), `summary` (4, `optional string`).

### TriageLayer

One ordered semantic walkthrough layer [`triage_messages.proto#L60-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L60-L64): `name` (1, `string`, for example storage, service, protocol, docs), `changes` (2, `repeated` [TriageChangeRef](#triagechangeref)), `narration` (3, `string`).

### TriageChangeRef

A change in a layer [`triage_messages.proto#L65-L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L65-L68): `change` (1, [Hash](../types.md#hash)) and `files` (2, `repeated string`).

### TriageRisk

One risk finding [`triage_messages.proto#L69-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L69-L73): `severity` (1, `string`, one of blocker, warning, note), `finding` (2, `string`), `change` (3, `optional` [Hash](../types.md#hash)).

## Open questions

- A transport request without `repository` reaches `.unwrap()` in both handlers [`services_triage.rs#L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L93) [`services_triage.rs#L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L126). This panics instead of returning a status; what a client then observes depends on the atomicd transport, which is not in libatomic [`lib.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L22-L24).
- The JSON layouts of `atomic.triage.candidates.v1` and `atomic.triage.report.v1` are the serde serializations of atomic-repository types [`services_triage.rs#L104`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L104) [`services_triage.rs#L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L141). libatomic defines no schema for them beyond the fields its test reads [`triage_service_test.rs#L108-L130`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/triage_service_test.rs#L108-L130).
- GenerateTriageReview takes the exclusive repository gate although it is a read [`services_triage.rs#L128-L129`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L128-L129). The proto README requires read-only operations to run as concurrent reads, never in the per-repository write queue [`README.md#L114-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/README.md?plain=1#L114-L117). Whether this is intentional is not stated.
- `GenerateTriageReviewRequest.report` is ignored, and its comment duplicates the response field's [`triage_messages.proto#L44-L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_messages.proto#L44-L47). Its purpose on a request is unclear.
- `candidates`, `layers` and `risks` are never filled [`services_triage.rs#L111`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L111) [`services_triage.rs#L151-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_triage.rs#L151-L152). COVERAGE.md lists triage layers among the SHAPE-CONFIRM shapes [`COVERAGE.md#L266-L268`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L266-L268), but the triage protos carry no SHAPE-CONFIRM marker. It is unclear whether these shapes are final.
- Both rpcs allow `CALLER_CLASS_REMOTE_IDENTITY` and `EXECUTION_SCOPE_BOTH` [`triage_service.proto#L15-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/triage/triage_service.proto#L15-L18), yet `resolve` serves only the `local` authority [`state.rs#L332-L340`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L332-L340) and libatomic contains no transport. Whether atomicd registers TriageService, and how it authorizes remote identities, is decided outside libatomic [`lib.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L22-L24).

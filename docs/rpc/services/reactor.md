# ReactorService

ReactorService is the contract for durable, cross-project event ingestion [`reactor_messages.proto#L1`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L1). The proto marks it **CONTRACT DRAFT ONLY**: the server lands in Phase 3 with atomicd, and the contract is drafted now because the Reactor is its own first consumer and the repository-outbox events must match its envelope [`reactor_messages.proto#L3-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L3-L6). The proto README says the same ("reactor (CONTRACT DRAFT — server lands Phase 3)") [`README.md#L40-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/README.md?plain=1#L40-L41). The service definition is marked DRAFT, add-only [`reactor_service.proto#L1-L3`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L1-L3).

**Not implemented in libatomic at this commit.** libatomic has no ReactorService handler. The daemon module declarations include no Reactor service module [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57), METHOD_MATRIX lists no ReactorService method [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119), and LocalService answers `UNIMPLEMENTED` for every method it does not route [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360). Everything below is the contract. The proto names the Reactor prototype in the source repository as the design spec for the eventual server [`reactor_messages.proto#L18-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L18-L20).

The word "reactor" also names the crate that holds the atomicd transport shell serving libatomic's handlers [`lib.rs#L17-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L17-L24). That crate is a different thing from this service.

**Where it is meant to be served.** Every rpc declares `EXECUTION_SCOPE_BOTH` with callers `CALLER_CLASS_LOCAL` and `CALLER_CLASS_REMOTE_IDENTITY` [`reactor_service.proto#L13-L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L13-L54): the local daemon and the hosted server, for local and remote-identity callers. COVERAGE.md lists the service as "all BOTH (contract draft)" [`COVERAGE.md#L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L44). Sandbox tokens cannot invoke Reactor operations [`CONTRACT.md#L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L348).

**Durability semantics the contract names** [`reactor_messages.proto#L8-L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L8-L16):

- An acknowledged event has been durably appended (append before acknowledgement).
- `event_id` is globally unique; ingesting a duplicate is deduplicated, not failed.
- The daemon's `ingestion_sequence` is the replay order. Repository and session sequences order only their own stream; there is no meaningful total order across unrelated repositories.
- `causation_id` and `correlation_id` survive daemon proxying.
- Consumer checkpoints are durable and delivery is at-least-once, so consumers must be idempotent or land in a dead-letter state.

**Principal boundaries** [`CONTRACT.md#L338-L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L338-L348): inbound `authenticated_principal` and daemon-assigned sequence or time values are refused, and middleware derives them. The `source`/delegation and every repository in PublishStream are authorized independently, and outer and inner repository IDs must agree. Deduplication never returns another principal's private outcome on an ID collision. Consumers belong to the authenticated principal; filters and replay never widen that ownership. Publish deduplicates by principal-bound `event_id` instead of the `request_id` replay used by other mutations [`CONTRACT.md#L54-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L54-L59) [`common.proto#L111-L112`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L111-L112).

| Method | Kind | Implemented | In-process `LocalService` | Advertised by GetCapabilities |
|---|---|---|---|---|
| [Publish](#publish) | unary | no | not routed | no |
| [PublishStream](#publishstream) | client streaming | no | not routed | no |
| [Subscribe](#subscribe) | server streaming | no | not routed | no |
| [Replay](#replay) | server streaming | no | not routed | no |
| [CheckpointConsumer](#checkpointconsumer) | unary | no | not routed | no |

## Methods

### Publish

`rpc Publish(PublishEventRequest) returns (PublishEventResponse)`, unary [`reactor_service.proto#L15-L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L15-L21).

Contract: publishes one event. An acknowledgement means the event was durably appended and sequenced [`reactor_service.proto#L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L14). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`reactor_service.proto#L16-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L16-L20).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_DAEMON_WRITE` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `reactor.publish` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `PublishEventRequest` [`reactor_messages.proto#L33-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L33-L38)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | `optional` [RepositoryRef](../types.md#repositoryref) | **Contract only** | Set for repository-scoped publishes; cross-project events omit it [`reactor_messages.proto#L34-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L34-L36). When both are present, it must agree with `event.repository_id` [`event.proto#L29-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L29-L31). |
| `event` | 2 | [EventEnvelope](../types.md#eventenvelope) | **Contract only** | The event [`reactor_messages.proto#L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L37). Daemon-assigned fields such as `ingestion_sequence` are absent on publish [`event.proto#L36-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L36-L37), and an inbound `authenticated_principal` is refused [`event.proto#L27-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L27-L28). |

**Response**: `PublishEventResponse` [`reactor_messages.proto#L39-L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L39-L42)

| Field | # | Type | Meaning |
|---|---|---|---|
| `ingestion_sequence` | 1 | `uint64` | Assigned at durable append [`reactor_messages.proto#L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L40). |
| `ingested_at` | 2 | `optional google.protobuf.Timestamp` | Ingest time [`reactor_messages.proto#L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L41); CONTRACT.md says ingest time uses the trusted server clock [`CONTRACT.md#L52-L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L52-L53). |

The response has no `ResponseMeta` [`reactor_messages.proto#L39-L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L39-L42).

**Errors**

No handler exists, so libatomic defines no errors for this method beyond LocalService's refusal.

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService ("local method is not supported: …") | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- A duplicate `event_id` is deduplicated, not failed [`reactor_messages.proto#L10`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L10), and deduplication is principal-bound [`CONTRACT.md#L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L59) [`CONTRACT.md#L343`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L343).
- `event_id` is a globally unique UUID v4 [`event.proto#L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L25).

### PublishStream

`rpc PublishStream(stream EventEnvelope) returns (PublishStreamResponse)`, client streaming [`reactor_service.proto#L23-L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L23-L29).

Contract: streaming publish, with one [EventEnvelope](../types.md#eventenvelope) per frame and one acknowledgement summary [`reactor_service.proto#L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L22). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`reactor_service.proto#L24-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L24-L28).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_DAEMON_WRITE` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `reactor.publish` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: a client stream of [EventEnvelope](../types.md#eventenvelope) messages, one event per frame [`reactor_service.proto#L22-L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L22-L23). There is no wrapper message, so a frame carries no separate `RepositoryRef`. Every frame independently authorizes the repository it names [`event.proto#L29-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L29-L31) [`CONTRACT.md#L341-L342`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L341-L342).

**Response**: `PublishStreamResponse`, sent once as the summary [`reactor_messages.proto#L44-L48`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L44-L48)

| Field | # | Type | Meaning |
|---|---|---|---|
| `accepted` | 1 | `uint64` | Events accepted [`reactor_messages.proto#L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L45). |
| `duplicates` | 2 | `uint64` | Deduplicated `event_id`s, which are not failures [`reactor_messages.proto#L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L46). |
| `last_ingestion_sequence` | 3 | `optional uint64` | The last ingestion sequence assigned [`reactor_messages.proto#L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L47). |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- Stream: the client sends frames, and the server answers with a single summary [`reactor_service.proto#L22-L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L22-L23). The contract does not say whether one refused frame aborts the stream or is only left out of `accepted` (see [Open questions](#open-questions)).
- Bounded stream, message and queue limits apply per principal, and slow streaming cannot keep a writer open [`CONTRACT.md#L69-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L69-L71).

### Subscribe

`rpc Subscribe(SubscribeRequest) returns (stream EventEnvelope)`, server streaming [`reactor_service.proto#L31-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L31-L37).

Contract: a live subscription with at-least-once delivery and per-consumer checkpoints [`reactor_service.proto#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L30). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`reactor_service.proto#L32-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L32-L36).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_DAEMON_READ` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `reactor.subscribe` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `SubscribeRequest` [`reactor_messages.proto#L50-L55`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L50-L55)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `consumer_id` | 1 | `string` | **Contract only** | Owned by the authenticated principal, never an ambient global ID [`reactor_messages.proto#L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L51). |
| `families` | 2 | `repeated string` | **Contract only** | `type_url` prefixes to deliver; empty means every authorized family [`reactor_messages.proto#L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L52). |
| `repository_id` | 3 | `optional bytes` | **Contract only** | Repository filter [`reactor_messages.proto#L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L53). |
| `session_id` | 4 | `optional string` | **Contract only** | Session filter [`reactor_messages.proto#L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L54). |

**Response**: a server stream of [EventEnvelope](../types.md#eventenvelope) messages [`reactor_service.proto#L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L31). The daemon assigns `ingestion_sequence`, `repository_sequence`, `session_sequence` and `ingested_at` at ingest [`event.proto#L36-L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L36-L42), and `authenticated_principal` is output-only [`event.proto#L27-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L27-L28).

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- Delivery is at-least-once, so consumers must be idempotent [`reactor_messages.proto#L15-L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L15-L16). Replay order is `ingestion_sequence`; repository and session sequences order only their own stream [`reactor_messages.proto#L11-L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L11-L13).
- Subscribing grants no checkpoint-write authority; advancing a checkpoint is [CheckpointConsumer](#checkpointconsumer) [`CONTRACT.md#L346-L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L346-L348).
- The contract does not say how or when the stream ends (see [Open questions](#open-questions)).

### Replay

`rpc Replay(ReplayRequest) returns (stream EventEnvelope)`, server streaming [`reactor_service.proto#L39-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L39-L45).

Contract: replays from a checkpoint or a sequence, using the same envelope shape as Subscribe [`reactor_service.proto#L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L38). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`reactor_service.proto#L40-L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L40-L44).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_DAEMON_READ` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `reactor.subscribe` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `ReplayRequest` [`reactor_messages.proto#L56-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L56-L64)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `consumer_id` | 1 | `string` | **Contract only** | Same ownership and event-family/repository filtering as Subscribe [`reactor_messages.proto#L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L57). |
| `ingestion_sequence` | 2 | `uint64` (oneof `from`) | **Contract only** | Start position in daemon replay order [`reactor_messages.proto#L58-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L58-L59). |
| `checkpoint` | 3 | [PageCursor](../types.md#pagecursor) (oneof `from`) | **Contract only** | Start from the last delivered checkpoint [`reactor_messages.proto#L60-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L60-L61). |
| `limit` | 4 | `optional uint64` | **Contract only** | Limit on replayed events [`reactor_messages.proto#L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L62). |
| `families` | 5 | `repeated string` | **Contract only** | `type_url` prefix filter, as in Subscribe [`reactor_messages.proto#L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L63) [`reactor_messages.proto#L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L52). |

**Response**: a server stream of [EventEnvelope](../types.md#eventenvelope) messages, the same shape as Subscribe [`reactor_service.proto#L38-L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L38-L39).

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- Replay never widens the consumer's ownership [`CONTRACT.md#L345-L346`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L345-L346).
- Order is `ingestion_sequence` [`reactor_messages.proto#L11`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L11).

### CheckpointConsumer

`rpc CheckpointConsumer(CheckpointConsumerRequest) returns (CheckpointConsumerResponse)`, unary [`reactor_service.proto#L47-L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L47-L53).

Contract: persists a consumer's delivery checkpoint [`reactor_service.proto#L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L46). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`reactor_service.proto#L48-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L48-L52).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_DAEMON_WRITE` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `reactor.checkpoint` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `CheckpointConsumerRequest` [`reactor_messages.proto#L66-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L66-L71)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `consumer_id` | 1 | `string` | **Contract only** | Owner only. Progress is monotonic and never advances beyond delivered state [`reactor_messages.proto#L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L67). |
| `ingestion_sequence` | 2 | `uint64` | **Contract only** | The checkpoint position in daemon replay order [`reactor_messages.proto#L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L68). |
| `session_sequence` | 3 | `optional uint64` | **Contract only** | Optional per-session position [`reactor_messages.proto#L69`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L69). |
| `meta` | 4 | [RequestMeta](../types.md#requestmeta) | **Contract only** | Request header at field 4 [`reactor_messages.proto#L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L70); CONTRACT.md requires a request ID [`CONTRACT.md#L346-L347`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L346-L347). |

**Response**: `CheckpointConsumerResponse` [`reactor_messages.proto#L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L72)

| Field | # | Type | Meaning |
|---|---|---|---|
| `meta` | 100 | [ResponseMeta](../types.md#responsemeta) | Mutation response header. |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- Requires `reactor.checkpoint`, a request ID, and monotonic progress bounded by delivered state [`CONTRACT.md#L346-L347`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L346-L347).

## Types

No message of this service is used by more than one method. The streamed message is the shared [EventEnvelope](../types.md#eventenvelope); its payload `oneof` has only `opaque` bytes so far, and the proto says stable named families land additively in Phase 3 [`event.proto#L47-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/event.proto#L47-L52).

## Open questions

- No server implementation exists in libatomic. The proto places it in Phase 3 with atomicd [`reactor_messages.proto#L3-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L3-L6), and the atomicd transport lives outside libatomic [`lib.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L22-L24). No libatomic source says when, or in which crate, the ReactorService server will exist.
- What a network client receives when calling these methods today depends on that transport; only LocalService's `UNIMPLEMENTED` is defined in libatomic [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360).
- Stream termination is not specified: the contract does not say whether Subscribe ever completes or how Replay ends (end of retained data, `limit` reached, or catch-up to live) [`reactor_service.proto#L30-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_service.proto#L30-L45) [`reactor_messages.proto#L56-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L56-L64).
- Replay start semantics are unclear: whether `ingestion_sequence` and `checkpoint` are inclusive or exclusive, and how a [PageCursor](../types.md#pagecursor) `{seq, offset}` maps onto ingestion sequences [`reactor_messages.proto#L58-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L58-L61) [`pagination.proto#L18-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/pagination.proto#L18-L24).
- PublishStream partial failure is unspecified: each frame is authorized independently [`CONTRACT.md#L341-L342`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L341-L342), but the summary only has `accepted`, `duplicates` and `last_ingestion_sequence` [`reactor_messages.proto#L44-L48`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L44-L48), with no count or list of refused frames.
- Consumer lifecycle is not defined: no rpc creates or deletes a consumer, and the protos do not say whether the first Subscribe or CheckpointConsumer with a new `consumer_id` creates one [`reactor_messages.proto#L50-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L50-L72).
- `CheckpointConsumerRequest.meta` is field 4, unlike the `meta = 2` used by other request messages [`reactor_messages.proto#L66-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/reactor/reactor_messages.proto#L66-L71). Publish carries no `RequestMeta` because it uses `event_id` deduplication [`common.proto#L111-L112`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L111-L112).

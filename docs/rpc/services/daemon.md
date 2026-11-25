# DaemonService

`DaemonService` covers daemon health, capability negotiation, the repository registry and the daemon's lifecycle ([`daemon_messages.proto#L1-L2`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L1-L2)). It replaces the liveness, lifecycle and storage-ownership parts of the legacy agent journal protocol: `Health` carries status and quick-gate feature tokens, and `GetCapabilities` carries the negotiated method matrix ([`daemon_messages.proto#L6-L12`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L6-L12)). The contract assumes one user-level daemon that opens repository databases lazily and evicts them when idle ([`daemon_messages.proto#L14-L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L14-L17)).

All six rpcs have handlers in `DaemonImpl` ([`services.rs#L126-L334`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L126-L334)). `Health` is implemented as declared. The other five differ from their contract in ways a client can observe, described under each method. None of the six handlers inspects request metadata or the caller's identity, so the declared caller allowlists and capabilities are not enforced in libatomic ([`services.rs#L126-L334`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L126-L334)); see [Authentication and authorization](../transport.md#authentication-and-authorization). Only `ResolveRepository` is routed through the in-process [LocalService](../transport.md#in-process-localservice) ([`local.rs#L92-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L92-L110)). All six appear in the `GetCapabilities` method matrix ([`services.rs#L61-L66`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L61-L66)).

`Health` and `GetCapabilities` are the negotiation pair. They declare no required capability, because a client has to discover what an endpoint supports before it can know which capabilities it holds ([`options.proto#L60-L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/options.proto#L60-L65)). See [Version negotiation](../transport.md#version-negotiation) for the full handshake.

## Methods

### Health

`rpc Health(HealthRequest) returns (HealthResponse)`, unary ([`daemon_service.proto#L16-L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L16-L22)).

Liveness check with quick-gate feature tokens; served by the local daemon and a hosted server alike ([`daemon_service.proto#L13-L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L13-L15)). The handler always reports `SERVING_STATUS_SERVING`, the serving process's ID and two feature tokens ([`services.rs#L128-L140`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L128-L140)).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([`daemon_service.proto#L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L17)); see [method options](../types.md#method-options) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY`, `CALLER_CLASS_SANDBOX` ([`daemon_service.proto#L18-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L18-L20)) |
| Effect | `RPC_EFFECT_DAEMON_READ` ([`daemon_service.proto#L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L21)) |
| Required capability | None; negotiation-pair exception ([`daemon_service.proto#L13-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L13-L14)) |
| Implemented | Yes ([`services.rs#L128-L140`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L128-L140)) |
| In-process `LocalService` | Not routed; fails with `UNIMPLEMENTED` ([`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360)) |
| Advertised by GetCapabilities | Yes ([`services.rs#L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L61)) |

**Request**: `HealthRequest` has no fields ([`daemon_messages.proto#L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L75)).

**Response**: `HealthResponse` ([`daemon_messages.proto#L77-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L77-L82)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `status` | 1 | `ServingStatus` | Always `SERVING_STATUS_SERVING` (1) ([`services.rs#L133`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L133)). The enum also defines `SERVING_STATUS_UNSPECIFIED` (0) and `SERVING_STATUS_NOT_SERVING` (2) ([`daemon_messages.proto#L26-L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L26-L30)), which the handler never returns. |
| `pid` | 2 | `uint32` | The operating-system process ID of the serving process ([`services.rs#L134`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L134)). |
| `features` | 3 | `repeated string` | Quick-gate feature tokens; the full matrix is in `GetCapabilities` ([`daemon_messages.proto#L80-L81`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L80-L81)). Always `["full-turn-slice", "repository-registry"]` ([`services.rs#L135-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L135-L138)). |

**Errors**: the handler returns no error of its own ([`services.rs#L128-L140`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L128-L140)).

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| — | — | None | [`services.rs#L132-L139`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L132-L139) |

**Behaviour**:

- Health's token list is not the same as `GetCapabilities`'s: it includes `repository-registry`, which `GetCapabilities` does not, and omits the two option-gate tokens ([`services.rs#L135-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L135-L138), [`services.rs#L221-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L221-L225)). Gate optional fields on `GetCapabilities`, not on `Health`.
- A status other than `SERVING` never comes from libatomic; a failed call is the only "not serving" signal a client gets from it.

### GetCapabilities

`rpc GetCapabilities(GetCapabilitiesRequest) returns (GetCapabilitiesResponse)`, unary ([`daemon_service.proto#L28-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L28-L34)).

The capability handshake: contract version, endpoint kind, method matrix, limits and authentication requirements. The contract says clients gate on it before invoking operations ([`daemon_service.proto#L24-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L24-L27)). The handler returns a fixed response built from a hard-coded method list, `METHOD_MATRIX`, and constants ([`services.rs#L142-L242`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L142-L242)).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_BOTH` ([`daemon_service.proto#L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L29)); see [method options](../types.md#method-options) |
| Allowed caller | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY`, `CALLER_CLASS_SANDBOX` ([`daemon_service.proto#L30-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L30-L32)) |
| Effect | `RPC_EFFECT_DAEMON_READ` ([`daemon_service.proto#L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L33)) |
| Required capability | None; negotiation-pair exception ([`daemon_service.proto#L24-L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L24-L25)) |
| Implemented | Partial: it responds, but its method matrix disagrees with the descriptors (see below) ([`services.rs#L142-L242`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L142-L242)) |
| In-process `LocalService` | Not routed; fails with `UNIMPLEMENTED` ([`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360)) |
| Advertised by GetCapabilities | Yes ([`services.rs#L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L62)) |

**Request**: `GetCapabilitiesRequest` has no fields ([`daemon_messages.proto#L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L84)). A client sends no capabilities or limits of its own.

**Response**: `GetCapabilitiesResponse` ([`daemon_messages.proto#L86-L97`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L86-L97)). Every field is always set, to the values below.

| Field | # | Type | Meaning |
|---|---|---|---|
| `protocol_version` | 1 | `uint32` | Contract version; a mismatch fails hard in both directions ([`daemon_messages.proto#L87-L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L87-L88)). Always 2 ([`services.rs#L217`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L217)). |
| `implementation_version` | 2 | `string` | The libatomic crate version the server was built from ([`services.rs#L218`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L218)). |
| `endpoint_kind` | 3 | `EndpointKind` | `ENDPOINT_KIND_UNSPECIFIED` (0), `ENDPOINT_KIND_LOCAL_DAEMON` (1) or `ENDPOINT_KIND_HOSTED_SERVER` (2) ([`daemon_messages.proto#L32-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L32-L36)). Always `ENDPOINT_KIND_LOCAL_DAEMON` ([`services.rs#L219`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L219)). |
| `methods` | 4 | `repeated` [`MethodDescriptorInfo`](#methoddescriptorinfo) | The method matrix ([`daemon_messages.proto#L91`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L91)). One entry per `METHOD_MATRIX` row, 58 in all ([`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119)); see [Advertised method matrix](#advertised-method-matrix). |
| `features` | 5 | `repeated string` | Feature tokens ([`daemon_messages.proto#L92-L94`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L92-L94)). Always `full-turn-slice`, `vault-memory-create-options-v1` and `record-file-options-v1` ([`services.rs#L221-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L221-L225)); see [Feature tokens](#feature-tokens). |
| `limits` | 6 | [`Limits`](#limits) | Negotiated limits ([`daemon_messages.proto#L95`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L95)), with the values in [Limits](#limits) ([`services.rs#L226-L234`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L226-L234)). |
| `auth` | 7 | [`AuthRequirements`](#authrequirements) | Authentication requirements ([`daemon_messages.proto#L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L96)), with the values in [AuthRequirements](#authrequirements) ([`services.rs#L235-L240`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L235-L240)). |

**Errors**: the handler returns no error of its own ([`services.rs#L142-L242`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L142-L242)).

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| — | — | None | [`services.rs#L216-L241`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L216-L241) |

#### Advertised method matrix

`METHOD_MATRIX` lists 58 of the contract's 105 rpcs ([`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119)):

| Service | Advertised methods | Source |
|---|---|---|
| DaemonService | Health, GetCapabilities, ResolveRepository, RegisterRepository, ListRepositories, Shutdown | [`services.rs#L61-L66`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L61-L66) |
| RepositoryQueryService | Status, Log, ListConflicts, PreviewMutation, PreviewRestore, Diff | [`services.rs#L67-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L67-L72) |
| RepositoryMutationService | AddFiles, Record, RemoveFiles, MoveFile, Unrecord, Restore, CreateStash, ApplyStash, ListStashes, DropStash, InsertChanges, Revise | [`services.rs#L73-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L73-L84) |
| VaultService | CreateVaultEntity, UpdateVaultEntity, GetVaultEntry, ListVaultEntries, ValidateVaultEntity, ReplayVaultEvidence, SyncVault | [`services.rs#L85-L91`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L85-L91) |
| AttestationService | RecordAttestation, VerifyAttestation | [`services.rs#L92-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L92-L93) |
| KnowledgeService | QueryGraph, GetProjectIdentity, GetWorkflowSnapshot | [`services.rs#L94-L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L94-L96) |
| ProvenanceService | DispatchTurnEvent, AppendHookLog, LoadHookLog, ReserveTurn, AppendEnvelopes, PrepareCheckpoint, LoadFrozenEnvelopes, BindCheckpointHash, AcknowledgeCheckpoint, UpdateTurn, GetTurn, GetSession, ListSessions | [`services.rs#L97-L109`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L97-L109) |
| MaintenanceService | Repair, CompactDatabase, CheckRepository | [`services.rs#L110-L112`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L110-L112) |
| ViewService | ListViews, CreateView, SwitchView, DeleteView, SetViewScope, SplitView | [`services.rs#L113-L118`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L113-L118) |

The other 47 rpcs are not in the matrix: all of SandboxService, SyncService, TagService, TriageService, GitInteropService and ReactorService; `GetChange` and `DiffWorkingCopy` of RepositoryQueryService; `InitVault`, `GetVaultContext`, `ExportVault`, `DeleteVaultEntity` and `LinkVaultEntities` of VaultService; `PrepareAttestation` and `ListAttestations` of AttestationService; `MaintainKnowledgeGraph`; `ReindexWorkspace`; and `ForkSession`, `RebuildSessionIndex`, `GetProvenance`, `ExportProvenance` and `ExplainTurns` of ProvenanceService. Several of these have handlers, for example TagService ([`services_tag.rs#L69-L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L69-L70)), and `GetChange` and `GetVaultContext` are even routed by `LocalService` ([`local.rs#L228-L237`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L228-L237), [`local.rs#L308-L317`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L308-L317)).

Six entries carry annotations, and these match their proto declarations:

| Method | Advertised scope, capabilities, callers, effects | Source | Proto declaration |
|---|---|---|---|
| MaintenanceService.CompactDatabase | `LOCAL`; `maintenance.admin`; `LOCAL`; `REPOSITORY_WRITE` | [`services.rs#L149-L157`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L149-L157) | [`maintenance_service.proto#L15-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/maintenance/maintenance_service.proto#L15-L20) |
| VaultService.ReplayVaultEvidence | `BOTH`; `vault.read`; `LOCAL`; `REPOSITORY_READ` | [`services.rs#L158-L166`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L158-L166) | [`vault_service.proto#L116-L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/vault/vault_service.proto#L116-L121) |
| KnowledgeService.GetProjectIdentity | `LOCAL`; `repository.read`; `LOCAL`; `REPOSITORY_READ` | [`services.rs#L167-L182`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L167-L182) | [`knowledge_service.proto#L14-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/knowledge/knowledge_service.proto#L14-L19) |
| KnowledgeService.GetWorkflowSnapshot | `LOCAL`; `knowledge.read`; `LOCAL`; `REPOSITORY_READ` | [`services.rs#L167-L182`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L167-L182) | [`knowledge_service.proto#L20-L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/knowledge/knowledge_service.proto#L20-L25) |
| ProvenanceService.AppendHookLog | `LOCAL`; `provenance.write`; `LOCAL`; `REPOSITORY_WRITE` | [`services.rs#L183-L203`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L183-L203) | [`provenance_service.proto#L14-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/provenance/provenance_service.proto#L14-L19) |
| ProvenanceService.LoadHookLog | `LOCAL`; `provenance.read`; `LOCAL`; `REPOSITORY_READ` | [`services.rs#L183-L203`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L183-L203) | [`provenance_service.proto#L20-L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/provenance/provenance_service.proto#L20-L25) |

Every other entry is advertised with scope `EXECUTION_SCOPE_BOTH` and empty capability, caller and effect lists ([`services.rs#L204-L212`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L204-L212)), whatever the proto declares. For example, `ResolveRepository` is declared `LOCAL` with `daemon.read` ([`daemon_service.proto#L41-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L41-L46)) but advertised as `BOTH` with nothing required. `CONTRACT.md` says the advertised matrix comes from the descriptors and that missing annotations fail closed ([`CONTRACT.md#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L22-L24)), so an empty list here means "not stated", not "nothing required". A client that needs a method's annotations should read them from the [descriptor set](../transport.md#descriptor-set), and should not treat absence from this matrix as proof that a method is unavailable.

#### Feature tokens

| Token | In `Health` | In `GetCapabilities` | Meaning |
|---|---|---|---|
| `full-turn-slice` | Yes ([`services.rs#L136`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L136)) | Yes ([`services.rs#L222`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L222)) | Not described in libatomic. |
| `repository-registry` | Yes ([`services.rs#L137`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L137)) | No | Not described in libatomic. |
| `vault-memory-create-options-v1` (`VAULT_MEMORY_CREATE_OPTIONS_FEATURE`) | No | Yes ([`services.rs#L223`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L223)) | The server accepts the add-only explicit memory ID and status fields ([`services.rs#L121-L122`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L121-L122)): `CreateVaultEntityRequest.memory_id` (14) and `memory_status` (15) ([`vault_messages.proto#L250-L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/vault/vault_messages.proto#L250-L251)). See [CreateVaultEntity](./vault.md#createvaultentity). |
| `record-file-options-v1` (`RECORD_FILE_OPTIONS_FEATURE`) | No | Yes ([`services.rs#L224`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L224)) | The server accepts the add-only record size and binary options ([`services.rs#L123-L124`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L123-L124)): `RecordRequest.skip_binary` (9) and `max_file_size` (10) ([`repository_mutation_messages.proto#L124-L125`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/repository_mutation/repository_mutation_messages.proto#L124-L125)). See [Record](./repository-mutation.md#record). |

A test checks that `GetCapabilities` advertises `vault-memory-create-options-v1` before using the memory fields ([`vault_local_test.rs#L40-L51`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/tests/vault_local_test.rs#L40-L51)).

**Behaviour: how a client uses the response.** The contract carries forward the legacy "additive capability flag" pattern: old clients ignore unknown tokens, old servers omit tokens they do not support, and clients check for a token before using the newer operation ([`daemon_messages.proto#L9-L12`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L9-L12)). In practice:

1. Call `GetCapabilities` first on a new connection, without `x-atomic-contract-version`.
2. Require `protocol_version == 2`, or stop ([Version negotiation](../transport.md#version-negotiation)).
3. Before sending a feature-gated field, check that its token is in `features`. A server that does not advertise the token may not honour the field.
4. Use `limits` to size requests, for example at most 64 changes per `GetChanges` request, which the handler enforces ([`services_sandbox.rs#L594-L612`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sandbox.rs#L594-L612)).
5. Do not rely on `methods` for availability or authorization (see above).

The handler ignores the request and builds the response from constants ([`services.rs#L142-L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L142-L146), [`services.rs#L216-L241`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L216-L241)), so it can be cached for the life of one daemon process; `Health.pid` identifies that process ([`services.rs#L134`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L134)).

### ResolveRepository

`rpc ResolveRepository(ResolveRepositoryRequest) returns (ResolveRepositoryResponse)`, unary ([`daemon_service.proto#L41-L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L41-L47)).

Turns a local working-copy path into the persistent [RepositoryRef](../types.md#repositoryref) and workspace context that every repository-scoped request needs ([`daemon_service.proto#L36-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L36-L38)). The handler finds the repository root above the path, registers it with this daemon state, and returns its reference, root, the path relative to the root, and the current view ([`services.rs#L244-L275`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L244-L275)). It is the usual first call; see [Repository resolution](../transport.md#repository-resolution).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_LOCAL` ([`daemon_service.proto#L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L42)); see [method options](../types.md#method-options) |
| Allowed caller | `CALLER_CLASS_LOCAL` ([`daemon_service.proto#L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L44)) |
| Effect | `RPC_EFFECT_DAEMON_READ`, `RPC_EFFECT_WORKSPACE_READ` ([`daemon_service.proto#L45-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L45-L46)) |
| Required capability | `daemon.read` ([`daemon_service.proto#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L43)) |
| Implemented | Yes, but it also registers, which the contract forbids (see Behaviour) ([`services.rs#L244-L275`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L244-L275)) |
| In-process `LocalService` | Routed, after canonicalizing the path ([`local.rs#L92-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L92-L110)) |
| Advertised by GetCapabilities | Yes, as `BOTH` with no annotations ([`services.rs#L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L63), [`services.rs#L204-L212`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L204-L212)) |

**Request**: `ResolveRepositoryRequest` ([`daemon_messages.proto#L105-L107`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L105-L107)).

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `path` | 1 | `string` | **Unconfirmed** | A working-copy path, for a local endpoint only ([`daemon_messages.proto#L106`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L106)). The handler passes it unchanged to `Repository::find_root`, which looks for the enclosing repository root ([`services.rs#L249-L250`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L249-L250)). It may name the root or a subdirectory of the working copy ([`daemon_service.proto#L36-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L36-L38)). There is no explicit empty-path check; see [Open questions](#open-questions). |

**Response**: `ResolveRepositoryResponse` ([`daemon_messages.proto#L108-L112`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L108-L112)). Field 2 is reserved, because registration is an explicit mutation ([`daemon_messages.proto#L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L110)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `repository` | 1 | [`RepositoryRef`](../types.md#repositoryref) | Always set. `authority` is `"local"`, `repository_id` is the BLAKE3 digest of the canonical root path, `workspace_id` is unset ([`services.rs#L262`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L262); [`state.rs#L87-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L87-L93), [`state.rs#L66-L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L66-L75)). |
| `workspace` | 3 | [`WorkspaceInfo`](#workspaceinfo) | Always set ([`services.rs#L263-L273`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L263-L273)). |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `NOT_FOUND` | `ERROR_CODE_REPOSITORY_NOT_FOUND` | No Atomic repository encloses `path`. The message carries the underlying error text. | [`services.rs#L249-L250`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L249-L250); [`state.rs#L433`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L433) |
| `NOT_FOUND` | none (plain status) | `LocalService` only: the path cannot be canonicalized, for example because it does not exist. | [`local.rs#L96-L99`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L96-L99) |
| `INVALID_ARGUMENT` | none (plain status) | `LocalService` only: the canonical path is not UTF-8. | [`local.rs#L100-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L100-L103) |
| `INTERNAL` | none (plain status) | `LocalService` only: the canonicalization task failed. | [`local.rs#L96-L98`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L96-L98) |
| `INVALID_ARGUMENT` | none (plain status) | `LocalService` only: the request bytes do not decode. | [`local.rs#L442-L444`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L442-L444) |

The message format of the first row is described in [how handlers produce errors](../errors.md#how-handlers-produce-errors).

**Behaviour**:

- **It registers.** The contract calls the method a lookup only: it must never register, and an unregistered path must fail with `REPOSITORY_NOT_FOUND` so that local clients call `RegisterRepository` explicitly ([`daemon_service.proto#L36-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L36-L40); [`CONTRACT.md#L107-L109`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L107-L109)). The handler registers the root it finds ([`services.rs#L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L251)), so any existing Atomic repository resolves without a prior `RegisterRepository`. The handler's behaviour is what a client sees.
- **Idempotent.** Registration is keyed by the canonical root, so resolving the same repository again returns the same `RepositoryRef` ([`state.rs#L314-L327`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L314-L327)).
- **No database access.** The handler does not take the per-repository gate or open the repository database, so it does not wait behind a running mutation ([`services.rs#L253-L256`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L253-L256)).
- **Send an absolute path.** The contract says the daemon's working directory is never consulted ([`daemon_messages.proto#L114-L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L114-L116)), and `relative_cwd` is computed by stripping the canonical root from the path exactly as sent; a path that is not literally under the canonical root, such as a relative path or one through a symbolic link, yields an empty `relative_cwd` ([`services.rs#L257-L260`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L257-L260); [`state.rs#L317-L318`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L317-L318)). `LocalService` canonicalizes the path in the calling process before the handler sees it ([`local.rs#L94-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L94-L103)).
- **References are per daemon state.** The registry is in memory; after a daemon restart, resolve again before reusing a reference ([`state.rs#L284-L299`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L284-L299), [`state.rs#L329-L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L329-L348)).
- **No caller check.** The contract says `WorkspaceInfo` is returned only to local principals ([`daemon_messages.proto#L114`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L114)); the handler does not check the caller ([`services.rs#L244-L275`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L244-L275)).

### RegisterRepository

`rpc RegisterRepository(RegisterRepositoryRequest) returns (RegisterRepositoryResponse)`, unary ([`daemon_service.proto#L49-L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L49-L56)).

The contract makes this the explicit, local, administrative registration step: it validates filesystem ownership and repository identity, refuses duplicate IDs at different stores, and assigns legacy IDs in the registry and `.atomic` ID record without opening a repository write transaction ([`daemon_messages.proto#L124-L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L124-L128); [`CONTRACT.md#L109-L111`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L109-L111)). The handler finds the repository root, adds it to the in-memory registry and returns its reference; it opens no database ([`services.rs#L277-L298`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L277-L298)).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_LOCAL` ([`daemon_service.proto#L50`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L50)); see [method options](../types.md#method-options) |
| Allowed caller | `CALLER_CLASS_LOCAL` ([`daemon_service.proto#L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L53)) |
| Effect | `RPC_EFFECT_DAEMON_WRITE`, `RPC_EFFECT_WORKSPACE_WRITE` ([`daemon_service.proto#L54-L55`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L54-L55)) |
| Required capability | `daemon.admin`, `working-copy.write` ([`daemon_service.proto#L51-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L51-L52)) |
| Implemented | Partial: registry entry only; no validation, no ID record, no `workspace` ([`services.rs#L277-L298`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L277-L298)) |
| In-process `LocalService` | Not routed; fails with `UNIMPLEMENTED` ([`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360)) |
| Advertised by GetCapabilities | Yes, as `BOTH` with no annotations ([`services.rs#L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L64), [`services.rs#L204-L212`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L204-L212)) |

**Request**: `RegisterRepositoryRequest` ([`daemon_messages.proto#L129-L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L129-L132)).

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `path` | 1 | `string` | **Unconfirmed** | Working-copy path. The handler passes it to `Repository::find_root`, as `ResolveRepository` does ([`services.rs#L283-L284`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L283-L284)). |
| `meta` | 2 | [`RequestMeta`](../types.md#requestmeta) | **Optional** | Idempotency header. When absent, the handler uses an empty `request_id` ([`services.rs#L282`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L282)). Only `request_id` is read ([`services.rs#L292-L296`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L292-L296)). |

**Response**: `RegisterRepositoryResponse` ([`daemon_messages.proto#L133-L137`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L133-L137)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `repository` | 1 | [`RepositoryRef`](../types.md#repositoryref) | Always set, the same reference `ResolveRepository` returns for the same root ([`services.rs#L285-L290`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L285-L290)). |
| `workspace` | 2 | [`WorkspaceInfo`](#workspaceinfo) | Never set ([`services.rs#L291`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L291)). Call `ResolveRepository` for the workspace context. |
| `meta` | 100 | [`ResponseMeta`](../types.md#responsemeta) | Always set: `request_id` echoed (empty when the request had no `meta`), `replayed: false`, no `snapshot` ([`services.rs#L292-L296`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L292-L296)). |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `NOT_FOUND` | `ERROR_CODE_REPOSITORY_NOT_FOUND` | No Atomic repository encloses `path`. | [`services.rs#L283-L284`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L283-L284); [`state.rs#L433`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L433) |

**Behaviour**:

- **Registry only.** The handler comment says no repository database is opened ([`services.rs#L287-L288`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L287-L288)). It does not validate ownership, refuse duplicate IDs or write an ID record, all of which the contract asks for ([`daemon_messages.proto#L124-L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L124-L128)). The ID is the canonical-path digest ([`state.rs#L317-L327`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L317-L327)).
- **Idempotent by construction, without replay.** Registering an already-registered root returns the existing entry ([`state.rs#L314-L327`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L314-L327)), so repeating the call is harmless, but the response never reports `replayed: true` ([`services.rs#L294`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L294)).
- **Redundant with ResolveRepository.** Because `ResolveRepository` also registers ([`services.rs#L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L251)), a client against libatomic does not need this call.

### ListRepositories

`rpc ListRepositories(ListRepositoriesRequest) returns (ListRepositoriesResponse)`, unary ([`daemon_service.proto#L58-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L58-L64)).

Lists the repositories this daemon can serve, with their open or idle state ([`daemon_service.proto#L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L58)). The handler returns every repository registered with this daemon state, sorted by root path ([`services.rs#L300-L315`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L300-L315); [`state.rs#L350-L355`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L350-L355)).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_LOCAL` ([`daemon_service.proto#L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L60)); see [method options](../types.md#method-options) |
| Allowed caller | `CALLER_CLASS_LOCAL` ([`daemon_service.proto#L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L62)) |
| Effect | `RPC_EFFECT_DAEMON_READ` ([`daemon_service.proto#L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L63)) |
| Required capability | `daemon.read` ([`daemon_service.proto#L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L61)) |
| Implemented | Partial: `open` is always `true` ([`services.rs#L308-L312`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L308-L312)) |
| In-process `LocalService` | Not routed; fails with `UNIMPLEMENTED` ([`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360)) |
| Advertised by GetCapabilities | Yes, as `BOTH` with no annotations ([`services.rs#L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L65), [`services.rs#L204-L212`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L204-L212)) |

**Request**: `ListRepositoriesRequest` has no fields ([`daemon_messages.proto#L99`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L99)).

**Response**: `ListRepositoriesResponse` ([`daemon_messages.proto#L101-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L101-L103)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `repositories` | 1 | `repeated RepositorySummary` | One entry per registered repository, in root-path order ([`services.rs#L304-L313`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L304-L313); [`state.rs#L350-L355`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L350-L355)). Empty until a repository has been resolved or registered. |

`RepositorySummary` ([`daemon_messages.proto#L139-L144`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L139-L144)):

| Field | # | Type | Meaning |
|---|---|---|---|
| `repository` | 1 | [`RepositoryRef`](../types.md#repositoryref) | The repository's reference ([`services.rs#L309`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L309)). |
| `open` | 2 | `bool` | The contract says it reports whether the repository database is currently open ([`daemon_messages.proto#L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L141)). The handler always sets `true` ([`services.rs#L310`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L310)), although the daemon never holds a database handle between requests ([`state.rs#L35-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L35-L40)). Do not use it. |
| `local_path` | 3 | `optional string` | The canonical root path; local daemon only, informational, not addressing ([`daemon_messages.proto#L142-L143`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L142-L143)). Always set ([`services.rs#L311`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L311)). |

**Errors**: the handler returns no error of its own ([`services.rs#L300-L315`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L300-L315)).

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| — | — | None | [`services.rs#L304-L314`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L304-L314) |

**Behaviour**: the list holds only repositories registered since this daemon state was created, because the registry is in memory ([`state.rs#L284-L299`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L284-L299)). There is no paging: the request has no fields ([`daemon_messages.proto#L99`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L99)).

### Shutdown

`rpc Shutdown(ShutdownRequest) returns (ShutdownResponse)`, unary ([`daemon_service.proto#L66-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L66-L73)).

The contract describes a graceful, local-only administrative shutdown that drains active and queued work, closes repository databases and releases locks ([`daemon_service.proto#L66-L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L66-L67)). The handler signals the daemon state's shutdown notifier and returns the process ID; it performs no draining itself ([`services.rs#L317-L333`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L317-L333)).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_LOCAL` ([`daemon_service.proto#L69`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L69)); see [method options](../types.md#method-options) |
| Allowed caller | `CALLER_CLASS_LOCAL` ([`daemon_service.proto#L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L71)) |
| Effect | `RPC_EFFECT_DAEMON_WRITE` ([`daemon_service.proto#L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L72)) |
| Required capability | `daemon.admin` ([`daemon_service.proto#L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L70)) |
| Implemented | Partial: signals shutdown; draining is left to the serving shell ([`services.rs#L323-L324`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L323-L324)) |
| In-process `LocalService` | Not routed; fails with `UNIMPLEMENTED` ([`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360)) |
| Advertised by GetCapabilities | Yes, as `BOTH` with no annotations ([`services.rs#L66`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L66), [`services.rs#L204-L212`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L204-L212)) |

**Request**: `ShutdownRequest` ([`daemon_messages.proto#L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L146)).

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `meta` | 1 | [`RequestMeta`](../types.md#requestmeta) | **Optional** | Idempotency header. When absent, an empty `request_id` is echoed ([`services.rs#L322`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L322), [`services.rs#L327-L331`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L327-L331)). |

**Response**: `ShutdownResponse` ([`daemon_messages.proto#L148-L151`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L148-L151)).

| Field | # | Type | Meaning |
|---|---|---|---|
| `pid` | 1 | `uint32` | The serving process's ID ([`services.rs#L326`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L326)). |
| `meta` | 100 | [`ResponseMeta`](../types.md#responsemeta) | Always set: echoed `request_id`, `replayed: false`, no `snapshot` ([`services.rs#L327-L331`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L327-L331)). |

**Errors**: the handler returns no error of its own ([`services.rs#L317-L333`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L317-L333)).

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| — | — | None | [`services.rs#L325-L332`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L325-L332) |

**Behaviour**:

- The handler calls `notify_one` on `DaemonState.shutdown` ([`services.rs#L323-L324`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L323-L324); [`state.rs#L286`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L286)). Nothing in libatomic waits on that notifier; what happens next, including whether in-flight requests finish, is decided by the serving shell ([`lib.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L22-L24)).
- The response is sent before the daemon stops, so a successful response does not mean the process has exited. A client that needs to know can poll [Health](#health) until the call fails or `pid` changes.
- The handler does not check the caller, so the `daemon.admin` requirement depends entirely on the serving shell ([`services.rs#L317-L333`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L317-L333)).

## Types

### WorkspaceInfo

Workspace context for a resolved path. The contract says it is returned only to local principals, that the CLI converts working-directory-relative arguments into root-relative paths, that the daemon's own working directory is never consulted, and that `workspace_id` is bound to this repository, root and current view ([`daemon_messages.proto#L114-L122`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L114-L122)). Used by [ResolveRepository](#resolverepository) and, in the contract, [RegisterRepository](#registerrepository) (which never sets it).

| Field | # | Type | Meaning |
|---|---|---|---|
| `workspace_id` | 1 | `string` | Always `"default"` ([`services.rs#L264`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L264)). |
| `root_path` | 2 | `string` | The canonical repository root ([`services.rs#L265`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L265); [`state.rs#L317-L318`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L317-L318)). |
| `relative_cwd` | 3 | `string` | The request path with the root stripped; empty when the path is the root or is not literally under it ([`services.rs#L257-L260`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L257-L260), [`services.rs#L266`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L266)). |
| `current_view` | 4 | [`ViewRef`](../types.md#viewref) | `name` is the view in `.atomic/current_view`, or `"dev"` when that file cannot be read; `view_id` is the BLAKE3 digest of the name ([`services.rs#L267-L272`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L267-L272); [`state.rs#L77-L85`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L77-L85)). Read without opening the database. |

### MethodDescriptorInfo

One row of the method matrix ([`daemon_messages.proto#L64-L71`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L64-L71)). Used by [GetCapabilities](#getcapabilities).

| Field | # | Type | Meaning |
|---|---|---|---|
| `service` | 1 | `string` | Unqualified service name, e.g. `DaemonService` ([`services.rs#L206`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L206), [`services.rs#L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L61)). |
| `method` | 2 | `string` | Rpc name, e.g. `Health` ([`services.rs#L207`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L207)). |
| `scope` | 3 | [`ExecutionScope`](../types.md#executionscope) | Advertised scope ([`daemon_messages.proto#L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L67)); see [Advertised method matrix](#advertised-method-matrix). |
| `required_capabilities` | 4 | `repeated string` | Advertised [capabilities](../types.md#required-capability) ([`daemon_messages.proto#L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L68)). |
| `allowed_callers` | 5 | `repeated` [`CallerClass`](../types.md#callerclass) | Advertised callers ([`daemon_messages.proto#L69`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L69)). |
| `effects` | 6 | `repeated` [`RpcEffect`](../types.md#rpceffect) | Advertised effects ([`daemon_messages.proto#L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L70)). |

### Limits

Negotiated limits, seeded from the legacy journal transport's defaults. The contract says the daemon may lower them per principal and never raise them above what clients negotiate ([`daemon_messages.proto#L38-L50`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L38-L50)). Used by [GetCapabilities](#getcapabilities).

| Field | # | Type | Meaning |
|---|---|---|---|
| `max_message_bytes` | 1 | `uint64` | Maximum message size ([`daemon_messages.proto#L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L39)). Advertised: 67108864 (64 MiB) ([`services.rs#L227`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L227)). |
| `max_frozen_page_fragments` | 2 | `uint32` | Fragments per frozen provenance page ([`daemon_messages.proto#L39-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L39-L40)). Advertised: 256 ([`services.rs#L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L228)). |
| `frozen_page_bytes` | 3 | `uint64` | Frozen page size ([`daemon_messages.proto#L39-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L39-L40)). Advertised: 1048576 (1 MiB) ([`services.rs#L229`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L229)). |
| `append_chunk_budget_bytes` | 4 | `uint64` | Client-side chunk size for appending provenance envelopes ([`daemon_messages.proto#L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L40)). Advertised: 4194304 (4 MiB) ([`services.rs#L230`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L230)). |
| `max_changes_per_request` | 5 | `uint32` | Advertised: 64 ([`services.rs#L231`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L231)). `SandboxService.GetChanges` refuses more with `ERROR_CODE_RESOURCE_EXHAUSTED` ([`services_sandbox.rs#L594-L612`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sandbox.rs#L594-L612)). |
| `max_inodes_per_request` | 6 | `uint32` | Advertised: 1024 ([`services.rs#L232`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L232)). `SandboxService.GetFileStates` does not enforce it ([`services_sandbox.rs#L564-L582`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sandbox.rs#L564-L582)); see [GetFileStates](./sandbox.md#getfilestates). |
| `max_write_queue_depth` | 7 | `uint32` | Advertised: 64 ([`services.rs#L233`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L233)). |

The values are constants; they do not vary by caller ([`services.rs#L226-L234`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L226-L234)).

### AuthRequirements

Authentication requirements for remote principals: TLS, an authenticated identity and audience-bound tokens. The TLS requirement refers to TCP; iroh provides its own authenticated transport ([`daemon_messages.proto#L52-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L52-L62)). Used by [GetCapabilities](#getcapabilities).

| Field | # | Type | Meaning |
|---|---|---|---|
| `tls_required` | 1 | `bool` | Whether TLS is required ([`daemon_messages.proto#L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L56)). Advertised: `false` ([`services.rs#L236`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L236)). |
| `audience_binding` | 2 | `bool` | Whether tokens must be audience-bound ([`daemon_messages.proto#L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L57)). Advertised: `false` ([`services.rs#L237`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L237)). |
| `identity_schemes` | 3 | `repeated string` | Accepted identity schemes ([`daemon_messages.proto#L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L58)). Advertised: `["did:atomic"]` ([`services.rs#L238`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L238)). |
| `credential_schemes` | 4 | `repeated string` | Accepted credential schemes, e.g. `sandbox-token-bin`, `bearer-jwt` ([`daemon_messages.proto#L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L59)). Advertised: `["local-socket-uid"]` ([`services.rs#L239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L239)). |

## Open questions

- **ResolveRepository registers.** The contract forbids registration on resolve ([`daemon_service.proto#L36-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L36-L40)); the handler does it ([`services.rs#L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L251)). It is not stated whether a later revision will make `ResolveRepository` fail for unregistered repositories, which would make `RegisterRepository` a required first step.
- **Empty or relative `path`.** `ResolveRepository` and `RegisterRepository` pass `path` to `Repository::find_root` unchanged ([`services.rs#L249`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L249), [`services.rs#L283`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L283)). How an empty or relative path is resolved is decided in `atomic-repository`, outside libatomic, and the contract says the daemon's working directory is never consulted ([`daemon_messages.proto#L115-L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L115-L116)).
- **Method matrix.** `CONTRACT.md` says the advertised matrix comes from the descriptors ([`CONTRACT.md#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L22-L24)). The handler advertises 58 hard-coded methods, most with no annotations ([`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119), [`services.rs#L204-L212`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L204-L212)), and omits 47 rpcs, some with handlers. Whether clients are expected to gate method use on the matrix at all, given this, is unclear.
- **Meaning of `full-turn-slice` and `repository-registry`.** Both tokens are returned ([`services.rs#L135-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L135-L138)), but libatomic does not say what a client may rely on when they are present.
- **Limits negotiation.** The contract speaks of limits that clients negotiate and that the daemon may lower per principal ([`daemon_messages.proto#L38-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L38-L41)), but `GetCapabilitiesRequest` carries nothing ([`daemon_messages.proto#L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L84)) and the values are constants ([`services.rs#L226-L234`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L226-L234)). Only `max_changes_per_request` is enforced in libatomic ([`services_sandbox.rs#L594-L612`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sandbox.rs#L594-L612)); the others are advisory there.
- **Advertised auth vs contract.** `AuthRequirements` advertises `local-socket-uid` ([`services.rs#L239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L239)), but libatomic contains no code that reads a socket peer's user ID; how `atomicd` applies it is outside libatomic ([`lib.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L22-L24)).
- **Shutdown semantics.** The contract promises draining, closing databases and releasing locks ([`daemon_service.proto#L66-L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_service.proto#L66-L67)); the handler only signals a notifier that nothing in libatomic awaits ([`services.rs#L323-L324`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L323-L324)). Whether `atomicd` drains, and how long the process lives after the response, is not visible in libatomic.
- **`ListRepositories.open`.** Always `true` ([`services.rs#L310`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L310)), contrary to its documented meaning ([`daemon_messages.proto#L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L141)).
- **`WorkspaceInfo.workspace_id`.** The contract binds it to the repository, root and current view ([`daemon_messages.proto#L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/daemon/daemon_messages.proto#L116)); the handler always returns `"default"` ([`services.rs#L264`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L264)), and no handler reads a workspace ID back ([RepositoryRef](../types.md#repositoryref)).

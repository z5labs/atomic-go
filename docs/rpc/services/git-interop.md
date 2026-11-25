# GitInteropService

GitInteropService is the contract for the Git import and push pipelines. The proto says Git interop is LOCAL-only because working copies and Git mirrors are local, that `git hooks install/uninstall` has no RPC because it only writes `.git/hooks` shim files, and that import may create the repository on its first run [`git_interop_messages.proto#L1-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L1-L6). The service definition is marked DRAFT, add-only [`git_interop_service.proto#L1-L3`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L1-L3).

**Not implemented in libatomic at this commit.** libatomic has no GitInteropService handler. The daemon module declarations include no Git interop module [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57), METHOD_MATRIX lists no GitInteropService method [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119), and LocalService answers `UNIMPLEMENTED` for every method it does not route [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360). Everything below is the contract.

**Where it is meant to be served.** Every rpc declares `EXECUTION_SCOPE_LOCAL` and `CALLER_CLASS_LOCAL` [`git_interop_service.proto#L12-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L12-L46), so it targets the local daemon and local callers only. COVERAGE.md lists the service as LOCAL [`COVERAGE.md#L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L41) and maps `atomic git import` (with `--dry-run` going to PreviewGitImport) and `atomic git push` onto these rpcs [`COVERAGE.md#L198-L199`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L198-L199). Sandbox tokens may never be delegated Git interop rights [`CONTRACT.md#L152-L153`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L152-L153).

| Method | Implemented | In-process `LocalService` | Advertised by GetCapabilities |
|---|---|---|---|
| [ImportFromGit](#importfromgit) | no | not routed | no |
| [PreviewGitImport](#previewgitimport) | no | not routed | no |
| [PushToGit](#pushtogit) | no | not routed | no |

## Methods

### ImportFromGit

`rpc ImportFromGit(ImportFromGitRequest) returns (ImportFromGitResponse)`, unary [`git_interop_service.proto#L16-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L16-L24).

Contract: imports Git history as Atomic changes and views for `atomic git import [--branch|--all|--incremental|--with-crdt]`. In its bootstrap form it also registers the repository and workspace, with the same ownership and identity checks as registration [`git_interop_service.proto#L13-L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L13-L15). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`git_interop_service.proto#L17-L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L17-L23).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_LOCAL` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_REPOSITORY_WRITE`, `RPC_EFFECT_WORKSPACE_WRITE` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `gitinterop.write`, `repository.write` and `working-copy.write` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `ImportFromGitRequest` [`git_interop_messages.proto#L16-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L16-L28)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Contract only** | Absent selects the bootstrap (create) form [`git_interop_messages.proto#L17`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L17). For an existing repository the reference must carry its registered `workspace_id` [`git_interop_messages.proto#L25-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L25-L27). |
| `meta` | 2 | [RequestMeta](../types.md#requestmeta) | **Contract only** | Mutation header [`git_interop_messages.proto#L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L18). |
| `source_path` | 3 | `optional string` | **Contract only** | The local Git checkout (LOCAL scope) [`git_interop_messages.proto#L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L19). |
| `branch` | 4 | `optional string` | **Contract only** | Branch to import (`--branch`) [`git_interop_messages.proto#L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L20) [`git_interop_service.proto#L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L13). |
| `all_branches` | 5 | `bool` | **Contract only** | Import every branch (`--all`) [`git_interop_messages.proto#L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L21). |
| `incremental` | 6 | `bool` | **Contract only** | Incremental import, used by the post-commit hook shims [`git_interop_messages.proto#L22`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L22). |
| (reserved) | 7 | | | Reserved [`git_interop_messages.proto#L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L23). |
| `with_crdt` | 8 | `bool` | **Contract only** | `--with-crdt` [`git_interop_messages.proto#L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L24). |
| `destination_path` | 9 | `optional string` | **Contract only** | Required only when `repository` is absent; bootstrap never guesses a destination from the daemon's cwd or `source_path` [`git_interop_messages.proto#L25-L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L25-L27). |

**Response**: `ImportFromGitResponse` [`git_interop_messages.proto#L29-L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L29-L35)

| Field | # | Type | Meaning |
|---|---|---|---|
| `changes_imported` | 1 | `uint64` | Changes imported [`git_interop_messages.proto#L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L30). |
| `views_created` | 2 | `repeated string` | Views created [`git_interop_messages.proto#L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L31). |
| (reserved) | 3 | | Reserved [`git_interop_messages.proto#L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L32). |
| `repository` | 4 | [RepositoryRef](../types.md#repositoryref) | The repository, including its registered workspace, also on bootstrap [`git_interop_messages.proto#L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L33). |
| `meta` | 100 | [ResponseMeta](../types.md#responsemeta) | Mutation response header [`git_interop_messages.proto#L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L34). |

**Errors**

No handler exists, so libatomic defines no errors for this method beyond LocalService's refusal.

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService ("local method is not supported: …") | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- Bootstrap Git import requires an explicit destination when no `RepositoryRef` exists, and no operation uses atomicd's cwd [`CONTRACT.md#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L113-L117).
- Ordinary mutations require a non-empty UUID `request_id`. Replay keys bind the repository, or a daemon/bootstrap namespace when there is none [`CONTRACT.md#L54-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L54-L58).
- Import may create the repository on first run, which the proto calls bootstrap-adjacent [`git_interop_messages.proto#L5-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L5-L6); COVERAGE.md marks the command "direct-RW (or creates)" [`COVERAGE.md#L198`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L198).

### PreviewGitImport

`rpc PreviewGitImport(PreviewGitImportRequest) returns (PreviewGitImportResponse)`, unary [`git_interop_service.proto#L25-L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L25-L32).

Contract: previews a Git import without applying it; it is the target of `atomic git import --dry-run` [`COVERAGE.md#L198`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L198). CONTRACT.md says PreviewGitImport cannot apply or create a repository [`CONTRACT.md#L331-L333`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L331-L333). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`git_interop_service.proto#L26-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L26-L31).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_LOCAL` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_REPOSITORY_READ`, `RPC_EFFECT_WORKSPACE_READ` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `gitinterop.read` and `working-copy.read` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `PreviewGitImportRequest` [`git_interop_messages.proto#L51-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L51-L58)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | `optional` [RepositoryRef](../types.md#repositoryref) | **Contract only** | Declared with explicit presence [`git_interop_messages.proto#L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L52). |
| `source_path` | 2 | `string` | **Contract only** | Git checkout to read; local principal only; the preview reads the source and never initializes [`git_interop_messages.proto#L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L53). |
| `branch` | 3 | `optional string` | **Contract only** | Branch to preview [`git_interop_messages.proto#L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L54). |
| `all_branches` | 4 | `bool` | **Contract only** | Preview every branch [`git_interop_messages.proto#L55`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L55). |
| `incremental` | 5 | `bool` | **Contract only** | Incremental preview [`git_interop_messages.proto#L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L56). |
| `with_crdt` | 6 | `bool` | **Contract only** | Preview with CRDT [`git_interop_messages.proto#L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L57). |

**Response**: `PreviewGitImportResponse` [`git_interop_messages.proto#L59-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L59-L62)

| Field | # | Type | Meaning |
|---|---|---|---|
| `changes` | 1 | `uint64` | Change count; the proto gives no further comment [`git_interop_messages.proto#L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L60). |
| `views` | 2 | `repeated string` | View names; the proto gives no further comment [`git_interop_messages.proto#L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L61). |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- The request carries no `RequestMeta` and the response no `ResponseMeta` [`git_interop_messages.proto#L51-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L51-L62); COVERAGE.md lists it among the read/write splits, as a preview separate from the importing rpc [`COVERAGE.md#L342`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L342).

### PushToGit

`rpc PushToGit(PushToGitRequest) returns (PushToGitResponse)`, unary [`git_interop_service.proto#L35-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L35-L45).

Contract: runs the shadow-commit pipeline and validator and pushes Atomic state to Git, for `atomic git push [-m|--no-push|--branch]` [`git_interop_service.proto#L33-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L33-L34). **Not implemented in libatomic at this commit.**

All values below come from the rpc's [method options](../types.md#method-options) [`git_interop_service.proto#L36-L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L36-L44).

| Attribute | Value |
|---|---|
| Execution scope | `EXECUTION_SCOPE_LOCAL` ([ExecutionScope](../types.md#executionscope)) |
| Allowed caller | `CALLER_CLASS_LOCAL` ([CallerClass](../types.md#callerclass)) |
| Effect | `RPC_EFFECT_REPOSITORY_READ`, `RPC_EFFECT_WORKSPACE_WRITE`, `RPC_EFFECT_EXTERNAL_IO`, `RPC_EFFECT_DAEMON_WRITE` ([RpcEffect](../types.md#rpceffect)) |
| Required capability | `gitinterop.write`, `repository.read` and `working-copy.write` ([required capability](../types.md#required-capability)) |
| Implemented | no; libatomic has no handler [`mod.rs#L41-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/mod.rs#L41-L57) |
| In-process `LocalService` | not routed; answers `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) (see [LocalService](../transport.md#in-process-localservice)) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `PushToGitRequest` [`git_interop_messages.proto#L37-L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L37-L43)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Contract only** | The repository to push [`git_interop_messages.proto#L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L38). |
| `meta` | 2 | [RequestMeta](../types.md#requestmeta) | **Contract only** | Mutation header [`git_interop_messages.proto#L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L39). |
| `message` | 3 | `optional string` | **Contract only** | The `-m` value [`git_interop_messages.proto#L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L40). |
| `no_push` | 4 | `bool` | **Contract only** | `--no-push`: create shadow commits only [`git_interop_messages.proto#L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L41). |
| `branch` | 5 | `optional string` | **Contract only** | Target branch (`--branch`) [`git_interop_messages.proto#L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L42). |

**Response**: `PushToGitResponse` [`git_interop_messages.proto#L44-L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L44-L49)

| Field | # | Type | Meaning |
|---|---|---|---|
| `shadow_commit` | 1 | `optional string` | The shadow commit; the proto gives no further comment [`git_interop_messages.proto#L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L45). |
| `pushed` | 2 | `bool` | Push outcome flag; `no_push` requests shadow commits only [`git_interop_messages.proto#L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L41) [`git_interop_messages.proto#L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L46). |
| `validation_findings` | 3 | `repeated string` | Findings of the validator named in the rpc comment [`git_interop_messages.proto#L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L47). |
| `meta` | 100 | [ResponseMeta](../types.md#responsemeta) | Mutation response header [`git_interop_messages.proto#L48`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L48). |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `UNIMPLEMENTED` | none (plain status) | Called through LocalService | [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |

**Behaviour (contract)**

- The declared effects include external I/O and daemon writes. For filesystem-only and external operations, CONTRACT.md says the daemon outcome ledger and explicit recovery apply, and that a request ID does not make network effects ACID [`CONTRACT.md#L65-L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L65-L67).

## Types

No message of this service is used by more than one method.

## Open questions

- No server implementation exists in libatomic, and the atomicd transport that would register one lives outside it [`lib.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L22-L24). The protos do not say which slice or phase delivers GitInteropService; they only fix it to the local daemon [`git_interop_service.proto#L12-L46`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_service.proto#L12-L46).
- What a network client receives when calling these methods depends on the transport, which libatomic does not contain [`lib.rs#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/lib.rs#L22-L24); only LocalService's `UNIMPLEMENTED` is defined here [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360).
- How bootstrap ImportFromGit identifies the repository for replay when `repository` is absent ("daemon/bootstrap namespace") is not specified further [`CONTRACT.md#L56-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L56-L57).
- `PreviewGitImportRequest.repository` is an explicitly optional message, but the proto does not say what a preview does differently with or without it [`git_interop_messages.proto#L51-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L51-L58).
- The meaning of `PreviewGitImportResponse.views` (views that would be created, or touched) and of `PushToGitResponse.shadow_commit` (a Git commit ID or another form) is not defined beyond the field names [`git_interop_messages.proto#L44-L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L44-L49) [`git_interop_messages.proto#L59-L62`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/git_interop/git_interop_messages.proto#L59-L62).

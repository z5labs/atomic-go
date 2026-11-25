# TagService

TagService manages tags, which the contract describes as named Merkle-state snapshots kept as repository metadata, readable and writable in both scopes [`tag_messages.proto#L1-L6`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L1-L6). It backs `atomic tag create|delete|list|show` [`COVERAGE.md#L157`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L157). The service definition is marked DRAFT and add-only [`tag_service.proto#L1-L3`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L1-L3). Every rpc has a handler in `TagImpl` [`services_tag.rs#L69-L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L69-L70), which reproduces the CLI's local tag bodies and returns the full tag record so the client can render the same reports [`services_tag.rs#L1-L8`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L1-L8). ListTags and GetTag implement their contract; CreateTag and DeleteTag are partial because they ignore the state and fence fields the contract defines.

The points below apply to every method.

- **Repository resolution.** Each handler calls `unwrap()` on `repository` [`services_tag.rs#L76`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L76) [`services_tag.rs#L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L113) [`services_tag.rs#L136`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L136) [`services_tag.rs#L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L172), so a request without it panics in the handler instead of returning a status (see [Open questions](#open-questions)). A present reference is resolved by `DaemonState::resolve`, which refuses any authority other than `local` and any unregistered `repository_id` [`state.rs#L329-L348`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L329-L348). Send the reference that [ResolveRepository](./daemon.md#resolverepository) returned.
- **Serialization and handles.** CreateTag and DeleteTag take the repository's exclusive gate [`services_tag.rs#L78-L79`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L78-L79) [`services_tag.rs#L115-L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L115-L116) and open the database through the writable path [`services_tag.rs#L85`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L85) [`services_tag.rs#L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L120) [`state.rs#L100-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L100-L110). ListTags and GetTag take no gate and open the database read-only [`services_tag.rs#L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L142) [`services_tag.rs#L176`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L176) [`state.rs#L219-L229`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L219-L229), matching COVERAGE's classification of `tag list` and `tag show` as read-only [`COVERAGE.md#L300`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/COVERAGE.md?plain=1#L300).
- **RequestMeta.** CreateTag and DeleteTag echo `request_id` with `replayed = false` when the request carries `meta`, and return no ResponseMeta when it does not [`services_agent.rs#L1233-L1239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L1233-L1239). Nothing deduplicates a repeated `request_id`, although the contract requires replay for ordinary mutations [`CONTRACT.md#L54-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L54-L58).
- **Not advertised, not routed.** No TagService method is in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119), so GetCapabilities does not advertise them. The in-process LocalService does not route them and returns `UNIMPLEMENTED` [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360); see [LocalService](../transport.md#in-process-localservice).
- **Transport.** Contract-version metadata, authentication and caller-class checks are transport concerns; see [version negotiation](../transport.md#version-negotiation) and [authentication and authorization](../transport.md#authentication-and-authorization). The methods are declared for remote identities, but these handlers serve only the `local` authority [`state.rs#L332-L340`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L332-L340).

<a id="common-errors"></a>**Errors common to every method.** Error codes are the [ErrorCode](../errors.md#errorcode) values; `domain_status` maps each to a gRPC status [`state.rs#L432-L446`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L432-L446) and puts the code in an `atomic:<CODE>:` message prefix [`state.rs#L447-L452`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L447-L452); see [how handlers produce errors](../errors.md#how-handlers-produce-errors).

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `INVALID_ARGUMENT` | `INVALID_ARGUMENT` | `repository.authority` is not `local` | [`state.rs#L332-L340`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L332-L340) |
| `NOT_FOUND` | `REPOSITORY_NOT_FOUND` | `repository_id` is not registered with this daemon | [`state.rs#L341-L347`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L341-L347) |
| `INTERNAL` | `REPOSITORY` | The database cannot be opened | [`state.rs#L100-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L100-L110), [`state.rs#L219-L229`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L219-L229) |
| `INTERNAL` | none | The blocking task fails; the message has no `atomic:` prefix | [`services_tag.rs#L100-L101`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L100-L101) |

## Methods

### CreateTag

`rpc CreateTag(CreateTagRequest) returns (CreateTagResponse)`, unary [`tag_service.proto#L13-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L13-L20).

Creates a tag, for `atomic tag create <NAME>` [`tag_service.proto#L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L13). The handler refuses a name that already resolves to a tag unless `force` is set, in which case it deletes that tag first; it then creates a release tag with the requested name and message [`services_tag.rs#L86-L97`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L86-L97).

| Attribute ([method options](../types.md#method-options)) | Value |
|---|---|
| [Execution scope](../types.md#executionscope) | `EXECUTION_SCOPE_BOTH` [`tag_service.proto#L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L15) |
| [Allowed caller](../types.md#callerclass) | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` [`tag_service.proto#L17-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L17-L18) |
| [Effect](../types.md#rpceffect) | `RPC_EFFECT_REPOSITORY_WRITE` [`tag_service.proto#L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L19) |
| [Required capability](../types.md#required-capability) | `repository.write` [`tag_service.proto#L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L16) |
| Implemented | partial: `state`, `view` and `expected_snapshot` are not read [`services_tag.rs#L71-L106`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L71-L106) |
| In-process [LocalService](../transport.md#in-process-localservice) | not routed [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `CreateTagRequest` [`tag_messages.proto#L18-L30`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L18-L30)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Required** | The repository; the handler unwraps it without a check [`services_tag.rs#L76`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L76) |
| `meta` | 2 | [RequestMeta](../types.md#requestmeta) | **Optional** | Echoed in the response when present [`services_tag.rs#L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L80) [`services_tag.rs#L104`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L104) |
| `name` | 3 | `string` | **Unconfirmed** | The tag name, passed to the domain as given [`services_tag.rs#L81`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L81) [`services_tag.rs#L88-L97`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L88-L97); libatomic does not check it for emptiness |
| `state` | 4 | `optional` [Hash](../types.md#hash) | **Ignored** | The contract: the Merkle state to snapshot, defaulting to the named view's own Merkle [`tag_messages.proto#L22-L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L22-L23). The handler reads only `meta`, `name`, `message` and `force` [`services_tag.rs#L80-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L80-L83) |
| `message` | 5 | `optional string` | **Optional** | The annotation message, passed to the domain [`services_tag.rs#L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L82) [`services_tag.rs#L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L96) |
| `view` | 6 | [ViewRef](../types.md#viewref) | **Ignored** | Not read [`services_tag.rs#L80-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L80-L83); the create call names no view [`services_tag.rs#L95-L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L95-L96) |
| `expected_snapshot` | 7 | [ViewSnapshot](../types.md#viewsnapshot) | **Ignored** | Not read [`services_tag.rs#L80-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L80-L83) |
| `force` | 8 | `bool` | **Optional** | Overwrite an existing tag of this name after deleting it first [`tag_messages.proto#L27-L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L27-L29) [`services_tag.rs#L88-L94`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L88-L94). Default false |

**Response**: `CreateTagResponse` [`tag_messages.proto#L31-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L31-L36)

| Field | # | Type | Meaning |
|---|---|---|---|
| `tag` | 1 | [TagInfo](#taginfo) | Always set: the created tag as the domain stored it [`services_tag.rs#L95-L98`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L95-L98) [`services_tag.rs#L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L103) |
| `meta` | 100 | [ResponseMeta](../types.md#responsemeta) | Set when the request carried `meta`: echoed `request_id`, `replayed = false` [`services_tag.rs#L104`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L104) [`services_agent.rs#L1233-L1239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L1233-L1239) |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `INVALID_ARGUMENT` | `INVALID_ARGUMENT` | A tag of this name exists and `force` is not set; message `Tag '<name>' already exists. Use --force to overwrite.` | [`services_tag.rs#L47-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L47-L52), [`services_tag.rs#L88-L93`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L88-L93) |
| `INVALID_ARGUMENT` | `INVALID_ARGUMENT` | The domain create reports that the tag already exists, or that the name is invalid | [`services_tag.rs#L57-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L57-L61) |
| `PERMISSION_DENIED` | `VIEW` | The domain create reports that the view does not exist | [`services_tag.rs#L62-L64`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L62-L64), [`state.rs#L442`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L442) |
| `INTERNAL` | `REPOSITORY` | Any other domain failure of the create | [`services_tag.rs#L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L65) |
| | | Everything in [common errors](#common-errors) | |

**Behaviour**

- The kind is always `TagKind::Release` [`services_tag.rs#L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L96).
- `force` is not atomic. The existing tag is deleted and the result of that delete is discarded; if the create then fails, nothing restores the deleted tag [`services_tag.rs#L88-L97`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L88-L97).
- An error from the existence lookup is treated as "no existing tag": only `Ok(Some(_))` triggers the refusal or the delete [`services_tag.rs#L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L88).
- The tag's view, sequence and state are chosen by the domain; read them from the returned `tag` [`services_tag.rs#L30-L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L30-L38).

### DeleteTag

`rpc DeleteTag(DeleteTagRequest) returns (DeleteTagResponse)`, unary [`tag_service.proto#L21-L28`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L21-L28).

Deletes a tag by name, for `atomic tag delete <NAME>` [`tag_service.proto#L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L21). The handler calls the domain `delete_tag` and returns its result [`services_tag.rs#L119-L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L119-L128).

| Attribute ([method options](../types.md#method-options)) | Value |
|---|---|
| [Execution scope](../types.md#executionscope) | `EXECUTION_SCOPE_BOTH` [`tag_service.proto#L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L23) |
| [Allowed caller](../types.md#callerclass) | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` [`tag_service.proto#L25-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L25-L26) |
| [Effect](../types.md#rpceffect) | `RPC_EFFECT_REPOSITORY_WRITE` [`tag_service.proto#L27`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L27) |
| [Required capability](../types.md#required-capability) | `repository.write` [`tag_service.proto#L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L24) |
| Implemented | partial: the required `expected_state` fence is not read [`services_tag.rs#L108-L129`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L108-L129) |
| In-process [LocalService](../transport.md#in-process-localservice) | not routed [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `DeleteTagRequest` [`tag_messages.proto#L38-L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L38-L43)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Required** | The repository; the handler unwraps it without a check [`services_tag.rs#L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L113) |
| `meta` | 2 | [RequestMeta](../types.md#requestmeta) | **Optional** | Echoed in the response when present [`services_tag.rs#L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L117) [`services_tag.rs#L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L127) |
| `name` | 3 | `string` | **Unconfirmed** | The tag name, passed to the domain as given [`services_tag.rs#L118`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L118) [`services_tag.rs#L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L121); libatomic does not check it for emptiness |
| `expected_state` | 4 | [Hash](../types.md#hash) | **Ignored** | The contract marks it required, to refuse deleting a raced tag [`tag_messages.proto#L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L42); the handler reads only `meta` and `name` [`services_tag.rs#L117-L118`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L117-L118) |

**Response**: `DeleteTagResponse` [`tag_messages.proto#L44-L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L44-L49)

| Field | # | Type | Meaning |
|---|---|---|---|
| `deleted` | 1 | `bool` | Whether the named tag existed and was deleted [`tag_messages.proto#L45-L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L45-L47); the domain's return value [`services_tag.rs#L119-L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L119-L126) |
| `meta` | 100 | [ResponseMeta](../types.md#responsemeta) | Set when the request carried `meta` [`services_tag.rs#L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L127) [`services_agent.rs#L1233-L1239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L1233-L1239) |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `INTERNAL` | `REPOSITORY` | The domain delete fails | [`services_tag.rs#L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L121) |
| | | Everything in [common errors](#common-errors) | |

**Behaviour**

- A missing tag is reported through `deleted = false`, from which the client renders its not-exist hint [`tag_messages.proto#L45-L47`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L45-L47), rather than through an error status.
- There is no fence: a tag replaced between the caller's read and this call is deleted anyway (`expected_state` is not read).

### ListTags

`rpc ListTags(ListTagsRequest) returns (ListTagsResponse)`, unary [`tag_service.proto#L29-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L29-L36).

Lists tags, for `atomic tag list` [`tag_service.proto#L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L29). With `view` set the handler lists that view's tags; otherwise it lists every tag across views; it then applies the pattern and annotated-only filters [`services_tag.rs#L143-L157`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L143-L157).

| Attribute ([method options](../types.md#method-options)) | Value |
|---|---|
| [Execution scope](../types.md#executionscope) | `EXECUTION_SCOPE_BOTH` [`tag_service.proto#L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L31) |
| [Allowed caller](../types.md#callerclass) | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` [`tag_service.proto#L33-L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L33-L34) |
| [Effect](../types.md#rpceffect) | `RPC_EFFECT_REPOSITORY_READ` [`tag_service.proto#L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L35) |
| [Required capability](../types.md#required-capability) | `repository.read` [`tag_service.proto#L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L32) |
| Implemented | yes [`services_tag.rs#L131-L165`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L131-L165) |
| In-process [LocalService](../transport.md#in-process-localservice) | not routed [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `ListTagsRequest` [`tag_messages.proto#L51-L60`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L51-L60)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Required** | The repository; the handler unwraps it without a check [`services_tag.rs#L136`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L136) |
| `view` | 2 | `optional string` | **Optional** | Present: list that view's tags; absent: every tag across views [`services_tag.rs#L143-L148`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L143-L148) |
| `pattern` | 3 | `optional string` | **Optional** | Keep tags whose name contains the pattern after every `*` is removed [`services_tag.rs#L149-L154`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L149-L154) |
| `annotated_only` | 4 | `bool` | **Optional** | Keep only annotated tags [`services_tag.rs#L155-L157`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L155-L157). Default false |

**Response**: `ListTagsResponse` [`tag_messages.proto#L61-L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L61-L63)

| Field | # | Type | Meaning |
|---|---|---|---|
| `tags` | 1 | `repeated` [TagInfo](#taginfo) | The tags after filtering, in the domain's order [`services_tag.rs#L162-L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L162-L164) |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `INTERNAL` | `REPOSITORY` | The domain listing fails | [`services_tag.rs#L146-L147`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L146-L147) |
| | | Everything in [common errors](#common-errors) | |

**Behaviour**

- `pattern` is a substring test, not a glob: `v1*` becomes `v1` and matches any name containing `v1` [`services_tag.rs#L151-L153`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L151-L153).
- No paging: every matching tag comes back in one response [`services_tag.rs#L162-L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L162-L164).
- The response carries no ResponseMeta [`tag_messages.proto#L61-L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L61-L63).

### GetTag

`rpc GetTag(GetTagRequest) returns (GetTagResponse)`, unary [`tag_service.proto#L37-L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L37-L44).

Returns one tag by name, for `atomic tag show <NAME>` [`tag_service.proto#L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L37). For a review-gate tag the handler also resolves each change hash listed in the tag's metadata to its presence in the store and the views carrying it [`services_tag.rs#L180-L215`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L180-L215).

| Attribute ([method options](../types.md#method-options)) | Value |
|---|---|
| [Execution scope](../types.md#executionscope) | `EXECUTION_SCOPE_BOTH` [`tag_service.proto#L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L39) |
| [Allowed caller](../types.md#callerclass) | `CALLER_CLASS_LOCAL`, `CALLER_CLASS_REMOTE_IDENTITY` [`tag_service.proto#L41-L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L41-L42) |
| [Effect](../types.md#rpceffect) | `RPC_EFFECT_REPOSITORY_READ` [`tag_service.proto#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L43) |
| [Required capability](../types.md#required-capability) | `repository.read` [`tag_service.proto#L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_service.proto#L40) |
| Implemented | yes [`services_tag.rs#L167-L221`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L167-L221) |
| In-process [LocalService](../transport.md#in-process-localservice) | not routed [`local.rs#L358-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/local.rs#L358-L360) |
| Advertised by GetCapabilities | no, not in METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) |

**Request**: `GetTagRequest` [`tag_messages.proto#L65-L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L65-L68)

| Field | # | Type | Required | Meaning |
|---|---|---|---|---|
| `repository` | 1 | [RepositoryRef](../types.md#repositoryref) | **Required** | The repository; the handler unwraps it without a check [`services_tag.rs#L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L172) |
| `name` | 2 | `string` | **Unconfirmed** | The tag name, passed to the domain lookup as given [`services_tag.rs#L174`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L174) [`services_tag.rs#L177`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L177); libatomic does not check it for emptiness |

**Response**: `GetTagResponse` [`tag_messages.proto#L69-L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L69-L75)

| Field | # | Type | Meaning |
|---|---|---|---|
| `tag` | 1 | [TagInfo](#taginfo) | Set when a tag of that name is found; unset, with an OK status, when none is [`services_tag.rs#L177-L179`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L177-L179) [`services_tag.rs#L216-L220`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L216-L220) |
| `records` | 2 | `repeated TagRecordPresence` | Only for a tag of kind `ReviewGate` whose metadata has a `changes.original_hashes` array: one entry per string element, in order; other elements are skipped [`services_tag.rs#L184-L192`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L184-L192). Empty otherwise [`services_tag.rs#L183`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L183) |

`TagRecordPresence` [`tag_messages.proto#L92-L99`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L92-L99):

| Field | # | Type | Meaning |
|---|---|---|---|
| `hash` | 1 | `string` | The element's text as stored [`services_tag.rs#L199`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L199) [`services_tag.rs#L206`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L206) |
| `present` | 2 | `bool` | Whether the change exists in the store [`services_tag.rs#L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L195); false when the text does not parse [`services_tag.rs#L207`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L207) |
| `views` | 3 | `repeated string` | The views carrying the change [`services_tag.rs#L196-L197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L196-L197); empty when the lookup fails [`services_tag.rs#L197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L197) or the text does not parse [`services_tag.rs#L208`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L208) |
| `parseable` | 4 | `bool` | Whether the text parsed as a base32 hash [`services_tag.rs#L193-L210`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L193-L210); the local render prints unparseable text raw [`tag_messages.proto#L97-L98`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L97-L98) |

**Errors**

| gRPC status | ErrorCode | When | Source |
|---|---|---|---|
| `INTERNAL` | `REPOSITORY` | The domain lookup fails | [`services_tag.rs#L177`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L177) |
| | | Everything in [common errors](#common-errors) | |

**Behaviour**

- A missing tag is not an error: check whether `tag` is set [`services_tag.rs#L177-L179`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L177-L179).
- The response carries no ResponseMeta [`tag_messages.proto#L69-L75`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L69-L75).

## Types

### TagInfo

The full domain tag record on the wire, used by [CreateTag](#createtag), [ListTags](#listtags) and [GetTag](#gettag) [`tag_messages.proto#L77-L90`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L77-L90). Fields 5 to 9 are add-only and carry what the local list and show render [`tag_messages.proto#L82-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L82-L84). The handlers build it with `tag_info` [`services_tag.rs#L28-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L28-L45).

| Field | # | Type | Meaning |
|---|---|---|---|
| `name` | 1 | `string` | The tag name [`services_tag.rs#L32`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L32) |
| `state` | 2 | [Hash](../types.md#hash) | Always set: the tagged Merkle state [`services_tag.rs#L33`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L33) |
| `message` | 3 | `optional string` | The annotation message, when the tag has one [`services_tag.rs#L34`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L34) |
| `created_at` | 4 | `optional google.protobuf.Timestamp` | Always set: the record's timestamp [`services_tag.rs#L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L35) |
| `view` | 5 | `string` | The owning view [`tag_messages.proto#L85`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L85) [`services_tag.rs#L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L36) |
| `sequence` | 6 | `uint64` | The log sequence [`tag_messages.proto#L86`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L86) [`services_tag.rs#L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L37) |
| `kind` | 7 | `string` | The tag-kind label [`tag_messages.proto#L87`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L87), the domain kind's text form [`services_tag.rs#L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L38). CreateTag always creates `TagKind::Release` [`services_tag.rs#L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L96); GetTag treats `TagKind::ReviewGate` specially [`services_tag.rs#L184`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L184) |
| `author` | 8 | `optional` [Author](../types.md#author) | The annotation author, when the tag has one [`tag_messages.proto#L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L88) [`services_tag.rs#L39-L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L39-L42) |
| `metadata` | 9 | `optional string` | The extensible metadata, as JSON text [`tag_messages.proto#L89`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L89) [`services_tag.rs#L43`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L43) |

## Open questions

- **Missing `repository` panics.** All four handlers call `unwrap()` on `repository` [`services_tag.rs#L76`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L76) [`services_tag.rs#L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L113) [`services_tag.rs#L136`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L136) [`services_tag.rs#L172`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L172), unlike RepositoryMutationService, which substitutes an empty reference that resolution refuses with `INVALID_ARGUMENT` [`services.rs#L1414-L1416`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L1414-L1416) [`state.rs#L332-L340`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/state.rs#L332-L340). What a caller observes (a reset stream, an `UNKNOWN` or `INTERNAL` status) depends on the transport, which is not in libatomic.
- **CreateTag ignores the target and fence.** The contract has CreateTag snapshot an explicit `state` (default: the named view's own Merkle) on an explicit `view`, fenced by `expected_snapshot` [`tag_messages.proto#L22-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L22-L26). The handler reads none of them and calls the domain create without a view [`services_tag.rs#L80-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L80-L83) [`services_tag.rs#L95-L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L95-L96). Which view and state the tag records is decided in atomic-repository and cannot be cited here.
- **DeleteTag ignores `expected_state`.** The contract marks it required, to refuse deleting a raced tag [`tag_messages.proto#L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/services/tag/tag_messages.proto#L42); the handler never reads it [`services_tag.rs#L117-L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L117-L121).
- **Which view a name lookup searches.** CreateTag's existence check, DeleteTag and GetTag pass only a name to the domain [`services_tag.rs#L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L88) [`services_tag.rs#L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L121) [`services_tag.rs#L177`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L177), while ListTags without `view` explicitly lists across views [`services_tag.rs#L143-L147`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L143-L147). Whether a name lookup covers one view or all views is decided in atomic-repository.
- **Name validation.** The handler maps a domain `InvalidTagName` error to `INVALID_ARGUMENT` [`services_tag.rs#L58-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L58-L61) but performs no validation of its own [`services_tag.rs#L81`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L81); which names (including the empty string) the domain refuses is not visible in libatomic.
- **Tag-kind vocabulary.** `kind` is the domain kind's text form [`services_tag.rs#L38`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L38); libatomic references only the `Release` and `ReviewGate` variants [`services_tag.rs#L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L96) [`services_tag.rs#L184`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_tag.rs#L184) and does not define the strings, so a client cannot learn the full set of values from libatomic.
- **ResponseMeta presence.** The contract says ResponseMeta is present on every successful ordinary mutation response [`common.proto#L111-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/atomic/common/common.proto#L111-L117); CreateTag and DeleteTag return it only when the request carried `meta` [`services_agent.rs#L1233-L1239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_agent.rs#L1233-L1239).
- **Discovery.** TagService is absent from METHOD_MATRIX [`services.rs#L60-L119`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services.rs#L60-L119) although its handlers exist; the contract says the advertised matrix comes from the descriptors [`CONTRACT.md#L22-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/proto/CONTRACT.md?plain=1#L22-L24). Whether a client may call TagService on an endpoint that does not advertise it is not stated.

# atomic-go
A client for working with Atomic repositories in Go

## Documentation

- [Atomic RPC API reference](docs/rpc/README.md): the gRPC contract that libatomic defines and serves, method by method, with every statement cited to the libatomic source.
- [Atomic remote protocol reference](docs/remote/README.md): the HTTP protocol the Atomic client speaks to a hosted server, request by request, and the V3 change-file format byte by byte, with every statement cited to the atomicdotdev/atomic source.

## Wiring a client at the program's edge

Nothing in this module reaches for the network, the clock or a random source
itself: the program's `main` builds each one and passes it in. A push wires
together like this, and
[`syncflow.ExamplePush`](remote/syncflow/authenticated_test.go) runs the same
wiring against in-memory fakes:

```go
// The machine's network, the one place it is named.
network := host.IPNetwork{} // github.com/z5labs/bedrock/host
base := &http.Transport{DialContext: network.DialTCP}

// Credentials: the identities and keys come from the program's own store.
issuer, err := auth.NewIssuer(store, time.Now, rand.Reader) // crypto/rand

// The project protocol (push, pull, clone). Tokens are minted per request
// for auth.ProjectAudience, and auth.Transport attaches them only to
// requests bound for the project's origin.
proj, _, err := serverurl.ParseProject(rawURL)
bearer, err := issuer.ForRemote(ctx, proj, sel, auth.ProjectAudience(proj))
tr, err := auth.NewTransport(base, bearer, proj.Origin())
client, err := project.New(proj, &http.Client{Transport: tr}, logger, version)
res, err := syncflow.Push(ctx, client, chain, src)

// The management API. It always authenticates as a human, and its tokens
// are for auth.ManagementAudience, the server's apex, even when the request
// goes to an org's host.
srv, err := serverurl.ParseServer("https://atomic.storage", false)
human, ok, err := auth.ResolveForManagement(ctx, store, profileIdentity)
mbearer, err := issuer.ForIdentity(ctx, human, auth.ManagementAudience(srv))
mtr, err := auth.NewTransport(base, mbearer, srv.Apex())
mgmt, err := storage.New(srv.Apex(), &http.Client{Transport: mtr}, logger, version)
```

For an org's management API, build the client and its `auth.Transport` on
`srv.OrgBase(org)` instead, with the same `auth.ManagementAudience(srv)`
token. Errors are elided above; every constructor refuses a nil dependency
rather than fall back to a default.

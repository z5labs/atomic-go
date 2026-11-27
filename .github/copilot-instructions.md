# atomic-go

A Go client for working with Atomic repositories. These are its coding
standards; they match `CLAUDE.md`, so change both together. The rules for
reaching the network, filesystem, clock and the rest of the host are in
`.github/instructions/go.instructions.md`.

## Before you push

Run these from the repository root. Every one must pass before you push:

```sh
go build ./...
go vet ./...
go test -race ./...
test -z "$(gofmt -l .)"
```

- Linting is `go vet` plus the `gofmt` check. The `gofmt` check fails when
  `gofmt -l` lists any file; run `gofmt -w .` and check again.
- Don't push while a check fails. Don't skip, delete or weaken a test to make a
  check pass; fix the code.
- CI runs the same list, in `.github/workflows/ci.yaml`, and the backlog loop
  runs it from the `verify` list in `.claude/backlog.json`. When you change one
  list, change the others.

## Commits and pull requests

Follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):
`<type>(<scope>): <description>`.

- **Type**: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`,
  `chore` or `revert`.
- **Scope**: when the work belongs to a labelled task, such as a GitHub issue,
  the scope is the task's ID. Work on issue #42 is committed as
  `feat(42): add a repository client`. When there is no task, the scope is
  optional.
- **Description**: short, imperative, lower case, no trailing period.
- **Breaking changes**: put `!` before the colon, as in
  `feat(42)!: rename Client.Open to Client.Clone`, and describe the break in a
  `BREAKING CHANGE:` footer.

Pull requests are squash-merged, so a pull request's title becomes its commit on
`main`. Pull request titles must follow the same format, even when the issue's
own title starts with a different prefix.

## Tests

- Assert only with
  [`github.com/stretchr/testify/require`](https://pkg.go.dev/github.com/stretchr/testify/require).
  Don't use `testify/assert`, any other assertion library, or hand-written
  checks like `if got != want { t.Errorf(...) }`.
- `require` stops a test with `t.FailNow`, which works only on the test's own
  goroutine. Don't call it inside a goroutine the test starts. Send the result
  back over a channel and assert on the test's goroutine.

## Composition over inheritance

Go has no inheritance, but struct embedding can be used to imitate it. Don't.

- Build a type from named fields that hold its collaborators. Extend behavior by
  wrapping an interface (middleware, decorators), not by embedding a "base" type
  and shadowing its methods.
- No `Base*` or `Abstract*` types, and no chains of embedded structs standing in
  for a class hierarchy.
- Embed only when the outer type deliberately offers the embedded type's whole
  method set as its own, as humus's `Host` embeds its `IPNetwork`. Embedding an
  exported type adds the field and every exported method it has to your public
  API.
- Keep interfaces small, and build larger ones from smaller ones, as
  `io.ReadWriter` is built from `io.Reader` and `io.Writer`.

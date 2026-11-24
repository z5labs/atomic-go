# atomic-go

A Go client for working with Atomic repositories. These standards apply to every
change, whether a person or an agent writes it.

Copilot reads the same rules from `.github/copilot-instructions.md` and
`.github/instructions/go.instructions.md`. They're split across two files
because Copilot code review reads only the first 4,000 characters of each one.
When you change a rule here, change it there too.

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
- The backlog loop runs the same commands, from the `verify` list in
  `.claude/backlog.json`. When you change one list, change the other.

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
`main`. Pull request titles must follow the same format. This overrides the
backlog loop's default of reusing the issue's title and its prefix: an issue
titled `story(client): ...` still lands as `<type>(<issue number>): ...`.

## Tests

- Assert only with
  [`github.com/stretchr/testify/require`](https://pkg.go.dev/github.com/stretchr/testify/require).
  Don't use `testify/assert`, any other assertion library, or hand-written
  checks like `if got != want { t.Errorf(...) }`.
- `require` stops a test with `t.FailNow`, which works only on the test's own
  goroutine. Don't call it inside a goroutine the test starts. Send the result
  back over a channel and assert on the test's goroutine.

## Take the host as an argument

This repository follows the TigerStyle-like approach of
[z5labs/humus](https://github.com/z5labs/humus): code never reaches out to the
machine it runs on. Everything outside the process's own memory comes in as an
argument or as a field set by the constructor. Tests can then pass in an
in-memory version of each one and run deterministically, without the real
network, disk or clock.

| Resource | Never use | Take instead |
|---|---|---|
| Network | `net.Dial`, `net.Listen`, `net.Lookup*`, `http.DefaultClient`, `http.DefaultTransport`, `http.Get`/`Head`/`Post`/`PostForm` | an `*http.Client`, an `http.RoundTripper`, or a dialer interface |
| Filesystem | `os.Open`, `os.Create`, `os.ReadFile`, `os.WriteFile`, `os.MkdirAll`, `os.Remove` and the rest of `os`'s file API | an `fs.FS` for reads; a small filesystem interface for writes |
| Clock | `time.Now`, `time.Since`, `time.Sleep`, `time.After`, `time.NewTimer`, `time.NewTicker` | a clock, such as a `func() time.Time`; deadlines come from the caller's `context.Context` |
| Randomness | the top-level functions of `math/rand` and `math/rand/v2`, `crypto/rand.Read` | an `io.Reader` or a `*rand.Rand` |
| Process | `os.Args`, `os.Getenv`, `os.LookupEnv`, `os.Environ`, `os.Stdin`, `os.Stdout`, `os.Stderr`, `os.Exit`, `signal.Notify`, `flag.Parse` | plain values, an `io.Reader` or an `io.Writer` |
| Logs and telemetry | `slog.Default`, `slog.SetDefault`, `log.Print*`/`log.Fatal*`, OpenTelemetry's globals (`otel.Tracer`, `otel.SetTracerProvider`, ...) | a `*slog.Logger` and explicit OpenTelemetry providers |

- **No package-level state.** No mutable package-level variables, and no `init`
  functions with side effects. Package-level `var`s are only for sentinel
  errors and other values that never change.
- **Required means required.** A constructor never falls back to the real
  implementation when a dependency is nil or left out. Only the program's edge
  wires in the OS-backed implementations: `main`, or a constructor whose name
  says it does, like humus's `OSHost()`.
- **Ask for the narrowest interface the code uses**, such as `io.Reader`,
  `fs.FS` or a one-method dialer, and declare it in the package that uses it.
- **Check what already exists.** `github.com/z5labs/bedrock/host` provides the
  OS-backed `IPNetwork` and `OSFilesystem` that humus uses, and
  `bedrock/host/hosttest` provides an in-memory network and a filesystem
  confined to the test's directory. Look there before writing a new network or
  filesystem abstraction.
- **Tests pass in fakes**: `fstest.MapFS`, `net.Pipe`, `hosttest`, a fixed
  clock, a seeded random source. If a test only works against the real
  network, files outside `t.TempDir()`, or the wall clock, the code still uses
  a global dependency. Fix the code, not the test.

```go
// Don't: reads the process's environment and the real disk.
func LoadConfig() (Config, error) {
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".atomic", "config.json"))
	// ...
}

// Do: the caller passes in the filesystem and the name.
func LoadConfig(fsys fs.FS, name string) (Config, error) {
	b, err := fs.ReadFile(fsys, name)
	// ...
}

// The test needs no disk:
//   LoadConfig(fstest.MapFS{"config.json": {Data: []byte(`{}`)}}, "config.json")
```

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

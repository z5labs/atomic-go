---
applyTo: "**/*.go"
---

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

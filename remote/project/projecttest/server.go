// Package projecttest provides an in-memory peer for testing clients of an
// Atomic project protocol endpoint without a network.
//
// A [Server] is an [http.Handler] that answers the requests push, pull and
// clone send: the sync push and pull on a project's /code URL, the project
// identity requests on ?project-identity=1, and the view inventory on
// refs/views. Serve it in memory with
// [github.com/z5labs/atomic-go/remote/remotetest.Client] and a whole flow
// runs without a socket, a goroutine or a clock.
//
// # The fake's choices
//
// The Atomic server is not in the source this package is written from. A
// Server models libatomic's sync test peer (docs/remote/README.md#open-questions),
// and where that peer is silent it makes a choice of its own. Every rule on
// [Server.ServeHTTP] is this fake's choice, not the server's contract: a
// client that passes against a Server has been tested against one plausible
// server, not against Atomic's. In particular:
//
//   - A ref moves when its ExpectOld equals the stored target; whether the
//     new target descends from the old one is not checked, though atomic-objects
//     says the server checks it.
//   - Object keys are not checked against their bytes.
//   - A pull answers with every stored object the client does not have, not
//     only those the requested views reach. A have is matched by key alone,
//     so it drops a stored object of any family under that key.
//   - An inventory row's change count is the length of the snapshot's
//     own_changes, which for a draft may or may not include its parent's
//     changes (docs/remote/endpoints/project.md#viewsnapshot).
package projecttest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/views"
)

// identityQuery is the query both project identity requests send.
const identityQuery = "project-identity=1"

// config is what the options set.
type config struct {
	minVersion       string
	noIdentityRoutes bool
}

// Option configures a [Server] built by [NewServer].
type Option func(*config)

// WithMinVersion makes every response carry v in the
// [remote.MinVersionHeader], as a server advising its oldest supported
// client does. An empty v sends no header, which is the default.
func WithMinVersion(v string) Option {
	return func(c *config) {
		c.minVersion = v
	}
}

// WithoutIdentityEndpoint makes the Server answer both project identity
// requests with 404, as a server that predates the identity endpoint does.
// [Server.Bind] still binds an identity; it is just never served.
func WithoutIdentityEndpoint() Option {
	return func(c *config) {
		c.noIdentityRoutes = true
	}
}

// Recorded is one request a [Server] received, as it received it.
type Recorded struct {
	// Method is the request's method.
	Method string

	// RequestURI is the request target exactly as sent, such as
	// "/workspaces/w/projects/p/code?project-identity=1".
	RequestURI string

	// Header is the request's header.
	Header http.Header

	// Body is the request's body, every byte of it, or nil when it had
	// none.
	Body []byte
}

// objectID names a stored object. Two families can hold the same key.
type objectID struct {
	family syncproto.Family
	key    string
}

// Server is an in-memory Atomic project server. Its zero value is not
// usable; build one with [NewServer].
//
// A Server is safe for concurrent use: every request and every method holds
// one mutex while it reads or changes the Server's state, so a push is
// checked and stored as one step.
type Server struct {
	cfg   config
	codec *syncproto.Codec

	mu sync.Mutex
	// objects holds every stored object, and order the IDs in the order
	// they were first stored, so a pull answers deterministically.
	objects  map[objectID][]byte
	order    []objectID
	refs     map[string]string
	identity json.RawMessage
	requests []Recorded
}

// NewServer returns a Server with no objects, no refs and no identity
// bound. A nil Option panics.
func NewServer(opts ...Option) *Server {
	var cfg config
	for _, opt := range opts {
		if opt == nil {
			panic("projecttest: NewServer: nil Option")
		}
		opt(&cfg)
	}
	return &Server{
		cfg:     cfg,
		codec:   syncproto.NewCodec(),
		objects: make(map[objectID][]byte),
		refs:    make(map[string]string),
	}
}

// PutObject stores a copy of obj, as though an earlier push had sent it,
// replacing any object of the same family and key. It panics if obj's
// family is not [syncproto.Family.Known].
func (s *Server) PutObject(obj syncproto.ObjectRecord) {
	if !obj.Family.Known() {
		panic(fmt.Sprintf("projecttest: PutObject: unknown family %d", uint32(obj.Family)))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store(obj)
}

// store stores a copy of obj. s.mu must be held.
func (s *Server) store(obj syncproto.ObjectRecord) {
	id := objectID{family: obj.Family, key: obj.Key}
	if _, ok := s.objects[id]; !ok {
		s.order = append(s.order, id)
	}
	s.objects[id] = bytes.Clone(obj.Bytes)
}

// SetRef points the ref of view at target, the key of a view snapshot,
// without the compare-and-swap a push goes through. The snapshot need not
// be stored, but a pull follows the view's parent only through a stored
// one, and the inventory refuses to list a view whose snapshot is missing.
func (s *Server) SetRef(view, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refs[view] = target
}

// Ref returns the target the ref of view holds, and whether it exists.
func (s *Server) Ref(view string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.refs[view]
	return target, ok
}

// Bind binds doc as the project's identity, replacing any identity already
// bound, as though an earlier bind had sent it. The bytes are kept as
// given and served verbatim. It panics if doc is not a JSON object.
func (s *Server) Bind(doc json.RawMessage) {
	if err := checkIdentity(doc); err != nil {
		panic("projecttest: Bind: " + err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identity = bytes.Clone(doc)
}

// Requests returns a copy of every request served so far, in the order they
// arrived, whatever was answered.
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recorded, len(s.requests))
	for i, r := range s.requests {
		out[i] = Recorded{
			Method:     r.Method,
			RequestURI: r.RequestURI,
			Header:     r.Header.Clone(),
			Body:       bytes.Clone(r.Body),
		}
	}
	return out
}

// ServeHTTP records r and answers it. A request is routed by its method,
// the end of its path and its query, and nothing else; the project's
// workspace and name are not checked, so one Server serves any project URL
// whose path ends in /code.
//
// POST {project}/code, with Atomic-Protocol: sync/1 and no query, is a sync
// push. Its body is decoded as a [syncproto.Pack], and every ref move is
// checked, in order, before anything is stored:
//
//   - The ref must hold ExpectOld, or be absent when ExpectOld is nil.
//     Otherwise the answer is 409, with a text body beginning
//     "ref CAS refused". An earlier move of the same ref in the pack counts.
//   - NewTarget must be the key of a [syncproto.View] object in the pack or
//     already stored. Otherwise the answer is 400.
//
// A pack holding an object whose family is not [syncproto.Family.Known] is
// a 400 too. A refused push stores nothing and moves no ref. An accepted
// one stores every object, moves every ref, and answers 200 with the JSON
// {"stored":<objects in the pack>,"refs_moved":<refs in the pack>}, as
// libatomic's test peer does: a ref moved twice in one pack counts twice.
//
// GET {project}/code, with Atomic-Protocol: sync/1 and no query, is a sync
// pull. Its body is decoded as a [syncproto.Wants], and the answer is 200
// with a [syncproto.Pack] and the Content-Type [syncproto.ContentType]:
//
//   - Its refs are those of the requested views, or of every view, by
//     name, when Refs is empty. Each view's parent_view chain is followed
//     through its stored snapshots, and every view on it is answered too,
//     after the view that named it. The chain ends at a view whose snapshot
//     is not stored, and a stored snapshot that does not parse is a 500. A
//     requested view with no ref is left out. No ref carries an ExpectOld.
//   - Its objects are every stored object, in the order they were stored,
//     except those whose key is in Haves, and only the [syncproto.View]
//     objects when RefsOnly is set.
//
// GET {project}/code?project-identity=1 answers {"bound":false}, or
// {"bound":true,"identity":<document>} once an identity is bound. POST
// to the same URL binds its body, which must be a JSON object, first write
// wins: the first bind and any repeat of the same document, white space
// aside, answer 200, and a different document answers 409 with
// {"code":"IDENTITY_CONFLICT",…}, keeping the first.
//
// GET {project}/refs/views answers one [views.Info] row per view, by name,
// written with [views.Info.AppendText] from the view's stored snapshot: its
// scope, parent view, the length of own_changes, its Merkle state and its
// own SetId. A view whose snapshot is not stored, or does not parse, is a
// 500.
//
// A body that does not decode is a 400. Anything else, and both identity
// requests under [WithoutIdentityEndpoint], is a 404 with a text body.
// Every response carries the [WithMinVersion] header, if there is one.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, readErr := io.ReadAll(r.Body)
	if len(body) == 0 {
		body = nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, Recorded{
		Method:     r.Method,
		RequestURI: r.RequestURI,
		Header:     r.Header.Clone(),
		Body:       bytes.Clone(body),
	})

	if s.cfg.minVersion != "" {
		w.Header().Set(remote.MinVersionHeader, s.cfg.minVersion)
	}
	if readErr != nil {
		writeText(w, http.StatusBadRequest, "request body could not be read: "+readErr.Error())
		return
	}

	path, query := r.URL.EscapedPath(), r.URL.RawQuery
	isSync := r.Header.Get(syncproto.ProtocolHeader) == syncproto.ProtocolVersion
	switch {
	case strings.HasSuffix(path, "/code") && query == "" && isSync && r.Method == http.MethodPost:
		s.push(w, body)
	case strings.HasSuffix(path, "/code") && query == "" && isSync && r.Method == http.MethodGet:
		s.pull(w, body)
	case strings.HasSuffix(path, "/code") && query == identityQuery && !s.cfg.noIdentityRoutes && r.Method == http.MethodGet:
		s.fetchIdentity(w)
	case strings.HasSuffix(path, "/code") && query == identityQuery && !s.cfg.noIdentityRoutes && r.Method == http.MethodPost:
		s.bindIdentity(w, body)
	case strings.HasSuffix(path, "/refs/views") && query == "" && r.Method == http.MethodGet:
		s.listViewRefs(w)
	default:
		writeText(w, http.StatusNotFound, "not found: "+r.Method+" "+r.RequestURI)
	}
}

// push answers a sync push. s.mu must be held.
func (s *Server) push(w http.ResponseWriter, body []byte) {
	p, err := s.codec.DecodePack(bytes.NewReader(body))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid sync pack: "+err.Error())
		return
	}

	inPack := make(map[string]bool)
	for i, o := range p.Objects {
		if !o.Family.Known() {
			writeText(w, http.StatusBadRequest, fmt.Sprintf("objects[%d] has unknown family %d", i, uint32(o.Family)))
			return
		}
		if o.Family == syncproto.View {
			inPack[o.Key] = true
		}
	}
	// staged is the refs as the pack's moves leave them, so a later move of
	// the same ref is checked against an earlier one.
	staged := make(map[string]*string)
	current := func(name string) *string {
		if t, ok := staged[name]; ok {
			return t
		}
		if t, ok := s.refs[name]; ok {
			return &t
		}
		return nil
	}
	for _, ref := range p.Refs {
		if old := current(ref.Name); !sameTarget(old, ref.ExpectOld) {
			writeText(w, http.StatusConflict, fmt.Sprintf("ref CAS refused: view %q holds %s, not %s",
				ref.Name, describeTarget(old), describeTarget(ref.ExpectOld)))
			return
		}
		if !inPack[ref.NewTarget] {
			if _, ok := s.objects[objectID{family: syncproto.View, key: ref.NewTarget}]; !ok {
				writeText(w, http.StatusBadRequest, fmt.Sprintf("view %q: target %q is neither in the pack nor stored",
					ref.Name, ref.NewTarget))
				return
			}
		}
		staged[ref.Name] = &ref.NewTarget
	}

	for _, o := range p.Objects {
		s.store(o)
	}
	for name, target := range staged {
		s.refs[name] = *target
	}
	writeJSON(w, http.StatusOK, map[string]int{"stored": len(p.Objects), "refs_moved": len(p.Refs)})
}

// sameTarget reports whether two optional targets are equal.
func sameTarget(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// describeTarget writes an optional target for a 409 body.
func describeTarget(t *string) string {
	if t == nil {
		return "nothing"
	}
	return fmt.Sprintf("%q", *t)
}

// pull answers a sync pull. s.mu must be held.
func (s *Server) pull(w http.ResponseWriter, body []byte) {
	wants, err := s.codec.DecodeWants(bytes.NewReader(body))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid sync wants: "+err.Error())
		return
	}

	requested := wants.Refs
	if len(requested) == 0 {
		requested = s.viewNames()
	}
	var p syncproto.Pack
	answered := make(map[string]bool)
	for _, name := range requested {
		// Follow the parent chain; answered also stops a cycle.
		for name != "" && !answered[name] {
			target, ok := s.refs[name]
			if !ok {
				break
			}
			answered[name] = true
			p.Refs = append(p.Refs, syncproto.RefRecord{Name: name, NewTarget: target})
			snap, stored, err := s.snapshot(target)
			if err != nil {
				writeText(w, http.StatusInternalServerError, fmt.Sprintf("view %q: %v", name, err))
				return
			}
			if !stored {
				break
			}
			name = snap.ParentView
		}
	}

	haves := make(map[string]bool, len(wants.Haves))
	for _, h := range wants.Haves {
		haves[h] = true
	}
	for _, id := range s.order {
		if haves[id.key] || (wants.RefsOnly && id.family != syncproto.View) {
			continue
		}
		p.Objects = append(p.Objects, syncproto.ObjectRecord{Family: id.family, Key: id.key, Bytes: s.objects[id]})
	}

	b, err := s.codec.EncodePack(p)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "sync pack will not encode: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", syncproto.ContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// viewNames returns the name of every view with a ref, sorted. s.mu must
// be held.
func (s *Server) viewNames() []string {
	names := make([]string, 0, len(s.refs))
	for name := range s.refs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// snapshot parses the stored view snapshot keyed target. stored is false,
// with no error, when there is none. s.mu must be held.
func (s *Server) snapshot(target string) (snap views.Snapshot, stored bool, err error) {
	b, ok := s.objects[objectID{family: syncproto.View, key: target}]
	if !ok {
		return views.Snapshot{}, false, nil
	}
	snap, err = views.ParseSnapshot(b)
	return snap, true, err
}

// fetchIdentity answers a project identity read. s.mu must be held.
func (s *Server) fetchIdentity(w http.ResponseWriter) {
	if s.identity == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"bound": false})
		return
	}
	// Written by hand: encoding/json would compact the document, and it is
	// served exactly as it was bound.
	b := make([]byte, 0, len(`{"bound":true,"identity":}`)+len(s.identity))
	b = append(b, `{"bound":true,"identity":`...)
	b = append(b, s.identity...)
	b = append(b, '}')
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// bindIdentity answers a project identity bind. s.mu must be held.
func (s *Server) bindIdentity(w http.ResponseWriter, body []byte) {
	if err := checkIdentity(body); err != nil {
		writeText(w, http.StatusBadRequest, "invalid project identity: "+err.Error())
		return
	}
	if s.identity == nil {
		s.identity = bytes.Clone(body)
		writeJSON(w, http.StatusOK, map[string]bool{"bound": true})
		return
	}
	if !sameDocument(s.identity, body) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"code":    "IDENTITY_CONFLICT",
			"message": "project is already bound to a different identity",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"bound": true})
}

// checkIdentity refuses doc unless it is one JSON object.
func checkIdentity(doc []byte) error {
	if !json.Valid(doc) {
		return errors.New("not valid JSON")
	}
	if t := bytes.TrimSpace(doc); t[0] != '{' {
		return errors.New("not a JSON object")
	}
	return nil
}

// sameDocument reports whether two valid JSON documents are the same but
// for insignificant white space.
func sameDocument(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// listViewRefs answers the view inventory. s.mu must be held.
func (s *Server) listViewRefs(w http.ResponseWriter) {
	var b []byte
	for _, name := range s.viewNames() {
		snap, stored, err := s.snapshot(s.refs[name])
		if err == nil && !stored {
			err = fmt.Errorf("view snapshot %q is not stored", s.refs[name])
		}
		if err != nil {
			writeText(w, http.StatusInternalServerError, fmt.Sprintf("view %q: %v", name, err))
			return
		}
		info := views.Info{
			Name:        name,
			Scope:       snap.Scope,
			Parent:      snap.ParentView,
			ChangeCount: uint64(len(snap.OwnChanges)),
			State:       snap.MerkleState,
			SetID:       snap.OwnSetID,
		}
		if b, err = info.AppendText(b); err != nil {
			writeText(w, http.StatusInternalServerError, fmt.Sprintf("view %q: %v", name, err))
			return
		}
		b = append(b, '\n')
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// writeText answers status with msg as a text body.
func writeText(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg)
}

// writeJSON answers status with v as a JSON body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "response will not encode: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

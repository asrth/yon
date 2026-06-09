package main

// Extra "Test APIs" for the bundled testserver (issue #51): a small in-memory
// CRUD resource, cookie set/echo, a gzip-encoded response, and a request-echo
// debug endpoint. These give testserver.yon realistic targets for chaining,
// captures, cookies, transparent decompression, and request inspection.

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// apiUser is one record in the /users CRUD resource.
type apiUser struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// userStore is a concurrency-safe in-memory store backing the /users endpoints.
// A fresh store is created per newMux call, so each test server starts from the
// same seeded state and tests stay isolated.
type userStore struct {
	mu     sync.Mutex
	users  map[int]apiUser
	nextID int
}

// newUserStore returns a store seeded with two users (ids 1 and 2).
func newUserStore() *userStore {
	return &userStore{
		users: map[int]apiUser{
			1: {ID: 1, Name: "Ada Lovelace", Email: "ada@example.com"},
			2: {ID: 2, Name: "Alan Turing", Email: "alan@example.com"},
		},
		nextID: 3,
	}
}

// list returns all users ordered by id.
func (s *userStore) list(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := make([]apiUser, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "count": len(out)})
}

// create adds a user from the JSON body and returns it with a fresh id (201).
func (s *userStore) create(w http.ResponseWriter, r *http.Request) {
	var in apiUser
	if !decodeJSON(w, r, &in) {
		return
	}
	s.mu.Lock()
	in.ID = s.nextID
	s.nextID++
	s.users[in.ID] = in
	s.mu.Unlock()
	w.Header().Set("Location", "/users/"+strconv.Itoa(in.ID))
	writeJSON(w, http.StatusCreated, in)
}

// get returns one user, or 404 when it does not exist.
func (s *userStore) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	u, found := s.users[id]
	s.mu.Unlock()
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found", "id": id})
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// replace overwrites an existing user's fields (PUT); 404 when absent.
func (s *userStore) replace(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in apiUser
	if !decodeJSON(w, r, &in) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.users[id]; !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found", "id": id})
		return
	}
	in.ID = id
	s.users[id] = in
	writeJSON(w, http.StatusOK, in)
}

// update applies a partial change (PATCH): only the provided fields change.
func (s *userStore) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var patch struct {
		Name  *string `json:"name"`
		Email *string `json:"email"`
	}
	if !decodeJSON(w, r, &patch) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, found := s.users[id]
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found", "id": id})
		return
	}
	if patch.Name != nil {
		u.Name = *patch.Name
	}
	if patch.Email != nil {
		u.Email = *patch.Email
	}
	s.users[id] = u
	writeJSON(w, http.StatusOK, u)
}

// remove deletes a user (204), or 404 when it does not exist.
func (s *userStore) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	_, found := s.users[id]
	if found {
		delete(s.users, id)
	}
	s.mu.Unlock()
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found", "id": id})
		return
	}
	w.Header().Set("X-Yon-Testserver", "1")
	w.WriteHeader(http.StatusNoContent)
}

// pathID parses the {id} path value as a positive int, writing a 400 and
// returning ok=false when it is missing or not a number.
func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id must be an integer", "got": r.PathValue("id")})
		return 0, false
	}
	return id, true
}

// decodeJSON decodes the request body into v, writing a 400 and returning false
// on a malformed body. An empty body decodes to the zero value (ok).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	body, _ := io.ReadAll(r.Body)
	if len(strings.TrimSpace(string(body))) == 0 {
		return true
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

// cookiesSet sets a cookie (name/value from the query, defaulting to a demo
// cookie) and reports what it set.
func cookiesSet(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "yon_session"
	}
	value := r.URL.Query().Get("value")
	if value == "" {
		value = "demo-cookie-value"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
	writeJSON(w, http.StatusOK, map[string]any{"set": map[string]string{name: value}})
}

// cookiesEcho reports the cookies the request carried.
func cookiesEcho(w http.ResponseWriter, r *http.Request) {
	got := map[string]string{}
	for _, c := range r.Cookies() {
		got[c.Name] = c.Value
	}
	writeJSON(w, http.StatusOK, map[string]any{"cookies": got, "count": len(got)})
}

// gzipJSON returns a gzip-encoded JSON body with Content-Encoding: gzip, to
// exercise transparent decompression. (Go's HTTP client decompresses this
// automatically when it adds Accept-Encoding itself.)
func gzipJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("X-Yon-Testserver", "1")
	gz := gzip.NewWriter(w)
	defer gz.Close()
	enc := json.NewEncoder(gz)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"gzipped": true,
		"message": "This JSON was sent gzip-encoded and decompressed on the client.",
		"items":   []string{"alpha", "beta", "gamma"},
	})
}

// debugEcho reflects the request back in detail: method, path, proto, host,
// remote address, query, headers, and the raw body.
func debugEcho(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	writeJSON(w, http.StatusOK, map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"proto":      r.Proto,
		"host":       r.Host,
		"remoteAddr": r.RemoteAddr,
		"query":      flatten(r.URL.Query()),
		"headers":    flatten(r.Header),
		"body":       string(body),
	})
}

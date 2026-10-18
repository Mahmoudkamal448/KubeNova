package rancher

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
)

const goodToken = "token-abc12:secret"

// fakeRancher mimics the server side of Rancher's CLI login flows.
type fakeRancher struct {
	v1        bool
	mu        sync.Mutex
	encrypted map[string]string // requestId -> encrypted token
}

// completeBrowserLogin is what Rancher does when the user finishes logging in
// on /dashboard/auth/login?requestId=…&publicKey=…: it encrypts a new token
// with the public key from the URL and stores it under the request id.
func (f *fakeRancher) completeBrowserLogin(t *testing.T, loginURL string) {
	u, _ := url.Parse(loginURL)
	q := u.Query()
	if q.Get("cli") != "true" || q.Get("responseType") == "" {
		t.Fatalf("login URL missing cli/responseType: %s", loginURL)
	}
	raw, err := base64.StdEncoding.DecodeString(q.Get("publicKey"))
	if err != nil {
		t.Fatal(err)
	}
	var pub rsa.PublicKey
	if err := json.Unmarshal(raw, &pub); err != nil {
		t.Fatalf("public key is not the JSON-encoded RSA key Rancher expects: %v", err)
	}
	enc, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &pub, []byte(goodToken), nil)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.encrypted[q.Get("requestId")] = base64.StdEncoding.EncodeToString(enc)
	f.mu.Unlock()
}

func (f *fakeRancher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p := r.URL.Path
	switch {
	case p == "/v1-public/authproviders" && f.v1, p == "/v3-public/authProviders" && !f.v1:
		_, _ = w.Write([]byte(`{"data":[{"id":"local","type":"localProvider"},{"id":"keycloakoidc","type":"keyCloakOIDCProvider"},{"id":"okta","type":"oktaProvider"}]}`))
	case p == "/v1-public/login" && f.v1:
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["username"] != "admin" || b["password"] != "pw" || b["type"] != "localProvider" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"apiVersion":"ext.cattle.io/v1","kind":"Token","status":{"bearerToken":"` + goodToken + `"}}`))
	case strings.HasPrefix(p, "/v3-public/localProviders/local") && !f.v1:
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"token":"` + goodToken + `"}`))
	case strings.HasPrefix(p, "/v1-public/authtokens/") && f.v1, strings.HasPrefix(p, "/v3-public/authTokens/") && !f.v1:
		id := p[strings.LastIndex(p, "/")+1:]
		f.mu.Lock()
		tok, ok := f.encrypted[id]
		if r.Method == http.MethodDelete {
			delete(f.encrypted, id)
		}
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"token":"` + tok + `"}`))
	case p == "/v3/users":
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"message":"must authenticate"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"username":"jane"}]}`))
	case p == "/v3/settings/server-version":
		_, _ = w.Write([]byte(`{"value":"v2.9.3"}`))
	default:
		w.WriteHeader(404)
	}
}

func newService(t *testing.T, v1 bool) (*Service, *fakeRancher, string) {
	f := &fakeRancher{v1: v1, encrypted: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	st, _ := store.New(t.TempDir())
	s := &Service{Store: st}
	sv, err := s.Upsert(Server{Name: "test", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return s, f, sv.ID
}

func token(t *testing.T, s *Service, id string) string {
	sv, err := s.server(id)
	if err != nil {
		t.Fatal(err)
	}
	return sv.Token
}

func TestBrowserSSOFlow(t *testing.T) {
	for _, v1 := range []bool{true, false} {
		s, f, id := newService(t, v1)
		ctx := context.Background()
		rid, loginURL, err := s.SSOStart(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if done, err := s.SSOPoll(ctx, id, rid); done || err != nil {
			t.Fatalf("v1=%v: before login want waiting, got done=%v err=%v", v1, done, err)
		}
		f.completeBrowserLogin(t, loginURL)
		done, err := s.SSOPoll(ctx, id, rid)
		if !done || err != nil {
			t.Fatalf("v1=%v: after login want done, got done=%v err=%v", v1, done, err)
		}
		if got := token(t, s, id); got != goodToken {
			t.Fatalf("v1=%v: stored token %q", v1, got)
		}
		if sv, _ := s.server(id); sv.User != "jane" {
			t.Fatalf("user not resolved: %q", sv.User)
		}
	}
}

func TestPasswordLogin(t *testing.T) {
	for _, v1 := range []bool{true, false} {
		s, _, id := newService(t, v1)
		if err := s.Login(context.Background(), id, "local", "admin", "pw"); err != nil {
			t.Fatalf("v1=%v: %v", v1, err)
		}
		if token(t, s, id) != goodToken {
			t.Fatalf("v1=%v: token not stored", v1)
		}
	}
	s, _, id := newService(t, true)
	if err := s.Login(context.Background(), id, "local", "admin", "wrong"); err == nil || !strings.Contains(err.Error(), "wrong username or password") {
		t.Fatalf("want clear error, got %v", err)
	}
}

func TestProvidersAndTokenValidation(t *testing.T) {
	s, _, id := newService(t, true)
	ps, err := s.AuthProviders(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"localProvider": "password", "keyCloakOIDCProvider": "window", "oktaProvider": "browser"}
	for _, p := range ps {
		if want[p.Type] != p.Login {
			t.Fatalf("%s: login %q, want %q", p.Type, p.Login, want[p.Type])
		}
	}
	if err := s.SetToken(context.Background(), id, "token-bad:nope", ""); err == nil {
		t.Fatal("invalid token accepted")
	}
	if err := s.SetToken(context.Background(), id, " "+goodToken+"\n", ""); err != nil {
		t.Fatal(err)
	}
}

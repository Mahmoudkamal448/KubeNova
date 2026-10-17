package rancher

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
)

// Rancher authentication, following the official Rancher CLI
// (github.com/rancher/cli, cmd/kubectl_token.go): /v1-public endpoints on
// Rancher 2.9+, falling back to the deprecated /v3-public ones.

// Provider is an authentication provider enabled on a Rancher server.
type Provider struct {
	Type string `json:"type"` // e.g. localProvider, keyCloakOIDCProvider
	Name string `json:"name"`
	// Login is how KubeNova can log in with it:
	//   password - username/password form
	//   browser  - Rancher's browser token flow (SAML providers)
	//   window   - log in on Rancher's own page in a KubeNova window
	//              (works for every provider, including OIDC and GitHub)
	Login string `json:"login"`
}

var passwordProviders = map[string]bool{"localProvider": true, "freeIpaProvider": true, "openLdapProvider": true, "activeDirectoryProvider": true}

// samlProviders support Rancher's requestId/publicKey browser token flow.
var samlProviders = map[string]bool{"pingProvider": true, "adfsProvider": true, "keyCloakProvider": true, "oktaProvider": true, "shibbolethProvider": true}

// raw performs an unauthenticated request and returns status and body.
func (s *Service) raw(ctx context.Context, sv Server, method, path string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, sv.URL+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient(sv.Insecure).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, b, err
}

func rancherError(status int, body []byte) error {
	var e struct{ Message string }
	_ = json.Unmarshal(body, &e)
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(body))
	}
	if len(e.Message) > 300 {
		e.Message = e.Message[:300]
	}
	return fmt.Errorf("rancher returned %d %s: %s", status, http.StatusText(status), e.Message)
}

// useV1 reports whether the server has the /v1-public API (Rancher 2.9+).
func (s *Service) useV1(ctx context.Context, sv Server) (bool, []byte, error) {
	st, b, err := s.raw(ctx, sv, "GET", "/v1-public/authproviders", nil)
	if err != nil {
		return false, nil, err
	}
	if st == http.StatusOK {
		return true, b, nil
	}
	st, b, err = s.raw(ctx, sv, "GET", "/v3-public/authProviders", nil)
	if err != nil {
		return false, nil, err
	}
	if st != http.StatusOK {
		return false, nil, rancherError(st, b)
	}
	return false, b, nil
}

// AuthProviders lists the providers enabled on the server and how KubeNova
// can log in with each.
func (s *Service) AuthProviders(ctx context.Context, id string) ([]Provider, error) {
	sv, err := s.server(id)
	if err != nil {
		return nil, err
	}
	_, body, err := s.useV1(ctx, sv)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	out := []Provider{}
	for _, d := range res.Data {
		p := Provider{Type: d.Type, Name: d.ID, Login: "window"}
		if d.Name != "" {
			p.Name = d.Name
		}
		switch {
		case passwordProviders[d.Type]:
			p.Login = "password"
		case samlProviders[d.Type]:
			p.Login = "browser"
		}
		out = append(out, p)
	}
	return out, nil
}

// Login authenticates with username/password against a provider
// (local, activeDirectory, openLdap, freeIpa) and stores the token.
func (s *Service) Login(ctx context.Context, id, provider, username, password string) error {
	sv, err := s.server(id)
	if err != nil {
		return err
	}
	if provider == "" {
		provider = "localProvider"
	}
	if !strings.HasSuffix(provider, "Provider") {
		provider += "Provider"
	}
	v1, _, err := s.useV1(ctx, sv)
	if err != nil {
		return err
	}
	body := map[string]any{"type": provider, "username": username, "password": password, "responseType": "json", "description": "KubeNova"}
	path := "/v1-public/login"
	if !v1 {
		path = fmt.Sprintf("/v3-public/%ss/%s?action=login", provider, strings.ToLower(strings.TrimSuffix(provider, "Provider")))
		body["responseType"] = "token"
	}
	st, b, err := s.raw(ctx, sv, "POST", path, body)
	if err != nil {
		return err
	}
	if st != http.StatusOK && st != http.StatusCreated {
		if st == http.StatusUnauthorized {
			return fmt.Errorf("wrong username or password for %s", provider)
		}
		return rancherError(st, b)
	}
	token, err := parseLoginResponse(b)
	if err != nil {
		return err
	}
	return s.SetToken(ctx, id, token, username)
}

func parseLoginResponse(b []byte) (string, error) {
	var t struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Token      string `json:"token"`
		Status     struct {
			BearerToken string `json:"bearerToken"`
		} `json:"status"`
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return "", fmt.Errorf("unexpected login response: %w", err)
	}
	if t.Kind == "Token" && t.Status.BearerToken != "" {
		return t.Status.BearerToken, nil
	}
	if t.Token == "" {
		return "", fmt.Errorf("login response contained no token")
	}
	return t.Token, nil
}

// SetToken validates a token (API key, or the session token captured from
// Rancher's login page) and stores it for the server.
func (s *Service) SetToken(ctx context.Context, id, token, user string) error {
	sv, err := s.server(id)
	if err != nil {
		return err
	}
	sv.Token = strings.TrimSpace(token)
	var me struct {
		Data []struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"data"`
	}
	if err := s.do(ctx, sv, "GET", "/v3/users?me=true", nil, &me); err != nil {
		return fmt.Errorf("token was rejected by Rancher: %w", err)
	}
	if len(me.Data) > 0 {
		user = me.Data[0].Username
		if user == "" {
			user = me.Data[0].Name
		}
	}
	sv.User = user
	_, err = s.Upsert(sv)
	return err
}

type ssoRequest struct {
	key *rsa.PrivateKey
	v1  bool
}

const ssoChars = "abcdfghjklmnpqrstvwxz12456789"

func ssoRequestID() (string, error) {
	b := make([]byte, 32)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(ssoChars))))
		if err != nil {
			return "", err
		}
		b[i] = ssoChars[n.Int64()]
	}
	return string(b), nil
}

// SSOStart begins Rancher's browser token flow, which Rancher supports for
// SAML providers (Okta, ADFS, Ping, Keycloak SAML, Shibboleth). It returns
// the URL to open; SSOPoll completes the login. For OIDC, GitHub, Azure AD
// and others use the in-app login window instead.
func (s *Service) SSOStart(ctx context.Context, id string) (string, string, error) {
	sv, err := s.server(id)
	if err != nil {
		return "", "", err
	}
	v1, _, err := s.useV1(ctx, sv)
	if err != nil {
		return "", "", err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	// Rancher expects the JSON encoding of the RSA public key, base64'd.
	pub, err := json.Marshal(key.PublicKey)
	if err != nil {
		return "", "", err
	}
	rid, err := ssoRequestID()
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	if s.pending == nil {
		s.pending = map[string]ssoRequest{}
	}
	s.pending[rid] = ssoRequest{key: key, v1: v1}
	s.mu.Unlock()
	q := url.Values{"cli": {"true"}, "requestId": {rid}, "publicKey": {base64.StdEncoding.EncodeToString(pub)}, "responseType": {"kubeconfig"}}
	return rid, sv.URL + "/dashboard/auth/login?" + q.Encode(), nil
}

// SSOPoll checks whether the browser login completed. It returns done=false
// while Rancher has not created the token yet (HTTP 404).
func (s *Service) SSOPoll(ctx context.Context, id, requestID string) (bool, error) {
	sv, err := s.server(id)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	req, ok := s.pending[requestID]
	s.mu.Unlock()
	if !ok {
		return false, fmt.Errorf("unknown or expired login request; start the login again")
	}
	path := "/v1-public/authtokens/" + requestID
	if !req.v1 {
		path = "/v3-public/authTokens/" + requestID
	}
	st, b, err := s.raw(ctx, sv, "GET", path, nil)
	if err != nil {
		return false, err
	}
	switch st {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	default:
		return false, rancherError(st, b)
	}
	var t struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &t); err != nil || t.Token == "" {
		return false, nil
	}
	enc, err := base64.StdEncoding.DecodeString(t.Token)
	if err != nil {
		return false, fmt.Errorf("decoding token: %w", err)
	}
	plain, err := req.key.Decrypt(nil, enc, &rsa.OAEPOptions{Hash: crypto.SHA256})
	if err != nil {
		return false, fmt.Errorf("decrypting token: %w", err)
	}
	_, _, _ = s.raw(ctx, sv, "DELETE", path, nil)
	s.mu.Lock()
	delete(s.pending, requestID)
	s.mu.Unlock()
	return true, s.SetToken(ctx, id, string(plain), "")
}

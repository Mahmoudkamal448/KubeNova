package enterprise

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml/samlsp"
	"github.com/go-ldap/ldap/v3"

	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
)

// User is the identity using the IDE.
type User struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Email    string   `json:"email,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Provider string   `json:"provider"` // local | oidc | saml | ldap
}

// IdentityConfig configures single sign-on for the application.
type IdentityConfig struct {
	OIDC struct {
		Issuer       string   `json:"issuer"`
		ClientID     string   `json:"clientId"`
		ClientSecret string   `json:"clientSecret,omitempty"`
		Scopes       []string `json:"scopes"`
		GroupsClaim  string   `json:"groupsClaim"`
	} `json:"oidc"`
	SAML struct {
		IDPMetadataURL string `json:"idpMetadataUrl"`
		EntityID       string `json:"entityId"`
	} `json:"saml"`
	LDAP struct {
		URL          string `json:"url"`
		BindDN       string `json:"bindDn"`
		BindPassword string `json:"bindPassword,omitempty"`
		BaseDN       string `json:"baseDn"`
		UserFilter   string `json:"userFilter"`  // e.g. (uid=%s)
		GroupFilter  string `json:"groupFilter"` // e.g. (member=%s)
		InsecureTLS  bool   `json:"insecureTls"`
	} `json:"ldap"`
	SCIMToken string `json:"scimToken,omitempty"`
}

// Identity manages the current user and SSO providers.
type Identity struct {
	Store   *store.Store
	BaseURL func() string // e.g. http://127.0.0.1:7443
	mu      sync.Mutex
	current *User
	oidc    map[string]oidcPending
	saml    *samlsp.Middleware
}

type oidcPending struct {
	verifier string
	nonce    string
}

// Config returns the identity configuration.
func (i *Identity) Config() IdentityConfig {
	var c IdentityConfig
	_ = i.Store.Load("identity", &c)
	return c
}

// SetConfig saves configuration.
func (i *Identity) SetConfig(c IdentityConfig) error {
	i.mu.Lock()
	i.saml = nil
	i.mu.Unlock()
	return i.Store.Save("identity", c)
}

// Current returns the signed-in user (defaults to the local OS account).
func (i *Identity) Current() User {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.current != nil {
		return *i.current
	}
	var saved User
	if err := i.Store.Load("session", &saved); err == nil && saved.ID != "" {
		i.current = &saved
		return saved
	}
	name := "local"
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return User{ID: "local:" + name, Name: name, Provider: "local"}
}

func (i *Identity) setCurrent(u User) {
	i.mu.Lock()
	i.current = &u
	i.mu.Unlock()
	_ = i.Store.Save("session", u)
}

// Logout reverts to the local user.
func (i *Identity) Logout() {
	i.mu.Lock()
	i.current = nil
	i.mu.Unlock()
	_ = i.Store.Delete("session")
}

// ---------- OIDC (authorization code + PKCE, loopback redirect) ----------

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	Issuer                string `json:"issuer"`
}

func discover(ctx context.Context, issuer string) (*oidcDiscovery, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var d oidcDiscovery
	return &d, json.NewDecoder(resp.Body).Decode(&d)
}

func randString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// OIDCStart returns the URL to open in the system browser.
func (i *Identity) OIDCStart(ctx context.Context) (string, error) {
	c := i.Config().OIDC
	if c.Issuer == "" || c.ClientID == "" {
		return "", fmt.Errorf("OIDC is not configured")
	}
	d, err := discover(ctx, c.Issuer)
	if err != nil {
		return "", err
	}
	state, verifier, nonce := randString(16), randString(32), randString(16)
	sum := sha256.Sum256([]byte(verifier))
	i.mu.Lock()
	if i.oidc == nil {
		i.oidc = map[string]oidcPending{}
	}
	i.oidc[state] = oidcPending{verifier: verifier, nonce: nonce}
	i.mu.Unlock()
	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email", "groups"}
	}
	q := url.Values{
		"response_type": {"code"}, "client_id": {c.ClientID}, "redirect_uri": {i.BaseURL() + "/api/auth/oidc/callback"},
		"scope": {strings.Join(scopes, " ")}, "state": {state}, "nonce": {nonce},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}
	return d.AuthorizationEndpoint + "?" + q.Encode(), nil
}

// OIDCCallback completes the login.
func (i *Identity) OIDCCallback(ctx context.Context, state, code string) (User, error) {
	i.mu.Lock()
	p, ok := i.oidc[state]
	delete(i.oidc, state)
	i.mu.Unlock()
	if !ok {
		return User{}, fmt.Errorf("invalid state")
	}
	c := i.Config().OIDC
	d, err := discover(ctx, c.Issuer)
	if err != nil {
		return User{}, err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {i.BaseURL() + "/api/auth/oidc/callback"},
		"client_id": {c.ClientID}, "code_verifier": {p.verifier}}
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
	}
	resp, err := http.PostForm(d.TokenEndpoint, form)
	if err != nil {
		return User{}, err
	}
	defer resp.Body.Close()
	var tok struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return User{}, err
	}
	if tok.IDToken == "" {
		return User{}, fmt.Errorf("token exchange failed: %s", tok.Error)
	}
	claims, err := verifyJWT(ctx, tok.IDToken, d.JWKSURI)
	if err != nil {
		return User{}, err
	}
	if claims["nonce"] != p.nonce {
		return User{}, fmt.Errorf("nonce mismatch")
	}
	if iss, _ := claims["iss"].(string); strings.TrimRight(iss, "/") != strings.TrimRight(d.Issuer, "/") {
		return User{}, fmt.Errorf("issuer mismatch")
	}
	if !audContains(claims["aud"], c.ClientID) {
		return User{}, fmt.Errorf("audience mismatch")
	}
	if exp, _ := claims["exp"].(float64); time.Unix(int64(exp), 0).Before(time.Now()) {
		return User{}, fmt.Errorf("token expired")
	}
	u := User{Provider: "oidc"}
	u.ID, _ = claims["sub"].(string)
	u.Email, _ = claims["email"].(string)
	u.Name, _ = claims["name"].(string)
	if u.Name == "" {
		u.Name, _ = claims["preferred_username"].(string)
	}
	gc := c.GroupsClaim
	if gc == "" {
		gc = "groups"
	}
	if gs, ok := claims[gc].([]any); ok {
		for _, g := range gs {
			u.Groups = append(u.Groups, fmt.Sprint(g))
		}
	}
	i.setCurrent(u)
	return u, nil
}

func audContains(aud any, id string) bool {
	switch a := aud.(type) {
	case string:
		return a == id
	case []any:
		for _, x := range a {
			if x == id {
				return true
			}
		}
	}
	return false
}

// verifyJWT validates an RS256 JWT against the issuer's JWKS.
func verifyJWT(ctx context.Context, token, jwksURI string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var hdr struct{ Alg, Kid string }
	_ = json.Unmarshal(hb, &hdr)
	if hdr.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported alg %s", hdr.Alg)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", jwksURI, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var jwks struct {
		Keys []struct{ Kid, Kty, N, E string } `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || (hdr.Kid != "" && k.Kid != hdr.Kid) {
			continue
		}
		nb, _ := base64.RawURLEncoding.DecodeString(k.N)
		eb, _ := base64.RawURLEncoding.DecodeString(k.E)
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}
		if rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig) == nil {
			pb, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var claims map[string]any
			return claims, json.Unmarshal(pb, &claims)
		}
	}
	return nil, fmt.Errorf("signature verification failed")
}

// ---------- LDAP / Active Directory ----------

// LDAPLogin authenticates with a directory and resolves group membership.
func (i *Identity) LDAPLogin(username, password string) (User, error) {
	c := i.Config().LDAP
	if c.URL == "" {
		return User{}, fmt.Errorf("LDAP is not configured")
	}
	if password == "" {
		return User{}, fmt.Errorf("password required")
	}
	conn, err := ldap.DialURL(c.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: c.InsecureTLS})) //nolint:gosec // explicit admin opt-in
	if err != nil {
		return User{}, err
	}
	defer conn.Close()
	if c.BindDN != "" {
		if err := conn.Bind(c.BindDN, c.BindPassword); err != nil {
			return User{}, fmt.Errorf("service bind: %w", err)
		}
	}
	filter := c.UserFilter
	if filter == "" {
		filter = "(|(uid=%s)(sAMAccountName=%s)(mail=%s))"
	}
	esc := ldap.EscapeFilter(username)
	sr, err := conn.Search(ldap.NewSearchRequest(c.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 10, false,
		strings.ReplaceAll(filter, "%s", esc), []string{"dn", "cn", "mail", "displayName", "memberOf"}, nil))
	if err != nil {
		return User{}, err
	}
	if len(sr.Entries) != 1 {
		return User{}, fmt.Errorf("user not found")
	}
	e := sr.Entries[0]
	if err := conn.Bind(e.DN, password); err != nil {
		return User{}, fmt.Errorf("invalid credentials")
	}
	u := User{ID: e.DN, Name: firstNonEmpty(e.GetAttributeValue("displayName"), e.GetAttributeValue("cn"), username), Email: e.GetAttributeValue("mail"), Provider: "ldap"}
	for _, g := range e.GetAttributeValues("memberOf") {
		if dn, err := ldap.ParseDN(g); err == nil && len(dn.RDNs) > 0 && len(dn.RDNs[0].Attributes) > 0 {
			u.Groups = append(u.Groups, dn.RDNs[0].Attributes[0].Value)
		}
	}
	i.setCurrent(u)
	return u, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---------- SAML 2.0 service provider ----------

// SAML returns (and lazily builds) the SAML SP middleware.
func (i *Identity) SAML(ctx context.Context) (*samlsp.Middleware, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.saml != nil {
		return i.saml, nil
	}
	c := i.Config().SAML
	if c.IDPMetadataURL == "" {
		return nil, fmt.Errorf("SAML is not configured")
	}
	key, cert, err := i.spKeyPair()
	if err != nil {
		return nil, err
	}
	mdURL, err := url.Parse(c.IDPMetadataURL)
	if err != nil {
		return nil, err
	}
	md, err := samlsp.FetchMetadata(ctx, http.DefaultClient, *mdURL)
	if err != nil {
		return nil, err
	}
	root, _ := url.Parse(i.BaseURL() + "/api/auth/saml")
	opts := samlsp.Options{URL: *root, Key: key, Certificate: cert, IDPMetadata: md, EntityID: c.EntityID, AllowIDPInitiated: true}
	m, err := samlsp.New(opts)
	if err != nil {
		return nil, err
	}
	i.saml = m
	return m, nil
}

// SAMLComplete reads the SAML session and makes it the current user.
func (i *Identity) SAMLComplete(r *http.Request) (User, error) {
	sess := samlsp.SessionFromContext(r.Context())
	sa, ok := sess.(samlsp.SessionWithAttributes)
	if !ok {
		return User{}, fmt.Errorf("no SAML session")
	}
	attrs := sa.GetAttributes()
	u := User{Provider: "saml", ID: attrs.Get("urn:oasis:names:tc:SAML:attribute:subject-id")}
	if jc, ok := sess.(samlsp.JWTSessionClaims); ok {
		u.ID = jc.Subject
	}
	u.Email = firstNonEmpty(attrs.Get("email"), attrs.Get("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"), attrs.Get("mail"))
	u.Name = firstNonEmpty(attrs.Get("displayName"), attrs.Get("http://schemas.microsoft.com/identity/claims/displayname"), attrs.Get("name"), u.Email, u.ID)
	u.Groups = append(attrs["groups"], attrs["http://schemas.microsoft.com/ws/2008/06/identity/claims/groups"]...)
	i.setCurrent(u)
	return u, nil
}

func (i *Identity) spKeyPair() (*rsa.PrivateKey, *x509.Certificate, error) {
	dir := filepath.Join(i.Store.Dir(), "saml")
	kp, cp := filepath.Join(dir, "sp.key"), filepath.Join(dir, "sp.crt")
	if kb, err := os.ReadFile(kp); err == nil {
		cb, err := os.ReadFile(cp)
		if err == nil {
			kblk, _ := pem.Decode(kb)
			cblk, _ := pem.Decode(cb)
			if kblk != nil && cblk != nil {
				key, err1 := x509.ParsePKCS1PrivateKey(kblk.Bytes)
				cert, err2 := x509.ParseCertificate(cblk.Bytes)
				if err1 == nil && err2 == nil {
					return key, cert, nil
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "KubeNova SAML SP"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	_ = os.WriteFile(kp, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600)
	_ = os.WriteFile(cp, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	cert, err := x509.ParseCertificate(der)
	return key, cert, err
}

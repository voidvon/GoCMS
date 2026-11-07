package site

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"gocms/internal/memberoauth"
	"golang.org/x/oauth2"
)

const microsoftAuthority = "https://login.microsoftonline.com"

var microsoftTenantID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var microsoftKeySet = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), googleHTTPClient), microsoftAuthority+"/common/discovery/v2.0/keys")

func microsoftOAuth(c memberoauth.Config, secret string) oauth2.Config {
	return oauth2.Config{ClientID: c.ClientID, ClientSecret: secret, RedirectURL: c.Callback(), Scopes: []string{"openid", "profile"}, Endpoint: oauth2.Endpoint{AuthURL: microsoftAuthority + "/common/oauth2/v2.0/authorize", TokenURL: microsoftAuthority + "/common/oauth2/v2.0/token", AuthStyle: oauth2.AuthStyleInParams}}
}

func (s *Server) microsoftAvailable(r *http.Request) bool {
	c, err := s.googleSite(r)
	if err != nil {
		return false
	}
	c, err = memberoauth.LoadProvider(r.Context(), s.database, c.SiteID, "microsoft")
	if err != nil || !c.Enabled {
		return false
	}
	s.oauthMu.RLock()
	defer s.oauthMu.RUnlock()
	_, err = s.oauthVault.Open(memberoauth.SecretScope(c), c.Encrypted)
	return err == nil
}

func verifyMicrosoft(ctx context.Context, c memberoauth.Config, secret, code, verifier string) (googleClaims, error) {
	ctx = oidc.ClientContext(ctx, googleHTTPClient)
	config := microsoftOAuth(c, secret)
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return googleClaims{}, err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return googleClaims{}, errors.New("missing id token")
	}
	return verifyMicrosoftIDToken(ctx, c, raw, microsoftKeySet)
}

func verifyMicrosoftIDToken(ctx context.Context, c memberoauth.Config, raw string, keys oidc.KeySet) (googleClaims, error) {
	// The common endpoint issues tenant-specific tokens. Read only the tenant
	// hint first, then verify signature, exact issuer, audience and expiry. Never
	// follow a URL or disable issuer verification based on untrusted token data.
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return googleClaims{}, errors.New("invalid id token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	var hint struct {
		Tenant string `json:"tid"`
	}
	if err != nil || json.Unmarshal(payload, &hint) != nil || !microsoftTenantID.MatchString(hint.Tenant) {
		return googleClaims{}, errors.New("invalid tenant")
	}
	issuer := microsoftAuthority + "/" + hint.Tenant + "/v2.0"
	v := oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{"RS256"}})
	id, err := v.Verify(ctx, raw)
	if err != nil {
		return googleClaims{}, err
	}
	var claims struct {
		googleClaims
		Tenant  string `json:"tid"`
		Version string `json:"ver"`
	}
	if err = id.Claims(&claims); err != nil {
		return googleClaims{}, err
	}
	if claims.Version != "2.0" || claims.Tenant != hint.Tenant || claims.Subject == "" || len(claims.Subject) > 200 || (claims.AuthorizedParty != "" && claims.AuthorizedParty != c.ClientID) {
		return googleClaims{}, errors.New("invalid Microsoft claims")
	}
	// Subjects are scoped to issuer and application. Contact email is not proof
	// of ownership: do not import it or use it to match existing members.
	return googleClaims{Subject: claims.Tenant + ":" + claims.Subject, Name: claims.Name, Nonce: claims.Nonce}, nil
}

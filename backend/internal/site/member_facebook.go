package site

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gocms/internal/memberoauth"
	"golang.org/x/oauth2"
	"net/http"
	"net/url"
	"time"
)

// Pin a supported Graph version; provider URLs never come from administrator input.
const facebookGraph = "https://graph.facebook.com/v24.0"

func facebookOAuth(c memberoauth.Config, secret string) oauth2.Config {
	return oauth2.Config{ClientID: c.ClientID, ClientSecret: secret, RedirectURL: c.Callback(), Scopes: []string{"public_profile"}, Endpoint: oauth2.Endpoint{AuthURL: "https://www.facebook.com/v24.0/dialog/oauth", TokenURL: facebookGraph + "/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}}
}
func (s *Server) facebookAvailable(r *http.Request) bool {
	c, e := s.googleSite(r)
	if e != nil {
		return false
	}
	c, e = memberoauth.LoadProvider(r.Context(), s.database, c.SiteID, "facebook")
	if e != nil || !c.Enabled {
		return false
	}
	s.oauthMu.RLock()
	defer s.oauthMu.RUnlock()
	_, e = s.oauthVault.Open(memberoauth.SecretScope(c), c.Encrypted)
	return e == nil
}
func facebookGet(ctx context.Context, endpoint string, params url.Values, bearer string, out any) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, facebookGraph+endpoint+"?"+params.Encode(), nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, e := googleHTTPClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("Facebook request rejected")
	}
	return json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(out)
}
func verifyFacebook(ctx context.Context, c memberoauth.Config, secret, code string) (googleClaims, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, googleHTTPClient)
	cfg := facebookOAuth(c, secret)
	token, e := cfg.Exchange(ctx, code)
	if e != nil {
		return googleClaims{}, e
	}
	var debug struct {
		Data struct {
			Valid       bool   `json:"is_valid"`
			AppID       string `json:"app_id"`
			UserID      string `json:"user_id"`
			Expires     int64  `json:"expires_at"`
			DataExpires int64  `json:"data_access_expires_at"`
		} `json:"data"`
	}
	e = facebookGet(ctx, "/debug_token", url.Values{"input_token": {token.AccessToken}}, c.ClientID+"|"+secret, &debug)
	d := debug.Data
	now := time.Now().Unix()
	if e != nil || !d.Valid || d.AppID != c.ClientID || d.UserID == "" || d.Expires <= now || (d.DataExpires != 0 && d.DataExpires <= now) {
		return googleClaims{}, errors.New("invalid Facebook token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token.AccessToken))
	proof := hex.EncodeToString(mac.Sum(nil))
	var profile struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	e = facebookGet(ctx, "/me", url.Values{"fields": {"id,name"}, "appsecret_proof": {proof}}, token.AccessToken, &profile)
	if e != nil || profile.ID != d.UserID {
		return googleClaims{}, errors.New("Facebook identity mismatch")
	}
	// Facebook supplies no email_verified claim. Email is contact data only; never
	// create or merge an account based on it. New accounts use an empty email.
	return googleClaims{Subject: profile.ID, Name: profile.Name}, nil
}

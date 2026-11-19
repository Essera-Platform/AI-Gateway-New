package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/model"
	"github.com/coreos/go-oidc/v3/oidc"
	"gorm.io/gorm"
)

// ErrSessionEnded means the Gateway session must end now: Cognito refused its refresh token,
// or its person is no longer active.
var ErrSessionEnded = errors.New("the Essera sign-in behind this session has ended")

// verifyIDToken checks an ID token from the user pool (signature by the pool's keys, issuer,
// audience = the app client, expiry, use) and returns its sub.
func verifyIDToken(ctx context.Context, client poolClient, raw string, now time.Time) (string, error) {
	verifier := oidc.NewVerifier(client.Issuer, auth.KeySetFor(client.Issuer), &oidc.Config{
		ClientID:             client.ClientID,
		SupportedSigningAlgs: []string{oidc.RS256},
		Now:                  func() time.Time { return now },
	})
	verified, err := verifier.Verify(ctx, raw)
	if err != nil {
		return "", refuse("the ID token is not valid: " + err.Error())
	}
	var claims struct {
		TokenUse string `json:"token_use"`
	}
	if err := verified.Claims(&claims); err != nil || claims.TokenUse != "id" {
		return "", refuse("the token is not an ID token")
	}
	if verified.Subject == "" {
		return "", refuse("the ID token names no subject")
	}
	return verified.Subject, nil
}

// sealer encrypts what the Gateway keeps of a Cognito sign-in (refresh tokens, pending
// sessions) with a key derived from CRYPTO_SECRET.
func sealer() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("essera/cognito-session/" + common.CryptoSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(plain string) (string, error) {
	aead, err := sealer()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func open(sealed string) (string, error) {
	aead, err := sealer()
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(data) < aead.NonceSize() {
		return "", errors.New("a sealed value is damaged")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	return string(plain), err
}

// Bind keeps the Cognito refresh token of a new Gateway session, sealed, keyed by the session.
func Bind(sessionID string, userID int, sub, refreshToken string) error {
	sealed, err := seal(refreshToken)
	if err != nil {
		return err
	}
	return model.DB.Save(&records.CognitoSession{SessionID: sessionID, UserID: userID, Sub: sub, RefreshToken: sealed, UpdatedAt: time.Now().UTC()}).Error
}

// refreshFallback are the answers of GetTokensFromRefreshToken from an app client without
// refresh-token rotation, which refreshes with InitiateAuth REFRESH_TOKEN_AUTH instead.
var refreshFallback = []string{"InvalidParameterException", "UnsupportedOperationException", "UnknownOperationException", "FeatureUnavailableInTierException"}

// Refresh renews the Cognito sign-in behind a Gateway session each time the Gateway session
// refreshes: the person must still be active, Cognito must still honour the refresh token, and
// the new ID token must name the same sub. A rotated refresh token replaces the old one. When
// any of that fails the user's sessions end at once (ErrSessionEnded). If Cognito cannot be
// reached the session continues on the configuration's word and Cognito is asked next time.
// A session without a Cognito sign-in is an operator's and is left to new-api, unless its user
// is a person's.
func Refresh(ctx context.Context, sessionID string, userID int, now time.Time) error {
	if !settings.Managed() {
		return nil
	}
	var row records.CognitoSession
	err := model.DB.Where("session_id = ?", sessionID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		person, err := IsPerson(userID)
		if err != nil || !person {
			return err
		}
		return endSessions(ctx, userID, "a person's session without an Essera sign-in")
	}
	if err != nil {
		return err
	}
	client, ok := signInClient()
	if !ok {
		return endSessions(ctx, userID, "the sign-in client is not configured")
	}
	if _, err := personUser(row.Sub); err != nil {
		var refusal *Refusal
		if errors.As(err, &refusal) {
			return endSessions(ctx, userID, refusal.Reason)
		}
		return err
	}
	refreshToken, err := open(row.RefreshToken)
	if err != nil {
		return endSessions(ctx, userID, "the stored refresh token cannot be read")
	}
	var answer struct {
		AuthenticationResult *authenticationResult `json:"AuthenticationResult"`
	}
	err = client.call(ctx, "GetTokensFromRefreshToken", map[string]any{
		"RefreshToken": refreshToken, "ClientId": client.ClientID, "ClientSecret": client.ClientSecret,
	}, &answer)
	var failure *cognitoError
	if errors.As(err, &failure) && slices.Contains(refreshFallback, failure.Type) {
		err = client.call(ctx, "InitiateAuth", map[string]any{
			"AuthFlow": "REFRESH_TOKEN_AUTH", "ClientId": client.ClientID,
			"AuthParameters": map[string]string{"REFRESH_TOKEN": refreshToken, "SECRET_HASH": client.secretHash(row.Sub)},
		}, &answer)
	}
	if err != nil {
		if errors.As(err, &failure) && failure.refused() {
			return endSessions(ctx, userID, "Cognito refused the refresh token: "+failure.Type)
		}
		common.SysError(common.LogText("essera: refreshing the sign-in of user %d: %s", userID, err.Error()))
		return nil
	}
	if answer.AuthenticationResult == nil {
		return endSessions(ctx, userID, "Cognito answered the refresh without tokens")
	}
	sub, err := verifyIDToken(ctx, client, answer.AuthenticationResult.IdToken, now)
	if err != nil || sub != row.Sub {
		return endSessions(ctx, userID, "the refreshed ID token does not name the session's person")
	}
	rotated := answer.AuthenticationResult.RefreshToken
	if rotated == "" || rotated == refreshToken {
		return model.DB.Model(&records.CognitoSession{}).Where("session_id = ?", sessionID).Update("updated_at", now.UTC()).Error
	}
	sealed, err := seal(rotated)
	if err != nil {
		return err
	}
	return model.DB.Model(&records.CognitoSession{}).Where("session_id = ?", sessionID).
		Updates(map[string]any{"refresh_token": sealed, "updated_at": now.UTC()}).Error
}

// endSessions ends every session of the user: a new authentication version voids their access
// tokens, their sessions are revoked, and their Cognito refresh tokens are revoked and dropped.
func endSessions(ctx context.Context, userID int, reason string) error {
	common.SysLog(common.LogText("essera: ending the sessions of user %d: %s", userID, reason))
	if _, err := model.BumpUserAuthVersion(userID); err != nil {
		return err
	}
	if _, err := model.RevokeAllUserSessions(userID, "essera_sign_in_ended"); err != nil {
		return err
	}
	var rows []records.CognitoSession
	if err := model.DB.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		SignOut(ctx, row.SessionID)
	}
	return ErrSessionEnded
}

// SignOut revokes the Cognito refresh token of a Gateway session that ends, and drops it.
func SignOut(ctx context.Context, sessionID string) {
	var row records.CognitoSession
	if err := model.DB.Where("session_id = ?", sessionID).First(&row).Error; err != nil {
		return
	}
	if client, ok := signInClient(); ok {
		if refreshToken, err := open(row.RefreshToken); err == nil {
			err = client.call(ctx, "RevokeToken", map[string]any{"Token": refreshToken, "ClientId": client.ClientID, "ClientSecret": client.ClientSecret}, nil)
			if err != nil {
				common.SysError(common.LogText("essera: revoking the sign-in of session %s: %s", sessionID, err.Error()))
			}
		}
	}
	if err := model.DB.Where("session_id = ?", sessionID).Delete(&records.CognitoSession{}).Error; err != nil {
		common.SysError(common.LogText("essera: dropping the sign-in of session %s: %s", sessionID, err.Error()))
	}
}

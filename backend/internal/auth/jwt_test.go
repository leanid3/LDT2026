package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
)

func TestIssuer_IssueAndParse(t *testing.T) {
	issuer := auth.NewIssuer("test-secret", time.Hour)
	userID := uuid.New()

	token, ttl, err := issuer.Issue(userID, auth.RoleInspector)
	require.NoError(t, err)
	require.InDelta(t, time.Hour.Seconds(), ttl.Seconds(), 1)
	require.NotEmpty(t, token)

	claims, err := issuer.Parse(token)
	require.NoError(t, err)
	require.Equal(t, auth.RoleInspector, claims.Role)

	gotID, err := claims.UserID()
	require.NoError(t, err)
	require.Equal(t, userID, gotID)
}

func TestIssuer_Parse_RejectsExpired(t *testing.T) {
	issuer := auth.NewIssuer("test-secret", -time.Minute) // уже истёк
	token, _, err := issuer.Issue(uuid.New(), auth.RoleAdmin)
	require.NoError(t, err)

	_, err = issuer.Parse(token)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestIssuer_Parse_RejectsWrongSecret(t *testing.T) {
	issuer := auth.NewIssuer("secret-a", time.Hour)
	token, _, err := issuer.Issue(uuid.New(), auth.RoleAdmin)
	require.NoError(t, err)

	other := auth.NewIssuer("secret-b", time.Hour)
	_, err = other.Parse(token)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestPasswordHashing(t *testing.T) {
	hash, err := auth.HashPassword("s3cr3t!")
	require.NoError(t, err)
	require.NotEqual(t, "s3cr3t!", hash)

	require.True(t, auth.CheckPassword(hash, "s3cr3t!"))
	require.False(t, auth.CheckPassword(hash, "wrong"))
}

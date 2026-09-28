package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func TestService_Login(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	repo := auth.NewRepository(database.Pool())
	issuer := auth.NewIssuer("test-secret", 12*time.Hour)
	service := auth.NewService(repo, issuer)

	hash, err := auth.HashPassword("correct-horse")
	require.NoError(t, err)

	activeID := uuid.New()
	require.NoError(t, repo.Create(ctx, auth.User{
		ID: activeID, Login: "inspector1", PasswordHash: hash, FullName: "Иван Инспекторов",
		Role: auth.RoleInspector, IsActive: true,
	}))

	inactiveID := uuid.New()
	require.NoError(t, repo.Create(ctx, auth.User{
		ID: inactiveID, Login: "disabled1", PasswordHash: hash, FullName: "Уволен",
		Role: auth.RoleInspector, IsActive: false,
	}))

	t.Run("correct credentials", func(t *testing.T) {
		token, ttl, user, err := service.Login(ctx, "inspector1", "correct-horse")
		require.NoError(t, err)
		require.NotEmpty(t, token)
		require.Equal(t, 12*time.Hour, ttl)
		require.Equal(t, activeID, user.ID)

		claims, err := issuer.Parse(token)
		require.NoError(t, err)
		require.Equal(t, auth.RoleInspector, claims.Role)
	})

	t.Run("wrong password", func(t *testing.T) {
		_, _, _, err := service.Login(ctx, "inspector1", "wrong")
		require.ErrorIs(t, err, auth.ErrInvalidCredentials)
	})

	t.Run("unknown login", func(t *testing.T) {
		_, _, _, err := service.Login(ctx, "ghost", "whatever")
		require.ErrorIs(t, err, auth.ErrInvalidCredentials)
	})

	t.Run("inactive user", func(t *testing.T) {
		_, _, _, err := service.Login(ctx, "disabled1", "correct-horse")
		require.ErrorIs(t, err, auth.ErrInvalidCredentials)
	})

	t.Run("duplicate login is idempotent no-op", func(t *testing.T) {
		err := repo.Create(ctx, auth.User{
			ID: uuid.New(), Login: "inspector1", PasswordHash: hash, FullName: "Дубликат",
			Role: auth.RoleAdmin, IsActive: true,
		})
		require.NoError(t, err)

		u, err := repo.FindByLogin(ctx, "inspector1")
		require.NoError(t, err)
		require.Equal(t, activeID, u.ID, "исходная запись не должна быть перезаписана")
	})
}

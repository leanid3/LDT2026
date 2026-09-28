package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidCredentials = errors.New("auth: неверный логин или пароль")

type Service struct {
	repo   *Repository
	issuer *Issuer
}

func NewService(repo *Repository, issuer *Issuer) *Service {
	return &Service{repo: repo, issuer: issuer}
}

// Login проверяет логин/пароль и выпускает токен. Неактивный пользователь тоже получает
// ErrInvalidCredentials — не раскрываем причину отказа (перечисление логинов).
func (s *Service) Login(ctx context.Context, login, password string) (token string, ttl time.Duration, user *User, err error) {
	u, err := s.repo.FindByLogin(ctx, login)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", 0, nil, ErrInvalidCredentials
		}
		return "", 0, nil, fmt.Errorf("find user: %w", err)
	}

	if !u.IsActive || !CheckPassword(u.PasswordHash, password) {
		return "", 0, nil, ErrInvalidCredentials
	}

	token, ttl, err = s.issuer.Issue(u.ID, u.Role)
	if err != nil {
		return "", 0, nil, fmt.Errorf("issue token: %w", err)
	}
	return token, ttl, u, nil
}

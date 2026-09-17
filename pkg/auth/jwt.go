package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/peterouob/seckill_service/pkg/config"
	"go.uber.org/fx"
)

var (
	ErrInvalidToken = errors.New("auth: token is invalid or expired")
	ErrEmptySecret  = errors.New("auth: signing secret must not be empty")
)

type Claims struct {
	UserID   string `json:"uid"`
	AccessID string `json:"aid"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(cfg *config.Config) (*Manager, error) {
	if cfg.JWT.Secret == "" {
		return nil, ErrEmptySecret
	}
	return &Manager{secret: []byte(cfg.JWT.Secret), ttl: cfg.JWT.TTL}, nil
}

func (m *Manager) Issue(userID string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(m.ttl)

	claims := Claims{
		UserID:   userID,
		AccessID: uuid.NewString(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, expiresAt, nil
}

func (m *Manager) Verify(tokenString string) (*Claims, error) {
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(tokenString, claims, m.keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidToken, err)
	}
	if !token.Valid || claims.UserID == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (m *Manager) keyFunc(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("unexpected signing method %v", token.Header["alg"])
	}
	return m.secret, nil
}

var Module = fx.Module("auth", fx.Provide(NewManager))

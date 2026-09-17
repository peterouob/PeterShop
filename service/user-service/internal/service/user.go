package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/peterouob/seckill_service/pkg/auth"
	"github.com/peterouob/seckill_service/service/user-service/internal/model"
	"github.com/peterouob/seckill_service/service/user-service/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("username or password is incorrect")
	ErrPasswordMismatch   = errors.New("password and check password do not match")
	ErrUsernameTaken      = errors.New("username is already taken")
)

type UserService interface {
	Login(ctx context.Context, req model.UserLoginReq) (string, error)
	Register(ctx context.Context, req model.UserRegisterReq) (string, error)
}

type userService struct {
	repo  repository.UserRepo
	token *auth.Manager
}

func NewUserService(repo repository.UserRepo, token *auth.Manager) UserService {
	return &userService{repo: repo, token: token}
}

func (s *userService) Login(ctx context.Context, req model.UserLoginReq) (string, error) {
	user, err := s.repo.GetByUsername(ctx, req.Username)
	if errors.Is(err, repository.ErrUserNotFound) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}

	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		return "", ErrInvalidCredentials
	}

	token, _, err := s.token.Issue(user.UserId)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *userService) Register(ctx context.Context, req model.UserRegisterReq) (string, error) {
	if req.Password != req.CheckPassword {
		return "", ErrPasswordMismatch
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("user: hash password: %w", err)
	}

	user, err := s.repo.Create(ctx, req.Username, string(hash))
	if errors.Is(err, repository.ErrUserExists) {
		return "", ErrUsernameTaken
	}
	if err != nil {
		return "", err
	}
	return user.UserId, nil
}

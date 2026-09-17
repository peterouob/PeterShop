package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/peterouob/seckill_service/service/user-service/internal/model"
	"gorm.io/gorm"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("username is already taken")
)

type UserRepo interface {
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	Create(ctx context.Context, username, passwordHash string) (*model.User, error)
}

type userRepo struct {
	db *gorm.DB
}

var _ UserRepo = (*userRepo)(nil)

func NewUserRepo(db *gorm.DB) UserRepo {
	return &userRepo{db: db}
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, ErrUserNotFound
	case err != nil:
		return nil, fmt.Errorf("user: query by username %q: %w", username, err)
	}
	return &user, nil
}

func (r *userRepo) Create(ctx context.Context, username, passwordHash string) (*model.User, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("user: generate user id: %w", err)
	}

	user := &model.User{
		UserId:   id.String(),
		Username: username,
		Password: passwordHash,
	}

	err = r.db.WithContext(ctx).Create(user).Error
	switch {
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return nil, ErrUserExists
	case err != nil:
		return nil, fmt.Errorf("user: create %q: %w", username, err)
	}
	return user, nil
}

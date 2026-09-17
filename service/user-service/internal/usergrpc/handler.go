package usergrpc

import (
	"context"
	"errors"

	"github.com/peterouob/seckill_service/api/userproto"
	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/peterouob/seckill_service/service/user-service/internal/model"
	"github.com/peterouob/seckill_service/service/user-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	userproto.UnimplementedUserServiceServer
	users service.UserService
}

func NewHandler(users service.UserService) *Handler {
	return &Handler{users: users}
}

func (h *Handler) UserLogin(ctx context.Context, in *userproto.UserLoginReq) (*userproto.UserLoginResp, error) {
	if in.GetUsername() == "" || in.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "username and password are required")
	}

	token, err := h.users.Login(ctx, model.UserLoginReq{
		Username: in.GetUsername(),
		Password: in.GetPassword(),
	})
	if err != nil {
		return nil, toStatus(err)
	}

	return &userproto.UserLoginResp{Msg: "success", Token: token}, nil
}

func (h *Handler) UserRegister(ctx context.Context, in *userproto.UserRegisterReq) (*userproto.UserRegisterResp, error) {
	if in.GetUsername() == "" || in.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "username and password are required")
	}

	userID, err := h.users.Register(ctx, model.UserRegisterReq{
		Username:      in.GetUsername(),
		Password:      in.GetPassword(),
		CheckPassword: in.GetCheckPassword(),
	})
	if err != nil {
		return nil, toStatus(err)
	}

	return &userproto.UserRegisterResp{Msg: userID}, nil
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		return status.Error(codes.Unauthenticated, "username or password is incorrect")
	case errors.Is(err, service.ErrPasswordMismatch):
		return status.Error(codes.InvalidArgument, "password and check password do not match")
	case errors.Is(err, service.ErrUsernameTaken):
		return status.Error(codes.AlreadyExists, "username is already taken")
	default:
		logger.Errorf(err, "user: unhandled failure")
		return status.Error(codes.Internal, "user request failed")
	}
}

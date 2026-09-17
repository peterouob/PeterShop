package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var grpcToHTTP = map[codes.Code]int{
	codes.OK:                 http.StatusOK,
	codes.InvalidArgument:    http.StatusBadRequest,
	codes.FailedPrecondition: http.StatusBadRequest,
	codes.Unauthenticated:    http.StatusUnauthorized,
	codes.PermissionDenied:   http.StatusForbidden,
	codes.NotFound:           http.StatusNotFound,
	codes.AlreadyExists:      http.StatusConflict,
	codes.Aborted:            http.StatusConflict,
	codes.ResourceExhausted:  http.StatusTooManyRequests,
	codes.Unavailable:        http.StatusServiceUnavailable,
	codes.DeadlineExceeded:   http.StatusGatewayTimeout,
	codes.Unimplemented:      http.StatusNotImplemented,
}

func badRequest(c *gin.Context, err error) {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func fromGRPC(c *gin.Context, err error) {
	st, ok := status.FromError(err)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	httpStatus, mapped := grpcToHTTP[st.Code()]
	if !mapped {
		httpStatus = http.StatusInternalServerError
	}
	c.AbortWithStatusJSON(httpStatus, gin.H{"error": st.Message()})
}

func ok(c *gin.Context, body gin.H) {
	c.JSON(http.StatusOK, body)
}

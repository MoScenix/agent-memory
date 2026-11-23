package utils

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type HTTPError struct {
	Status  int
	Message string
}

func (err *HTTPError) Error() string { return err.Message }

func SendErrResponse(ctx context.Context, c *app.RequestContext, code int, err error) {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		code = httpErr.Status
		err = httpErr
	}
	if code == consts.StatusInternalServerError {
		slog.ErrorContext(ctx, "agent memory request failed", "error", err)
		c.JSON(code, map[string]string{"error": "internal server error"})
		return
	}
	c.JSON(code, map[string]string{"error": err.Error()})
}

func SendSuccessResponse(ctx context.Context, c *app.RequestContext, code int, data proto.Message) {
	body, err := (protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: true, EmitDefaultValues: true}).Marshal(data)
	if err != nil {
		SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}
	c.Data(code, "application/json; charset=utf-8", body)
}

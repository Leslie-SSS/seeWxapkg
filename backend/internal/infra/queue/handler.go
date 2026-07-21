package queue

import (
	"context"
	"fmt"
)

func invokeHandler(handler func(context.Context, string) error, ctx context.Context, taskID string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("queue handler panic: %v", recovered)
		}
	}()
	return handler(ctx, taskID)
}

package biz

import (
	"io"

	"github.com/go-kratos/kratos/v2/log"
)

func discardLogger() log.Logger {
	return log.NewStdLogger(io.Discard)
}

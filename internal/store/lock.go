package store

import "context"

type lockKey struct{}

// ForUpdate 让本次事务里的读取带行锁。PostgreSQL 使用 FOR UPDATE，内存库靠事务互斥。
func ForUpdate(ctx context.Context) context.Context {
	return context.WithValue(ctx, lockKey{}, true)
}

func wantsLock(ctx context.Context) bool {
	v, _ := ctx.Value(lockKey{}).(bool)
	return v
}

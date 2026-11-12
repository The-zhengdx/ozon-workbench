package storage

import "context"

// None is the default provider. The app starts, and any step that needs a public URL is refused.
type None struct{}

func (None) Name() string { return "none" }

func (None) Configured() bool { return false }

func (None) Message() string { return ErrUnconfigured.Error() }

func (None) Upload(context.Context, string, string) (Object, error) {
	return Object{}, ErrUnconfigured
}

func (None) Delete(context.Context, string) error { return ErrUnconfigured }

func (None) Health(context.Context) error { return ErrUnconfigured }

func (None) Refresh(context.Context, string) (string, error) { return "", ErrUnconfigured }

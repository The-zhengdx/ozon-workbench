package biz

// pool bounds how many Codex turns run at once.
type pool struct {
	sem chan struct{}
}

func NewPool(workers int) Scheduler {
	if workers < 1 {
		workers = 1
	}
	return &pool{sem: make(chan struct{}, workers)}
}

func (p *pool) Go(fn func()) {
	go func() {
		p.sem <- struct{}{}
		defer func() { <-p.sem }()
		fn()
	}()
}

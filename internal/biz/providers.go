package biz

import "ozon-workbench/internal/conf"

// NewTaskCatalog adapts the category repository to the workbench port.
func NewTaskCatalog(repo CategoryRepo) TaskCatalog { return repo }

// NewTaskRuntime copies the media and Codex timeout settings.
func NewTaskRuntime(cfg *conf.Runtime) TaskRuntime {
	return TaskRuntime{
		MediaRoot:       cfg.MediaRoot,
		MaxZipBytes:     cfg.MaxZipBytes,
		MaxUncompressed: cfg.MaxUncompressedBytes,
		MaxFiles:        cfg.MaxFileCount,
		CodexTimeout:    cfg.CodexTimeout,
	}
}

// NewScheduler builds the in-process Codex worker pool.
func NewScheduler(cfg *conf.Runtime) Scheduler {
	return NewPool(cfg.CodexWorkers)
}

// NewCodexRunner returns the mock runner unless codex.mock is turned off.
func NewCodexRunner(cfg *conf.Runtime) CodexRunner {
	if cfg.CodexMock {
		return NewMockRunner()
	}
	return NewCLIRunner(CLIConfig{
		Binary:     cfg.CodexBinary,
		NewArgs:    cfg.CodexNewArgs,
		ResumeArgs: cfg.CodexResumeArgs,
	})
}

package job

import "sync/atomic"

type Metrics struct {
	runFailures        atomic.Uint64
	handlerFailures    atomic.Uint64
	retries            atomic.Uint64
	deadLetters        atomic.Uint64
	repositoryFailures atomic.Uint64
}

type MetricsSnapshot struct {
	RunFailures        uint64
	HandlerFailures    uint64
	Retries            uint64
	DeadLetters        uint64
	RepositoryFailures uint64
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		RunFailures:        m.runFailures.Load(),
		HandlerFailures:    m.handlerFailures.Load(),
		Retries:            m.retries.Load(),
		DeadLetters:        m.deadLetters.Load(),
		RepositoryFailures: m.repositoryFailures.Load(),
	}
}

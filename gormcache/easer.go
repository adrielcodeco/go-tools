package caches

import (
	"sync"
)

func ease(t task, queue *sync.Map) task {
	eq := &eased{
		task: t,
		wg:   &sync.WaitGroup{},
	}
	eq.wg.Add(1)

	runner, ok := queue.LoadOrStore(t.GetId(), eq)
	if ok {
		// Another goroutine is already running this query — wait for it.
		et := runner.(*eased)
		et.wg.Wait()
		return et.task
	}

	// This goroutine is the designated runner.
	eq.task.Run()
	// Signal waiters before removing from the map so that any goroutine that
	// loaded the entry between Delete and Done still gets the result via Wait,
	// rather than starting a duplicate execution.
	eq.wg.Done()
	queue.Delete(t.GetId())
	return eq.task
}

type eased struct {
	task task
	wg   *sync.WaitGroup
}

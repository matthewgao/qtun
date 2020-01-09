package timer

import (
	"context"
	"fmt"
	"time"
)

type Timer struct {
	Timers      map[time.Duration]func()
	CancelFuncs map[time.Duration]*context.CancelFunc
}

func NewTimer() Timer {
	return Timer{
		Timers:      make(map[time.Duration]func()),
		CancelFuncs: make(map[time.Duration]*context.CancelFunc),
	}
}

func (this *Timer) RegisterTask(fn func(), interval time.Duration) {
	this.Timers[interval] = fn
}

func (this *Timer) RunTask(ctx context.Context, fn func(), interval time.Duration) {
	t := time.NewTimer(interval)
	fn()
	for {
		select {
		case <-t.C:
			fn()
			t.Reset(interval)
		case <-ctx.Done():
			fmt.Printf("%d task has exited \n", interval)
			return
		}
	}
}

func (this *Timer) Start() {
	bgctx := context.Background()
	for k, v := range this.Timers {
		ctx, cancelFunc := context.WithCancel(bgctx)
		this.CancelFuncs[k] = &cancelFunc
		go this.RunTask(ctx, v, k)
	}
}

func (this *Timer) Stop() {
	for k, v := range this.CancelFuncs {
		(*v)()
		fmt.Printf("%d task has been canceled \n", k)
	}
}

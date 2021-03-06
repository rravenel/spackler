package spackler

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Caddy tracks multiple goroutines ensuring they exit before the
// main routine exits.
type Caddy struct {
	o             *sync.Once
	wg            *sync.WaitGroup
	stopChan      chan bool
	sigChan       chan os.Signal
	notifyDefault *bool
	isTopLevel    bool
}

var ErrStopping = errors.New("spackler: stopping")

// New returns a new, initilized Caddy instance.
// If true is passed in, this instance will stop on SIGINT and SIGTERM.
func New(stopOnOS bool) *Caddy {
	c := &Caddy{}
	c.o = &sync.Once{}
	c.wg = &sync.WaitGroup{}
	c.stopChan = make(chan bool)
	c.sigChan = make(chan os.Signal)
	c.notifyDefault = &stopOnOS
	c.isTopLevel = true // prevent new goroutines while stopping

	return c
}

// public methods //

// Go provides goroutine tracking.  It is used in place of the go statement.
// It calls the provided function passing in a *Caddy which may be used to make
// subsequent calls to Go() and Looper().
func (c *Caddy) Go(f func(caddy *Caddy)) error {
	c.listen()

	c2 := c
	if c.isTopLevel {
		// prevent new goroutines while stopping
		select {
		case <-c.stopChan:
			return ErrStopping
		default:
		}

		c2 = c.copy()
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		f(c2)
	}()

	return nil
}

// Looper provides cancelable task execution on the specified interval.  It does
// not start new goroutines, but the provided function may.  On the stop signal,
// the loop exits and Looper returns.  Function execution is synchronous and
// delays due to long function run time are handled per time.Ticker.
func (c *Caddy) Looper(interval time.Duration, runImmediately bool, f func()) {
	c.listen()

	// time.NewTicker will panic on duration < 1
	var t <-chan time.Time
	if 0 == interval {
		ch := make(chan time.Time)
		close(ch)
		t = (<-chan time.Time)(ch)
	} else {
		t = time.NewTicker(interval).C
	}

	if runImmediately {
		f()
	}

	for {
		select {
		case <-t:

			// select has no precedence
			// always return on quit
			select {
			case <-c.stopChan:
				return
			default:
			}

			f()
		case <-c.stopChan:
			return
		}
	}

}

// SigChan exposes write access to the sigChan for the purpose of making
// os.signal calls such as Notify().  Writing to or closing this channel is
// equivalent to calling Stop().
func (c *Caddy) SigChan() (ch chan<- os.Signal) {
	return (chan<- os.Signal)(c.sigChan)
}

// Stop sends a stop signal.
func (c *Caddy) Stop() {
	c.sigChan <- syscall.SIGINT
}

// Stopping exposes read access to stopChan.
func (c *Caddy) Stopping() (ch <-chan bool) {
	return (<-chan bool)(c.stopChan)
}

// Wait wraps sync.WaitGroup.Wait() on all tracked goroutines.
func (c *Caddy) Wait() {
	c.wg.Wait()
}

// private methods //

func (c *Caddy) listen() {
	c.o.Do(func() {
		// wait until we need the SIG before capturing
		if *c.notifyDefault {
			signal.Notify(c.sigChan, syscall.SIGINT, syscall.SIGTERM)
		}

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			<-c.sigChan       // wait on the signal channel
			close(c.stopChan) // broadcast on the stop channel
		}()
	})
}

func (c *Caddy) copy() *Caddy {
	c2 := &Caddy{}
	c2.o = c.o
	c2.wg = c.wg
	c2.stopChan = c.stopChan
	c2.sigChan = c.sigChan
	c2.notifyDefault = c.notifyDefault
	c2.isTopLevel = false // enables new goroutines while stopping

	return c2
}

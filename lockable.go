// Package lockable defines Lockable type to help construct concurrent use struct.
package lockable

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Lockable is an object that is safe for concurrent use
// by multiple goroutines.
//
// A Lockable must not be copied after first use.
type Lockable[T any] struct {
	noCopy noCopy

	// L is held while invoking Do.
	// It must not be changed after first use.
	L sync.Locker

	data  T
	rlock sync.Locker

	checker copyChecker
}

// New returns a new Lockable with Locker l and using data as its
// initial contents. The new Lockable takes ownership of data, and the
// caller should not use data after this call.
func New[T any](l sync.Locker, data T) *Lockable[T] {
	return &Lockable[T]{L: l, data: data}
}

// A rLocker represents an object that can obtains a Locker interface that implements
// the Lock and Unlock methods by calling RLock and RUnlock.
type rLocker interface {
	RLocker() sync.Locker
}

// Do calls the function f while l.L is held.
//
// If readOnly is true and l.L implements 'RLocker() sync.Locker' method,
// it will call it to obtains a Locker interface that implements
// the Lock and Unlock methods by calling RLock and RUnlock.
//
// Because no call to Do returns until the one call to f returns, if f causes
// Do to be called, it will deadlock.
func (l *Lockable[T]) Do(readOnly bool, f func(*T) error) (err error) {
	l.checker.check()

	lock := l.L
	if readOnly {
		lock.Lock()
		// Check at most once whether l.L is a RLocker.
		if l.rlock == nil {
			if rl, ok := l.L.(rLocker); ok {
				l.rlock = rl.RLocker()
			} else {
				l.rlock = l.L
			}
		}
		lock.Unlock()
		lock = l.rlock
	}

	lock.Lock()
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case error:
				err = e
			case string:
				err = errors.New(e)
			default:
				err = fmt.Errorf("%v", r)
			}
		}
		lock.Unlock()
	}()
	return f(&l.data)
}

// copyChecker holds back pointer to itself to detect object copying.
type copyChecker uintptr

func (c *copyChecker) check() {
	if uintptr(unsafe.Pointer(c)) != atomic.LoadUintptr((*uintptr)(c)) &&
		!atomic.CompareAndSwapUintptr((*uintptr)(c), 0, uintptr(unsafe.Pointer(c))) &&
		uintptr(unsafe.Pointer(c)) != atomic.LoadUintptr((*uintptr)(c)) {
		panic("lockable.Lockable is copied")
	}
}

// noCopy may be added to structs which must not be copied
// after the first use.
//
// See https://golang.org/issues/8005#issuecomment-190753527
// for details.
//
// Note that it must not be embedded, due to the Lock and Unlock methods.
type noCopy struct{}

// Lock is a no-op used by -copylocks checker from `go vet`.
func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

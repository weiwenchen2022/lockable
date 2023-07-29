package lockable_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/weiwenchen2022/lockable"
)

func TestLockable(t *testing.T) {
	t.Parallel()

	// check that f calling synchronously
	l := Lockable[map[uint64]string]{
		L: new(sync.Mutex),
	}

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	start := time.Now()
	for i := 0; i < n; i++ {
		go func() {
			_ = l.Do(false, func(*map[uint64]string) error {
				time.Sleep(100 * time.Millisecond)
				wg.Done()
				return nil
			})
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	if elapsed < n*100*time.Millisecond || elapsed >= (n+1)*100*time.Millisecond {
		t.Fatal("f calling not synchronously")
	}

	// check mu is locked
	mu := new(sync.Mutex)
	l = Lockable[map[uint64]string]{L: mu}
	if err := l.Do(false, func(*map[uint64]string) error {
		if mu.TryLock() {
			err := errors.New("mu is unlocked")
			mu.Unlock()
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if !mu.TryLock() {
		t.Error("mu is locked")
	} else {
		mu.Unlock()
	}
}

func TestLockableCopy(t *testing.T) {
	defer func() {
		if g, w := recover(), "lockable.Lockable is copied"; w != g {
			t.Fatalf("got %v, expect %q", g, w)
		}
	}()

	l := Lockable[map[uint64]string]{L: new(sync.Mutex)}
	_ = l.Do(false, func(*map[uint64]string) error {
		return nil
	})

	var l2 Lockable[map[uint64]string]
	reflect.ValueOf(&l2).Elem().Set(reflect.ValueOf(&l).Elem()) // l2 := l, hidden from vet
	_ = l2.Do(false, func(*map[uint64]string) error {
		return nil
	})
}

// hand-coded concurrent use map
type syncMap struct {
	lock sync.Mutex
	data map[uint64]string
}

func (m *syncMap) add(seq uint64, v string) {
	m.lock.Lock()
	m.data[seq] = v
	m.lock.Unlock()
}

func (m *syncMap) delete(seq uint64) error {
	m.lock.Lock()
	if _, exists := m.data[seq]; !exists {
		err := fmt.Errorf("not found entry for seq %d", seq)
		m.lock.Unlock()
		return err
	}
	delete(m.data, seq)
	m.lock.Unlock()
	return nil
}

func BenchmarkLockable(b *testing.B) {
	b.Run("syncMap", func(b *testing.B) {
		var m = syncMap{data: make(map[uint64]string)}
		var seq atomic.Uint64
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				seq := seq.Add(1)
				m.add(seq, "hello world")

				time.AfterFunc(100*time.Millisecond, func() {
					if err := m.delete(seq); err != nil {
						b.Fatal(err)
					}
				})
			}
		})
	})

	b.Run("Lockable", func(b *testing.B) {
		l := New(new(sync.Mutex), make(map[uint64]string))
		var seq atomic.Uint64
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				seq := seq.Add(1)
				_ = l.Do(false, func(m *map[uint64]string) error {
					(*m)[seq] = "hello world"
					return nil
				})

				time.AfterFunc(10*time.Millisecond, func() {
					if err := l.Do(false, func(m *map[uint64]string) error {
						if _, exists := (*m)[seq]; !exists {
							return fmt.Errorf("not found entry for seq %d", seq)
						}
						delete(*m, seq)
						return nil
					}); err != nil {
						b.Fatal(err)
					}
				})
			}
		})
	})
}

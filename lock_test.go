package rostam

import (
	"context"
	"testing"
	"time"

	"github.com/goravel/framework/process"
	"github.com/stretchr/testify/suite"
)

type LockTestSuite struct {
	suite.Suite
	cache *Cache
}

func TestLockTestSuite(t *testing.T) {
	suite.Run(t, &LockTestSuite{})
}

func (s *LockTestSuite) SetupTest() {
	cache, err := NewCache(context.Background(), newTestConfig(s.T()), process.New(), testStore)
	s.Require().NoError(err)

	s.cache = cache
}

func (s *LockTestSuite) TestGetAndRelease() {
	lock := s.cache.Lock("lock", time.Second)
	s.True(lock.Get())
	s.False(s.cache.Lock("lock").Get())

	s.True(lock.Release())
	s.True(s.cache.Lock("lock").Get())
}

func (s *LockTestSuite) TestReleaseByAnotherOwnerDoesNothing() {
	lock := s.cache.Lock("lock", time.Second)
	s.True(lock.Get())

	s.False(s.cache.Lock("lock").Release())
	s.False(s.cache.Lock("lock").Get())
}

func (s *LockTestSuite) TestReleaseAfterExpiryLeavesTheNextHolderAlone() {
	first := s.cache.Lock("lock", 200*time.Millisecond)
	s.True(first.Get())

	time.Sleep(400 * time.Millisecond)

	second := s.cache.Lock("lock", 10*time.Second)
	s.True(second.Get())

	s.False(first.Release())
	s.False(s.cache.Lock("lock").Get())
	s.True(second.Release())
}

func (s *LockTestSuite) TestGetWithCallbackRunsItAndReleases() {
	ran := false
	s.True(s.cache.Lock("lock", time.Second).Get(func() {
		ran = true
	}))

	s.True(ran)
	s.True(s.cache.Lock("lock").Get())
}

func (s *LockTestSuite) TestLockWithoutDurationDoesNotExpire() {
	lock := NewLock(s.cache, "lock")
	s.True(lock.Get())

	remaining, err := s.cache.instance.TTL(context.Background(), lock.key())
	s.Nil(err)
	s.Equal(int64(-1), remaining)
	s.True(lock.Release())
}

func (s *LockTestSuite) TestBlockWaitsForTheHolder() {
	s.True(s.cache.Lock("lock", 300*time.Millisecond).Get())

	start := time.Now()
	s.True(s.cache.Lock("lock", time.Second).BlockWithTicker(2*time.Second, 50*time.Millisecond))
	s.GreaterOrEqual(time.Since(start), 200*time.Millisecond)
}

func (s *LockTestSuite) TestBlockGivesUpAtTheDeadline() {
	s.True(s.cache.Lock("lock").Get())

	start := time.Now()
	s.False(s.cache.Lock("lock").BlockWithTicker(300*time.Millisecond, 50*time.Millisecond))
	s.GreaterOrEqual(time.Since(start), 250*time.Millisecond, "it gave up before its deadline")
	s.Less(time.Since(start), 2*time.Second)
}

func (s *LockTestSuite) TestBlockWithANonPositiveTickerPollsInsteadOfPanicking() {
	s.True(s.cache.Lock("lock", 300*time.Millisecond).Get())

	s.NotPanics(func() {
		s.True(s.cache.Lock("lock").BlockWithTicker(3*time.Second, 0))
	})
}

func (s *LockTestSuite) TestACallbackThatPanicsStillReleasesTheLock() {
	s.Panics(func() {
		s.cache.Lock("lock").Get(func() {
			panic("the work failed")
		})
	})

	s.True(s.cache.Lock("lock").Get(), "a panicking callback left a lock without a duration held for good")
}

func (s *LockTestSuite) TestBlockRunsTheCallback() {
	s.True(s.cache.Lock("lock", 200*time.Millisecond).Get())

	ran := false
	s.True(s.cache.Lock("lock").BlockWithTicker(2*time.Second, 50*time.Millisecond, func() {
		ran = true
	}))
	s.True(ran)
}

func (s *LockTestSuite) TestBlockStopsWhenTheContextIsCancelled() {
	s.True(s.cache.Lock("lock").Get())

	ctx, cancel := context.WithCancel(context.Background())
	waiting := s.cache.WithContext(ctx).Lock("lock")
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	s.False(waiting.Block(10 * time.Second))
	s.GreaterOrEqual(time.Since(start), 80*time.Millisecond, "it gave up before the context was cancelled")
	s.Less(time.Since(start), 5*time.Second)
}

func (s *LockTestSuite) TestForceRelease() {
	s.True(s.cache.Lock("lock").Get())

	s.True(s.cache.Lock("lock").ForceRelease())
	s.True(s.cache.Lock("lock").Get())
}

func (s *LockTestSuite) TestExtend() {
	lock := NewLock(s.cache, "lock", 200*time.Millisecond)
	s.True(lock.Get())

	s.True(lock.Extend(10 * time.Second))
	time.Sleep(400 * time.Millisecond)
	s.False(s.cache.Lock("lock").Get())

	s.False(lock.Extend(0))
	s.True(lock.Release())
	s.False(lock.Extend(time.Second))
}

func (s *LockTestSuite) TestRestoreLockReleasesForTheOriginalOwner() {
	lock := NewLock(s.cache, "lock", 10*time.Second)
	s.True(lock.Get())

	restored := s.cache.RestoreLock("lock", lock.Owner())
	s.True(restored.Release())
	s.True(s.cache.Lock("lock").Get())
}

func (s *LockTestSuite) TestOwnersAreUnique() {
	s.NotEqual(NewLock(s.cache, "lock").Owner(), NewLock(s.cache, "lock").Owner())
}

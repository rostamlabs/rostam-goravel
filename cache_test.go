package rostam

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goravel/framework/process"
	"github.com/rostamlabs/rostam/client"
	"github.com/stretchr/testify/suite"
)

const testStore = "rostam"

type CacheTestSuite struct {
	suite.Suite
	config *testConfig
	cache  *Cache
}

func TestCacheTestSuite(t *testing.T) {
	suite.Run(t, &CacheTestSuite{})
}

func (s *CacheTestSuite) SetupTest() {
	s.config = newTestConfig(s.T())
	s.cache = s.newCache(testStore)
}

func (s *CacheTestSuite) newCache(store string) *Cache {
	cache, err := NewCache(context.Background(), s.config, process.New(), store)
	s.Require().NoError(err)

	return cache
}

func (s *CacheTestSuite) storedTTL(cache *Cache, key string) int64 {
	qualified, err := cache.namespace.qualify(context.Background(), key)
	s.Require().NoError(err)

	remaining, err := cache.instance.TTL(context.Background(), []byte(qualified))
	s.Require().NoError(err)

	return remaining
}

func (s *CacheTestSuite) TestAdd() {
	s.Nil(s.cache.Put("name", "Goravel", time.Second))
	s.False(s.cache.Add("name", "World", time.Second))
	s.Equal("Goravel", s.cache.Get("name"))

	s.True(s.cache.Add("name1", "World", 200*time.Millisecond))
	s.True(s.cache.Has("name1"))
	time.Sleep(400 * time.Millisecond)
	s.False(s.cache.Has("name1"))
}

func (s *CacheTestSuite) TestAddWithoutExpiry() {
	s.True(s.cache.Add("forever", "World", 0))
	s.Equal(int64(-1), s.storedTTL(s.cache, "forever"))
}

func (s *CacheTestSuite) TestAddWithNegativeDurationStoresNothing() {
	s.False(s.cache.Add("negative", "World", -time.Second))
	s.False(s.cache.Has("negative"))
}

func (s *CacheTestSuite) TestDecrement() {
	res, err := s.cache.Decrement("decrement")
	s.Nil(err)
	s.Equal(int64(-1), res)
	s.Equal(int64(-1), s.cache.GetInt64("decrement"))

	res, err = s.cache.Decrement("decrement", 2)
	s.Nil(err)
	s.Equal(int64(-3), res)

	res, err = s.cache.Decrement("decrement1", 2)
	s.Nil(err)
	s.Equal(int64(-2), res)
}

func (s *CacheTestSuite) TestDecrementConcurrently() {
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.cache.Decrement("concurrent")
			s.Nil(err)
		}()
	}
	wg.Wait()

	s.Equal(-100, s.cache.GetInt("concurrent"))
}

func (s *CacheTestSuite) TestForever() {
	s.True(s.cache.Forever("name", "Goravel"))
	s.Equal("Goravel", s.cache.Get("name", "").(string))
	s.Equal(int64(-1), s.storedTTL(s.cache, "name"))
}

func (s *CacheTestSuite) TestForget() {
	s.Nil(s.cache.Put("name", "Goravel", time.Second))
	s.True(s.cache.Forget("name"))
	s.False(s.cache.Has("name"))
	s.True(s.cache.Forget("missing"))
}

func (s *CacheTestSuite) TestGet() {
	s.Nil(s.cache.Put("name", "Goravel", time.Second))
	s.Equal("Goravel", s.cache.Get("name", "").(string))
	s.Equal("World", s.cache.Get("name1", "World").(string))
	s.Equal("World1", s.cache.Get("name2", func() any {
		return "World1"
	}).(string))
	s.Nil(s.cache.Get("name3"))
}

func (s *CacheTestSuite) TestGetBool() {
	s.Equal(true, s.cache.GetBool("test-get-bool", true))
	s.Nil(s.cache.Put("test-get-bool", true, time.Second))
	s.Equal(true, s.cache.GetBool("test-get-bool", false))
	s.Nil(s.cache.Put("test-get-bool", false, time.Second))
	s.Equal(false, s.cache.GetBool("test-get-bool", true))
}

func (s *CacheTestSuite) TestGetInt() {
	s.Equal(2, s.cache.GetInt("test-get-int", 2))
	s.Nil(s.cache.Put("test-get-int", 3, time.Second))
	s.Equal(3, s.cache.GetInt("test-get-int", 2))
	s.Nil(s.cache.Put("test-get-int", "4", time.Second))
	s.Equal(4, s.cache.GetInt("test-get-int", 2))
	s.Nil(s.cache.Put("test-get-int", "four", time.Second))
	s.Equal(2, s.cache.GetInt("test-get-int", 2))
}

func (s *CacheTestSuite) TestGetInt64() {
	s.Equal(int64(2), s.cache.GetInt64("test-get-int64", 2))
	s.Nil(s.cache.Put("test-get-int64", int64(1)<<40, time.Second))
	s.Equal(int64(1)<<40, s.cache.GetInt64("test-get-int64", 2))
}

func (s *CacheTestSuite) TestGetString() {
	s.Equal("2", s.cache.GetString("test-get-string", "2"))
	s.Nil(s.cache.Put("test-get-string", "3", time.Second))
	s.Equal("3", s.cache.GetString("test-get-string", "2"))
	s.Nil(s.cache.Put("test-get-string", 4, time.Second))
	s.Equal("4", s.cache.GetString("test-get-string", "2"))
}

func (s *CacheTestSuite) TestHas() {
	s.False(s.cache.Has("name"))
	s.Nil(s.cache.Put("name", "Goravel", time.Second))
	s.True(s.cache.Has("name"))
}

func (s *CacheTestSuite) TestIncrement() {
	res, err := s.cache.Increment("increment")
	s.Nil(err)
	s.Equal(int64(1), res)
	s.Equal(int64(1), s.cache.GetInt64("increment"))

	res, err = s.cache.Increment("increment", 2)
	s.Nil(err)
	s.Equal(int64(3), res)
}

func (s *CacheTestSuite) TestIncrementCreatesCounterWithoutExpiry() {
	_, err := s.cache.Increment("fresh-counter")
	s.Nil(err)
	s.Equal(int64(-1), s.storedTTL(s.cache, "fresh-counter"))
}

func (s *CacheTestSuite) TestIncrementKeepsTheExpiryOfAPutCounter() {
	s.Nil(s.cache.Put("windowed", 5, 300*time.Millisecond))

	res, err := s.cache.Increment("windowed")
	s.Nil(err)
	s.Equal(int64(6), res)
	s.Greater(s.storedTTL(s.cache, "windowed"), int64(0))

	time.Sleep(500 * time.Millisecond)
	s.False(s.cache.Has("windowed"))
}

func (s *CacheTestSuite) TestIncrementConvertsANumericString() {
	s.Nil(s.cache.Put("numeric", "41", 2*time.Second))

	res, err := s.cache.Increment("numeric")
	s.Nil(err)
	s.Equal(int64(42), res)
	s.Equal("42", s.cache.Get("numeric"))

	remaining := s.storedTTL(s.cache, "numeric")
	s.Greater(remaining, int64(0))
	s.LessOrEqual(remaining, int64(2000))
}

func (s *CacheTestSuite) TestIncrementConvertsANumericStringWithoutExpiry() {
	s.True(s.cache.Forever("numeric-forever", "-3"))

	res, err := s.cache.Increment("numeric-forever", 10)
	s.Nil(err)
	s.Equal(int64(7), res)
	s.Equal(int64(-1), s.storedTTL(s.cache, "numeric-forever"))
}

func (s *CacheTestSuite) TestIncrementConvertsASevenByteNumericString() {
	s.Nil(s.cache.Put("padded", "1234567", time.Second))

	res, err := s.cache.Increment("padded")
	s.Nil(err)
	s.Equal(int64(1234568), res)
}

func (s *CacheTestSuite) TestIncrementRefusesANonNumericValue() {
	s.Nil(s.cache.Put("text", "Goravel", time.Second))

	_, err := s.cache.Increment("text")
	s.True(errors.Is(err, ErrNotACounter))
	s.Equal("Goravel", s.cache.Get("text"))
}

func (s *CacheTestSuite) TestIncrementRefusesAFloat() {
	s.Nil(s.cache.Put("float", 1.5, time.Second))

	_, err := s.cache.Increment("float")
	s.True(errors.Is(err, ErrNotACounter))
}

func (s *CacheTestSuite) TestPull() {
	s.Nil(s.cache.Put("name", "Goravel", time.Second))
	s.True(s.cache.Has("name"))
	s.Equal("Goravel", s.cache.Pull("name", "").(string))
	s.False(s.cache.Has("name"))
	s.Equal("World", s.cache.Pull("name", "World"))
	s.Nil(s.cache.Pull("name"))
}

func (s *CacheTestSuite) TestPullHandsTheValueToOneCaller() {
	for round := range 20 {
		s.Nil(s.cache.Put("once", "token", time.Second))

		var (
			wg    sync.WaitGroup
			hits  atomic.Int32
			start = make(chan struct{})
		)
		for range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if s.cache.Pull("once") != nil {
					hits.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()

		s.Equal(int32(1), hits.Load(), "round %d", round)
	}
}

func (s *CacheTestSuite) TestPut() {
	s.Nil(s.cache.Put("name", "Goravel", 200*time.Millisecond))
	s.True(s.cache.Has("name"))
	s.Equal("Goravel", s.cache.Get("name", "").(string))
	time.Sleep(400 * time.Millisecond)
	s.False(s.cache.Has("name"))
}

func (s *CacheTestSuite) TestPutWithNegativeDurationRemovesTheKey() {
	s.Nil(s.cache.Put("name", "Goravel", time.Second))
	s.Nil(s.cache.Put("name", "World", -time.Second))
	s.False(s.cache.Has("name"))
}

func (s *CacheTestSuite) TestPutWithSubMillisecondDurationStillExpires() {
	s.Nil(s.cache.Put("brief", "Goravel", time.Nanosecond))
	s.NotEqual(int64(-1), s.storedTTL(s.cache, "brief"))
}

func (s *CacheTestSuite) TestPutRefusesUnsupportedTypes() {
	err := s.cache.Put("struct", struct{ Name string }{"Goravel"}, time.Second)
	s.True(errors.Is(err, ErrUnsupportedValue))
}

func (s *CacheTestSuite) TestRemember() {
	s.Nil(s.cache.Put("name", "Goravel", time.Second))
	value, err := s.cache.Remember("name", time.Second, func() (any, error) {
		return "World", nil
	})
	s.Nil(err)
	s.Equal("Goravel", value)

	value, err = s.cache.Remember("name1", time.Second, func() (any, error) {
		return "World1", nil
	})
	s.Nil(err)
	s.Equal("World1", value)
	s.Equal("World1", s.cache.Get("name1"))

	_, err = s.cache.Remember("name2", time.Second, func() (any, error) {
		return nil, errors.New("boom")
	})
	s.EqualError(err, "boom")
	s.False(s.cache.Has("name2"))
}

func (s *CacheTestSuite) TestRememberForever() {
	value, err := s.cache.RememberForever("name", func() (any, error) {
		return "Goravel", nil
	})
	s.Nil(err)
	s.Equal("Goravel", value)
	s.Equal(int64(-1), s.storedTTL(s.cache, "name"))
}

func (s *CacheTestSuite) TestGetIgnoresValuesInAnotherEncoding() {
	qualified, err := s.cache.namespace.qualify(context.Background(), "foreign")
	s.Require().NoError(err)
	s.Require().NoError(kvPut(context.Background(), s.cache.instance, qualified, []byte("hello"), time.Second))

	s.Equal("default", s.cache.Get("foreign", "default"))
}

func (s *CacheTestSuite) TestWithContextUsesTheGivenContext() {
	s.Nil(s.cache.Put("name", "Goravel", time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := s.cache.WithContext(ctx)

	s.Equal("fallback", cancelled.Get("name", "fallback"))
	s.Error(cancelled.Put("name", "World", time.Second))
	s.Equal("Goravel", s.cache.Get("name"))
}

func (s *CacheTestSuite) TestEpochFlushRetiresOnlyThisPrefix() {
	s.config.Add("cache.stores.other.prefix", "rg-other-"+randomSuffix())
	other := s.newCache("other")

	session, err := NewSession(context.Background(), s.config, "rostam")
	s.Require().NoError(err)
	s.Nil(session.Write("sid", "payload"))

	s.Nil(s.cache.Put("name", "Goravel", 10*time.Second))
	s.Nil(other.Put("name", "Other", 10*time.Second))

	s.True(s.cache.Flush())

	s.False(s.cache.Has("name"))
	s.Equal("Other", other.Get("name"))
	data, err := session.Read("sid")
	s.Nil(err)
	s.Equal("payload", data)

	s.Nil(s.cache.Put("name", "Fresh", 10*time.Second))
	s.Equal("Fresh", s.cache.Get("name"))
}

func (s *CacheTestSuite) TestEpochFlushLeavesLocksAlone() {
	lock := s.cache.Lock("held", 10*time.Second)
	s.True(lock.Get())

	s.True(s.cache.Flush())

	s.False(s.cache.Lock("held").Get())
	s.True(lock.Release())
}

func (s *CacheTestSuite) TestEpochFlushInAnotherProcessIsSeenWithinTheRefresh() {
	s.config.Add("cache.stores."+testStore+".epoch_refresh", time.Duration(0))
	resetSharedState()
	cache := s.newCache(testStore)

	s.Nil(cache.Put("name", "Goravel", 10*time.Second))

	elsewhere := &generation{instance: cache.instance, key: cache.prefix + "#epoch", refresh: 0}
	s.Nil(elsewhere.bump(context.Background()))

	s.False(cache.Has("name"))
}

func (s *CacheTestSuite) TestCachedGenerationHidesAnotherProcessFlushUntilRefresh() {
	s.config.Add("cache.stores."+testStore+".epoch_refresh", time.Hour)
	resetSharedState()
	cache := s.newCache(testStore)

	s.Nil(cache.Put("name", "Goravel", 10*time.Second))

	elsewhere := &generation{instance: cache.instance, key: cache.prefix + "#epoch", refresh: 0}
	s.Nil(elsewhere.bump(context.Background()))

	s.True(cache.Has("name"))
}

func (s *CacheTestSuite) TestLostGenerationIsPushedBackUpNotReused() {
	s.config.Add("cache.stores."+testStore+".epoch_refresh", time.Duration(0))
	resetSharedState()
	cache := s.newCache(testStore)

	s.Nil(cache.Put("retired", "before flush", 10*time.Second))
	s.True(cache.Flush())
	s.Nil(cache.Put("current", "after flush", 10*time.Second))

	s.Require().NoError(kvDel(context.Background(), cache.instance, cache.prefix+"#epoch"))

	s.Nil(cache.Get("retired"))
	s.Equal("after flush", cache.Get("current"))

	stored, err := kvGet(context.Background(), cache.instance, cache.prefix+"#epoch")
	s.Require().NoError(err)
	s.Equal(int64(1), decodeCounter(stored))
}

func (s *CacheTestSuite) TestFlushUnsupportedLeavesEverything() {
	s.config.Add("cache.stores.locked.flush", FlushUnsupported)
	cache := s.newCache("locked")

	s.Nil(cache.Put("name", "Goravel", 10*time.Second))
	s.False(cache.Flush())
	s.Equal("Goravel", cache.Get("name"))
}

func (s *CacheTestSuite) TestUnknownFlushModeIsRefused() {
	s.config.Add("cache.stores.typo.flush", "everything")

	_, err := NewCache(context.Background(), s.config, process.New(), "typo")
	s.True(errors.Is(err, ErrUnknownFlushMode))
}

func (s *CacheTestSuite) TestMissingServerConfigIsRefused() {
	s.config.Add("cache.stores.nowhere.connection", "nowhere")

	_, err := NewCache(context.Background(), s.config, process.New(), "nowhere")
	s.True(errors.Is(err, ErrServersNotConfigured))
}

func (s *CacheTestSuite) TestServersListOverridesHost() {
	s.config.Add("database.rostam.listed.servers", []string{s.config.GetString("database.rostam.default.host") + ":" + s.config.GetString("database.rostam.default.port")})
	s.config.Add("cache.stores.listed.connection", "listed")
	cache := s.newCache("listed")

	s.Nil(cache.Put("name", "Goravel", time.Second))
	s.Equal("Goravel", cache.Get("name"))
}

func (s *CacheTestSuite) TestStoresOnOneConnectionShareTheClient() {
	other := s.newCache("other")

	s.Same(s.cache.instance, other.instance)
}

func TestServerFlushWipesTheWholeServer(t *testing.T) {
	config := newTestConfig(t)
	requireDisposableServer(t)

	config.Add("cache.stores.wipe.flush", FlushServer)
	cache, err := NewCache(context.Background(), config, process.New(), "wipe")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewCache(context.Background(), config, process.New(), "rostam")
	if err != nil {
		t.Fatal(err)
	}

	if err := cache.Put("name", "Goravel", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := other.Put("name", "Other", 10*time.Second); err != nil {
		t.Fatal(err)
	}

	if !cache.Flush() {
		t.Fatal("Flush returned false")
	}

	if cache.Has("name") {
		t.Error("this store's key survived a server flush")
	}
	if _, err := kvGet(context.Background(), other.instance, other.prefix+"#epoch"); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("another store's generation survived a server flush: %v", err)
	}
}

package rostam

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

const testSessionDriver = "rostam"

type SessionTestSuite struct {
	suite.Suite
	config  *testConfig
	session *Session
}

func TestSessionTestSuite(t *testing.T) {
	suite.Run(t, &SessionTestSuite{})
}

func (s *SessionTestSuite) SetupTest() {
	s.config = newTestConfig(s.T())
	s.config.Add("session.lifetime", 1)

	session, err := NewSession(context.Background(), s.config, testSessionDriver)
	s.Require().NoError(err)

	s.session = session
}

func (s *SessionTestSuite) TestReadMissingSessionIsEmpty() {
	data, err := s.session.Read("missing")
	s.Nil(err)
	s.Equal("", data)
}

func (s *SessionTestSuite) TestWriteThenRead() {
	s.Nil(s.session.Write("sid", `{"user":1}`))

	data, err := s.session.Read("sid")
	s.Nil(err)
	s.Equal(`{"user":1}`, data)
}

func (s *SessionTestSuite) TestPayloadIsStoredAsIs() {
	s.Nil(s.session.Write("sid", "1234567"))

	stored, err := kvGet(context.Background(), s.session.instance, s.session.prefix+"sid")
	s.Nil(err)
	s.Equal([]byte("1234567"), stored)
}

func (s *SessionTestSuite) TestWriteSetsTheLifetimeAsExpiry() {
	s.Nil(s.session.Write("sid", "payload"))

	remaining, err := s.session.instance.TTL(context.Background(), []byte(s.session.prefix+"sid"))
	s.Nil(err)
	s.Greater(remaining, int64(50*time.Second/time.Millisecond))
	s.LessOrEqual(remaining, int64(time.Minute/time.Millisecond))
}

func (s *SessionTestSuite) TestDestroy() {
	s.Nil(s.session.Write("sid", "payload"))
	s.Nil(s.session.Destroy("sid"))

	data, err := s.session.Read("sid")
	s.Nil(err)
	s.Equal("", data)

	s.Nil(s.session.Destroy("sid"))
}

func (s *SessionTestSuite) TestPrefixDefaultsToTheCookieName() {
	s.Equal(s.config.GetString("session.cookie")+":", s.session.prefix)
}

func (s *SessionTestSuite) TestPrefixCanBeConfigured() {
	s.config.Add("session.drivers."+testSessionDriver+".prefix", "custom:")

	session, err := NewSession(context.Background(), s.config, testSessionDriver)
	s.Require().NoError(err)
	s.Nil(session.Write("sid", "payload"))

	stored, err := kvGet(context.Background(), session.instance, "custom:sid")
	s.Nil(err)
	s.Equal([]byte("payload"), stored, "the session was not written under the configured prefix")
}

func (s *SessionTestSuite) TestLifetimeUnderOneMinuteIsRefused() {
	s.config.Add("session.lifetime", 0)

	_, err := NewSession(context.Background(), s.config, testSessionDriver)
	s.True(errors.Is(err, ErrSessionLifetimeTooShort))
}

func (s *SessionTestSuite) TestNoOps() {
	s.Nil(s.session.Open("", ""))
	s.Nil(s.session.Gc(60))
	s.Nil(s.session.Close())

	s.Nil(s.session.Write("sid", "still usable after Close"))
}

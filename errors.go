package rostam

import "github.com/goravel/framework/errors"

var (
	ErrServiceProviderNotRegistered = errors.New("please register rostam service provider").SetModule("Rostam")
	ErrStoreIsRequired              = errors.New("store is required").SetModule("Rostam")
	ErrConnectionIsRequired         = errors.New("connection is required").SetModule("Rostam")
	ErrSessionDriverIsRequired      = errors.New("session driver is required").SetModule("Rostam")
	ErrServersNotConfigured         = errors.New("rostam servers are not configured for connection [%s]: set %s.host or %s.servers").SetModule("Rostam")
	ErrUnknownFlushMode             = errors.New("unknown flush mode [%s] for cache store [%s]: use epoch, server or unsupported").SetModule("Rostam")
	ErrSessionLifetimeTooShort      = errors.New("session.lifetime must be at least 1 minute, got %d").SetModule("Rostam")
	ErrNotACounter                  = errors.New("value of cache key [%s] is not an integer").SetModule("Rostam")
	ErrCorruptValue                 = errors.New("value is not in the rostam-goravel encoding").SetModule("Rostam")
	ErrUnsupportedValue             = errors.New("can't store a value of type %T: use a string, []byte, number, bool, time.Time or encoding.BinaryMarshaler").SetModule("Rostam")
	ErrGenerationNotACounter        = errors.New("generation key [%s] does not hold an 8-byte counter").SetModule("Rostam")
)

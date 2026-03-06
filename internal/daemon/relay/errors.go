package relay

import "errors"

// ErrRelayClosed is returned when Send is called after Close.
var ErrRelayClosed = errors.New("relay is closed")

// DeliveryError wraps an underlying I/O error from message delivery.
type DeliveryError struct {
	Cause error
}

func (e *DeliveryError) Error() string {
	return "relay delivery failed: " + e.Cause.Error()
}

func (e *DeliveryError) Unwrap() error {
	return e.Cause
}

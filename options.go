package gosocketio

//ServerOption is a functional server option returned by various functions
//in this package.
type ServerOption func(*Server)

//RecoveryHandler is called when one or more interally spawned goroutines panic, this
//includes goroutines that service user code. The passed interface is guaranteed to
//be non-nil, however the handler should check if the channel is. If the channel is
//non-nil the handler may use it to communicate a problem.
//RecoveryHandler must be safe to call concurrently.
type RecoveryHandler func(*Channel, interface{})

func (rh RecoveryHandler) call(c *Channel) {
	//Only call ourselves and then recover if we've actually been set.
	if rh != nil {
		if val := recover(); val != nil {
			//RecoveryHandler must check if channel is available
			rh(c, val)
		}
	}
}

//WithRecoveryHandler sets the given RecoveryHandler in the returned ServerOption.
//If rh is nil the returned ServerOption does nothing.
func WithRecoveryHandler(rh RecoveryHandler) ServerOption {
	if rh == nil {
		//just don't do anything
		return func(_ *Server) {}
	}

	return func(s *Server) {
		s.rh = rh
	}
}

//ErrorHandler is called when an internal error occurs that cannot otherwise be
//returned to the caller.
type ErrorHandler func(error)

func (eh ErrorHandler) call(err error) {
	//Only call ourselves if we've been set
	//TODO do we want to print something to stderr if we're not set
	//so a user at least knows something is happening?
	if eh != nil {
		eh(err)
	}
}

//WithErrorHandler sets the given ErrorHandler in the returned ServerOption.
//If eh is nil the returned ServerOption does nothing.
func WithErrorHandler(eh ErrorHandler) ServerOption {
	if eh == nil {
		//just don't do anything
		return func(_ *Server) {}
	}

	return func(s *Server) {
		s.eh = eh
	}
}

type rateLimiter chan struct{}

func (rl rateLimiter) acquire() bool {
	select {
	case rl <- struct{}{}:
		return true
	default:
		return false
	}
}

func (rl rateLimiter) release() { <-rl }

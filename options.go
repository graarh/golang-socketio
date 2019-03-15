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

//WithRateLimiter returns a ServerOption that limits the number of messages concurrently handled per Channel.
//It does not limit the number of Channels or Websocket connections, connections may be limited via the underlying
//net.Listener used in the http.Server, or shadowing the Server's ServeHTTP method to limit how many requests become websockets.
//If limit is below 0, an unlimited number of goroutines will be allowed to spawn in response to socketio messages.
//If the limit is 0 then the socketio input loop will work on each function itself, this is only recommended if the user
//wants to control how goroutines are spawned in their on callbacks or if their callbacks are fast and don't block long.
//Any limit greater than 0 means 'limit' goroutines are allowed to run concurrently to service user code. If the
//limit is reached an incoming message will be dropped and the Servers ErrorHandler will be called, if it has one,
//with ErrRateLimiting. Once a goroutine servicing user code is done the limit will reduce allowing more to be run.
func WithRateLimiter(limit int) ServerOption {
	return func(s *Server) {
		s.limit = limit
	}
}

type rateLimiter func(func())

//newRateLimiter closes over a Channel and goroutine limit and returns a function that rate
//limits functions passed to it. If the limit hasn't been reached the function will be spawned as a goroutine,
//otherwise ErrRateLimiting will be based into the Channels ErrorHandler. For unlimited, the limit should be
//below 0, this will spawn each passed function in a new goroutine without limiting. If limit is 0 then
//no goroutines are spawned, this means the caller will be consumed in working on the passed function.
func newRateLimiter(c *Channel, limit int) rateLimiter {
	if limit < 0 {
		return func(work func()) {
			c.server.inc()
			go func() {
				defer c.server.dec()

				work()
			}()
		}
	}

	if limit == 0 {
		return func(work func()) {
			work()
		}
	}

	rl := make(limiter, limit)

	return func(work func()) {
		if !rl.acquire() {
			c.eh.call(ErrRateLimiting)
			return
		}

		c.server.inc()
		go func() {
			defer func() {
				rl.release()
				c.server.dec()
			}()

			work()
		}()
	}
}

type limiter chan struct{}

func (l limiter) acquire() bool {
	select {
	case l <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l limiter) release() { <-l }

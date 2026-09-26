// The Go client is its own module so that depending on it does not drag
// mobium's own requirements — cobra, gorilla/websocket — into the consumer's
// module graph. It needs nothing but the standard library, and should stay
// that way.
module github.com/mobiumdev/mobium/clients/go

go 1.24

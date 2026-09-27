module quickstart

go 1.24

require github.com/mobiumdev/mobium/clients/go v0.0.0

// Builds the example against the client in this repository. In your own
// project, delete this line and run `go get github.com/mobiumdev/mobium/clients/go`.
replace github.com/mobiumdev/mobium/clients/go => ../../../../clients/go

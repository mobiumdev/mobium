# Mobium for Go

Drive native apps on Android emulators, Android phones, iOS simulators and
iPhones — map the screen, tap, type, wait and screenshot — through the same
tools as the [mobium](https://github.com/mobiumdev/mobium) command line and
MCP server. Standard library only.

```sh
go get github.com/mobiumdev/mobium/clients/go
go install github.com/mobiumdev/mobium/cmd/mobium@latest   # the binary it drives
```

```go
ctx := context.Background()
dev, err := mobium.Start(ctx, mobium.WithPlatform("android"), mobium.WithApp("com.android.settings"))
if err != nil {
	log.Fatal(err)
}
defer dev.Quit(ctx) // ends the session on the device

elements, err := dev.Map(ctx)
// find a row by its label, tap its ref, wait for the next screen ...
```

`Start` opens a session on the device and launches the app fresh;
`Quit` ends it. `WithPlatform("ios")` drives an
iOS simulator or iPhone the same way.

- [Quick start for Go](https://github.com/mobiumdev/mobium/blob/main/docs/quickstart/go.md)
  — the whole program, run on Android and iOS
- [Every tool](https://github.com/mobiumdev/mobium/blob/main/docs/API.md) and
  [setting up devices](https://github.com/mobiumdev/mobium/blob/main/docs/SETUP.md)

MIT.

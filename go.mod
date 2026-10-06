module github.com/starcat-app/starcat-cli

go 1.26.0

// Release binaries must include the standard-library security fixes in Go 1.26.6.
toolchain go1.26.6

require (
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/mod v0.41.0
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/sys v0.27.0 // indirect
)

module github.com/starcat-app/starcat-cli

go 1.25.0

// Release binaries must use a toolchain that contains the GO-2026-5856 crypto/tls fix.
toolchain go1.26.5

require (
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/mod v0.38.0
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/sys v0.27.0 // indirect
)

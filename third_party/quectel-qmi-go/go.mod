module github.com/iniwex5/quectel-qmi-go

go 1.24.0

replace github.com/iniwex5/netlink => ../netlink

require (
	github.com/iniwex5/netlink v1.3.3
	github.com/sirupsen/logrus v1.9.4
	github.com/warthog618/sms v0.3.0
	go.uber.org/zap v1.27.1
)

require (
	github.com/vishvananda/netns v0.0.5 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/sys v0.19.0 // indirect
)

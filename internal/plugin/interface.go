package plugin

import "context"

type Interface interface {
	Start(context.Context) error
	Stop(context.Context) error
}

package apmcore

import (
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidisotel"
)

// InstrumentRueidis wraps client with OpenTelemetry tracing and metrics via
// rueidisotel and returns the instrumented client. It mirrors InstrumentRedis
// for the rueidis client used by gsrueidis.
//
// Pass the client returned by rueidis.NewClient — the returned client is a
// drop-in replacement that emits OTel spans and metrics through the global
// providers configured by SetupOTelSDK.
//
// Example:
//
//	client, err := rueidis.NewClient(rueidis.ClientOption{...})
//	if err != nil { ... }
//	client = apmcore.InstrumentRueidis(client)
func InstrumentRueidis(client rueidis.Client) rueidis.Client {
	return rueidisotel.WithClient(client)
}

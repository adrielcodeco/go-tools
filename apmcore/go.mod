module github.com/adrielcodeco/go-tools/apmcore

go 1.25.0

require github.com/adrielcodeco/go-tools v0.0.0

require (
	github.com/mattn/go-sqlite3 v1.14.22
	github.com/redis/go-redis/extra/redisotel/v9 v9.16.0
	github.com/redis/go-redis/v9 v9.16.0
	github.com/redis/rueidis v1.0.75
	github.com/redis/rueidis/rueidisotel v1.0.75
	github.com/valyala/fasthttp v1.71.0
	go.elastic.co/apm/module/apmhttp/v2 v2.7.1
	go.elastic.co/apm/module/apmotel/v2 v2.7.1
	go.elastic.co/apm/module/apmsql/v2 v2.7.1
	go.elastic.co/apm/module/apmzap/v2 v2.7.1
	go.elastic.co/apm/v2 v2.7.1
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/sdk/metric v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	go.uber.org/zap v1.27.0
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.30.0
)

require (
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/armon/go-radix v1.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/elastic/go-sysinfo v1.7.1 // indirect
	github.com/elastic/go-windows v1.0.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/joeshaw/multierror v0.0.0-20140124173710-69b34d4ec901 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/redis/go-redis/extra/rediscmd/v9 v9.16.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	go.elastic.co/fastjson v1.5.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	howett.net/plist v1.0.0 // indirect
)

replace github.com/adrielcodeco/go-tools => ../

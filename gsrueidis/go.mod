module github.com/adrielcodeco/go-tools/gsrueidis

go 1.25.0

require (
	github.com/adrielcodeco/go-tools v0.0.0
	github.com/adrielcodeco/go-tools/gormcache v0.0.0
	github.com/redis/rueidis v1.0.75
)

require (
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	gorm.io/gorm v1.30.0 // indirect
)

replace (
	github.com/adrielcodeco/go-tools => ../
	github.com/adrielcodeco/go-tools/gormcache => ../gormcache
)

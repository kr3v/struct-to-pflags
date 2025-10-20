//go:generate struct-to-pflags -file=config.go -struct=config -output=config.gen.go

package example

type config struct {
	// path to file where logs will be written
	logFile string
	// enable debug mode
	debug bool
	// port number to listen on
	port int
	// internal version field
	version string `pflags:"-"`
}

var defaultConfig = config{
	logFile: "/var/log/app.log",
	debug:   false,
	port:    8080,
	version: "v1.0.0",
}

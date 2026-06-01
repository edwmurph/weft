package version

var Version = "9.0.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

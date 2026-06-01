package version

var Version = "8.1.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

package version

var Version = "7.3.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

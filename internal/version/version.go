package version

var Version = "7.14.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

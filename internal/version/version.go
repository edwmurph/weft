package version

var Version = "7.3.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

package version

var Version = "7.9.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

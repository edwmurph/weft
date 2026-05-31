package version

var Version = "7.12.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

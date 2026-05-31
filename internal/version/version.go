package version

var Version = "7.13.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

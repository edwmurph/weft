package version

var Version = "7.13.3"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

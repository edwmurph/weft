package version

var Version = "7.13.9"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

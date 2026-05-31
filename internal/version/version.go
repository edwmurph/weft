package version

var Version = "7.13.5"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

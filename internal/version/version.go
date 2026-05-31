package version

var Version = "7.5.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

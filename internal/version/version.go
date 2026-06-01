package version

var Version = "7.19.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

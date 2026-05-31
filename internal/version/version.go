package version

var Version = "7.15.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

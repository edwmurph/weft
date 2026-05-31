package version

var Version = "7.15.3"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

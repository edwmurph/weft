package version

var Version = "7.16.3"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}

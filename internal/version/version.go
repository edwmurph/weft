package version

var Version = "7.5.3"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
